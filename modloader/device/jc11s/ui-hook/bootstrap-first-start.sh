#!/bin/sh
# Idempotent JC11S first-boot preparation. It only creates files on /data.
set -eu
PU=/data/prime-unlimited
LOG="$PU/logs/bootstrap.log"
MARKER="$PU/bootstrap/ready"
RUNENGINE=/usr/Engine/Scripts/runengine
ENGINE=/usr/Engine/Scripts/engine

[ -e "$MARKER" ] && exit 0
[ -s "$PU/lib/libprime-ui-hook.so" ] || exit 1
[ -s "$RUNENGINE" ] || exit 1
[ -s "$ENGINE" ] || exit 1
grep -q '/usr/Engine/Engine -d0' "$ENGINE" || exit 1

mkdir -p "$PU/bootstrap" "$PU/logs"
cp "$RUNENGINE" "$PU/bin/runengine.real.tmp"
chmod 755 "$PU/bin/runengine.real.tmp"
mv "$PU/bin/runengine.real.tmp" "$PU/bin/runengine.real"
sed 's#^[[:space:]]*/usr/Engine/Engine -d0#\tQML_DISABLE_DISK_CACHE=1 LD_PRELOAD=/data/prime-unlimited/lib/libprime-ui-hook.so /usr/Engine/Engine -d0#' \
    "$ENGINE" > "$PU/bin/engine-hooked.sh.tmp"
grep -q 'LD_PRELOAD=/data/prime-unlimited/lib/libprime-ui-hook.so /usr/Engine/Engine' \
    "$PU/bin/engine-hooked.sh.tmp"
chmod 755 "$PU/bin/engine-hooked.sh.tmp"
mv "$PU/bin/engine-hooked.sh.tmp" "$PU/bin/engine-hooked.sh"
rm -rf '/data/AppDataUser/AIR Music Technology/Engine/.cache/qml'
: > "$MARKER"
chmod 600 "$MARKER"
printf '%s\n' 'JC11S bootstrap prepared from stock launchers' >> "$LOG"
