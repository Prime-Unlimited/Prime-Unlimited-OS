# Verified JC11S catalog status

Verified on 2026-11-17 against a Denon DJ Prime 4+ (`JC11S`) running Engine
OS 5.3.0.

## Deployed artifacts

- Runtime binary: `/data/prime-unlimited/bin/prime-unlimited-runtime-api`
  - ELF 32-bit ARM EABI5, statically linked and stripped
  - size: 5,898,424 bytes
  - SHA-256: `c6a57dad342a44f7b929bd60ba8d7ff498c65bc3e4c188f672ebf7ae7fe41e92`
- Catalog: `/data/prime-unlimited/catalog/catalog.json`
  - source: `modloader/catalog/jc11s-smoke-test.json`
  - mode: `0644`
  - SHA-256: `65ef5ba66568c12f24d633b7da503b436181989355244bb77d3203a4328f8693`
- Previous runtime backup:
  `/data/prime-unlimited/backups/catalog-v1/prime-unlimited-runtime-api.pre-catalog-v1`

## Device verification

- `prime-unlimited-modloader-api.service`: active
- `engine.service`: active and not restarted by this deployment
- UI hook remained mapped in the Engine process
- `/proc/<api-pid>/net/tcp` showed `0100007F:1FA2`, confirming
  `127.0.0.1:8098`; no `00000000:1FA2` wildcard listener was present
- `GET /api/v1/health`: HTTP 200 with `catalogStatus: valid`
- `GET /api/v1/mods`: HTTP 200 with catalog SHA-256, model `JC11S`, firmware
  `5.3.0`, and `catalog-smoke-test` marked `compatible: true`
- `POST /api/v1/install`: HTTP 401 `mutation authorization required`

The smoke-test entry is deliberately non-installable. Its package URL uses
the reserved `example.invalid` domain, its digest is a placeholder, and every
mutation endpoint remains closed.

## Rollback

Stop the API service, restore the backed-up runtime binary, remove the
smoke-test catalog (or restore a backed-up prior catalog), and start the API
service again. The previous runtime treats a missing catalog as an empty
catalog. No Engine restart or reboot is required.

Physical display confirmation is still pending. API verification proves that
the exact data consumed by `ModLoader.qml` contains one compatible catalog
entry; it does not claim that a person observed the rendered tile.
