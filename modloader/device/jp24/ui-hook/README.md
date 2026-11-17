# JP24 QML preload hook (port in progress, not yet hardware-verified)

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
  length) without touching any memory. This hook has not yet run on a real
  JP24 process; the diagnostic build is the first thing to actually deploy
  and check.

## Open questions, to resolve from the diagnostic build's log

- Does `/usr/Engine/Engine` actually call `ControlCenterTab.qml` through
  `qRegisterResourceData` the same way JC11S's does (same resource name,
  similar `flags`/`len`), or does JP24's Prime 4 G2 UI use a different
  Control Center layout entirely?
- The Engine binary exports `qt_resourceFeatureZstd` - is the
  `ControlCenterTab.qml` resource actually Zstd-compressed (`flags & 4`) on
  JP24? The JC11S `applypatch` assumes uncompressed bytes; a compressed
  resource would need a different patch strategy (decompress, patch,
  recompress, or replace the whole frame) before `PRIME_UI_HOOK_PATCH=1` is
  safe to use.
- JC11S's `activate.sh` relies on an early ALSA/SSH-hook entrypoint and a
  scheduled `systemctl restart engine.service`. JP24 already has an
  equivalent ALSA boot hook (`devices/jp24/ssh-bootstrap/alsa-boot-hook.c`)
  and does use systemd, so the same restart-scheduling approach should
  port, but this has not been written or tested yet.

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

`activate-jp24.sh` only does the mod-loader wiring (bind-mounted launcher
wrappers, the `LD_PRELOAD` drop-in, one scheduled `engine.service`
restart) - not yet tested end to end via a real reboot. Confirmed live
during manual testing: restarting `engine.service` also kills the
ALSA-hook-forked `sshd`, even though sshd isn't started by that unit at
all - a real device will lose SSH for the duration of one restart cycle
every boot until this is understood better.

## Next step

Run `install-device.sh` on a real JP24, then reboot the device (not just
`systemctl restart engine.service`) and confirm via `verify-device.sh`
that both SSH and the Mod Loader tile come up automatically, with no
manual `/run` drop-in needed. The mod-management Go runtime-api backend
(`modloader/device/jc11s/runtime-api/`) has not been ported to aarch64
yet - `activate-jp24.sh` deliberately does not try to start it.
