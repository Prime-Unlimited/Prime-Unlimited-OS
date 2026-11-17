# JC11S native runtime API

This is the small, dependency-free ARMv7 service used to make the Mod Loader
QML operational on firmware without Python or Node. It binds only to
`127.0.0.1:8098`, reads at most 1 MiB of catalog JSON, and initially exposes
read-only health, catalog, installed-state, library and transaction endpoints.
All mutation requests fail closed with HTTP 401 until the package transaction
engine and device-owned authorization token are wired in.

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

The runtime currently supports catalog schema v1 only. Validation is strict:
unknown fields, duplicate IDs or list values, non-HTTPS URLs, URL credentials,
fragments, malformed lowercase SHA-256 values, trailing JSON, and catalogs
larger than 1 MiB are rejected. Schema v2 is rejected until its complete trust
and package model is implemented.

The API remains healthy when a catalog is invalid. `/api/v1/health` reports
`catalogStatus` and `/api/v1/mods` returns HTTP 503 with a validation error.
A missing catalog reports `missing` and returns an empty mod list. A valid
catalog reports `valid` and includes its SHA-256 in the mods response.

For a non-installable device visibility check, provision
`modloader/catalog/jc11s-smoke-test.json` as
`/data/prime-unlimited/catalog/catalog.json`. Back up an existing catalog
first, use mode `0644`, restart only `prime-unlimited-modloader-api.service`,
and verify the health and mods endpoints. The entry uses the reserved
`example.invalid` domain and mutations remain unauthorized, so it cannot be
installed. Restore the backup and restart the API to roll back.
