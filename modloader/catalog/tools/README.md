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
