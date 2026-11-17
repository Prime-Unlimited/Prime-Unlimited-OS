# Verified JC11S signed catalog-v2 inspection status

Verified on 2026-11-17 against a Denon DJ Prime 4+ (`JC11S`) running Engine
OS 5.3.0.

## Deployed artifacts

- Runtime binary: 6,619,320-byte static ARM EABI5 executable
  - SHA-256: `da9c71daf928384796c1d57393037d66d015bb66fda2c3c26994b8ab59513578`
- Start script SHA-256:
  `9baedb634b0f734d8367c1e2eae6eb1bdc907804e973088ddde6e89ea6173ba7`
- Pre-milestone backups: `/data/prime-unlimited/backups/catalog-v2-v1/`

## Trust and eligibility boundary

The runtime accepts catalog v2 only through the existing detached Ed25519
signature chain. Approval, architecture, ABI and entrypoint metadata come from
that signed catalog and are never accepted from a package or API caller.
Embedded authenticity is rejected to avoid a second ambiguous trust path.

JC11S eligibility requires exact model `JC11S`, firmware `5.3.0`, architecture
`armv7l`, ABI `gnueabihf`, approval for `JC11S`, and a non-revoked record.
Dependency-, library- or conflict-bearing records remain ineligible until the
native resolver and installed-state transaction model exist.

## Real-device verification

A temporary signed v2 catalog used the immutable project ZIP at Git commit
`44e96b9d45a2f5c34d1cee4649c7d96b0db7dd65`:

- catalog ID: `prime-unlimited-device-test`
- package size: 385,918 bytes
- package SHA-256:
  `e753d28ab4e522048ab0be0584b31ca9c3832a447c01b69a4193e34343033d14`
- declared entrypoint: the archive's `README.md`
- declared architecture/ABI: `armv7l` / `gnueabihf`

The exact catalog and detached signature used for the test are retained as
`modloader/catalog/jc11s-inspection-test.json` and its adjacent
`.sig.json`; the private signing key remains outside the repository.

The signed catalog appeared as schema v2 and compatible through the read-only
API. Inspection before cache and staging failed with HTTP 422. After verified
cache and atomic staging, authenticated `POST /api/v1/inspect` returned HTTP
200 and `eligible: true`, echoing only the signed identity and eligibility
fields. The declared entrypoint remained mode `0600` and was not executed.

A schema-v1 record returned HTTP 409 because it has no trusted install
metadata. Changing one byte in the staged entrypoint without changing its size
made inspection fail with HTTP 422. Local tests additionally reject missing
v2 root fields, embedded authenticity, missing approval, unsafe entrypoints,
wrong architecture/ABI, revocation, duplicate conflicts and explicit v2 fields
smuggled into schema v1 at zero values.

## Restored device state

The temporary v2 catalog, package cache and content-addressed stage were
removed. The signed v1 `catalog-smoke-test` catalog was restored. API and
Engine remained active, loopback-only binding and the UI hook remained intact,
and no Engine restart or device reboot occurred.

No package content was executed, installed or activated. No installed-state
registry, boot hook or Engine file was modified.

## Rollback

Stop the API service, restore the runtime binary and start script from
`/data/prime-unlimited/backups/catalog-v2-v1/`, and restart only the API
service. The Engine service does not need to restart.
