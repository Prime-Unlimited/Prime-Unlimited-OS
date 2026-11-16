# Local development

All tests are filesystem-only and use synthetic packages. They never modify a
device or install a boot hook.

From the repository root:

```sh
python3 -m unittest discover -s modloader/backend -p 'test_*.py'
python3 -m unittest discover -s modloader/ui -p 'test_*.py'
python3 modloader/backend/server.py --catalog modloader/catalog/demo.json --model JC11S --firmware DEMO-ONLY --port 8098
curl http://127.0.0.1:8098/api/v1/health
curl http://127.0.0.1:8098/api/v1/mods
```

The demo entry is intentionally hosted at an invalid domain and cannot be
installed. Mutation mode additionally requires a private root, a verified
catalog decision and a local bearer-token file; without all three the server
intentionally remains read-only.

`ui/ModLoader.qml` is an unmounted QML component; it requires an appropriate QtQuick host for preview. It is **not** wired into the device's menu or startup process.

Before deployment, require a target-verified runtime, a validated /data-only startup path, an authenticated mutation API, strict package extraction safeguards, and an atomic rollback mechanism. No other partition may be written.

## Persistent state service

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

The resolver supports exact/comparison/caret/tilde/wildcard constraints,
transitive mod and library dependencies, deterministic order, conflicts,
cycles, and model/firmware/architecture/ABI checks. Downloads use only URLs
from trusted records. ZIP extraction rejects traversal, absolute paths, links,
hard links, duplicate/conflicting paths and size overflow.

Remaining device work is deliberately outside this phase: provision and
protect the bearer token, verify catalog signatures with a device-owned
Ed25519 trust root, implement a reliable running-process library usage adapter,
install the already-existing boot hook's call to `modloader-start.sh`, and
validate the runtime adapter plus activation timeout on target hardware.
