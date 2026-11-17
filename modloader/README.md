# Prime Unlimited Mod Loader

Independent, /data-only mod management for Prime Unlimited OS.

## Status

The platform-independent v1 manager is implemented: persistent registry,
deterministic dependency solving, verified ZIP installation, shared libraries,
update/rollback, recovery journals, generated startup configuration, isolated
activation, authenticated loopback mutations and an extended QML client. A
JC11S-specific, `/data`-only QML resource preload hook and Control Center tile
have additionally been verified on Prime 4+ hardware running Engine OS 5.3.0.
The complete source, bootstrap, build, deployment and verification record is in
[`device/jc11s/ui-hook/`](device/jc11s/ui-hook/README.md). The backend service
is not yet activated by that UI integration.

## Runtime layout

All persistent files and executable components must live under `/data/prime-unlimited/`:

- `bin/`: local mod management service
- `ui/`: touch-first QML interface
- `hooks/`: runtime-only UI integration
- `mods/installed/`: installed packages
- `mods/staging/`: validated packages awaiting activation
- `catalog/`: downloaded and cached metadata
- `logs/`: diagnostics
- `registry/`: durable state, transaction records and generated startup data
- `transactions/`: private download and extraction scratch space

The service must bind to loopback only. Catalog access must be optional; offline operation must remain possible.

## Non-negotiable constraints

- Never write outside `/data`, including other mounted filesystems.
- Never modify protected firmware partitions, system service files, or the original application binaries.
- Never require an installer or firmware build to develop or test this module.
- Runtime hooks must be explicitly installed by a model- and firmware-specific
  adapter. The JC11S adapter is documented separately and fails closed on a
  different model.
- Fail closed for unknown device identifiers, firmware versions, invalid checksums, and malformed package paths.
- Package activation must support rollback and a disabled-mods startup mode.

## Trust and authorization

The native JC11S runtime requires a detached Ed25519 signature for catalog v1
and verifies it against a deployment-owned public key provisioned separately
from the catalog. The private signing key remains off-device. Package-declared
approval is ignored. Catalog v2 can carry signature metadata, but still must be
verified against a trust root shipped outside the catalog. SHA-256 binds
downloaded bytes to the signed catalog record; it does not independently
authenticate a publisher.

Mutation endpoints are disabled unless all of `--root`, `--auth-token-file`,
and `--trusted-catalog` are supplied. Tokens must be at least 32 characters and
are compared in constant time. The eventual trusted QML launcher must inject
the token without exposing it to other local processes. Read-only API use needs
no token.

## Catalog

See `catalog/schema-v1.json` for the initial metadata contract. The example file is a schema, not a live catalog.
Extended catalogs use `catalog/schema-v2.json` and support compatibility
constraints, approvals, dependencies, conflicts, integrity, activation,
updates and revocation.

## Startup

`modloader-start.sh` is the stable central entrypoint. It reads only the
atomically generated `registry/startup.json`, performs no network access, uses
an isolated `LD_LIBRARY_PATH` per mod, applies a bounded activation timeout and
continues after independent failures. `MODLOADER_SAFE_MODE=1` skips all mod
activation. The script is not installed into any boot hook by this repository.
