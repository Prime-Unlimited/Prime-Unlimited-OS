# Creating a new mod

Practical checklist for adding a new overlay or service mod.
All paths are relative to the repository root.
Work on the branch `feat/prime-unlimited-mod-loader` unless told otherwise.

## 1. Directory layout (copy from Prime Clock)

Create:

```
mods/<mod-name>/
  VERSION                 # single line, e.g. 0.1.0
  README.md
  scripts/
    package.py            # builds both device ZIPs
    validate_package.py
  src/
    entrypoint.sh
    ui/<ModName>.qml
    platform/
      jc11s.json
      jp24.json
  tests/
```

Reference: `mods/prime-clock/`

## 2. Workflow

Copy `.github/workflows/prime-clock.yml` to:

```
.github/workflows/<mod-name>.yml
```

Change the inputs:

```yaml
jobs:
  build-and-release:
    uses: ./.github/workflows/reusable-mod-release.yml
    with:
      mod-id: <mod-name>
      mod-dir: mods/<mod-name>
      release-title: <Human Name>
      tag-prefix: <mod-name>-v
```

The reusable workflow builds both JC11S and JP24 packages, checks they are identical except for `platform.json`, and publishes a GitHub Release when a matching tag is pushed.

## 3. Catalog entries

Edit both files:

- `modloader/catalog/jc11s.json`
- `modloader/catalog/jp24.json`

Add one entry per device. Example for an overlay mod:

```json
{
  "id": "example-jc11s",
  "name": "Example",
  "version": "0.1.0",
  "revision": 1,
  "description": "Short description.",
  "models": ["JC11S"],
  "firmware": ["5.3.0"],
  "downloadUrl": "https://github.com/Prime-Unlimited/Prime-Unlimited-OS/releases/download/example-v0.1.0/example-v0.1.0-jc11s.zip",
  "sha256": "<sha256 of the zip>",
  "size": 1234,
  "architecture": "armv7l",
  "abi": "gnueabihf",
  "entrypoint": "example/start.sh",
  "overlay": "example/ui/Example.qml",
  "approval": {
    "maintainer": "Prime Unlimited",
    "devices": ["JC11S"]
  }
}
```

For JP24 change:
- `id` → `example-jp24`
- `models` / `approval.devices` → `JP24`
- `architecture` → `aarch64`
- `abi` → `gnu`
- `downloadUrl` and `sha256` / `size` to the JP24 ZIP

Service mods (like Splash Manager) additionally need a `service` object:

```json
"service": {
  "binary": "example/bin/server-armhf",
  "sha256": "<sha256 of the ELF>",
  "capability": "splash-image"
}
```

## 4. Signing the catalog

Private key (local, never commit):

```
~/Download/prime-unlimited-catalog.key   # hex, 64 bytes
```

Public / trust key (already in the repo):

```
modloader/catalog/trust/prime-unlimited-2026-1.json
```

After editing the catalogs, re-sign them. The signature files must be updated:

```
modloader/catalog/jc11s.sig.json
modloader/catalog/jp24.sig.json
```

Use the existing signing tool or a small Go/Python script that does Ed25519 over the exact catalog bytes. The signature JSON looks like:

```json
{
  "algorithm": "ed25519",
  "keyId": "prime-unlimited-2026-1",
  "signature": "<base64>"
}
```

Commit the catalog + signature files together.

## 5. What to commit and where

| What | Where |
|------|--------|
| Mod source, tests, package scripts | `mods/<mod-name>/` |
| Caller workflow | `.github/workflows/<mod-name>.yml` |
| Catalog entries | `modloader/catalog/jc11s.json` and `jp24.json` |
| Catalog signatures | `modloader/catalog/*.sig.json` |
| (Optional) release notes / hardware notes | `mods/<mod-name>/README.md` or `HARDWARE_TEST_*.md` |

Do **not** commit:
- the private key (`prime-unlimited-catalog.key`)
- built ZIPs (they are produced by CI)
- device-specific binaries that the reusable workflow builds

## 6. Release

1. Push the branch.
2. Create and push a tag that matches the VERSION file:
   ```
   git tag <mod-name>-v0.1.0
   git push origin <mod-name>-v0.1.0
   ```
3. The workflow publishes the two ZIPs as a GitHub Release.
4. Copy the asset URLs, sizes and SHA-256 values into the catalog entries.
5. Re-sign the catalogs and commit the updated `.sig.json` files.
6. On the device: open Modloader → SYNC CATALOG.

## 7. Useful existing docs

- Overlay contract: `modloader/OVERLAY_MOD_DEVELOPMENT.md`
- Service mods: `modloader/SERVICE_MOD_DEVELOPMENT.md`
- Catalog sync rules: `modloader/CATALOG_SYNC_AND_UPDATES.md`
- Reference implementation: `mods/prime-clock/`
