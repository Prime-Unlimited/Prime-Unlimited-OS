# Catalog signing tool

`sign_catalog.go` signs the exact catalog bytes with Ed25519 and emits a
deployment-owned public trust-key document plus a detached signature document.
The private key must be stored outside the repository and never copied to a
device.

```sh
go run sign_catalog.go \
  -catalog ../jc11s-smoke-test.json \
  -private-key /secure/outside/repository/catalog-ed25519.key \
  -key-id prime-unlimited-2026-1 \
  -public-key-out ../trust/prime-unlimited-2026-1.json \
  -signature-out ../jc11s-smoke-test.sig.json \
  -generate
```

`-generate` refuses to replace an existing key. Private keys must be regular,
owned by the current user and mode `0600`. Subsequent catalog updates omit
`-generate` and reuse the protected key. The tool prints only the key
fingerprint and catalog digest, never private or public key material or the
signature.

Verify the exact catalog bytes independently with the same detached formats
consumed by both device runtimes:

```sh
go run sign_catalog.go \
  -verify \
  -catalog ../jc11s.json \
  -signature ../jc11s.sig.json \
  -public-key ../trust/prime-unlimited-2026-1.json
```

Verification rejects unknown JSON fields, trailing data, key-ID mismatches,
malformed Ed25519 material, unsafe group/world-writable metadata, and any
change to the catalog bytes. It prints only the key ID, public-key fingerprint,
and catalog digest. Never pass a private key to verification mode.
