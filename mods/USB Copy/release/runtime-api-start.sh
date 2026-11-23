#!/bin/sh
set -eu
ROOT=/data/prime-unlimited
exec "$ROOT/bin/prime-unlimited-runtime-api" \
    -media-import-writes \
    -media-import-live \
    -listen 127.0.0.1:8098 \
    -model JP24 \
    -firmware 5.3.0 \
    -architecture aarch64 \
    -abi gnu \
    -root "$ROOT" \
    -catalog "$ROOT/catalog/catalog.json" \
    -catalog-signature "$ROOT/catalog/catalog.sig.json" \
    -catalog-trust-key "$ROOT/trust/catalog-key.json" \
    -catalog-url "https://api.github.com/repos/Prime-Unlimited/Prime-Unlimited-OS/releases/assets/783082385" \
    -catalog-signature-url "https://api.github.com/repos/Prime-Unlimited/Prime-Unlimited-OS/releases/assets/783082388" \
    -auth-token "$ROOT/auth/mutation.token" \
    -cache-root "$ROOT/cache/packages" \
    -staging-root "$ROOT/staging/packages"
