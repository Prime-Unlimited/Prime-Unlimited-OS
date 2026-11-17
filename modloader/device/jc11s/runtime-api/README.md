# JC11S native runtime API

This is the small, dependency-free ARMv7 service used to make the Mod Loader
QML operational on firmware without Python or Node. It binds only to
`127.0.0.1:8098`, reads at most 1 MiB of catalog JSON, and initially exposes
read-only health, catalog, installed-state, library and transaction endpoints,
plus authenticated verified-package caching, isolated ZIP staging, disabled
installation and disabled uninstallation. Activation remains unavailable.

Build from Termux:

```sh
CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 \
  go build -trimpath -ldflags='-s -w' -o prime-unlimited-runtime-api .
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

The runtime supports the detached-signature profile of catalog schemas v1 and
v2. Validation is strict:
unknown fields, duplicate IDs or list values, non-HTTPS URLs, URL credentials,
fragments, missing or out-of-range package sizes, malformed lowercase SHA-256
values, trailing JSON, and catalogs larger than 1 MiB are rejected. The file
must be regular, owned by the service user, and not group- or world-writable.
Schema v2 adds a catalog ID plus externally approved architecture, ABI and
entrypoint metadata. The JC11S runtime currently accepts exact firmware lists
only; `firmwareConstraint`, embedded authenticity, update rules and dependency
resolution remain fail-closed until their runtime implementations exist.

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
return HTTP 401. Health output reports only `authorizationStatus: ready` and
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
are removed. A valid existing cache file is rehashed before reuse.

The catalog signature authenticates which package digest and size the
deployment approved. The cache then verifies downloaded bytes against that
authenticated record. Individual package signatures and archive-content
signatures remain a separate defense-in-depth step.

## Isolated ZIP staging

`POST /api/v1/stage` also accepts only `{"id":"catalog-mod-id"}` and never
downloads. It requires the compatible entry from the verified catalog and its
already cached package, then rechecks the cache file's ownership, mode, exact
size and SHA-256 before opening it.

Extraction is atomic into
`/data/prime-unlimited/staging/packages/<sha256>`. Temporary and final
directories are mode `0700`; every extracted file is forced to mode `0600`,
so executable archive permissions are not retained. The extractor rejects
absolute, parent-traversing, Windows-style, non-UTF-8, duplicate and overly
deep paths; encrypted entries; symlinks and other special file types; path
type conflicts; and archives exceeding entry, path, per-file or total-size
limits. CRC/read failures leave no published stage. A private marker records
the catalog identity and inventory, which is checked before an existing stage
is reused.

Staging does not execute package content and does not modify the installed-mod
registry, Engine files, boot hooks or activation state. Installation requires
the additional signed inspection and transaction stages below; rollback and
activation remain separate later milestones.

## Trusted package inspection

`POST /api/v1/inspect` is authenticated and accepts only a catalog mod ID. It
requires a detached-signature-verified v2 catalog record approved for the
device, exact `armv7l` architecture and `gnueabihf` ABI, a verified cache file,
and a completely revalidated stage. It then confirms that the entrypoint named
by the signed catalog exists as a service-owned mode `0600` regular file.

Approval, compatibility and the entrypoint are never read from package
contents. Revoked, incompatible and library-bearing records cannot become
eligible. Mod dependencies and conflicts are preserved as signed registry
metadata. A successful response remains non-executing.

## Atomic disabled installation

`POST /api/v1/install` accepts only a signed-v2 catalog ID and synchronously
installs an already inspected package as disabled. It revalidates the cache,
complete stage inventory and signed entrypoint before copying only staged
payload files into a private temporary directory under
`mods/installed/<id>/`. The stage marker is excluded. Files remain `0600` and
directories `0700`; an atomic rename publishes the version directory.

`registry/state.json` records the signed catalog and package identity,
architecture, ABI, entrypoint and `enabled: false`. `registry/pending.json`
durably records the previous and next states around publication. Startup
recovers a prepared journal by restoring the previous state and removing only
its validated target; a committed journal completes the next state only when
the target remains safe. Completed or recovered operations are exposed through
`GET /api/v1/transactions` and the installed state through
`GET /api/v1/installed`.

This milestone does not make files executable or invoke an entrypoint.
Library-bearing records remain ineligible; mod dependencies and conflicts are
installed as immutable activation metadata.

## Atomic disabled uninstallation

`POST /api/v1/uninstall` accepts only an installed mod ID and does not depend
on the current catalog, so an unlisted or revoked mod remains removable. Only
a disabled registry entry with a safe private payload directory is eligible.
The payload is atomically renamed to a transaction-specific quarantine path
before the registry commit and is deleted only after the committed journal is
durable.

Recovery of a prepared uninstall restores both the payload and previous
registry. Recovery of a committed uninstall completes removal of the payload
and quarantine and retains the next registry. Completed and recovered
uninstalls are recorded through the same transaction API as installations.
The operation never runs package content and cannot uninstall an enabled mod.

## Atomic enable/disable manifest

`POST /api/v1/enable` and `POST /api/v1/disable` change only an installed
mod's registry flag and `/data/prime-unlimited/activation/manifest.json`.
The manifest contains a sorted projection of enabled IDs, versions, roots and
signed entrypoints. It is regenerated from the registry at every API start.

Registry and manifest updates share the durable transaction journal. Recovery
before the committed-journal boundary restores both previous files; recovery
after that boundary completes both next files. Enable validates that the
installed entrypoint remains a mode `0600` regular file. It is deliberately
not made executable, loaded, sourced or invoked. No boot or Engine hook reads
the manifest in this milestone.

## Dependency and conflict preflight

`POST /api/v1/resolve` accepts a signed-v2 catalog mod ID and returns a
deterministic dependency-first order without changing device state. The
resolver supports exact and comparison ranges, comma intersections, caret,
tilde and wildcard constraints. It checks transitive mod and library
dependencies, device, firmware, architecture and ABI compatibility, cycles,
conflicting combined constraints, catalog conflicts and conflicts with
currently enabled installed mods.

The enable endpoint applies the resolved closure only across mods that are
already installed. Every installed dependency must satisfy its signed version
constraint and have a safe entrypoint. The complete disabled closure is
enabled in one registry/manifest journal transaction. Missing dependencies,
cycles or direct and reverse conflicts leave both files unchanged. Disable
refuses to break an enabled dependent and therefore requires dependent-first
order. Automatic dependency download and library installation remain blocked.
