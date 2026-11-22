# Mods

This directory will host mod manifests and integration adapters. Planned integrations include SSH setup, DOOM and USB Copy to SSD. Existing implementations and their licenses must be reviewed before importing code.

Each mod should declare supported devices, firmware versions, dependencies, install steps and rollback behavior. Mod packages must not run automatically simply because they appear in this directory.

`manifest.example.json` documents the current strict format. Its zero digest is a placeholder and cannot validate a real package. DOOM and USB Copy to SSD remain planned packages; no binaries or download claims are included.

## Publishing

Catalog entries have two lifecycle states: `dev` and `published` (missing `status` is treated as
`published` for existing catalogs). A `dev` entry is only shown in DEV MODE and may be sent to
`POST /api/v1/publish`; a `published` entry is shown normally. A commit that adds or changes
exactly one `dev` entry on `main` automatically runs `.github/workflows/publish-mod.yml`. Its
device package must be committed as `mods/<id>/dist/<device>.zip`. The workflow creates a
device-specific release, records its URL,
digest and size, changes the state to `published`, signs the catalog and commits both catalog
files.

Configure the protected `mod-publishing` GitHub environment with `CATALOG_SIGNING_KEY` (a
base64-encoded 32-byte Ed25519 seed) and `CATALOG_KEY_ID`. GitHub credentials must never be
stored on a controller. The local runtime API must relay the authenticated publish request through
a trusted backend and expose its state through the transaction endpoint.

