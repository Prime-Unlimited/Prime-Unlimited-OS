# JC11S QML preload hook

This directory records the complete hardware-tested integration used on a
Denon DJ Prime 4+ (`JC11S`) running Engine OS 5.3.0. It keeps every persistent
Prime Unlimited file below `/data/prime-unlimited/`; the only other persistent
file changed is the already-installed SSH bootstrap entrypoint
`/data/prime-ssh/activate.sh`.

The integration is deliberately split into reproducible pieces:

- `prime_ui_hook.c` intercepts Qt's `qRegisterResourceData`, locates
  `ControlCenterTab.qml`, replaces its bytes in memory, and changes the qrc
  timestamp from a content hash so Qt cannot reuse stale compiled QML.
- `ControlCenterTab.qml` adds the final Control Center tile, loads the repo's
  SVG, and opens `/data/prime-unlimited/ui/ModLoader.qml`.
- `Version.qml` changes only the visible About headline to read the separate
  `/data/prime-unlimited/os-version.qml` (with a plain companion version file
  at `/data/prime-unlimited/os-version`). Engine's own version model, build hash,
  `/tmp/com.denondj.engine.version`, update logic, and compatibility data stay
  untouched.
- `activate.sh` is the early ALSA/SSH-hook entrypoint. It bind-mounts the two
  launch wrappers, creates the runtime systemd drop-in, and schedules exactly
  one systemd-managed Engine restart per boot.
- `runengine-hooked.sh` preserves the original launcher while exporting the
  preload. `make-engine-wrapper.sh` creates the final Engine launcher from the
  firmware's own script and changes only the `/usr/Engine/Engine` invocation.
- `build-hook.sh`, `install-device.sh`, and `verify-device.sh` make the tested
  build, installation, cache invalidation, controlled restart, and validation
  steps explicit.

No private key, passphrase, `authorized_keys`, SSH host key, runtime log,
firmware binary, or copied vendor launcher is stored in this repository.

## Why all three preload paths exist

`engine.service` normally starts before the ALSA-triggered SSH bootstrap has
written its drop-in. The first EMain therefore starts without the preload.
During that first start the ALSA hook runs `activate.sh`, which prepares:

1. a bind-mounted `runengine` wrapper;
2. a bind-mounted final `engine` launcher;
3. `/run/systemd/system/engine.service.d/99-prime-ui.conf`;
4. one delayed `systemctl restart engine.service` managed by systemd.

The second service start inherits the preload, and the final launcher injects
it immediately around `/usr/Engine/Engine`. This is necessary because the
hook's constructor environment is not sufficient evidence that the library
remained mapped in EMain.

## Build

On Termux with `clang` installed:

```sh
cd modloader/device/jc11s/ui-hook
./build-hook.sh
readelf -h build/libprime-ui-hook.so
```

The result must be ELF32, little-endian, ARM, shared object. The build uses no
Android C runtime headers or linked libc; firmware symbols are resolved by the
dynamic loader at runtime.

## Install

Copy this directory plus `modloader/ui/ModLoader.qml` and
`modloader/ui/assets/prime_unlimited_mod_loader.svg` to the device, then run as
root:

```sh
./install-device.sh
```

The installer is fail-closed, refuses a non-JC11S machine, backs up the
existing `/data/prime-ssh/activate.sh`, copies the original runengine launcher
to `/data/prime-unlimited/bin/runengine.real`, generates the final launcher,
clears only Engine's QML cache, and schedules a controlled service restart.
It never uses `killall` and never requests update/loader mode.

## Verification

After the restart:

```sh
./verify-device.sh
```

Success requires all of the following:

- `engine.service` is active;
- the live `/usr/Engine/Engine` PID has `LD_PRELOAD` in `/proc/PID/environ`;
- `/data/prime-unlimited/lib/libprime-ui-hook.so` is present in its maps;
- the log contains `swap applied ControlCenterTab.qml timestamped`;
- the Control Center tile appears after Day Mode and opens the Mod Loader.

## Tested runtime layout

```text
/data/prime-unlimited/
  bin/engine-hooked.sh
  bin/runengine-hooked.sh
  bin/runengine.real
  lib/libprime-ui-hook.so
  logs/ui-hook.log
  ui/ModLoader.qml
  ui/prime_unlimited_mod_loader.svg
  ui/originals/ControlCenterTab.qml
  ui/originals/EmbeddedMain.qml
  ui/patches/ControlCenterTab.qml
  ui/patches/Version.qml
  os-version
```

Runtime-only state is created under `/run`, including the systemd drop-in and
the once-per-boot restart flag. The bind mounts disappear at reboot and are
recreated by the existing ALSA bootstrap.

## Troubleshooting chronology

These details matter when reproducing the result:

1. Seeing the hook constructor in the EMain PID is not enough; check maps.
2. A drop-in written after the first service start cannot affect that process.
3. Do not restart EMain with signals. Use `systemctl restart engine.service`.
4. Qt may keep the old Control Center component even when the byte swap logs.
   The hook therefore derives a qrc node timestamp from the replacement bytes.
5. Remove exactly `/data/AppDataUser/AIR Music Technology/Engine/.cache/qml`
   after changing the patch. Setting `QML_DISABLE_DISK_CACHE=1` alone was not
   sufficient on the tested firmware.
6. The qrc replacement is in-place. `ControlCenterTab.qml` must not exceed the
   original resource's 1156-byte data slot; the checked-in patch is validated
   by `install-device.sh`.
7. The SVG's viewBox is cropped and QML uses `Image.Stretch`, because the
   original artwork had transparent top/bottom margins that made the tile look
   smaller than its 256 by 104 pixel neighbours.

## Second-stage splash

`prime_ui_hook.c` also calls `splash_visit()`.
`/data/prime-unlimited/ui/splash/SplashScreen.png` is
`assets/prime-unlimited-os-brand.png` stretched to 1280x800. The hook replaces
only the Engine resource `img/SplashScreen.png`, and when the boot splash QML
matches it anchors that image to the full parent with `Image.Stretch`. The
bottom-right text uses the same `os-version.qml` version as the About headline. The
firmware splash partition is not modified. A missing or rejected file leaves
the stock picture in place. Details are in `docs/SECOND_STAGE_SPLASH.md`.

## Known scope

The Control Center integration and QML resource swap are verified on hardware.
The Mod Loader backend remains a separate component; showing the screen does
not by itself provision the loopback API, authorization token, or catalog.
