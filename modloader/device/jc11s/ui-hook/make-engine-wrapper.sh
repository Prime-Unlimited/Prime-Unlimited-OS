#!/bin/sh
set -eu
SOURCE=${1:-/usr/Engine/Scripts/engine}
TARGET=${2:-/data/prime-unlimited/bin/engine-hooked.sh}
[ -s "$SOURCE" ] || { echo "missing firmware launcher: $SOURCE" >&2; exit 1; }
grep -q '/usr/Engine/Engine -d0' "$SOURCE" || {
    echo "unsupported firmware launcher: Engine invocation not found" >&2
    exit 1
}
sed 's#^[[:space:]]*/usr/Engine/Engine -d0#\tQML_DISABLE_DISK_CACHE=1 LD_PRELOAD=/data/prime-unlimited/lib/libprime-ui-hook.so /usr/Engine/Engine -d0#' \
    "$SOURCE" > "$TARGET"
chmod 755 "$TARGET"
grep -q 'LD_PRELOAD=/data/prime-unlimited/lib/libprime-ui-hook.so /usr/Engine/Engine' "$TARGET"
