# JP24 QML preload hook and activation

Port of `devices/jc11s/ui-hook/` to the Prime 4 G2 (`JP24`, Rockchip RK3588,
aarch64). The interception technique is unchanged: `LD_PRELOAD`-shadow Qt's
`qRegisterResourceData`, walk the Qt RCC resource tree it receives, and swap
`ControlCenterTab.qml`'s bytes in place. That tree format is part of Qt
itself, not the device, and JP24 runs the same Engine OS version (5.3.0)
that the JC11S hook was verified against - confirmed live via
`/tmp/com.denondj.engine.version` on real JP24 hardware.

## What is actually different from JC11S

- **Platform**: JC11S's Engine runs as a 32-bit Android (Bionic) process
  (`armv7a-linux-androideabi24`). JP24's Engine is a 64-bit aarch64
  glibc/systemd process (confirmed live: `uname -m` -> `aarch64`, and the
  downloaded `/usr/Engine/Engine` is `ELF 64-bit ... ARM aarch64 ...
  dynamically linked ... for GNU/Linux`, not an Android binary). JP24 also
  uses a real systemd `engine.service` (`Type=forking`, two-layer
  `runengine` -> `engine` -> `Engine` launcher chain), structurally similar
  to JC11S's but with different script names and no Android init.
- **Pointer width bug, fixed here**: the JC11S hook's `mprotect`
  page-alignment math casts pointers through `u32`
  (`u32 pg=((u32)p/4096u)*4096u;`). That's harmless on JC11S, where pointers
  are genuinely 32 bits, but would truncate a real 64-bit address to
  garbage on JP24's process (addresses well above 4 GiB are routine in a
  64-bit process). This port uses `up` (`unsigned long`, 64-bit on this
  LP64 aarch64 target) for that arithmetic instead - see `prime_ui_hook.c`.
- **DIAGNOSTIC_ONLY by default**: `build-hook.sh` builds with
  `-DDIAGNOSTIC_ONLY` unless `PRIME_UI_HOOK_PATCH=1` is set, which makes
  `applypatch` log what `qRegisterResourceData` saw (resource name, flags,
  length) without touching any memory. Both diagnostic and patch builds
  have been reported working on real JP24 hardware. Use the diagnostic
  build first when validating a different firmware release.

## Hardware findings and persistence

- Live diagnostic output on JP24 reported `ControlCenterTab.qml flags=0
  len=1156`: the resource is uncompressed and matches the JC11S patch slot.
- The real patch build displayed the Control Center tile and opened the
  Mod Loader screen on JP24.
- The owner reports that the UI Mod Loader remained available across a
  device reboot. A repeatable clean-install verification should still be
  recorded before treating this as a release-ready installer path.
- Restarting `engine.service` kills an sshd process forked directly by the
  ALSA hook. `activate-jp24.sh` now starts a separate systemd-managed SSH
  unit before scheduling the Engine restart. The unit retries a temporary
  port-2222 conflict with the old direct sshd and remains outside Engine's
  cgroup. This handoff was verified on real JP24 hardware on 2026-11-18:
  the installer-triggered Engine restart briefly closed the original SSH
  session, the systemd service recovered, and key-authenticated SSH worked.
  After a complete device reboot the service was recreated under
  `/system.slice/prime4-mod-sshd.service`, listened on port 2222, and again
  accepted the enrolled public key.

## Build

```sh
cd modloader/device/jp24/ui-hook
./build-hook.sh                      # diagnostic-only (default)
PRIME_UI_HOOK_PATCH=1 ./build-hook.sh # applies the real patch - do not use until the diagnostic build's log confirms the resource layout
file build/libprime-ui-hook.so
```

Requires `zig`, `clang` (targeting `aarch64-linux-gnu`), or
`aarch64-linux-gnu-gcc`.

## Status: verified live, activation chain now wired in

The diagnostic build was deployed behind `LD_PRELOAD` on real JP24 hardware
via a temporary `/run/systemd/system/engine.service.d/` drop-in: the hook
intercepted `qRegisterResourceData` and reported `ControlCenterTab.qml
flags=0 len=1156` - uncompressed, the same 1156-byte slot documented for
JC11S. The real patch build (`PRIME_UI_HOOK_PATCH=1`), using JC11S's own
`ControlCenterTab.qml` unmodified, made the Control Center tile appear,
clickable, opening the Mod Loader screen.

`activate-jp24.sh`, `install-device.sh`, and `verify-device.sh` now exist,
mirroring JC11S's own versions. The one architectural difference: JP24's
SSH bootstrap is compiled C
(`devices/jp24/ssh-bootstrap/alsa-boot-hook.c`), not a shell script like
JC11S's `/data/prime-ssh/activate.sh`, so `activate-jp24.sh` is called
from a second, independent `fork()` in that same hook
(`start_modloader_activation()`), guarded by its own once-per-boot stamp
file separate from the SSH one - a bug in the mod-loader activation can
never affect SSH access, and vice versa.

`activate-jp24.sh` wires the mod-loader launch path (bind-mounted launcher
wrappers, the `LD_PRELOAD` drop-in, and one scheduled `engine.service`
restart). Before the restart it starts `/data/prime4-mod/boot-activate.sh`,
which creates `/run/systemd/system/prime4-mod-sshd.service`. That separate
unit owns the port-2222 listener and is designed to survive Engine restarts.
On a legacy installation, the directly forked sshd can briefly hold port
2222 while the service retries; the Engine restart ends that conflict. The
hardware verification observed three retries before the service became
active. The generated unit is intentionally `disabled` because it lives in
`/run`; the persistent ALSA hook recreates and starts it on every boot.

## Next step

For a fresh firmware or clean `/data` image, run `install-device.sh`, reboot,
and confirm via `verify-device.sh` that the SSH unit and Mod Loader tile
come up automatically. The mod-management Go runtime-api backend
(`modloader/device/jc11s/runtime-api/`) has not been ported to aarch64
yet - `activate-jp24.sh` deliberately does not try to start it.
