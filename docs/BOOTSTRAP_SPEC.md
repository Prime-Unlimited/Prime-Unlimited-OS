# Prime Unlimited OS — Bootstrap specification v0.1

Status: design only, not a tested installer. Source basis: EngineXPRIME4PLUS.md (7 Nov 2026), EngineXG2.md (Brownie, 14 Nov 2026), and historical PRIMEBOMB JC11S report.

## User flow

1. Connect device by USB in its supported update mode.
2. Detect transport and collect non-destructive device information.
3. Identify JC11S or JP24 **unambiguously**, checking firmware compatibility and partition layout; otherwise stop.
4. Generate a new Ed25519 keypair with a passphrase, or import a public key with proof that the user has the matching private key. **SSH setup cannot be skipped.**
5. Select the pinned, licensed, integrity-checked, device-specific bootstrap package.
6. Inject only the public key and necessary SSH startup configuration into the device-specific image.
7. Validate ext4 filesystem, permissions, content, image size and checksums.
8. Verify a usable backup/recovery procedure, explain destructive `/data` overwrite, require explicit consent.
9. Write over the correct transport, reboot, discover IP and verify authenticated SSH login.
10. Only then unlock the desktop mod manager. Later mod installation occurs over SSH/SFTP, not by repeating a full USB flash.

## Device profiles

### JC11S — Prime 4+

- Rockchip RK3288, ARM32.
- Fastboot bootloader; observed `data` ext4 partition size 2,413,280,768 bytes **on one device**.
- Tested base firmware: Engine OS 5.3.0; EngineX bundle 5.3.0.2.
- Observed SSH: root, key-only, TCP 22.
- Successful trial included separate systemd services generated from a boot hook. The later cleaned installation script was **not** independently re-tested as a fresh full install.
- Original V1–V4 PRIMEBOMB payloads **did not** establish SSH and must not be shipped as working bootstrap.

### JP24 — Prime 4 G2

- Rockchip RK3588, ARM64.
- Proprietary imbupd USB update protocol, **no Fastboot** in documented update mode.
- Reported USB VID:PID `15e4:d014`, vendor-specific interface. This VID:PID alone is **not sufficient** to establish model identity.
- Documented data image: 256 MiB ext4 with encryption feature; boot trigger via ALSA configuration and ARM64 shared-library hook.
- Reported SSH: root on TCP 2222 using an authorized public key.
- Rootfs is dm-verity protected; no writable `/etc` overlay.
- The report asserts persistence across firmware updates; the installer must **not** generalize this without version-specific tests.
- Do not copy the report's `StrictHostKeyChecking=no` practice into the app. Verify/pin host keys securely.

## Verified native Windows JC11S Fastboot (14 Nov 2026)

A real Prime 4+ on Windows was accessed directly through Microsoft's existing WinUSB driver. The WinUSB API successfully exchanged `getvar:version` / `OKAY0.4`; a subsequent explicit `reboot` command caused the controller to restart. Google's `fastboot.exe` 37.0.0 did not enumerate the device, despite working native WinUSB protocol access. **WSL2 is not required for these proven operations.** This does not validate flashing or backups. See [JC11S Windows WinUSB Fastboot bench notes](JC11S_WINDOWS_WINUSB_FASTBOOT.md). Keep destructive operations fail-closed pending recovery validation.

## Transport boundaries

`FastbootTransport` and `ImbupdTransport` must implement separate read-only probing and guarded writing. A transport being detected does not by itself prove a device profile. Never fall back from a failed JP24 probe to a JC11S image.

## Security and recovery

- No shared passwords, private SSH keys, firmware binaries or credentials in Git.
- Do not bundle unlicensed EngineX/Denon binaries; preserve upstream licenses and attribution.
- No unauthenticated Telnet or HTTP debug services.
- Never flash automatically on connection or detection.
- Backup and verified recovery path are release blockers.
- SSH verification must use authenticated login and a pinned/verified host key; a listening port is not proof of success.
- All mods require device compatibility, integrity checks, explicit install consent, logs and rollback behavior.

## First implementation milestones

- [x] Document two transport paths and mandatory SSH requirement.
- [x] Build safe read-only Fastboot output parser.
- [ ] Implement read-only imbupd probe with confirmed JP24 identity.
- [ ] Define validated firmware/device compatibility manifests.
- [ ] Add Windows/macOS desktop shell and key setup.
- [ ] Independently reproduce fresh bootstrap on each device.
- [ ] Test recovery and backups before enabling write operations.
- [ ] Add SSH-based mod management.
