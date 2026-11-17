# Verified JC11S non-executing activation-manifest status

Verified on 2026-11-17 against a Denon DJ Prime 4+ (`JC11S`) running Engine
OS 5.3.0.

## Deployed artifacts

- Runtime binary: 6,619,320-byte static ARM EABI5 executable
  - SHA-256: `c14e33fdd5d023be1d2c3bb5d428ce9fd523f827628cdeb591869c58c148abd7`
- Pre-milestone backups: `/data/prime-unlimited/backups/activation-v1/`

## Real-device verification

The signed device-test package passed cache, stage and inspection, then was
installed disabled. The authenticated enable operation atomically produced an
enabled registry entry and a manifest containing only its ID, version, root
and signed entrypoint.

- enable transaction: `completed` at `manifest-committed`
- enabled state survived an API-only restart
- entrypoint mode while enabled: `0600`
- disabled manifest: `{"schemaVersion":1,"mods":[]}`
- disable transaction: `completed` at `manifest-committed`
- final uninstall: completed with empty installed registry
- pending journal after completion: absent
- Engine PID before and after: 698

No executable bit was added and no package file was invoked. No Engine
restart, device reboot or boot-hook change occurred.

Local failure injection covers interruption after the prepared journal,
registry commit, manifest commit and committed journal. Before the final
commit recovery restores the previous registry and manifest; afterward it
completes the next pair deterministically. Enabled mods cannot be uninstalled.

## Restored device state

The signed v1 smoke catalog was restored and all package, stage, installed,
registry and transaction test artifacts were removed. The runtime now creates
an empty activation manifest at startup as its fail-closed baseline. API and
Engine remain active.

This milestone does not execute mods. A later launcher milestone must add
dependency ordering, process isolation, failure policy and safe-mode handling
before any manifest entry can run.
