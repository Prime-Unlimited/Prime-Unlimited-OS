<p align="center">
  <img src="assets/prime-unlimited-os-brand.png" alt="Prime Unlimited OS" width="100%" />
</p>

# Prime Unlimited OS

**Open-source installer for SSH bootstrapping on Denon DJ Prime 4+ and Prime 4 G2.**

Prime Unlimited OS is a desktop installer for Windows and macOS that flashes a custom image to the internal eMMC of supported Denon DJ controllers. It overwrites the `/data` partition and enables SSH access.

The installer ships with everything required. No additional tools or runtimes need to be installed by the user.

## Supported devices

| Device              | Hardware profile | USB transport | Target         | SSH port |
|---------------------|------------------|---------------|----------------|----------|
| Denon DJ Prime 4+   | JC11S            | Fastboot      | `/data` (ext4) | 22       |
| Denon DJ Prime 4 G2 | JP24             | imbupd        | `/data` (ext4) | 2222     |

## What the installer does

1. Detects the connected device and selects the correct transport.
2. Prepares or imports a passphrase-protected Ed25519 key pair.
3. Builds or selects a device-specific `/data` image with the public key and SSH configuration.
4. Writes the image to the internal eMMC (overwriting `/data`).
5. Reboots the device and verifies an authenticated SSH login.
6. *(Mod manager unlock — under development)*

## Download

Pre-built installers for Windows and macOS are available in the [Releases](https://github.com/Prime-Unlimited/Prime-Unlimited-OS/releases).

## Important notes

- Flashing overwrites `/data`. Settings, Wi-Fi configuration, and local library metadata are lost.
- Only use the installer with a matching device. Never flash a JC11S image onto a JP24 (or vice versa).
- A verified recovery path is strongly recommended before writing.

Further technical details: [Bootstrap specification](docs/BOOTSTRAP_SPEC.md) · [Device selection](docs/DEVICE_SELECTION.md) · [JC11S technical record](docs/JC11S_PROCESS.md)

## Disclaimer

Unofficial community project. Not affiliated with or endorsed by Denon DJ or inMusic.  
Flashing the internal storage can brick the device and voids the warranty. Use at your own risk.
