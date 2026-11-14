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

## Windows JC11S transport

On Windows, JC11S uses a native SetupAPI/WinUSB backend rather than Google's `fastboot.exe`. Interface-class GUIDs are enumerated at runtime, then candidates are filtered by WinUSB service and the observed `15E4:D007` hardware ID. The transport opens each candidate with `CreateFileW` and the hardware-required `FILE_FLAG_OVERLAPPED`, validates the actual `FF/42/03` interface descriptor, discovers bulk endpoints, applies bounded pipe timeouts, and verifies Fastboot with `getvar:version`. Each transfer owns an event, `OVERLAPPED` state, and buffer through completion or cancellation; timeouts use `CancelIoEx` and bounded waits.

The protocol layer exposes diagnostic `getvar` queries and an explicitly confirmed reboot only. The Windows native module is loaded lazily and is never imported on macOS. Detection, identity authorization, firmware compatibility, and permission to flash remain separate gates; this backend contains no flash, erase, or download operation.
