# Device catalogs, updates and rollback

This document is the implementation contract for Mod Loader catalog delivery
on JC11S and JP24. Both devices run the same runtime source and QML UI. Device
adapters only provide the exact model, firmware, architecture, ABI and catalog
URL; adding a mod does not require changing the Mod Loader.

## Canonical catalogs

- JC11S: `modloader/catalog/jc11s.json` plus `jc11s.sig.json`
- JP24: `modloader/catalog/jp24.json` plus `jp24.sig.json`
- Trust root: `modloader/catalog/trust/prime-unlimited-2026-1.json`

Each catalog entry has one device-specific mod ID and immutable package URL,
size and SHA-256. `version` describes the public release. Optional integer
`revision` starts at 1 and is increased when packaging or integration metadata
changes without a public version bump. A changed SHA-256 with unchanged version
and revision is also surfaced, but publishers should always increase revision.

## Startup and manual sync

The runtime downloads the device catalog and detached signature from
`raw.githubusercontent.com` at startup. The exact downloaded bytes must pass:

1. HTTPS host allow-list and response-size limits;
2. strict JSON/schema validation;
3. Ed25519 verification against the device-owned trust root;
4. model, firmware, architecture, ABI and explicit approval checks before any
   package can enter Cache -> Stage -> Inspect -> Install/Update.

The UI's **SYNC CATALOG** button calls the authenticated
`POST /api/v1/catalog/sync` endpoint. Network failure never replaces the active
catalog. Before a verified pair is replaced, it is retained as `.last-good`.
After interrupted writes or without Internet, startup uses that complete signed
pair. An unsigned, malformed or wrong-device catalog is never an offline
fallback.

## Developer mode

Developer mode is a display filter only. Normally AVAILABLE shows compatible
entries. With **DEV MODE ON**, it also shows entries targeting the current
model that fail firmware, ABI, architecture, approval or revocation checks.
Install and update remain disabled by the runtime unless compatibility and all
signature checks pass. Developer mode never bypasses security.

## Update decision

`GET /api/v1/mods` compares catalog and registry values and returns installed
version, revision and SHA-256 plus `updateAvailable` and `updateReason`:

1. higher semantic version;
2. same version with higher revision;
3. same version/revision with different SHA-256.

Downgrades are not offered as updates. They require an explicit rollback to the
previous verified payload.

## Transactional update and rollback

The UI disables an enabled mod, then performs Cache -> Stage -> Inspect ->
Update. Only after package hash, ZIP safety and manifest inspection succeed is
the installed tree exchanged. The old tree is moved into the private
`/data/prime-unlimited/mods/rollback/<id>/` area. If publishing the new tree or
registry fails, the old payload is restored immediately. A successful update
retains one rollback record; **ROLLBACK** restores it while the mod is disabled.
The UI restores the former enabled state only after a successful update.

Safe Mode, quarantine, dependency checks, mutation authorization and `/data`
write boundaries remain unchanged.

## Publisher checklist

1. Build both packages through the reusable release workflow.
2. Use separate IDs and immutable asset URLs for JC11S and JP24.
3. Set the public version; increase revision for every changed artifact.
4. Record exact size and SHA-256.
5. Update only the applicable canonical device catalog.
6. Sign the final catalog bytes offline; never commit a private key.
7. Verify both catalog/signature pairs in CI.
8. Test offline startup, manual sync, update and rollback before hardware
   promotion.

Do not use Developer mode to weaken compatibility, publish mutable package
URLs, reuse a revision for changed bytes, replace the trust root from a catalog,
or modify Engine/ALSA hooks for an ordinary mod release.
