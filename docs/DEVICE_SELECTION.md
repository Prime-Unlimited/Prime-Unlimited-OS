# Installer device selection and guided setup

Status: UX and safety specification, not an implemented installer.

## First screen

Show exactly two manually selectable options:

- **Denon DJ Prime 4+ (JC11S)** — Fastboot transport
- **Denon DJ Prime 4 G2 (JP24)** — imbupd transport

The user chooses the device **before** the installer begins USB detection. Do not automatically switch profiles if another device is detected.

## Guided flow: Prime 4+ (JC11S)

1. Show JC11S-specific instructions to enter Fastboot mode.
2. Detect Fastboot and collect read-only device information.
3. Verify identity, supported firmware and target partition against a tested profile. A Fastboot connection or matching partition size **alone is not sufficient**.
4. Set up mandatory SSH keys (generate or securely import a matching keypair).
5. Prepare and validate the JC11S SSH bootstrap image.
6. Confirm backup/recovery readiness and explicit consent to overwrite `/data`.
7. Write via Fastboot, reboot, and verify key-authenticated SSH on port 22.
8. Unlock SSH-based mod management only after successful verification.

## Guided flow: Prime 4 G2 (JP24)

1. Show JP24-specific instructions to enter firmware update mode.
2. Probe the proprietary imbupd USB interface (reported VID:PID `15e4:d014`).
3. Verify the actual device model, supported firmware and target partition. Matching USB VID:PID **alone is not sufficient**.
4. Set up mandatory SSH keys (generate or securely import a matching keypair).
5. Prepare and validate the JP24 ALSA-hook SSH bootstrap image.
6. Confirm backup/recovery readiness and explicit consent to overwrite `/data`.
7. Write via imbupd, reboot, and verify key-authenticated SSH on port 2222.
8. Unlock SSH-based mod management only after successful verification.

## Wrong-device behavior

- Selected **JC11S** but only an imbupd device is found: show **“The connected device does not match Prime 4+ (JC11S). Go back and select Prime 4 G2.”** No writes.
- Selected **JP24** but only a Fastboot device is found: show **“The connected device does not match Prime 4 G2 (JP24). Go back and select Prime 4+.”** No writes.
- No device, multiple devices, or ambiguous identity: stop safely with diagnostic guidance.
- Never guess, switch transport silently, or fall back to another image.
- The absence of a Fastboot/imbupd response is not by itself a definitive model identification; verify positive evidence before writing.

## Mandatory SSH

SSH is required for Prime Unlimited OS. There is no skip option. The desktop app uses SSH/SFTP for all later compatible mods, updates and file transfers. A successful USB write without verified SSH access is **not** a completed setup.

## User-facing language

All application labels, instructions, error messages, logs intended for users, and public project documentation are in English by default.
