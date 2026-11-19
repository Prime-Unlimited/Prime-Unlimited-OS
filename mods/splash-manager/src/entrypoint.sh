#!/bin/sh
MOD_DIR="$(cd "$(dirname "$0")" && pwd)"
PID_FILE="$MOD_DIR/server.pid"

case "$1" in
    start)
        [ -f "$PID_FILE" ] && kill "$(cat "$PID_FILE")" 2>/dev/null
        case "$(uname -m)" in
            aarch64|arm64) BIN="$MOD_DIR/bin/splash-server-aarch64" ;;
            arm*|armv7*)   BIN="$MOD_DIR/bin/splash-server-armhf" ;;
            *)             echo "Unsupported architecture: $(uname -m)" >&2; exit 1 ;;
        esac
        nohup "$BIN" >/dev/null 2>&1 &
        echo $! > "$PID_FILE"
        exit 0
        ;;
    stop)
        [ -f "$PID_FILE" ] && kill "$(cat "$PID_FILE")" 2>/dev/null
        rm -f "$PID_FILE"
        exit 0
        ;;
esac
