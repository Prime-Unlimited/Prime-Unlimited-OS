# Prime Clock v0.1.2

Prime Clock is a compact Engine OS overlay and the reference implementation for
Prime Unlimited overlay mods. A tap switches between local `HH:MM` time and a
monotonic stopwatch. Play/Pause, resume and Reset are available only in stopwatch
mode. Hold for 600 ms before dragging; release persists the clamped display
position under `/data/prime-unlimited/settings/` through the Modloader API.

## Device status

| Device | Build target | Package validation | Modloader | Hardware |
| --- | --- | --- | --- | --- |
| Prime 4+ / JC11S | `armv7l`, `gnueabihf`, QtQuick 2.12, Engine OS 5.3.0 | automated | install/enable/disable/launch/uninstall verified | hardware verified |
| Prime 4 G2 / JP24 | RK3588/AArch64, `aarch64-linux-gnu.2.17` toolchain target; runtime identity, Qt and firmware unconfirmed | automated structure validation | integration prepared, not eligible | not tested |

JC11S hardware verification covered visible single-instance rendering, clock and
stopwatch touch UI, smooth long-press dragging, persisted position,
Enable/Disable and Install/Uninstall while physical controls remained available.

The JP24 package is deliberately not placed in a trusted live catalog until the
device reports its exact Modloader ABI, firmware and Qt/QML compatibility. A
successful package build is not a hardware result.

## Build and validate

```sh
python3 mods/prime-clock/scripts/package.py --target jc11s --output dist
python3 mods/prime-clock/scripts/package.py --target jp24 --output dist
python3 mods/prime-clock/scripts/validate_package.py dist/prime-clock-v0.1.2-jc11s.zip --target jc11s
python3 mods/prime-clock/scripts/validate_package.py dist/prime-clock-v0.1.2-jp24.zip --target jp24
```

Packages are deterministic ZIPs. They contain only QML/JavaScript, an inert
manual-launch shell entrypoint, and a target profile; no host-native libraries
are linked into them. The JC11S Modloader uses catalog values `overlay` =
`prime-clock/ui/PrimeClock.qml` and `entrypoint` = `prime-clock/start.sh`.

Enable exposes the overlay to the private OverlayHost; Disable, Safe Mode and
Uninstall remove it dynamically without restarting Engine. Launch validates the
normal lifecycle and exits successfully without creating a second overlay.
Uninstall leaves the harmless position preference available for reinstall.

## CI and releases

`.github/workflows/prime-clock.yml` runs shared tests, runtime tests, a two-target
matrix, deterministic rebuild comparison, ZIP inspection, metadata/SHA-256
generation and artifact upload. A `prime-clock-v*` tag uploads both immutable
ZIPs and metadata to a GitHub Release. Catalog signing remains an authorized
second step: CI has no private key and never claims an unsigned catalog is trusted.

## Reusing the overlay interface

Add a mode-0600 QML root `Item` that fills its parent, intercepts input only in
its visible control, and optionally implements `restorePosition(x,y)` plus
`positionSaveRequested(x,y)`. Declare its safe relative path in catalog v2 as
`overlay`. Keep device-specific facts in platform profiles, use separate IDs or
records when package compatibility differs, and never mark an untested target as
hardware verified. The host owns authorization and `/data` persistence; package
QML never receives the Modloader bearer token.

## Known limitations

- The first JC11S deployment requires installing the updated reusable UI hook;
  that controlled Engine restart must happen outside an active set.
- JP24 runtime identity, firmware, QML version, adapter and hardware behavior are
  genuine blockers to trusted catalog publication, not inferred values.
- Stopwatch state itself is intentionally session-only; only overlay position is
  persistent.
