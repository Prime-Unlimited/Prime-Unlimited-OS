# JP24 Build Artifact Record

## Local image build (2026-11-14)

- Profile: 256 MiB ext4 image labeled `data`, built from the JP24 payload on the developer workstation.
- Local path: `bootstrap/jp24/build/prime4-g2-ssh.img` (generated output; not committed or uploaded).
- SHA-256: `669d18ef7df9930eb5f01144af43f84548d877ec09d8081fd3ab3b9b9a93294c`.
- Build inputs included the ALSA hook with constructor activation, `99-prime4-mod.conf`, `boot-activate.sh`, and the installer's Ed25519 public key. No private key is included in the image.

## Checks performed

- The build script completed its ext4 filesystem check and confirmed the `encrypt` filesystem feature.
- Read-only inspection of the generated image confirmed the expected `/prime4-mod` and `/system/var-lib/alsa/conf.d` payload paths.
- Extracted AArch64 ELF shared object had these dynamic symbols: `prime4_mod_alsa_hook`, `snd_config_hook_prime4_mod_alsa_hook`, `_prime4_mod_alsa_hook_dlsym_config_hook_001`, and `_snd_config_hook_prime4_mod_alsa_hook_dlsym_config_hook_001`.
- Both version-symbol strings contained `ALSA_0.9`; the ELF contained a non-empty `.init_array` section for constructor execution.
- `/prime4-mod/boot-activate.sh` had mode `0755` in the image.

## Limits

These checks establish the local image contents and ELF metadata only. They do not prove that the G2's modified ALSA loader accepts the symbol version contract, that the constructor runs under the firmware's runtime conditions, or that LEDs/SSH work on hardware. The image has not been transferred to a device. The recorded hash applies only to this local build; generated filesystem metadata may make later builds differ.

This recorded artifact is 256 MiB. The builder and writer now also accept an exact 64 MiB profile, matching a separate user-reported successful JP24 transfer. Neither reported image size establishes maximum `/data` capacity. The Vault summarizes `/data` as `56 MB` at 24% used, but raw `df` output and exact units are missing. A later note reports a successful 256 MiB transfer. The exact partition size must be independently confirmed. No raw `/data` backup or demonstrated restore path is documented. The Vault's signed-firmware recovery idea and UART/U-Boot UMS route remain unverified. Resolve capacity and recovery before enabling any write.


## Additional local build: 64 MiB profile

- Path: `bootstrap/jp24/build/prime4-g2-ssh-64.img` (generated output; not committed or uploaded).
- SHA-256: `c0fd695de9d7ab28554a8820adb1dc1d5616e18a95481677004b0e3fccddce75`.
- The builder completed the ext4 filesystem check and confirmed the `encrypt` feature. Both profiles use 4096-byte ext4 blocks, matching the Vault recipe.
- The same source payload and public SSH key were used as the 256 MiB profile.

The installer and native preflight now allow exactly 64 MiB and 256 MiB images. These are observed working image sizes, not a measurement of maximum `/data` capacity. Neither local artifact has been written to hardware.
