# JC11S offline SSH PoC

This implementation builds and checks a complete raw ext4 filesystem offline and
rehearses a flash against an in-memory target. It does **not** establish that a
fresh Prime 4+ can boot this filesystem or start SSH yet. Hardware writes are
unconditionally disabled. No existing SSH session, device filesystem access,
EngineX installation, library, service or UI patch is used by the builder.

Base: `feat/jc11s-native-windows-fastboot`, commit
`09864f4ee9548ab9e5eff580605c864d0b966dfe`. Target scope: JC11S ARMv7 only.

## Evidence and feasibility

| Observation | What it establishes | Remaining uncertainty |
| --- | --- | --- |
| Current Windows probe: product `rk3288-az01-jc11s`, Fastboot `0.4`, U-Boot `2026.07-inmusic-20260721`, secure bootloader mode | Strong model and transport identity for the probed unit | Firmware compatibility beyond this build is not established |
| Current per-device probe: `data`=`ext4`, size `0x8fd7be00`, max download `0x20000000`; `userdata` unsupported | Exact geometry and transfer limit for this one unit | Values must be queried again for every connection; they are never a JC11S default |
| Historical JC11S logs record successful `flash data` operations as `sparse 1/1` | This JC11S bootloader family has processed Android sparse input | Current vendor source/config is unavailable; no write probe was performed in this task |
| User reports ext4 `/dev/mmcblk0p11` mounted at `/data` | Linux block device and mount point on one working unit | No proof that Fastboot `data` or `userdata` maps to p11 |
| Uploaded ALSA config registers a shared-library config hook | A plausible independent activation mechanism | Stock ALSA includes, callback privileges and execution order on a fresh filesystem |
| Uploaded archive lacks full stock `/data` and stock boot units | Cannot derive a complete vendor initialization contract | Required directories, bind mounts, symlinks, mkfs checks and defaults still need evidence |

`userdata` is only another bootloader candidate queried separately. `/data` is a
Linux mount point. `/dev/mmcblk0p11` is a Linux block device. None is automatically
translated into another. An image filename containing “userdata” proves nothing.

The supplied archive was inspected without executing scripts or loading its
binary. Archive SHA256:
`f733ba70d65d9308c72d384597565eaaecb851557d8268c8b002a5325c6a5b1e`.
Only the hook declaration and service-activation mechanism informed this design;
no archive content is copied into the payload. Its scripts are reference data,
not instructions for this implementation.

### Dynamic geometry and transfer strategy

The installer queries `version`, `product`, `version-bootloader`, `secure`,
`max-download-size`, `partition-size:data`, `partition-type:data`, `is-userspace`
and `current-slot` through the selected native WinUSB session. Missing, malformed,
zero, non-sector-aligned or incompatible values block the build. The current PoC
requires product `rk3288-az01-jc11s`, protocol 0.4, bootloader mode and ext4. It
uses the returned partition size as the sole image-size source. The measured
**2,413,280,768 bytes belongs only to the probed modified unit** and is neither a
default nor a compatibility profile. Tests cover several artificial sizes and
download limits, including a size not divisible by 4096.

* The complete flow is: read-only probe → strict geometry validation → exact-size
  ext4 build → ext4/hash verification → Android sparse conversion → independent
  sparse reconstruction → memory-only transfer rehearsal.
* The filesystem uses extents and a journal, 4096-byte blocks, 256-byte inodes,
  no 64bit, metadata_csum, orphan_file, encryption or other modern incompatibility
  features. These conservative choices still require a target-kernel mount test.
* The converter emits Android sparse v1 with 512-byte logical blocks. RAW chunks
  carry non-zero sectors; zero runs use `FILL(0)`, so every sector is explicitly
  reproduced and old free-space contents are not retained through `DONT_CARE`.
  `DONT_CARE` is used only to position independently flashable pieces.
* Every sparse file is strictly smaller than that device's measured
  `max-download-size`, including the sparse and chunk headers. Dense input is
  split into multiple full-logical-size sparse files. Each file writes a disjoint
  extent; all files together reconstruct the raw SHA256 exactly.
