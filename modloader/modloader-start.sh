#!/bin/sh
# Stable central entrypoint for the existing Prime Unlimited boot integration.
# This file does not install a hook and never performs network activity.
set -eu
ROOT="${MODLOADER_ROOT:-/data/prime-unlimited}"
ADAPTER="${MODLOADER_RUNTIME_ADAPTER:-$ROOT/bin/activate.py}"
if [ ! -f "$ADAPTER" ]; then
    echo "modloader: runtime adapter unavailable" >&2
    exit 0
fi
if [ "${MODLOADER_SAFE_MODE:-0}" = "1" ]; then
    exec python3 "$ADAPTER" --root "$ROOT" --safe-mode
fi
exec python3 "$ADAPTER" --root "$ROOT"
