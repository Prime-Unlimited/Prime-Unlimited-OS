# Mods

This directory will host mod manifests and integration adapters. Planned integrations include SSH setup, DOOM and USB Copy to SSD. Existing implementations and their licenses must be reviewed before importing code.

Each mod should declare supported devices, firmware versions, dependencies, install steps and rollback behavior. Mod packages must not run automatically simply because they appear in this directory.

`manifest.example.json` documents the current strict format. Its zero digest is a placeholder and cannot validate a real package. DOOM and USB Copy to SSD remain planned packages; no binaries or download claims are included.

## One source, two device packages

Mods keep one shared implementation. Device differences belong only in target
profiles and packaging metadata; do not fork QML, JavaScript or other mod
source merely because JC11S is 32-bit and JP24 is 64-bit.

The reusable `.github/workflows/reusable-mod-release.yml` workflow builds the
same mod directory for both targets, checks deterministic output, verifies that
all packaged files except `platform.json` are byte-identical, and publishes
both ZIPs from one version tag. A mod directory using it provides:

- `VERSION`
- `tests/`
- `scripts/package.py --target jc11s|jp24 --output <directory>`
- `scripts/validate_package.py <zip> --target jc11s|jp24`

Its small caller workflow supplies `mod-id`, `mod-dir`, `release-title`, and
`tag-prefix`. Prime Clock is the reference caller. The reusable workflow also
builds the 32-bit JC11S and 64-bit JP24 UI hooks, keeps artifact metadata unique
per target, and refuses a release tag that does not match the shared `VERSION`.
