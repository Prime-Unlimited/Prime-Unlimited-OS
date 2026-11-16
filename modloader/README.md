# Prime Unlimited Mod Loader

Independent, /data-only mod management for Prime Unlimited OS.

## Status

The platform-independent v1 manager is implemented: persistent registry,
deterministic dependency solving, verified ZIP installation, shared libraries,
update/rollback, recovery journals, generated startup configuration, isolated
activation, authenticated loopback mutations and an extended QML client. No
device hook, firmware modification, service activation or hardware write is
included.

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
- Do not activate runtime hooks automatically in this scaffold.
- Fail closed for unknown device identifiers, firmware versions, invalid checksums, and malformed package paths.
- Package activation must support rollback and a disabled-mods startup mode.

## Trust and authorization

Catalog v1 remains readable. Installations require a trusted, deployment-owned
catalog approval decision; package-declared approval is ignored. Catalog v2 can
carry Ed25519 signature metadata, but a device adapter must verify it against a
key shipped outside the catalog before starting the service with
`--trusted-catalog`. SHA-256 authenticates downloaded bytes against the trusted
record; it does not authenticate a publisher.

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
