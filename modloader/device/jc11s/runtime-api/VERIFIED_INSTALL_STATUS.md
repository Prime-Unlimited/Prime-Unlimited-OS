# Verified JC11S atomic disabled-install status

Verified on 2026-11-17 against a Denon DJ Prime 4+ (`JC11S`) running Engine
OS 5.3.0.

## Deployed artifacts

- Runtime binary: 6,619,320-byte static ARM EABI5 executable
  - SHA-256: `7e791e25a5844d4da2bb740fcb15ed94d3c2dcda2b0abcb5ddd7e32a2aa2247d`
- Start script SHA-256:
  `e28ba08b49ff2e108676003844a357fac0daef736d45a3eeef569db4ea726228`
- Pre-milestone backups: `/data/prime-unlimited/backups/install-v1/`

## Real-device installation verification

The signed `jc11s-inspection-test` v2 record and its immutable project ZIP were
cached, staged, inspected and installed through the authenticated API. The
operation returned HTTP 202 with a random 128-bit transaction ID,
`installed: true` and `enabled: false`.

- installed payload files: 131
- target: `mods/installed/inspect-device-test/44e96b9`
- all installed directories: `0700 root:root`
- all installed files: `0600 root:root`
- executable installed files: zero
- private stage marker copied into the installation: no
- registry and transaction files: regular `0600 root:root`
- pending journal after completion: absent
- duplicate installation: HTTP 409

`GET /api/v1/installed` reported the signed catalog ID, package SHA-256,
architecture, ABI and entrypoint with `enabled: false`. The transaction API
reported `completed` at `registry-committed`. After restarting only the API,
the same disabled installed state remained. The installed path was not mapped
into the Engine process and no entrypoint was invoked.

Local failure injection covers interruption immediately after the prepared
journal, file publication, registry commit and committed journal. Prepared
states restore the previous registry and remove only the validated target;
the committed-journal state completes deterministically. Additional tests
cover unsafe registry state, restrictive modes, duplicate installation and
transaction persistence.

## Restored device state

The exact test transaction, registry state, installed target, cache file and
stage were removed after verification. Directories created solely by the test
were removed only after they were empty. The signed v1 smoke catalog was
restored; `/api/v1/installed` again returned an empty map. API and Engine
remained active and the device was not rebooted.

This milestone never enables or executes a mod and does not generate startup
configuration. Enable/disable, activation, uninstall and user-facing rollback
remain unavailable.

## Rollback

Stop the API service, restore the runtime and start script from
`/data/prime-unlimited/backups/install-v1/`, restore the pre-existing registry
state when that backup exists, and restart only the API service. The Engine
service does not need to restart.
