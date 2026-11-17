# Verified JC11S package-staging status

Verified on 2026-11-17 against a Denon DJ Prime 4+ (`JC11S`) running Engine
OS 5.3.0.

## Deployed artifacts

- Runtime binary: `/data/prime-unlimited/bin/prime-unlimited-runtime-api`
  - ELF 32-bit ARM EABI5, statically linked and stripped
  - size: 6,553,784 bytes
  - SHA-256: `74167d9ef25498942205b3d4ae6dea837419ee2baafe0d2070b25a92a93d96bb`
- Start script: `/data/prime-unlimited/bin/modloader-api-start.sh`
  - SHA-256: `7c25d2f4fb67fe47df09bae504761ad3500eba3a6d5a1bf1f9a706d5a7e11d83`
- Pre-milestone runtime and start-script backups are under
  `/data/prime-unlimited/backups/staging-v1/`.

## Real-device staging verification

A temporary signed catalog referenced the project's immutable GitHub archive
at commit `44e96b9d45a2f5c34d1cee4649c7d96b0db7dd65`.

- ZIP size: 385,918 bytes
- ZIP SHA-256:
  `e753d28ab4e522048ab0be0584b31ca9c3832a447c01b69a4193e34343033d14`
- staged regular files: 131, plus the private inventory marker
- extracted payload bytes: 933,102
- staging response: HTTP 200 with `staged: true`
- a second request returned the same inventory without replacing the stage
- all staged directories were `0700 root:root`
- all staged files were `0600 root:root`; none had executable bits
- no staged symlink was present

The endpoint returned HTTP 401 without the device-owned bearer token and HTTP
422 when the package was not cached. Changing the cache file to mode `0644`
made staging fail with HTTP 422. Restoring the cache and changing one byte in
an extracted file without changing its size also failed with HTTP 422, as did
changing the published stage directory to mode `0755`. These checks confirm
that both cached input and every staged file digest are revalidated before
reuse.

Local adversarial tests additionally cover traversal and absolute paths,
Windows separators, symlinks, duplicate paths, file/directory conflicts,
reserved marker collision, excessive depth, per-file and total extracted-size
limits, entry-count limits, corrupt ZIPs, missing cache files, catalog binding,
caller-supplied path rejection, private output modes and atomic reuse.

## Restored device state

After the bounded test, only the exact fixture cache file and content-addressed
stage were removed. The signed non-downloadable `catalog-smoke-test` catalog
and its detached signature were restored. The cache and staging package
directories were empty. The API and Engine services remained active; Engine
was not restarted and the device was not rebooted.

No staged content was executed, installed or activated, and no Engine file,
boot hook or installed-mod registry was changed by this milestone.

## Rollback

Stop the API service, restore the runtime binary and start script from
`/data/prime-unlimited/backups/staging-v1/`, remove only
`/data/prime-unlimited/staging/packages`, and restart the API service. The
Engine service does not need to restart.
