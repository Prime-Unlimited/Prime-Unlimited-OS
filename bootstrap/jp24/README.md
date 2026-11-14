# JP24 / Prime 4 G2 SSH bootstrap module

Standalone integration module for the Denon DJ Prime 4 G2 (JP24). It is designed to be called by the desktop installer later; importing the package performs no USB operations and never writes to a device.

## Status and identity gate

The module recognizes the imbupd USB transport (`15e4:d014`, interface class `ff/61/7a`) and queries the protocol. A matching VID:PID or protocol version alone identifies only the transport. JP24 identity is accepted only from the exact imbupd product code `JP24`, or from the exact USB manufacturer/product strings `DENON DJ` / `PRIME 4 G2`, together with a successful `1.4.2` protocol response and the expected imbupd interface.

A read-only macOS IORegistry inspection of the connected controller confirmed vendor ID `5604` (`0x15e4`), product ID `53268` (`0xd014`), manufacturer `DENON DJ`, product `PRIME 4 G2`, and the `ff/61/7a` interface signature. No serial number is recorded here. The standalone C query still needs USB access permission to collect protocol properties. Identity evidence by itself does not establish a safe write: the verified `/data` backup and recovery gate remains mandatory.

Hardware query result (2026-11-14; serial omitted): imbupd returned `version: 1.4.2` and `power-source: mains`; the `product` and `serial` properties returned `not found`. The USB descriptor pair independently identifies the connected model, so the missing imbupd product property is not the remaining identity blocker. The current native tool implements partition erase/write, but no `/data` read-back or backup path has been verified.

This is a known blocker, not an installer option that can be overridden by manual selection.

## Module interface

```python
from pathlib import Path
from bootstrap.jp24 import FlashPlan, Jp24Bootstrap, Jp24IdentityProbe

inspection = Jp24IdentityProbe(Path("build/imbupd_write_mac")).probe()
# Pass the installer-generated public key to build-image.sh; never pass a private key.
# Build and validate the image before constructing a FlashPlan.
bootstrap = Jp24Bootstrap(Path("build/imbupd_write_mac"))
bootstrap.flash(plan, confirm=installer_confirmation_dialog)
marker = bootstrap.verify_ssh(
    host=device_ip,
    private_key=private_key_path,
    passphrase=key_passphrase,
    known_hosts=known_hosts_path,
    expected_host_fingerprint=independently_verified_fingerprint,
)
```

`flash()` requires verified JP24 identity, mains power, a matching SHA-256, a verified backup/recovery procedure, and an explicit confirmation callback. The native writer separately requires the literal phrase `FLASH JP24 /data`. It writes only the `data` partition and then reboots. Do not connect the callback until the desktop UI has a clear, affirmative confirmation dialog.

SSH verification uses the repository's `core.ssh_service.SshService`: it pins only the expected host-key fingerprint provided by the caller, then confirms authenticated root SSH on port 2222. A listening port alone is not success.

## Dependencies

- Python 3.12+ (same runtime as the desktop application)
- Existing repository dependencies, including `paramiko` and `cryptography`
- `PyUSB` for descriptor-level interface inspection: `python -m pip install -e '.[jp24]'` or `python -m pip install -r bootstrap/jp24/requirements.txt`
- `libusb` development headers and `pkg-config` to compile the native imbupd tool
- `e2fsprogs` (`mke2fs`, `debugfs`, `e2fsck`) to build and inspect ext4 images
- Zig with AArch64 Linux target support, or an AArch64 cross-compiler, to build the ALSA hook

On macOS with Homebrew: `brew install libusb pkg-config e2fsprogs`; install Zig separately. Linux package names vary by distribution. Windows needs a libusb-compatible backend/driver and a supported AArch64 cross-compiler; this target still needs integration validation.

The current build helpers are Bash scripts intended for macOS and Linux. A native Windows build adapter is not included yet.

Build the read-only/native protocol tool from the repository root with `./bootstrap/jp24/build-native.sh`. Build the image with `./bootstrap/jp24/build-image.sh /path/to/installer-key.pub [output-image]`.

## Build inputs and image

Use `payload/alsa-boot-hook.c`, `payload/99-prime4-mod.conf`, and `payload/boot-activate.sh` with `build-image.sh`. The installer supplies the user's **public** Ed25519 key. The 256 MiB ext4 image contains the ALSA hook and a key-only SSH service on port 2222. Private keys, generated images, and third-party EngineX binaries are not stored here.

The image builder validates the public-key format, expected paths, ext4 health, the encryption feature, and prints a SHA-256. `build_image()` returns the image size and SHA-256 to the installer. The installer must independently validate these values before offering a flash confirmation.

## Evidence and release gates

- The Vault records a successful JP24 SSH setup on port 2222 using the ALSA hook and `imbupd` data-image transfer.
- That does not yet establish a redistributable, independently reproducible Prime-Unlimited bootstrap package.
- The imbupd query returned version `1.4.2` and mains power; `product` and `serial` were not found. Exact host-visible USB manufacturer/product descriptors independently confirmed the connected unit as JP24.
- `/data` replacement is destructive. A tested backup and recovery path is still required before production writing is enabled.
- No flash has been run by this module during development.
