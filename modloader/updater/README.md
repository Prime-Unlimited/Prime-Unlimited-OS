# Independent Modloader self-updater

The updater is a separate static service on `127.0.0.1:8099`. It remains
running while the Modloader runtime on port 8098 is replaced, verifies the
new runtime's health, and restores its private backup on failure. Its package
allowlist contains only the two binaries, their start scripts, the two QML
templates and `VERSION.json`. Mods, registry, settings, catalogs, trust keys,
Engine hooks and hardware bootstraps cannot occur in an accepted bundle.

Build both device binaries with `./build.sh`. Reproducible release bundles are
created by `package.py` and `.github/workflows/modloader-self-update.yml`.
The published bundle is not trusted until its immutable GitHub asset ID, size
and SHA-256 are written into `modloader/update/manifest.json` and those exact
manifest bytes are signed by the existing offline Ed25519 catalog key.

Endpoints:

- `GET /api/v1/status` – installed/available version and revision.
- `POST /api/v1/check` – authenticated manual manifest refresh.
- `POST /api/v1/update` – authenticated verified update.
- `POST /api/v1/rollback` – authenticated restore of the retained version.

The service checks automatically at startup. Mutation endpoints use the same
device-owned bearer token as the main runtime; it is never stored in GitHub.

Current signed channel: Modloader `0.2.0` revision `9`. Revision 8 extended
the signed update and rollback boundary to the QRC-backed Control Center tile.
The manifest pins the
immutable GitHub assets for JC11S and JP24 by asset ID, byte size and SHA-256.
Earlier r1/r2 build-validation releases are not referenced by a signed manifest
and therefore cannot be installed by the updater.

Revision 8 is the compatibility bridge for QRC-backed Control Center updates.
The revision 7 updater accepts exactly seven archive entries, so r8 embeds the
canonical, 1156-byte-slot-checked `ControlCenterTab.qml` in the signed updater
binary. On first r8 start it backs up the installed patch under
`updater/backup/ui/patches/`, stages the replacement atomically, and removes the
regenerable Engine QML cache. Rollback restores the previous patch and clears
the cache again. Revision 9 and later include the now-allowlisted QML path as a
normal eighth bundle entry. The live QRC resource adopts a staged tile change
on the next controlled Engine/device restart; the updater never restarts Engine
during a set.

Revision 9 safely migrates catalog and trust directories created by early
installers from `0755` to `0700`, and their regular root-owned metadata files
from `0644` to `0600`. Symlinks, unexpected file types and wrong ownership are
rejected. The migration runs in the shared runtime before its automatic signed
catalog sync, so both JC11S and JP24 recover during the verified self-update
without changing installed mods, registry state, settings or trust material.
