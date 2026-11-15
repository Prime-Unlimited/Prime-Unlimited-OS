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
| Windows bench report: `15E4:D007`, `FF/42/03`, `OKAY0.4`, physical reboot | Working native WinUSB transport | Not flash support or model/firmware authorization |
| `docs/JC11S_PROCESS.md` records historical `flash data` OKAY and ext4 `data` size `0x8fd7be00` | Historical evidence for bootloader name **data**, stronger than guessing userdata | Must reconfirm on the target firmware; historical report is not a new hardware test |
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

### Size and transfer constraints

* Historical size is **2,413,280,768 bytes**, not an exact 2.1 GiB assumption.
  It is sector aligned but **not** 4096-byte aligned. The builder accepts an exact
  512-byte-sector size; ext4 occupies whole 4096-byte blocks and any remaining
  partition tail stays zero. Integration tests include a non-block-aligned size.
* No default hardware size is selected. Supply an observed size explicitly.
  The builder supports 64 MiB through 4 GiB for research; this is not a firmware
  compatibility matrix. The flash rehearsal requires exact partition size.
* The filesystem uses extents and a journal, 4096-byte blocks, 256-byte inodes,
  no 64bit, metadata_csum, orphan_file, encryption or other modern incompatibility
  features. These conservative choices still require a target-kernel mount test.
* `max-download-size` must be measured, not guessed. Fastboot 0.4 uses an eight
  hexadecimal-digit download size. The PoC refuses raw files larger than either
  the reported limit or `0xffffffff` bytes. USB endpoint packet size is **not** a
  maximum image size. Python I/O chunks are also not independent flash chunks.
* Historical Android sparse flashing does not validate sparse support in this
  native implementation. Sparse conversion, split sparse transfers, partition
  erasure semantics and interrupted-write recovery are intentionally unimplemented.

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
# 64 MiB research fixture, NOT a proposed device partition size:
python3 -m core.jc11s_image build --key /path/to/selected_ed25519.pub \
  --hook output/libprime-ssh.so --size-bytes 67108864 --output output/data.ext4
python3 -m core.jc11s_image verify output/data.ext4
python3 -m core.jc11s_flash simulate output/data.ext4 --partition data \
  --partition-size 67108864 --max-download 67108864
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

## Windows offline entry point

```powershell
.\tools\build-jc11s-image.ps1 -PublicKey C:\keys\prime.pub `
  -Output C:\images\data.ext4 -SizeBytes 67108864 -Distribution Debian
```

This PoC uses **preprovisioned WSL** for ext4 tooling and ARM compilation. It does
not download or install anything during the offline build. WSL is only a build
backend; USB probing continues to use native Windows WinUSB and needs no USB
passthrough. Python path arguments are passed as arrays, not interpolated shell
commands. The checkout must include `bootstrap/jc11s` sources. This is a source
checkout PoC, not an integrated PyInstaller installer or a native Windows ext4
writer. The PowerShell/WSL entry point still needs a Windows-host execution test;
only its Linux backend was executed in this cloud machine.

Read-only native hardware evidence collector, from Windows Python with repository
requirements installed:

```powershell
python -m core.jc11s_flash probe-windows
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

No hardware was attached to this cloud task. Consequently flash support on the
current target, fresh `/data` boot compatibility and end-to-end SSH installation
are **open**, even though the offline filesystem and mock PoC are executable.

## Cloud validation record (2026-11-15)

* Full repository suite: **99 passed**; changed Python modules pass Ruff.
* Real ARM hook compiled; ELF32 ARM EABI5, exported hook/version symbols, no
  shared-library dependencies or unresolved symbols.
* Built and verified a **2,413,280,768-byte** raw ext4 image, including `e2fsck -fn`
  and extraction/hash comparison of all five payload files.
* Rehearsed all 2,413,280,768 bytes through the memory mock; resulting command
  sequence: `download:8fd7be00`, `flash:data`, `reboot`. The rehearsal's transfer
  limit was a synthetic test input, **not** a measured JC11S download limit.
* Small-image repeated builds were byte-identical. No claim of a second
  full-size rebuild, Windows-host run, device flash, cold boot or SSH login.
* The full-size cloud fixture uses a test-only public key with no retained
  matching private key. It is not an installable user configuration and is not
  committed. Rebuild with the user's selected public key for future bench work.
