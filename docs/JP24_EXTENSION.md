# Extending the Image Builder and CI from JC11S to JP24 (Prime 4 G2)

**Status:** Documentation only. No JP24 production code changes, no deployment, no hardware actions.

This guide describes how the existing JC11S image builder, bootstrap payload, installer, and GitHub Actions workflows can later be extended to produce independent JP24 (Prime 4 G2) `/data` images and installer artifacts.

It reuses current functions and files wherever possible and marks unconfirmed hardware assumptions and missing components explicitly.

## Current baseline (main)

| Component | JC11S (Prime 4+) | JP24 (Prime 4 G2) |
|-----------|------------------|-------------------|
| Image builder | `core/jc11s_image.py` (Python + e2fsprogs) | `devices/jp24/ssh-bootstrap/build-data-image.sh` (bash + e2fsprogs) |
| Bootstrap payload | `bootstrap/jc11s/` + `core/jc11s_bootstrap.py` (Modloader) | ALSA hook in `devices/jp24/ssh-bootstrap/` |
| Flash path | `core/jc11s_flash.py` / Fastboot | `core/jp24_bootstrap.py` + `imbupd_write_mac.c` |
| Architecture | ARMv7 (ARM32) | AArch64 (ARM64) |
| USB transport | Fastboot | imbupd (proprietary) |
| SSH port | 22 | 2222 |
| CI hook build | `.github/workflows/desktop-installers.yml` → `jc11s-hook` job | Not yet present |

Relevant existing JP24 files on `main`:

- `devices/jp24/ssh-bootstrap/alsa-boot-hook.c`
- `devices/jp24/ssh-bootstrap/99-prime4-mod.conf`
- `devices/jp24/ssh-bootstrap/build-data-image.sh`
- `devices/jp24/ssh-bootstrap/imbupd_write_mac.c`
- `core/jp24_bootstrap.py`
- `core/transports.py` (`ImbupdTransport`)
- `devices/jp24/README.md`

## 1. JC11S-specific places that must be adapted or extended

### Image builder

- **`core/jc11s_image.py`**
  - Currently hard-codes JC11S paths (`bootstrap/jc11s`), ARM32 hook validation (`validate_hook` expects ELF32 / machine 40), Modloader payload via `modloader_payload()`, and a fixed UUID/features/label.
  - For JP24: either introduce a parallel `core/jp24_image.py` that mirrors the structure, or parameterize the existing builder with a device profile (preferred for reuse).
  - JP24 needs AArch64 hook validation (ELF64, machine 183) and different overlay layout (`prime4-mod/` instead of `prime-ssh/`).

- **`devices/jp24/ssh-bootstrap/build-data-image.sh`**
  - Already builds a minimal JP24 `/data` image with the ALSA hook and public key.
  - Must be extended (or replaced by a Python equivalent) to also embed the Modloader payload and second-stage splash once those are ready.

### Bootstrap / payload

- **`bootstrap/jc11s/`** and **`core/jc11s_bootstrap.py`**
  - Contain the Modloader files and pin logic.
  - JP24 equivalent does not yet exist. Create `bootstrap/jp24/` (or a shared payload directory with device-specific overlays) that supplies the same Modloader files adapted for ARM64 if required.

- **`core/jp24_bootstrap.py`**
  - Already implements the flash path for JP24.
  - Image construction is delegated to the shell script. When the image builder is unified, this call site changes.

### Installer / GUI

- **`app/window.py`** and related wizard code
  - Already has JP24 flash entry points (see `core/jp24_bootstrap.py` usage).
  - Device selection and diagnostics are present (`core/profiles.py`, `core/device_detection.py`).
  - Mod-manager unlock after SSH is still under development for both devices.

## 2. Integrating Modloader and Second-Stage Splash into the JP24 `/data` image

Current JP24 image (`build-data-image.sh`) only places:

- ALSA conf (`99-prime4-mod.conf`)
- `libalsa-boot-hook.so`
- `authorized_keys`

To add Modloader (and any second-stage splash):

1. Reuse the existing Modloader payload mechanism from `core/jc11s_bootstrap.py` (`payload()` / pin).
2. Adapt the file layout for the JP24 overlay paths used by the ALSA hook (`prime4-mod/` and the `system/var*/alsa/conf.d/` locations).
3. Place second-stage splash assets (if any) under a path the hook or a later systemd unit can reach. **Assumption (unconfirmed):** splash integration will follow the same overlay style already used for the ALSA conf.
4. Update `build-data-image.sh` (or the future Python builder) to copy these additional files and set correct modes/ownership.
5. Extend the image manifest (currently only produced by the JC11S Python builder) so both devices emit a comparable JSON manifest with `model`, hashes, and `modloader_bootstrap` pin.

