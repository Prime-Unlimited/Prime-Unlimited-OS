# Prime 4 G2 SSH bootstrap

This packages the team's documented JP24 `/data` ALSA-hook approach. The hook uses the corrected Cd1s ALSA symbol names so Engine LEDs continue to work. The builder creates an image locally; it does not flash the device.

## Build the image

Requirements on macOS: Homebrew `e2fsprogs` and either Zig or an AArch64 GNU cross-compiler.

```sh
brew install e2fsprogs
ssh-keygen -t ed25519 -f ./denon_prime4_ed25519 -C prime4-mod
./alsa-trigger/build-data-image.sh ./denon_prime4_ed25519.pub
```

The output defaults to `alsa-trigger/prime4-data.img` at 256 MiB. Set `DATA_SIZE_MB` to change it. Keep the private key out of the repository; only its `.pub` file is embedded in the image.

## Flashing

The repository includes `flash-tool/imbupd_write_mac.c`, which communicates with the device's firmware update mode. A flash replaces the device's `/data` partition and may remove existing mods or user data stored there. Keep a verified backup and the original firmware available. The native tool only accepts the `data` partition and refuses to open USB for a write unless the exact confirmation phrase is supplied. Flashing is a separate, manual step; the image builder never invokes it.

Compile the tool with libusb as documented in its source. After the device is in firmware update mode, query it before any write. A write requires the explicit confirmation phrase:

```sh
./flash-tool/imbupd_write_mac query
./flash-tool/imbupd_write_mac write data /path/to/prime4-data.img --confirm "FLASH JP24 /data"
```

The write operation is destructive to `/data`.

## Connect after boot

Find the player's IP in its network settings or router, then run:

```sh
./ssh/connect.sh 192.168.1.50 ./denon_prime4_ed25519
```

The service accepts public-key authentication on port 2222 and disables password authentication. The first connection records the host key using OpenSSH's `accept-new` behavior.

## Device paths

- ALSA configuration: `/data/system/var-lib/alsa/conf.d/99-prime4-mod.conf`
- Hook library: `/data/prime4-mod/lib/libalsa-boot-hook.so`
- Activation script and authorized key: `/data/prime4-mod/`
- SSH listener: port `2222`

This is community software for the team's own device testing. It is not affiliated with Denon DJ or inMusic. Treat device-specific behavior as verified only where the project notes record a successful test on the same hardware and firmware.
