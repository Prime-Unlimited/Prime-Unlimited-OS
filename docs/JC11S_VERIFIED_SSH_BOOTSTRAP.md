# JC11S: hardware-verified SSH bootstrap (Windows-native installer)

## Scope

This recipe applies only to JC11S (rk3288-az01-jc11s, ARMv7). The existing Windows-native installer remains responsible for device detection, Fastboot geometry checks, Ed25519 key generation, bundled native e2fsprogs, Android sparse conversion, flash authorization and authenticated SSH verification. No WSL is required by the installer. JP24 bootstrap sources and behavior are unchanged.

## Confirmed hardware experiment (2026-11-16)

The tester built a separate JC11S image with the project's existing `core.jc11s_image` builder, using the normal ARMv7 hook plus the ALSA compatibility exports, a direct-sshd activation script, and both ALSA config locations. The resulting raw ext4 image was 2,413,280,768 bytes (`0x8fd7be00`), with SHA-256 `d90dff2c8086f33f1635e5bd8b46e220523fe94d42cfd68f5807569be773f12c`. Offline image verification and `e2fsck -fn` passed. This hash is evidence for that one image with that tester's public key, not an expected hash for other users.

Fastboot reported product `rk3288-az01-jc11s`, protocol `0.4`, bootloader `U-Boot 2026.07-inmusic-20260721`, `secure: yes`, `partition-type:data: ext4`, and the matching data partition size. Flashing the raw image with `fastboot flash data` reported success. The device rebooted, port 22 opened, and an authenticated root login using the newly enrolled Ed25519 key succeeded. A second reboot followed by a second authenticated SSH login succeeded. The activation log contained `direct sshd start requested` and `Accepted publickey for root`. The sshd listener command was `/usr/sbin/sshd -D -e -f /data/prime-ssh/sshd_config`.

This establishes successful boot and SSH activation for the tested device/firmware, not a universal firmware or production rollout guarantee. The exact ALSA trigger was not isolated independently; both configuration paths were present in the successful image, and `/var/lib/alsa/conf.d/99-prime-ssh.conf` was visible after boot.

## Implementation used by the native Windows installer

1. `NativeWindowsImageBuilder` in `core/windows_installer.py` validates the packaged ARMv7 hook and its SHA-256, and uses bundled Windows-native e2fsprogs. Its existing checks, keys and flash transport are not changed by this patch.
2. The CI workflow compiles `bootstrap/jc11s/hook.c` via `tools/build-jc11s-hook.sh`, bundles the ELF32 ARM library and SHA-256 in the Windows executable.
3. `core/jc11s_image.py` embeds the public key, ARM hook, `activate.sh`, `sshd_config` and the same ALSA configuration in both `system/var/overlay/lib/alsa/conf.d/` and `system/var-lib/alsa/conf.d/`.
4. On hook activation, `activate.sh` checks root and the enrolled public key, creates `/run/sshd`, generates a device-local Ed25519 host key if absent, validates sshd configuration, then `exec`s `/usr/sbin/sshd -D -e -f /data/prime-ssh/sshd_config`. It does not create or start a systemd unit.
5. SSH is key-only on port 22. The installer must verify authenticated login and retain the existing destructive-flash/recovery safeguards.

## Reproduce and verify (development only)

On a Linux development host with the ARM cross-compiler and e2fsprogs, the existing offline test builder is:

```sh
sh tools/build-jc11s-hook.sh /tmp/libprime-ssh.so
python3 -m core.jc11s_image build --key /path/to/key.pub --hook /tmp/libprime-ssh.so --size-bytes 2413280768 --output /tmp/jc11s-data.img
python3 -m core.jc11s_image verify /tmp/jc11s-data.img
e2fsck -fn /tmp/jc11s-data.img
```

The Windows installer does **not** use this Linux build path: it uses its already implemented native builder and its own Fastboot-probed partition size. Do not hardcode the experimental size for other devices. Do not flash without a verified backup and recovery path.

## Remaining verification

Run the Windows installer artifact on a clean Windows machine; verify bundled e2fsprogs, packaged hook digest, complete native image build, sparse transfer, first boot, SSH authentication and repeat reboot. The hardware experiment above was performed using the separate test image, not an installer-produced image.
