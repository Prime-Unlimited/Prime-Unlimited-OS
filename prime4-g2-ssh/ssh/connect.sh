#!/usr/bin/env bash
set -euo pipefail

if [[ $# -lt 2 || $# -gt 3 ]]; then
    echo "Usage: $0 <device-ip-or-hostname> <private-key> [port]" >&2
    exit 2
fi

HOST="$1"
KEY="$2"
PORT="${3:-2222}"
[[ -r "$KEY" ]] || { echo "Cannot read private key: $KEY" >&2; exit 1; }
[[ "$PORT" =~ ^[0-9]+$ ]] || { echo "Port must be numeric" >&2; exit 2; }

exec ssh -i "$KEY" -p "$PORT" \
    -o IdentitiesOnly=yes \
    -o StrictHostKeyChecking=accept-new \
    "root@$HOST"
