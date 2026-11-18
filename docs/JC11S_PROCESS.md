# Prime Unlimited OS — Prime 4+ (JC11S) Process Documentation

**Updated:** November 18, 2026 · **Status:** Technical record, **not a production-ready installation guide**.

## 1. Purpose and scope

This document records the work performed on the Denon DJ Prime 4+ (JC11S): Fastboot access, the ext4 `/data` image, PRIMEBOMB V1–V4 experiments, observed results, a later successful SSH access, and prerequisites for a reproducible Prime Unlimited OS installer.

**Evidence boundary:** None of the four original PRIMEBOMB builds demonstrably enabled SSH. A subsequent working installation is treated as a separate development phase. The original V1–V4 report did not document the exact adaptation steps of that later successful configuration; a separate successful-installation report is available and should be retained as a primary reference.

## 2. Hardware and Fastboot

| Property | Recorded value |
|---|---|
| Device | Denon DJ Prime 4+ |
| Board family | JC11S |
| SoC | Rockchip RK3288 |
| Architecture | ARMv7 / ARM32 |
| Firmware during V1–V4 experiments | Engine OS 5.0.4 |
| Later reported firmware | Engine OS 5.3.0 |
| Test device Fastboot serial | A12506258617421 |
| Active slot | a |
| Fastboot mode | Bootloader; `is-userspace: no` |
| Target partition | `data`, ext4 |
| Reported partition size | 2,413,280,768 bytes (`0x8fd7be00`) |
| Fastboot version under WSL2 | 34.0.5-debian |
| Original experimental image | 2,306,867,200 bytes (2,200 MiB), label `az01-data` |

The experiment log reports entering Fastboot by holding **SHIFT + EJECT on the right deck while powering on**. This is a device-specific observation, not a guarantee for every firmware release.

The serial number identifies one test unit, **not** a model. The installer must verify genuine device identifiers and refuse ambiguous devices. A definitive `fastboot getvar product` response has not yet been recorded.

### Read-only Fastboot diagnostics

```sh
fastboot devices
fastboot getvar all
fastboot getvar current-slot
fastboot getvar is-userspace
fastboot getvar partition-type:data
fastboot getvar partition-size:data
```

Fastboot may write results to stderr. Redact serial numbers and other device-specific identifiers before publishing diagnostic logs. These commands do **not** back up any partition.

## 3. Build environment and original working image

The historical tests used **Ubuntu under WSL2**, Fastboot, ext4 utilities, `debugfs`, and `e2fsck`. The working file was `work/data-jc11s.img`, a 2,200-MiB ext4 image. It was **not a factory backup**.

`fastboot fetch data` was unsupported in the tested setup. No verified full original `/data` backup or recovery procedure existed. **Flashing `data` can erase settings, Wi-Fi configuration, library metadata, and other local content.** A separate music SSD does not protect the `/data` partition.

A validated backup and recovery procedure is a release blocker for an unattended or public installer.

## 4. Boot, mount, and overlay sequence

The earlier firmware investigation indicated the following order:

1. `az0x-data-mkfs.service` runs before the `/data` mount.
2. `data.mount` makes `/data` available.
3. `overlayfs-setup.service` depends on `/data`.
4. `/etc` is overlaid using upperdir `/data/system/etc/overlay`.
5. New systemd unit files in that overlay may become visible **after** systemd has already loaded some dependency information.

A `systemctl daemon-reload` following the overlay mount was not established for the stock firmware. This timing is a plausible explanation for failed activation, **not a proven root cause** of the later Wi-Fi regressions.

## 5. Original PRIMEBOMB payload

Static examination of the experimental image found:

| File or component | Intended purpose |
|---|---|
| `/ssh/freemymprime.so` | ARM32 preload library, mode 0755 |
| `/ssh/setup.sh` | Setup and launch script, mode 0755 |
| `/ssh/sshd_config` | SSH server configuration |
| `/ssh/authorized_keys` | Allowed SSH public keys |
| `/system/etc/overlay/ld.so.preload` | References `/data/ssh/freemymprime.so` |
| `freemymprime.service` | Service with boot-time symlink |

The preload constructor was intended to set a marker, log, fork, and run `/data/ssh/setup.sh`. **Actual execution on the device was never verified.** The marker was set before successful `exec`, so its existence alone would not have proved the setup script ran.

