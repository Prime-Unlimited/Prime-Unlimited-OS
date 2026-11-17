# Verified JC11S atomic disabled-uninstall status

Verified on 2026-11-17 against a Denon DJ Prime 4+ (`JC11S`) running Engine
OS 5.3.0.

## Deployed artifacts

- Runtime binary: 6,619,320-byte static ARM EABI5 executable
  - SHA-256: `848debf7ff2305036f27ce4fa395bc8a9f4130a88b207d96cfd1d455d260ea34`
- Pre-milestone backups: `/data/prime-unlimited/backups/uninstall-v1/`

## Real-device lifecycle verification

The signed `jc11s-inspection-test` v2 record and immutable project ZIP passed
the authenticated cache, stage and inspection endpoints. The package was
installed disabled and then removed through `POST /api/v1/uninstall`.

- install transaction: `completed` at `registry-committed`
- uninstall transaction: `completed` at `registry-committed`
- registry after uninstall: empty
- installed payload after uninstall: absent
- quarantine and pending journal after completion: absent
- API service after the operation: active
- Engine service after the operation: active with unchanged PID 698

The uninstall endpoint used only the installed registry identity; catalog
presence was not required by the implementation. The payload was renamed
atomically into a private transaction quarantine before the registry commit.
No mod file was made executable or invoked.

Local failure injection covers interruption after the prepared journal,
payload quarantine, registry commit and committed journal. Prepared states
restore the previous registry and payload. A committed state completes the
deletion deterministically. Tests also cover missing IDs, authenticated API
access, transaction persistence and rejection of unsafe payload state.

## Restored device state

The exact test transactions, registry, package cache and stage were removed
after verification. The signed v1 smoke catalog was restored. API and Engine
remain active, the Engine process was not restarted, and the device was not
rebooted.

## Rollback

Stop the API service, restore the runtime and start script from
`/data/prime-unlimited/backups/uninstall-v1/`, restore the saved catalog and
signature, then restart only the API service. The Engine service does not need
to restart.
