# Verified JC11S authorization status

Verified on 2026-11-17 against a Denon DJ Prime 4+ (`JC11S`) running Engine
OS 5.3.0.

## Deployed artifacts

- Runtime binary: `/data/prime-unlimited/bin/prime-unlimited-runtime-api`
  - ELF 32-bit ARM EABI5, statically linked and stripped
  - size: 5,898,424 bytes
  - SHA-256: `dd2672ada4bcdde6dfb773559cdafbf1e6e67697a5b9d1aad45fb754ab46a4f7`
- Start script: `/data/prime-unlimited/bin/modloader-api-start.sh`
  - mode: `0755`
  - SHA-256: `4f21b6868ca154b80257d45db11bc2e12c2b87d13aa20be36e989875c456f30d`
- Pre-milestone backups:
  `/data/prime-unlimited/backups/auth-v1/prime-unlimited-runtime-api.pre-auth-v1`
  and `modloader-api-start.sh.pre-auth-v1`

## Device-owned secret

The first service start created `/data/prime-unlimited/auth` as `0700
root:root` and `mutation.token` as a regular `0600 root:root` file. Its length
and lowercase hexadecimal format were validated without printing the token or
a token-derived hash. Inode, size, and modification time remained unchanged
across a second service restart, proving that the existing token was reused.

The token value is intentionally absent from this document, Git, service logs,
diagnostic output, and HTTP responses.

## Device verification

- `GET /api/v1/health`: HTTP 200 with mode `authenticated-read-only` and
  `authorizationStatus: ready`
- mutation without credentials: HTTP 401
- mutation with an incorrect token: HTTP 401
- mutation with duplicate Authorization headers: HTTP 401
- mutation with the correct device token: HTTP 501 `mutation engine
  unavailable`
- API remained bound only to `127.0.0.1:8098`
- the smoke-test catalog remained visible and valid
- `prime-unlimited-modloader-api.service` and `engine.service` remained active
- the Prime Unlimited UI hook remained mapped in the Engine process

No mutation was executed or simulated as successful. Supplying the correct
credential only proves authentication and still fails closed before any
package action.

## Rollback

Stop the API service, restore both files from
`/data/prime-unlimited/backups/auth-v1/`, and restart the API service. The
device-owned token can remain unused for a forward retry, or its exact file
and then the empty auth directory can be removed to restore the prior on-disk
state. No Engine restart or device reboot is required.

Trusted delivery of the token to a privileged UI action path is deliberately
not part of this milestone. `ModLoader.qml` continues to keep mutations
disabled because its `authToken` property is empty.

This historical limitation was superseded by the later manager-UI milestone;
see `VERIFIED_MANAGER_UI_STATUS.md`.
