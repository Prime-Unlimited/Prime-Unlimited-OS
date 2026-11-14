# Prime Unlimited OS architecture

## Goals

- Desktop application for Windows and macOS.
- Explicit device profiles for Prime 4+ (JC11S) and Prime 4 G2 (JP24).
- Separate transport layers for Fastboot and SSH/SFTP.
- Modular extension system for community mods.
- Never flash when device identity or compatibility is uncertain.

## Components

- `app/`: desktop interface.
- `core/`: device discovery, transport and installation orchestration.
- `devices/`: device-specific compatibility data.
- `mods/`: modular features and install recipes.
- `docs/`: setup, testing and recovery instructions.

## Safety boundaries

Device support is not implied by the existence of a profile. A profile must be verified against real hardware and firmware before enabling installation. Any destructive operation requires an explicit confirmation and a documented recovery strategy. No proprietary firmware images, private keys or credentials should be committed.