* Upstream U-Boot v2025.10 selects `IMAGE_SPARSE` when Rockchip `FASTBOOT_FLASH`
  is enabled. Its MMC path detects the sparse magic, writes RAW/FILL chunks and
  treats DONT_CARE as an offset advance. This matches the historical JC11S
  `sparse 1/1` logs, but is not proof that InMusic's unpublished 2026 fork is
  byte-for-byte identical. Sources:
  [Kconfig](https://github.com/u-boot/u-boot/blob/v2025.10/drivers/fastboot/Kconfig),
  [MMC Fastboot](https://github.com/u-boot/u-boot/blob/v2025.10/drivers/fastboot/fb_mmc.c),
  [sparse parser](https://github.com/u-boot/u-boot/blob/v2025.10/lib/image-sparse.c).
* A sparse file's expanded size may exceed the download buffer; its transmitted
  byte size may not. USB packets and Python I/O chunks are not separate Fastboot
  downloads. Upstream MMC sparse flashing does not erase the whole partition for
  each part, which permits disjoint repeated `flash:data` calls. The InMusic fork
  must still be verified before relying on multipart transfer. Interrupted
  multipart recovery and real vendor-fork behavior remain hardware gates.

## Payload

The root of the image becomes `/data`; do not put a second `data/` directory inside.

| Path relative to `/data` | Owner | Mode |
| --- | --- | --- |
| `prime-ssh/`, `prime-ssh/hostkeys/` | 0:0 | 0700 |
| `prime-ssh/authorized_keys` | 0:0 | 0600 |
| `prime-ssh/activate.sh` | 0:0 | 0700 |
| `prime-ssh/sshd_config` | 0:0 | 0600 |
| `prime-ssh/libprime-ssh.so` | 0:0 | 0644 |
| `system/var/overlay/lib/alsa/conf.d/99-prime-ssh.conf` | 0:0 | 0644 |
| overlay parent directories | 0:0 | 0755 |

Only these five payload files, their directories and ext4 metadata/lost+found are
created. No vendor symlinks are guessed. Any symlinks required by stock initialization
remain a hardware-evidence blocker, not silently fabricated vendor content.

The ARM EABI5 shared object exports the ALSA callback and its version marker.
It has no ELF NEEDED dependencies or unresolved symbols. It double-forks a shell
activation script without changing Engine UI or using LD_PRELOAD. ALSA hook ABI
marker follows `SND_CONFIG_DLSYM_VERSION_HOOK` / `SND_DLSYM_BUILD_VERSION` in public
ALSA headers. On-device loading has not yet been verified.

Activation requires root and systemd PID 1; it serializes concurrent callbacks,
creates a unique device host key on first boot, checks `sshd -t`, writes only a
runtime drop-in for the existing `sshd.service`, reloads systemd, and restarts that
service. It never changes rootfs, shadow, passwords or root authorized_keys.
The complete isolated sshd config points directly to the selected public key.
Only root public-key authentication is allowed; password, interactive and forwarding
access are disabled. No private client key or pre-generated shared host key is in
the image. Configuration validation failure prevents service changes. First-boot
host-key generation failure also stops activation.

Stock sshd/ssh-keygen versions, service conditions, root account lock policy,
privilege-separation paths, ALSA timing and networking must be checked on hardware.
A locked root account may prevent public-key login; the PoC does not unlock it.
No password or direct-daemon fallback is included.

## Offline build on Linux / provisioned WSL

Provision once from signed distribution packages, **before** working offline:
`python3`, `python3-cryptography`, `e2fsprogs`, `gcc-arm-linux-gnueabihf`,
`binutils-arm-linux-gnueabihf`. Tests additionally require the repository's Python
dev dependencies. No mounts, root privileges or running services are required by
the build. Tested here: Python 3.12.14, e2fsprogs 1.47.2, GCC 14.2.0.

From the repository root:

```sh
mkdir -p output
sh tools/build-jc11s-hook.sh output/libprime-ssh.so
# probe.json is captured from this specific connected device.
python3 -m core.jc11s_flash build-device --probe probe.json \
  --key /path/to/selected_ed25519.pub --hook output/libprime-ssh.so \
  --output output/data.ext4 --sparse-output output/data.sparse
python3 -m core.jc11s_flash simulate output/data.ext4 --probe probe.json \
  --sparse-output output/rehearsal.sparse
pytest
```

Select your own **public** Ed25519 key; retain its matching private key separately.
The builder accepts no passwords or existing device access. Existing image or
manifest files are never overwritten. Outputs are a raw image and JSON manifest
with file hashes, full SHA256, byte size and explicit unverified hardware/boot
status. The manifest is a local integrity record, not signed firmware provenance
or authorization to flash. Do not treat a passed ext4 check as boot validation.

The build fixes UUID, filesystem times, inode ownership/modes, feature selection
and hash seed. Tests rebuild with the same inputs and toolchain and compare the
entire raw image SHA256. Cross-version e2fsprogs/compiler reproducibility has not
been established. Keep the recorded tool versions for reproduction.

## Native Windows offline build

The packaged desktop application builds directly on Windows. Its PyInstaller
payload contains MinGW e2fsprogs (`mke2fs`, `debugfs`, `e2fsck` and their runtime
DLLs/configuration) plus an ARMv7 hook built from `bootstrap/jc11s/hook.c` by the
Linux CI job. The hook is accepted only when both its ELF32/ARM ABI and the bundled
SHA-256 provenance record match. The Windows job obtains the tools while assembling
the release artifact, then performs a complete 64 MiB ext4 build, payload extraction,
`e2fsck`, sparse conversion, transfer-limit check and SHA-256 verification before
PyInstaller runs.

The installed EXE performs no download and invokes no WSL, Debian, Linux VM or
external compiler. It reuses those bundled native executables in a private temporary
workspace. Missing tools, configuration, hook, or a hook hash mismatch block the
build. The raw image still takes its exact size solely from the connected device's
validated `partition-size:data`; sparse part sizing solely uses its reported
`max-download-size`.

## Desktop installer and guarded hardware transport

The PySide6 assistant now covers detection, key management, device-specific build,
sparse verification, data-loss/recovery review, flash status, reboot and
host-key-pinned SSH verification. Normal users do not need to enter Linux commands.

The native WinUSB transport implements Fastboot 0.4 `download`, bounded payload
streaming, terminal `OKAY`/`FAIL` processing and the allowlisted `flash:data`
command. It re-probes product, firmware, partition type/size and download limit
on the same connection immediately before writing. Any mismatch, timeout, USB
disconnect, partial transfer or bootloader failure stops the sequence; reboot is
sent only after every part reports successful flash.

Implementation and authorization are separate. `HardwareReleasePolicy` currently
keeps all real writes disabled. The GUI cannot turn evidence gates into true by
checking a box. The immediate `ERASE DATA` confirmation is necessary but cannot
substitute for engineering validation. Multipart plans additionally require a
target-specific proof that repeated sparse `flash:data` calls do not erase earlier
parts.

Read-only native hardware evidence collector, from Windows Python with repository
requirements installed:

```powershell
python -m core.jc11s_flash probe-windows > this-device-probe.json
```

It uses the existing discovery, descriptor validation, timeouts and cleanup,
queries `data` and `userdata` separately, and reports unsupported variables.
There is no reboot, erase, download or flash in this path. Transport/protocol
errors abort collection; FAIL responses are recorded as unsupported. Keep the
output as target-specific evidence; do not substitute USB identification for a
verified partition map.

## Tests and remaining hardware gates

Automated tests exercise real ext4 creation and `e2fsck -fn`, extract every payload
file with debugfs, check key/directory ownership and modes, compare byte-identical
rebuilds, reject corrupt images and invalid keys/architectures, inspect ARM ELF
symbols/dependencies, execute activation logic in a filesystem/command sandbox,
and check mock flash limits/failures and native read-only probe cleanup. The
sandbox does not claim that the ARM hook or stock sshd executed on the device.

Before any future implementation enables hardware writing:

1. Collect target product, board/revision and firmware identity, Fastboot variables,
   exact size/type, download limit and lock/security state. Unsupported getvars
   need another authoritative source; no speculative write probes.
2. Correlate the bootloader partition table/GPT with Linux p11 and `/data` using
   read-only evidence from stock recovery, a vendor image, or a separately
   provided device inventory. Do not require pre-existing SSH for installation.
3. Inspect stock `az0x-data-mkfs`, `data.mount`, overlay setup and ALSA include order.
   Determine required first-boot skeleton, labels, symlinks and feature flags;
   verify that a fresh image is neither reformatted nor rejected. The reference
   archive cannot answer these questions.
4. Obtain a complete recoverable backup and demonstrate stock recovery, including
   bootloader re-entry after a broken `/data` and power-loss/interrupted transfer.
   Historical notes say `fastboot fetch data` was unsupported; getvar is not backup.
5. On a recoverable bench unit, validate ext4 mount/boot, independent ALSA callback,
   stock SSH key generation, root login policy, Wi-Fi/Ethernet and normal audio/UI.
   Test an image built without any EngineX directory and a second cold boot.
6. Verify authenticated SSH with the chosen private key and a trusted host-key
   fingerprint using existing `core.ssh_service.SshService`; confirm the
   `PRIME_UNLIMITED_SSH_OK` result. Do not disable host-key checks. A secure first
   host-key enrollment channel is still required; a listening port is insufficient.

The two observations “Fastboot partition `data`” and “Linux `/data` is mounted
from `/dev/mmcblk0p11`” agree with the intended target, but neither Fastboot
getvars nor the reference archive exposes the GPT entry number. Therefore the
`data`→p11 mapping is **not yet independently proven**. A read-only GPT/firmware
partition table, stock recovery inventory or equivalent authoritative evidence
is still required. The installer names only bootloader partition `data`; it never
turns `/data` or `mmcblk0p11` into a Fastboot command.

## Cloud validation record (2026-11-15)

* The current suite passes **128 tests**. The dynamic/sparse and installer revisions add
  multiple artificial geometries, limits, dense multi-part conversion, explicit
  zero-fill reconstruction and full transfer simulation; see the PR checks for
  the current count.
* Real ARM hook compiled; ELF32 ARM EABI5, exported hook/version symbols, no
  shared-library dependencies or unresolved symbols.
* The earlier full-size fixture matched one modified device only. It is retained
  as historical validation, not as a size constant or reusable install image.
* The current converter transformed that historical 2,413,280,768-byte fixture
  under the newly measured 512 MiB limit into one 99,852-byte Android sparse
  transfer (145 chunks). Offline reconstruction matched raw SHA256
  `a8295f7e5d7882af12bb57b7eebc61191326089334ba84e6f2b820891581a56a`.
  The memory rehearsal completed `download:0001860c`, `flash:data`, `reboot`.
  This validates the converter and simulator against those inputs, not a hardware write.
* Small-image repeated builds were byte-identical. No claim of a second
  full-size rebuild, Windows-host run, device flash, cold boot or SSH login.
* The full-size cloud fixture uses a test-only public key with no retained
  matching private key. It is not an installable user configuration and is not
  committed. Rebuild with the user's selected public key for future bench work.
