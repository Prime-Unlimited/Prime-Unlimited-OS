# Verified device status

Snapshot taken 2026-11-17 on a Denon DJ Prime 4+ (`JC11S`) with Engine OS
5.3.0. This file records evidence, not secrets or mutable device logs.

## Live checks

```text
engine.service: active
Engine command PID: 4381
environment: LD_PRELOAD=/data/prime-unlimited/lib/libprime-ui-hook.so
maps: /data/prime-unlimited/lib/libprime-ui-hook.so present
qrc resource: ControlCenterTab.qml flags=0 len=1156
hook result: swap applied ControlCenterTab.qml timestamped
replacement length: 1113 bytes (maximum verified slot: 1156 bytes)
```

The Control Center tile was visually verified after Day Mode. Its click opens
the Mod Loader QML. The artwork was subsequently verified with the cropped SVG
viewBox and `Image.Stretch` so the 256 by 104 pixel tile fills like its peers.

## Deployed checksums

```text
5441883e74870411eb11ea096307ad0a38ed144a5dd83db244fb7909a32d6bde  lib/libprime-ui-hook.so
446a8486c736a7e52201368e4a9207f1edc2174efc4057f42d2db98b34884ad3  ui/patches/ControlCenterTab.qml
ccf67722c1ac7de9be647a401ea2ef6414a80548b14b6454585aab5dccd81a55  ui/ModLoader.qml
d8fd23319f613a3bf11ae0a50de8797cf29433baacd84cc161bcab71525b3bcd  ui/prime_unlimited_mod_loader.svg
275abeeb93b85b28dad4de447f288ca84caa7dc847a77f240813fae7e33857aa  bin/runengine-hooked.sh
206c9905650f8659c4dea752bbf1eb539aedf2c92db903ef615f10afab4a7260  /data/prime-ssh/activate.sh
```

The checked-in `artifacts/libprime-ui-hook.so` is the exact binary represented
by the first checksum. Source and build script are checked in beside it so the
artifact can be audited and rebuilt.

## Runtime systemd drop-in

The ALSA bootstrap creates this volatile file on every boot:

```ini
# /run/systemd/system/engine.service.d/99-prime-ui.conf
[Service]
Environment=LD_PRELOAD=/data/prime-unlimited/lib/libprime-ui-hook.so
```

The `/run` location is intentional: the protected vendor filesystem is never
modified, and early activation recreates the state after a reboot.

## Attempts that were insufficient

- Writing only the systemd drop-in: it happened after the first Engine start.
- Relying on the constructor log: the library was not necessarily retained in
  `/proc/PID/maps`.
- Changing only the QML replacement bytes: Qt reused cached compiled QML.
- `QML_DISABLE_DISK_CACHE=1` without clearing the exact Engine QML cache.
- Closing and reopening Control Center: this firmware keeps that component
  loaded for the process lifetime.

The successful combination is the early bind-mount/drop-in preparation, one
systemd-managed restart, final-process preload injection, content-derived qrc
timestamp, and exact cache removal documented in this directory.
