# Verified JC11S safety and quarantine status

Verified on 2026-11-17 against a Denon DJ Prime 4+ (`JC11S`) running Engine
OS 5.3.0.

## Deployed artifacts

- Runtime binary: 6,815,928-byte static ARM EABI5 executable
  - SHA-256: `707f2a3f00654d1946268e535157620ec74ed380126edb1c47e42450ca38aa77`
- Immutable fixture commit: `9cf3debae5942e5bcb31bfb3ebcff58fecef8a5b`
- Pre-milestone backups: `/data/prime-unlimited/backups/safety-v1/`

## Real-device verification

Global safe mode blocked an enabled mod before its entrypoint started and did
not increment its failure counter. After safe mode was disabled, three forced
entrypoint failures produced persistent counts 1, 2 and 3. The third failure
quarantined and atomically disabled the mod. A fourth launch was rejected as
`blocked-quarantine`.

Explicit recovery cleared the quarantine without enabling the mod. A separate
enable followed by a successful dependency-ordered launch completed with exit
code zero and cleared the failure state. Disable and uninstall then succeeded,
leaving an empty installed registry. Engine retained PID 698 throughout.

Local tests additionally cover direct-launch blocking and strict
malformed-state rejection.

## Restored device state

The signed v1 smoke catalog was restored. Exact package, stage, installed,
registry, run, status, log, safety and temporary test artifacts were removed.
API and Engine remain active; Engine was not restarted and the device was not
rebooted.

Safe mode and quarantine protect manual launches. Automatic startup and
persistent process supervision remain unavailable.
