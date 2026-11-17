# Verified JC11S manual isolated-launcher status

Verified on 2026-11-17 against a Denon DJ Prime 4+ (`JC11S`) running Engine
OS 5.3.0.

## Deployed artifacts

- Runtime binary: 6,750,392-byte static ARM EABI5 executable
  - SHA-256: `859ca8fc75ee84f9e6193b5ff9ad37c23f93534669b0f6fbef6a9b5c87b85e8d`
- Immutable fixture commit: `9cf3debae5942e5bcb31bfb3ebcff58fecef8a5b`
- Pre-milestone backups: `/data/prime-unlimited/backups/launcher-v1/`

## Real-device verification

The signed launcher fixture passed cache, stage, inspection, disabled install
and enable. A manual authenticated launch completed with exit code zero and
persisted status `completed`. Its bounded log contained only:

```text
prime-unlimited-launcher-smoke-ok
```

The child work directory was owned by UID/GID 65534 with mode `0700`. The log
remained `0600 root:root`. The shell entrypoint remained `0600` and was passed
through standard input rather than executed directly. Disable and uninstall
then succeeded, leaving an empty registry. Engine retained PID 698.

Local tests cover disabled and unsupported entrypoint rejection, successful
minimal-environment execution, bounded persisted logs/status and process-group
termination on timeout.

## Restored device state

The signed v1 smoke catalog was restored. Exact package, stage, installed,
registry, runtime, log, status and temporary test artifacts were removed. API
and Engine remain active; Engine was not restarted and the device was not
rebooted.

This launcher is manual and one-shot. Network and mount namespaces, dependency
process ordering, long-running supervision, safe mode and boot integration
remain later milestones.
