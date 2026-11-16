# JC11S: EngineX bootstrap confirmed on hardware (2026-11-16)

## Evidence
- Fastboot product: `rk3288-az01-jc11s`
- `partition-size:data`: `0x000000008fd7be00` (2,413,280,768 bytes)
- Offline image builder: release SHA256 verified; payload readback verified; `e2fsck -fn` passed
- After flashing the raw `/data` image, Windows `Test-NetConnection 192.168.178.67 -Port 22` returned `TcpTestSucceeded : True`.
- SSH host-key handshake succeeded. **Public-key login is NOT verified**: the tested Windows client had no default private key (`type -1`).

## Source of truth
`bootstrap/jc11s/enginex/build-offline.sh` and `boot-activate.sh` reproduce the hardware-observed EngineX 5.3.0.2 approach. The older `bootstrap/jc11s` ALSA-only PoC did **not** produce reachable SSH and must not be represented as validated.

## Key contract
The desktop installer already generates a passphrase-protected Ed25519 pair in `core/keys.py`.
- Private key remains on the user's computer; **never** include it in an image or Git.
- Feed its matching public key into the EngineX image builder using `PUBKEY=/path/to/key.pub`.
- Verify `/sc5000-custom/authorized_keys` bytes and mode `0600` after injection.
- On successful flash, use the installer-held private key for the authenticated SSH test. Port 22 alone is not sufficient to declare login success.

## Remaining installer integration work (not yet implemented)
- Replace `NativeWindowsImageBuilder`'s minimal ALSA PoC path in `core/windows_installer.py` with this EngineX payload path; preserve the existing key-generation flow.
- Build and validate the payload from the desktop application on Windows **without silently requiring WSL**; determine packaging for Python/e2fsprogs/tar/EngineX release and download verification.
- Reuse the existing device-specific Fastboot geometry guard and guarded flash transport. Never flash without explicit confirmation.
- Run automated tests for key propagation, file permissions, checksum failure, and missing dependencies.
- Re-test **authenticated** SSH with the installer-generated private key on JC11S.
- JP24 is a separate profile and is not covered by this hardware observation.

The committed shell scripts are a tested-path reference and a manual Linux/WSL builder, **not yet a completed GUI installer feature**.
