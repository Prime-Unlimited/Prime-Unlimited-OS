# Verified JC11S atomic dependency-activation status

Verified on 2026-11-17 against a Denon DJ Prime 4+ (`JC11S`) running Engine
OS 5.3.0.

## Deployed artifacts

- Runtime binary: 6,684,856-byte static ARM EABI5 executable
  - SHA-256: `f3e2085c211eaded120a61391fcf8d7c694713d050f597c1bded0c99874fa927`
- Pre-milestone backups:
  `/data/prime-unlimited/backups/dependency-activation-v1/`

## Real-device verification

A temporary signed v2 catalog described an app `1.0.0` requiring a dependency
matching `^2.0`, plus the compatible dependency `2.1.0`. Both packages passed
cache, stage, inspection and disabled installation independently.

The resolver returned dependency-first order. One authenticated enable request
for the app returned both IDs in `affected` and atomically wrote registry and
manifest states with both mods enabled. Their entrypoints remained mode `0600`
and were never invoked.

Disable of the app followed by its dependency succeeded. Both mods were then
uninstalled. The final registry and manifest were empty, no pending journal
remained, and Engine retained PID 698 throughout.

Local tests cover atomic closure activation, missing and incompatible
dependencies, cycles, direct and reverse conflicts, protected disable order,
failure recovery at every journal boundary and strict persistence of signed
dependency metadata.

## Restored device state

The signed v1 smoke catalog was restored. Exact package, stage, installed,
registry, transaction and temporary test artifacts were removed. API and
Engine remain active; Engine was not restarted and the device was not
rebooted.

This milestone does not automatically install dependencies or libraries and
does not execute manifest entries.
