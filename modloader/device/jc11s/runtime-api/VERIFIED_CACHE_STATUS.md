# Verified JC11S package-cache status

Verified on 2026-11-17 against a Denon DJ Prime 4+ (`JC11S`) running Engine
OS 5.3.0.

## Deployed artifacts

- Runtime binary: `/data/prime-unlimited/bin/prime-unlimited-runtime-api`
  - ELF 32-bit ARM EABI5, statically linked and stripped
  - size: 6,422,712 bytes
  - SHA-256: `495b05b9ed0785e28316425c9c3e45d79e2acca12a4b9b93f87a70e3ce0bb77a`
- Start script: `/data/prime-unlimited/bin/modloader-api-start.sh`
  - SHA-256: `4388d6404027bb954a6861ff3ed0f0f558244fc75afb55eb878c340cd45001b1`
- Restored smoke-test catalog:
  - SHA-256: `012d03b43099961a9a1bf4be1b0f2ad982a3d9b614d9bda3eea62188c5f561f4`
- Pre-milestone backups are under
  `/data/prime-unlimited/backups/cache-v1/`.

## Fail-closed device verification

- unauthenticated `POST /api/v1/cache`: HTTP 401
- authenticated request for an ID absent from the catalog: HTTP 404
- authenticated request for the deliberately unavailable smoke-test package:
  HTTP 502
- failed download left no partial or final cache file
- cache directory: `0700 root:root`

## Successful device transport verification

For one bounded transport test, a temporary root-owned catalog referenced the
project's own `README.md` at immutable Git commit
`7aff537d19d7e2d5d0e83fb1bd49fc074676315f` on
`raw.githubusercontent.com`. This fixture was used as opaque transport bytes;
it was never treated as an installable archive.

- expected and received size: 3,199 bytes
- expected and received SHA-256:
  `70ecf8cbf14c1eb18fa171d181487f276d00083195169de4d588c6565baaa17a`
- cache file: regular `0600 root:root` file named from that SHA-256
- first authenticated cache request: HTTP 200
- second request: HTTP 200 and the same inode, size, and modification time,
  proving verified cache reuse without another replacement

After verification, the transport fixture catalog and cached fixture were
removed. The non-downloadable `catalog-smoke-test` catalog was restored and
verified through `/api/v1/mods`. The cache directory was empty. The API and
Engine services remained active, the device-owned token remained unchanged,
and no Engine restart or device reboot was performed.

## Security boundary

The endpoint accepts only a catalog mod ID. It does not accept caller-provided
URLs, paths, sizes, hashes, filenames, or redirect targets. HTTPS/TLS, public
address filtering, exact byte count, SHA-256, restrictive cache permissions,
temporary-file cleanup, and atomic publication were tested. No ZIP parsing,
extraction, signature verification, execution, installation, activation, or
registry mutation is implemented by this milestone.

## Rollback

Stop the API service, restore the runtime, start script, and catalog from
`/data/prime-unlimited/backups/cache-v1/`, remove only the package-cache
directory introduced by this milestone, and restart the API service. The
Engine service does not need to restart.