The setup attempted to generate a server host key, modify the root password hash, and start SSH. Historical debug fallbacks included unauthenticated BusyBox Telnet on TCP 2323 and an HTTP server exposing `/data` on TCP 8080. **These are unsafe for public distribution and must not be included in Prime Unlimited OS.** The historical fixed root test password must likewise never be shipped.

## 6. Experimental version history

### V1 — direct SSH startup

- Initial image with preload library, setup script, SSH configuration, and password login.
- Setup tried to execute `/usr/sbin/sshd -f /data/ssh/sshd_config` directly.
- SHA-256 measured before V2: `0eaebfea606f0b8875de10504002d5d0df38a7c0e43ad1a4ff7fcd8c2d942a2a`.
- Fastboot: sparse 1/1, 1,836 KB; write 71.420 seconds; total 72.734 seconds; `OKAY`.
- Unexplained AVB warning included the implausible value `140730698971296`.
- **Result:** Engine OS booted, Wi-Fi address `192.168.178.67`; TCP ports 22, 2323, and 8080 refused connections. No evidence of working SSH.

### V2 — systemd reload and service restart

- Built from a copy of V1; the preserved V1 copy was **not** a factory backup.
- Setup first called `systemctl daemon-reload`, then `systemctl restart sshd.service`; direct SSH startup remained as a fallback.
- Overlay drop-in `/etc/systemd/system/sshd.service.d/10-freemymprime.conf` set `ExecStart=/usr/sbin/sshd -D -f /data/ssh/sshd_config`.
- No additional boot `multi-user.target.wants` symlink for `sshd.service` was included.
- A failed `debugfs` replacement attempt temporarily produced a 75-byte `setup.sh`; the error was caught and corrected **before flashing** by rebuilding the image.
- Verified setup script: 1,860 bytes, mode 0755. Drop-in: 75 bytes, mode 0644.
- `e2fsck -fn`: clean. Image: 2,306,867,200 bytes.
- Fastboot: sparse 1/1, 1,844 KB; write 67.420 seconds; total 68.075 seconds; `OKAY`.
- AVB warning value: `140733051970544`.
- **Result:** Wi-Fi remained available, but TCP ports 22, 2323, and 8080 refused connections. No working SSH. A later rollback from V3 to V2 restored Wi-Fi operation.

### V3 — systemd boot symlink, unit copy, and host key

- Added a copy of the firmware's `sshd.service` to the overlay.
- Added `multi-user.target.wants/sshd.service -> /etc/systemd/system/sshd.service`.
- Generated an SSH **server host key** on WSL and installed it at `/ssh/ssh_host_ed25519_key`, mode 0600. This was **not** the client login key.
- Retained the V2 drop-in and `systemctl restart`.
- Unit: 356 bytes, mode 0644. Host key: 411 bytes, mode 0600.
- `e2fsck -fn`: clean.
- SHA-256: `96fe223eab4898858da3631180328b7b722f17eaedeacd846377b5580c4c0523`.
- Fastboot: sparse 1/1, 1,856 KB; write 67.171 seconds; total 68.075 seconds; `OKAY`.
- AVB warning value: `140731954370624`.
- **Result:** Wi-Fi toggle could no longer be enabled. SSH at `.67:22` returned “No route to host”; `.107:22` timed out. The server state could not be established without network access. Flashing V2 again restored Wi-Fi.

### V4 — start instead of restart

- Rebuilt from V2 with the same unit, symlink, drop-in, and host key as V3.
- Changed `systemctl restart sshd.service` to `systemctl start sshd.service` in the setup script.
- Historical settings still included `PasswordAuthentication yes`, `PermitRootLogin yes`, and `AuthorizedKeysFile /data/ssh/authorized_keys`.
- Several `debugfs` replacement attempts reported “Ext2 file already exists”; the image was subsequently rebuilt in a Python-driven `debugfs` session.
- Verified setup: 1,858 bytes, mode 0755; host key: 411 bytes, mode 0600.
- `e2fsck -fn`: clean; image: 2,306,867,200 bytes.
- Fastboot: sparse 1/1, 1,856 KB; write 66.985 seconds; total 68.202 seconds; `OKAY`.
- AVB warning value: `140721357274304`. No V4 SHA-256 was recorded.
- **Result:** Wi-Fi toggle remained unusable and SSH to `.107:22` timed out. Replacing `restart` with `start` did not fix the regression.

