# JC11S native runtime API

This is the small, dependency-free ARMv7 service used to make the Mod Loader
QML operational on firmware without Python or Node. It binds only to
`127.0.0.1:8098`, reads at most 1 MiB of catalog JSON, and initially exposes
read-only health, catalog, installed-state, library and transaction endpoints,
plus an authenticated verified-package cache. Installation, extraction and
activation remain unavailable.

Build from Termux:

```sh
CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 \
  go build -trimpath -ldflags='-s -w' -o prime-unlimited-runtime-api main.go
```

Install the binary as `/data/prime-unlimited/bin/prime-unlimited-runtime-api`,
install `start.sh` as `/data/prime-unlimited/bin/modloader-api-start.sh`, and
start it as a transient systemd service:

```sh
systemd-run --unit=prime-unlimited-modloader-api \
  --property=Restart=on-failure --property=RestartSec=2s \
  /data/prime-unlimited/bin/modloader-api-start.sh
```

The JC11S activation hook repeats that start on every boot. A missing catalog
is treated as a valid empty catalog, allowing the UI and health endpoint to
come up before catalog provisioning.

## Catalog behavior

The runtime currently supports signed catalog schema v1 only. Validation is strict:
unknown fields, duplicate IDs or list values, non-HTTPS URLs, URL credentials,
fragments, missing or out-of-range package sizes, malformed lowercase SHA-256
values, trailing JSON, and catalogs larger than 1 MiB are rejected. The file
must be regular, owned by the service user, and not group- or world-writable.
Schema v2 is rejected until its complete trust and package model is implemented.

Every present catalog requires a detached Ed25519 signature and a separately
provisioned deployment trust key. The service verifies the signature over the
exact catalog bytes before parsing or exposing entries. Algorithm and key ID
must match, and catalog, signature, and trust-key files must all satisfy the
service ownership and write-permission rules. Missing or invalid trust
material leaves health diagnostics available but marks the catalog invalid and
blocks package caching.

The API remains healthy when a catalog is invalid. `/api/v1/health` reports
`catalogStatus` and `/api/v1/mods` returns HTTP 503 with a validation error.
A missing catalog reports `missing` and returns an empty mod list. A valid
catalog reports `valid`, `catalogTrust: verified`, the key ID, public-key
fingerprint, and its SHA-256. Neither private keys nor signatures are returned.

For a non-installable device visibility check, provision
`modloader/catalog/jc11s-smoke-test.json` as
`/data/prime-unlimited/catalog/catalog.json`. Back up an existing catalog
first, use mode `0644`, restart only `prime-unlimited-modloader-api.service`,
provision its matching detached signature and public trust key, then verify
the health and mods endpoints. The entry uses the reserved
`example.invalid` domain and mutations remain unauthorized, so it cannot be
installed. Restore the backup and restart the API to roll back.

## Device-owned authorization token

At first start, the runtime creates 32 bytes from the operating system random
source and stores them as 64 lowercase hexadecimal characters in
`/data/prime-unlimited/auth/mutation.token`. The directory is mode `0700` and
the file is mode `0600`. Both must be real paths owned by the service user;
symlinks, weak permissions, malformed content, or ownership mismatches make
startup fail closed. An existing valid token is reused across service and
device restarts.

Mutation requests require exactly one `Authorization: Bearer <token>` header.
Comparison is constant-time. Missing, duplicated, or incorrect credentials
return HTTP 401. Correct credentials permit only the package-cache endpoint;
installation and all other mutations return HTTP 501 `mutation engine
unavailable`. Health output reports only `authorizationStatus: ready` and
never returns the token or a token-derived value.

## Verified package cache

`POST /api/v1/cache` accepts only `{"id":"catalog-mod-id"}`. URL, expected
size, SHA-256 and cache filename cannot be supplied by the caller; they are
derived from the validated, compatible catalog entry. The transport requires
HTTPS with TLS 1.2 or newer, ignores proxy environment variables, refuses all
redirects, and rejects DNS results in loopback, private, link-local,
unspecified or multicast ranges.

Downloads stream through a mode `0600` temporary file in the service-owned
mode `0700` package cache. Both Content-Length (when present) and the actual
byte count must equal the catalog size, which is capped at 128 MiB. SHA-256 is
verified before an atomic rename to `<sha256>.zip`; partial or invalid files
are removed. A valid existing cache file is rehashed before reuse. No archive
is opened, extracted, executed, installed, or activated in this milestone.
Package signatures and publisher authenticity remain a separate next step.

The catalog signature authenticates which package digest and size the
deployment approved. The cache then verifies downloaded bytes against that
authenticated record. Individual package signatures and archive-content
signatures remain a separate defense-in-depth step.
