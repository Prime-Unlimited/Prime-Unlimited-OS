#!/bin/sh
set -eu
HERE=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
PU=/data/prime-unlimited
CACHE='/data/AppDataUser/AIR Music Technology/Engine/.cache/qml'
[ "$(id -u)" = 0 ] || { echo 'run as root' >&2; exit 1; }
grep -q 'JC11S\|JC11' /proc/device-tree/model 2>/dev/null || {
    echo 'refusing non-JC11S target' >&2; exit 1;
}
[ -s "$HERE/build/libprime-ui-hook.so" ] || { echo 'run build-hook.sh first' >&2; exit 1; }
[ -s /data/prime-ssh/activate.sh ] || { echo 'existing SSH bootstrap not found' >&2; exit 1; }
[ -s /usr/Engine/Scripts/runengine ] || { echo 'firmware runengine missing' >&2; exit 1; }
[ "$(wc -c < "$HERE/ControlCenterTab.qml")" -le 1156 ] || {
    echo 'ControlCenterTab.qml exceeds the verified qrc slot' >&2; exit 1;
}

mkdir -p "$PU/bin" "$PU/lib" "$PU/logs" "$PU/ui/originals" "$PU/ui/patches"
cp "$HERE/build/libprime-ui-hook.so" "$PU/lib/libprime-ui-hook.so"
cp "$HERE/runengine-hooked.sh" "$PU/bin/runengine-hooked.sh"
cp /usr/Engine/Scripts/runengine "$PU/bin/runengine.real"
"$HERE/make-engine-wrapper.sh" /usr/Engine/Scripts/engine "$PU/bin/engine-hooked.sh"
cp "$HERE/ControlCenterTab.qml" "$PU/ui/patches/ControlCenterTab.qml"
cp "$HERE/../../../ui/ModLoader.qml" "$PU/ui/ModLoader.qml"
cp "$HERE/../../../ui/assets/prime_unlimited_mod_loader.svg" "$PU/ui/prime_unlimited_mod_loader.svg"
cp /data/prime-ssh/activate.sh "/data/prime-ssh/activate.sh.bak.$(date +%Y%m%d%H%M%S)"
cp "$HERE/activate.sh" /data/prime-ssh/activate.sh
chmod 755 "$PU/bin/runengine-hooked.sh" "$PU/bin/runengine.real" "$PU/bin/engine-hooked.sh" /data/prime-ssh/activate.sh
chmod 644 "$PU/lib/libprime-ui-hook.so" "$PU/ui/ModLoader.qml" "$PU/ui/prime_unlimited_mod_loader.svg" "$PU/ui/patches/ControlCenterTab.qml"

/data/prime-ssh/activate.sh &
[ ! -d "$CACHE" ] || rm -rf "$CACHE"
systemd-run --quiet --unit=prime-unlimited-engine-restart-install --on-active=2s \
    /bin/systemctl restart engine.service
echo 'installation staged; run verify-device.sh after Engine returns'
