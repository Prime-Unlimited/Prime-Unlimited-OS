# Mods

This directory will host mod manifests and integration adapters. Planned integrations include SSH setup, DOOM and USB Copy to SSD. Existing implementations and their licenses must be reviewed before importing code.

Each mod should declare supported devices, firmware versions, dependencies, install steps and rollback behavior. Mod packages must not run automatically simply because they appear in this directory.

`manifest.example.json` documents the current strict format. Its zero digest is a placeholder and cannot validate a real package. DOOM and USB Copy to SSD remain planned packages; no binaries or download claims are included.
