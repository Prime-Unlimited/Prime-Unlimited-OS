#!/bin/sh
set -eu
ROOT=/data/prime-unlimited
exec "$ROOT/bin/prime-unlimited-self-updater" \
  -listen 127.0.0.1:8099 -root "$ROOT" -model JC11S -architecture armv7l -abi gnueabihf \
  -manifest-url "https://raw.githubusercontent.com/Prime-Unlimited/Prime-Unlimited-OS/feat/prime-unlimited-mod-loader/modloader/update/manifest.json" \
  -signature-url "https://raw.githubusercontent.com/Prime-Unlimited/Prime-Unlimited-OS/feat/prime-unlimited-mod-loader/modloader/update/manifest.sig.json" \
  -trust-key "$ROOT/trust/catalog-key.json" -auth-token "$ROOT/auth/mutation.token"
