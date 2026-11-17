#!/bin/sh
set -eu
ROOT=/data/prime-unlimited
exec "$ROOT/bin/prime-unlimited-runtime-api" \
    -listen 127.0.0.1:8098 \
    -model JC11S \
    -firmware 5.3.0 \
    -architecture armv7l \
    -abi gnueabihf \
    -catalog "$ROOT/catalog/catalog.json" \
    -catalog-signature "$ROOT/catalog/catalog.sig.json" \
    -catalog-trust-key "$ROOT/trust/catalog-key.json" \
    -auth-token "$ROOT/auth/mutation.token" \
    -cache-root "$ROOT/cache/packages" \
    -staging-root "$ROOT/staging/packages"
