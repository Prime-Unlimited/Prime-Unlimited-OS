# Prime Unlimited Mod Loader

Independent, /data-only mod management for Prime Unlimited OS.

## Status

Early development scaffold. No device integration, service activation, installer changes, or firmware modifications are included.

## Runtime layout

All persistent files and executable components must live under `/data/prime-unlimited/`:

- `bin/`: local mod management service
- `ui/`: touch-first QML interface
- `hooks/`: runtime-only UI integration
- `mods/installed/`: installed packages
- `mods/staging/`: validated packages awaiting activation
- `catalog/`: downloaded and cached metadata
- `logs/`: diagnostics

The service must bind to loopback only. Catalog access must be optional; offline operation must remain possible.

## Non-negotiable constraints

- Never write outside `/data`, including other mounted filesystems.
- Never modify protected firmware partitions, system service files, or the original application binaries.
- Never require an installer or firmware build to develop or test this module.
- Do not activate runtime hooks automatically in this scaffold.
- Fail closed for unknown device identifiers, firmware versions, invalid checksums, and malformed package paths.
- Package activation must support rollback and a disabled-mods startup mode.

## Development phases

1. Standalone touch UI with fixture data.
2. Local read-only catalog API and device compatibility filtering.
3. Transactional package installation and recovery.
4. Runtime UI integration validated against the target device.
5. Explicit, separately reviewed inclusion in the `/data` image.

## Catalog

See `catalog/schema-v1.json` for the initial metadata contract. The example file is a schema, not a live catalog.
