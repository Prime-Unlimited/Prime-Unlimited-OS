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

## Disclaimer and risk notice

**Prime Unlimited OS is an independent, unofficial community project.** It is not affiliated with, sponsored by, approved by, or endorsed by Denon DJ, inMusic Brands, or any device manufacturer. All product names and trademarks remain the property of their respective owners.

**Use at your own risk.** This project includes an installer, a modloader, and device modifications (mods). Installing or using any of these components may modify internal storage, system configuration, device behavior, or files on connected storage devices. These changes carry risks, including:

- **Data loss:** Flashing overwrites the `/data` partition and removes settings, Wi-Fi configuration, and local library metadata. Modifications may also cause unintended deletion, overwriting, corruption, or incomplete copying of files on internal or connected storage.
- **Device failure:** An incorrect image, interrupted installation, incompatible firmware, or malfunctioning mod may leave a device unable to boot or operate normally and may require recovery or repair. Recovery is not guaranteed.
- **Performance and reliability:** Mods may cause crashes, unexpected behavior, interrupted playback, or other malfunctions. Do not rely on experimental modifications during critical performances.
- **Warranty and support:** Modifying a device may affect manufacturer warranty coverage or support. Applicable statutory consumer rights depend on the relevant law and circumstances.

**Before proceeding:** Back up important data, verify device model and firmware compatibility, confirm that a suitable recovery procedure is available, and understand which storage devices and partitions may be changed. Never flash an image intended for a different hardware profile. Do not disconnect the device or interrupt a write operation.

**No warranty or guarantee.** To the maximum extent permitted by applicable law, Prime Unlimited OS and its project-provided mods are supplied **"AS IS"**, without warranties of any kind, including any guarantee of compatibility, uninterrupted operation, data integrity, or successful recovery. To the extent permitted by applicable law, the authors and contributors disclaim liability for losses or damages arising from installation, modification, use, or inability to use the software, including device damage, data loss, and business interruption.

**Nothing in this notice excludes or limits liability or statutory rights that cannot legally be excluded or limited.** Third-party components remain subject to their own applicable licenses and notices.

