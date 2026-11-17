# Verified JC11S catalog-signature status

Verified on 2026-11-17 against a Denon DJ Prime 4+ (`JC11S`) running Engine
OS 5.3.0.

## Trust root

- algorithm: Ed25519
- key ID: `prime-unlimited-2026-1`
- public-key SHA-256 fingerprint:
  `17bafd5349bc7338b09697ef346b3fd796b2af09aa7c54f82d0c68d95e6f1441`
- device trust file: `/data/prime-unlimited/trust/catalog-key.json`
- device trust-file SHA-256:
  `c2dde77732ee15269dd616f8a8674c64f50f5a20fd1d95127b155480332135b2`

The private signing key is retained outside the Git repository in protected
Raspberry Pi credentials storage with mode `0600`. It was never copied to the
Prime, printed, logged, included in HTTP output, or committed. It requires a
separate secure backup before the Raspberry Pi can be treated as disposable.

## Deployed artifacts

- Runtime binary:
  - size: 6,422,712 bytes
  - SHA-256: `95e799a01d047cc475c6201dc623b276825ca1ca65a98613ca370f5aca1d635e`
- Start script SHA-256:
  `6b66a7c1fd4fc1d562c5cf2dc0e76b0f75d444ebfda94426158f8784ca21dd05`
- Smoke catalog SHA-256:
  `012d03b43099961a9a1bf4be1b0f2ad982a3d9b614d9bda3eea62188c5f561f4`
- Detached signature SHA-256:
  `c4df72eda0fb1efdf4de11dfd0c0364aadfe45b4b322e5ccb6e2f46de4d06022`
- Catalog, signature, and trust document are regular `0644 root:root` files.
- Pre-milestone backups are under
  `/data/prime-unlimited/backups/signature-v1/`.

## Valid-chain verification

- `GET /api/v1/health`: HTTP 200, `catalogStatus: valid`,
  `catalogTrust: verified`, expected key ID and public-key fingerprint
- `GET /api/v1/mods`: signed catalog SHA-256 and compatible smoke-test entry
- authenticated caching remained available only through a verified catalog
- the separately signed immutable GitHub transport fixture downloaded,
  verified and reused successfully; it was then removed and the signed smoke
  catalog/signature pair restored

## Tamper verification

Appending one newline to a temporary copy of the signed catalog produced:

- health remained available but reported `catalogStatus: invalid` and
  `catalogTrust: unverified`
- error: `catalog signature verification failed`
- `/api/v1/mods`: HTTP 503
- authenticated `/api/v1/cache`: HTTP 404

Restoring the exact signed bytes immediately restored `catalogTrust:
verified`. The API and Engine services remained active, the API remained
loopback-only, the device-owned mutation token was unchanged, and no Engine
restart or device reboot occurred.

## Security boundary

The catalog signature authenticates the exact catalog bytes and therefore the
approved package URL, size, SHA-256 and compatibility metadata. Package bytes
are still checked against that authenticated size and digest. Individual
archive-content signatures, safe ZIP extraction, installation, activation and
rollback are not implemented by this milestone.

## Rollback

Stop the API service; restore runtime, start script and catalog from
`/data/prime-unlimited/backups/signature-v1/`; remove only
`catalog.sig.json` and `trust/catalog-key.json`; then restart the API service.
No Engine restart is required.