**Missing today:** No JP24 Modloader payload directory or pin logic exists on `main`.

## 3. Correctly wiring existing JP24 hooks and device-specific files

Existing pieces to keep and call:

- `devices/jp24/ssh-bootstrap/alsa-boot-hook.c` — must be cross-compiled for AArch64 glibc (`aarch64-linux-gnu` or `zig -target aarch64-linux-gnu.2.17`).
- `99-prime4-mod.conf` — already placed at the two overlay locations the device expects.
- `imbupd_write_mac.c` — the flash tool (currently macOS-only in `core/jp24_bootstrap.py`).
- `core/jp24_bootstrap.py` — `build_data_image()`, `flash_bootstrap()`, confirmation phrase `"FLASH JP24 /data"`.

Wiring steps for a unified builder:

1. Compile the ALSA hook in CI (new job or matrix entry) and place the `.so` where the image builder expects it.
2. Keep the existing shell script as a reference implementation until the Python builder covers JP24.
3. Ensure `core/transports.py` (`ImbupdTransport`) and the wizard continue to call the same flash entry points.

**Unconfirmed hardware assumptions:**

- Exact `/data` partition size and geometry on production JP24 units (current script defaults to 256 MiB).
- Whether additional overlay paths beyond the ALSA conf are required for Modloader persistence.
- Long-term stability of the ALSA-hook injection method across firmware updates.

## 4. GitHub Actions changes for independent device builds

Current workflow: `.github/workflows/desktop-installers.yml`

- `jc11s-hook` job: cross-compiles the ARM32 hook and verifies the Modloader pin.
- `build` job (matrix Windows + macOS): downloads the hook, runs tests, builds the PyInstaller executable.

Required extensions (no implementation yet):

1. Add a parallel `jp24-hook` job (or a matrixed hook job) that:
   - Installs an AArch64 cross compiler (`gcc-aarch64-linux-gnu` or uses `zig`).
   - Compiles `devices/jp24/ssh-bootstrap/alsa-boot-hook.c` → `libalsa-boot-hook.so`.
   - Uploads the artifact.
2. Extend the `build` job to download both hooks (or make hook download conditional on a device matrix).
3. Optionally introduce a device matrix so Windows/macOS installers can be built with both payloads present, or produce separate artifacts (`Prime-Unlimited-OS-JP24-…`).
4. Keep path filters so changes under `devices/jp24/**` or `bootstrap/jp24/**` trigger the workflow.
5. Preserve the existing `usb-test-installers.yml` pattern if hardware-regression artifacts are needed.

Release strategy remains unchanged:

- Major installer builds continue to be produced by `desktop-installers.yml` (or a future release workflow) and published as GitHub Releases.
- Intermediate updates continue to go through the independent Modloader self-updater (current tags `modloader-v0.2.0-r*`).

## 5. Tests and hardware checks required before JP24 release

### Automated

- Unit tests covering JP24 image construction (file presence, modes, hashes) analogous to existing JC11S tests.
- Hook ELF validation (AArch64).
- Transport isolation tests (already partially present in `tests/test_usb_backend_isolation.py`).
- Manifest pin verification for the JP24 Modloader payload.

### Hardware (must be performed on real JP24 devices)

- [ ] Confirm Firmware Update Mode entry and imbupd communication.
- [ ] Flash a test `/data` image and verify automatic reboot.
- [ ] Confirm SSH on port 2222 with the injected key (already demonstrated in earlier work — re-verify after Modloader integration).
- [ ] Verify Modloader files are present and functional after boot.
- [ ] Confirm second-stage splash (if included) appears correctly.
- [ ] Test survival across a firmware update (rootfs replacement must not wipe `/data`).
- [ ] Validate recovery path (manual SSH removal of payload).
- [ ] Measure actual `/data` partition size and adjust image size if needed.

**Blocked until:**

- JP24 Modloader payload and pin exist.
- Image builder produces a JP24-compatible image with the additional files.
- CI can build and package both devices independently.

## Summary of reuse vs. new work

| Reuse | New / Adapt |
|-------|-------------|
| `core/jp24_bootstrap.py` flash path | JP24 Modloader payload directory |
| ALSA hook + conf files | AArch64 hook build in CI |
| Image size / e2fsprogs approach | Parameterized or parallel image builder |
| Desktop installer matrix | Hook download for both devices |
| Release + self-updater strategy | JP24-specific hardware validation checklist |

All changes described here are documentation only. Implementation remains future work.
