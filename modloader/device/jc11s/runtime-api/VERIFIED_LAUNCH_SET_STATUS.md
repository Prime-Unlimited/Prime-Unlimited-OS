# Verified JC11S dependency-ordered launch-run status

Verified on 2026-11-17 against a Denon DJ Prime 4+ (`JC11S`) running Engine
OS 5.3.0.

## Deployed artifacts

- Runtime binary: 6,815,928-byte static ARM EABI5 executable
  - SHA-256: `507a392c44c77582ca14de026f75cd7bce96b5c11916187fa7616a655267073e`
- Immutable fixture commit: `9cf3debae5942e5bcb31bfb3ebcff58fecef8a5b`
- Pre-milestone backups: `/data/prime-unlimited/backups/launch-set-v1/`

## Real-device verification

A signed app `1.0.0` requiring dependency `^2.0` and its compatible dependency
`2.1.0` were installed disabled and enabled atomically. One authenticated
launch-set request produced a completed run with dependency-first steps:

1. `launch-dependency-test` — completed, exit 0
2. `launch-app-test` — completed, exit 0

The same ordered record was persisted under its random 128-bit run ID. Both
bounded logs contained the expected inert fixture output. Disable and
uninstall then completed in safe dependent-first order. Final registry was
empty, no journal remained and Engine retained PID 698.

Local tests additionally prove fail-fast behavior: when a dependency exits
nonzero, its dependent receives no status file and is never started. Disabled,
missing, incompatible, cyclic and conflicting graph states fail closed before
launch.

## Restored device state

The signed v1 smoke catalog was restored. Exact package, stage, installed,
registry, run, status, log and temporary test artifacts were removed. API and
Engine remain active; Engine was not restarted and the device was not
rebooted.

Launch sets remain manual and one-shot. Persistent supervision, stop actions,
safe mode, rollback and boot integration remain unavailable.
