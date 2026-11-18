# Prime Unlimited Mod Loader

Catalog synchronization, revision comparison, transactional updates and
rollback are documented in [CATALOG_SYNC_AND_UPDATES.md](CATALOG_SYNC_AND_UPDATES.md).
The separately serviced Modloader self-update path is documented in
[updater/README.md](updater/README.md).

Overlay authors must follow [OVERLAY_MOD_DEVELOPMENT.md](OVERLAY_MOD_DEVELOPMENT.md).
It records the verified generic host contract, packaging/lifecycle sequence,
JC11S integration details, and mandatory Do's/Don'ts for future mods.

Independent, /data-only mod management for Prime Unlimited OS.

## Status

The platform-independent v1 manager is implemented: persistent registry,
deterministic dependency solving, verified ZIP installation, shared libraries,
update/rollback, recovery journals, generated startup configuration, isolated
activation, authenticated loopback mutations and an extended QML client. A
JC11S-specific, `/data`-only QML resource preload hook, Control Center tile and
native lifecycle service have additionally been verified on Prime 4+ hardware
running Engine OS 5.3.0.
The complete source, bootstrap, build, deployment and verification record is in
[`device/jc11s/ui-hook/`](device/jc11s/ui-hook/README.md). The backend service
is connected to the touch UI through its loopback API.

The second Engine splash (`Skins/Embedded/img/SplashScreen.png`) can be replaced
from `/data/prime-unlimited/ui/splash/SplashScreen.png` by the same preload hook.
The firmware `splash` partition is not part of that path. See
[docs/SECOND_STAGE_SPLASH.md](../docs/SECOND_STAGE_SPLASH.md).

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

The native device service owns a random 32-byte token in a root-only `0600`
file and compares authorization headers in constant time. At service start it
replaces exactly one marker in the public QML template and atomically publishes
a root-only `0600` runtime copy for the root-owned Engine process. The token is
never returned by HTTP, rendered, logged, committed or stored in the catalog.
Read-only API use needs no token.

## JC11S touch UI

The device UI exposes catalog, installed and activity views. Installation is a
strict ordered client workflow of cache, stage, signed inspection and disabled
installation; a failed step stops the chain. Enable, disable and uninstall use
the atomic registry transactions. Manual dependency-ordered launch, quarantine
recovery and global safe-mode control are also wired to authenticated endpoints.
The activity view shows persisted transaction operation, stage and status.

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
