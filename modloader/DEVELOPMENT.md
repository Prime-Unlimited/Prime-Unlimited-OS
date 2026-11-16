# Local development

The current prototype is **read-only**. It does not install, activate, update or delete mods and does not write device files.

From the repository root:

```sh
python3 -m unittest discover -s modloader/backend -p 'test_*.py'
python3 modloader/backend/server.py --catalog modloader/catalog/demo.json --model JC11S --firmware DEMO-ONLY --port 8098
curl http://127.0.0.1:8098/api/v1/health
curl http://127.0.0.1:8098/api/v1/mods
```

The demo entry is intentionally hosted at an invalid domain and cannot be installed.

`ui/ModLoader.qml` is an unmounted QML component; it requires an appropriate QtQuick host for preview. It is **not** wired into the device's menu or startup process.

Before deployment, require a target-verified runtime, a validated /data-only startup path, an authenticated mutation API, strict package extraction safeguards, and an atomic rollback mechanism. No other partition may be written.

## Experimental persistent state service

`backend/registry.py` adds an offline filesystem service, separate from the
existing read-only HTTP API. Its root must be `/data/prime-unlimited/` on a
device; tests use a temporary directory. Nothing automatically deploys or
executes this module. The existing catalog v1 and HTTP responses are unchanged.

The registry lives at `registry/state.json`; its pending journal is
`registry/pending.json`. `registry/startup.json` is regenerated from enabled
entries. This is **data**, not a boot hook or an executable activation script.
The state format is independently versioned with `schemaVersion: 1`. Existing
catalog files require no migration. Legacy v1 entries have no trusted approval
or architecture/ABI metadata and therefore cannot be installed through the
new service without an explicitly reviewed trusted catalog record.

A trusted install record currently requires `approval: "approved"`,
`architecture`, `abi`, `entrypoint`, and existing v1 fields. It can
optionally specify `libraries` (id/version/architecture/abi/sha256),
`dependencies` (id/version) and `conflicts` (mod IDs). Approval must come
from a trusted catalog, never an archive manifest. Library archives are
provisioned independently and must pass SHA-256 verification. Library identity
includes the package digest, architecture and ABI. Reclamation requires a
caller-supplied snapshot of libraries still used by running processes; the
service refuses blind garbage collection.

**Not yet production-ready:** no authenticated mutation HTTP endpoints,
network download manager, signature-based catalog trust chain, dependency
solver, automatic rollback of every interrupted filesystem transition,
running-process inspector, verified boot activation, or device integration.
Do not invoke untrusted entrypoints or connect the service to a boot hook.