### Comparison

| Version | Wi-Fi after boot | SSH result | Assessment |
|---|---|---|---|
| V1 | Available | Port 22 refused | SSH not enabled |
| V2 | Available | Port 22 refused | Network baseline only |
| V3 | Wi-Fi toggle unusable | Inconclusive without network | Regression |
| V4 | Wi-Fi toggle unusable | Timed out | Regression persisted |

**Important:** Fastboot reporting `OKAY` confirms a reported write operation; it does **not** prove that a boot payload ran successfully.

## 7. Later successful SSH installation — separate phase

A later installation on JC11S produced the following confirmed observations:

- Root SSH login with a private Ed25519 client key succeeded.
- Historical example, using the device's temporary LAN address:

  ```sh
  ssh -i ~/.ssh/prime4plus root@192.168.178.67
  ```

- Shell prompt: `root@prime4plus:~#`.
- Web interface was observed on port 8080.
- Services and UI behavior were observed during the successful session.

**Important distinction:** A dedicated installation report documents the successful image creation, diagnostic repairs, and the actual flashed image in more detail. Its later cleaned end-to-end procedure was **not independently rerun** as a fresh installation. The historical V1–V4 attempts must not be presented as the working procedure.

Do not assume upstream binaries or Denon firmware can be redistributed without checking licenses and permissions.

## 8. Security and recovery decisions

1. Flashing `/data` can destroy local content. No unattended flash without validated backup and recovery.
2. SSH is **mandatory** for the Prime Unlimited OS bootstrap and cannot be skipped.
3. Use per-user Ed25519 keys; keep private keys on the host and inject only public keys.
4. Do not ship shared root passwords, unauthenticated Telnet, or public debug HTTP servers.
5. Never commit proprietary firmware, third-party binaries without permission, private keys, or credentials.
6. Verify firmware, device identity, partition layout, and image integrity before any write; refuse unknown devices.
7. Test normal Engine OS boot, Wi-Fi, music library, and authenticated SSH independently.
8. Subsequent mod deployment uses SSH/SFTP rather than reflashing `/data` whenever the mod permits it.

## 9. Windows/macOS installer requirements

**Proposed design; not yet implemented:**

1. Detect the connected USB update transport and verify exactly one supported device.
2. Select JC11S Fastboot or JP24 imbupd through validated device profiles; never infer model identity from a serial number or partition size alone.
3. Check supported firmware, target partition, and backup/recovery prerequisites.
4. Generate or securely import a user-specific Ed25519 public key with access to the matching private key.
5. Obtain a pinned, licensed, integrity-checked device-specific bootstrap package.
6. Inject the public key and tested SSH startup configuration into the image.
7. Validate filesystem, permissions, hashes, and expected paths.
8. Require explicit user confirmation before destructive writing.
9. Reboot and verify an authenticated SSH session using a trusted host key.
10. Unlock the mod manager **only after SSH succeeds**; all subsequent supported mods are transferred and installed over SSH.

## 10. Outstanding verification

- [ ] Record and redact the complete JC11S `fastboot getvar all` output.
- [ ] Preserve and validate the exact successful build and repair scripts.
- [ ] Confirm rights to use or redistribute required SSH/bootstrap components.
- [ ] Verify a complete `/data` backup and recovery path.
- [ ] Reproduce the cleaned bootstrap from scratch on JC11S.
- [ ] Establish compatibility beyond the observed Engine OS 5.3.0 combination.
- [ ] Integrate JP24 findings as a **separate device profile**, not as JC11S Fastboot steps.
- [ ] Test the desktop installer on real Windows and macOS hosts.
- [ ] Confirm successful SSH reconnect after an additional normal device reboot.

## 11. Source record

- Historical primary source: **“PRIMEBOMB V1–V4 – technische Dokumentation”**, dated November 5, 2026.
- Later primary source: successful SSH installation report, dated November 7, 2026.
- Companion project document: `docs/BOOTSTRAP_SPEC.md`.

This file is an engineering record. It is **not** a verified recovery guide or a release-ready installer procedure.
