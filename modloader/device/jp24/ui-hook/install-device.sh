#!/bin/sh
# Installs the JP24 Control Center UI hook and its activation chain.
#
# Unlike JC11S's install-device.sh, this does NOT touch the SSH bootstrap
# at all (no activate.sh to replace) - JP24's SSH access is a separate,
# already-proven mechanism entirely in devices/jp24/ssh-bootstrap/. This
# script only ever adds activate-jp24.sh, called from a second,
# independent fork in that hook (see alsa-boot-hook.c's
# start_modloader_activation()) that does nothing if the script is
# missing. It does not modify, replace, or depend on the existing hook.
set -eu
HERE=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
PU=/data/prime-unlimited
CACHE='/data/AppDataUser/AIR Music Technology/Engine/.cache/qml'
SSH_ACTIVATE_SOURCE="$HERE/../../../../devices/jp24/ssh-bootstrap/boot-activate.sh"
SSH_ACTIVATE_TARGET=/data/prime4-mod/boot-activate.sh
RUNTIME="$HERE/../runtime-api/build/prime-unlimited-runtime-api"
RUNTIME_START="$HERE/../runtime-api/start.sh"
[ "$(id -u)" = 0 ] || { echo 'run as root' >&2; exit 1; }
grep -q 'JP24' /sys/firmware/devicetree/base/inmusic,product-code 2>/dev/null || {
    echo 'refusing non-JP24 target' >&2; exit 1;
}
[ -s "$HERE/build/libprime-ui-hook.so" ] || { echo 'run PRIME_UI_HOOK_PATCH=1 build-hook.sh first' >&2; exit 1; }
[ -s "$RUNTIME" ] || { echo 'run ../runtime-api/build.sh first' >&2; exit 1; }
[ -s "$RUNTIME_START" ] || { echo 'JP24 runtime API start script is missing' >&2; exit 1; }
[ -s "$SSH_ACTIVATE_SOURCE" ] || { echo 'JP24 systemd SSH activation script is missing' >&2; exit 1; }
[ -s /usr/Engine/Scripts/runengine ] || { echo 'firmware runengine missing' >&2; exit 1; }
[ "$(wc -c < "$HERE/../../jc11s/ui-hook/ControlCenterTab.qml")" -le 1156 ] || {
    echo 'ControlCenterTab.qml exceeds the verified qrc slot' >&2; exit 1;
}

mkdir -p "$PU/bin" "$PU/lib" "$PU/logs" "$PU/ui/originals" "$PU/ui/patches"
mkdir -p "$(dirname -- "$SSH_ACTIVATE_TARGET")"
cp "$SSH_ACTIVATE_SOURCE" "$SSH_ACTIVATE_TARGET"
cp "$HERE/build/libprime-ui-hook.so" "$PU/lib/libprime-ui-hook.so"
cp "$HERE/runengine-hooked.sh" "$PU/bin/runengine-hooked.sh"
# Do not copy through an already active bind mount on an upgrade: that path
# exposes our wrapper rather than the firmware launcher and would make
# runengine.real recursively invoke itself. Preserve the previously captured
# originals when the hook is already mounted.
if awk '$2 == "/usr/Engine/Scripts/runengine" { found=1 } END { exit !found }' /proc/mounts; then
    [ -s "$PU/bin/runengine.real" ] || { echo 'active runengine mount has no saved firmware launcher' >&2; exit 1; }
else
    cp /usr/Engine/Scripts/runengine "$PU/bin/runengine.real"
fi
if awk '$2 == "/usr/Engine/Scripts/engine" { found=1 } END { exit !found }' /proc/mounts; then
    [ -x "$PU/bin/engine-hooked.sh" ] || { echo 'active engine mount has no saved wrapper' >&2; exit 1; }
else
    [ -s /usr/Engine/Scripts/engine ] || { echo 'firmware engine launcher missing' >&2; exit 1; }
    "$HERE/make-engine-wrapper.sh" /usr/Engine/Scripts/engine "$PU/bin/engine-hooked.sh"
fi
cp "$HERE/../../jc11s/ui-hook/ControlCenterTab.qml" "$PU/ui/patches/ControlCenterTab.qml"
cp "$HERE/../../../ui/ModLoader.qml" "$PU/ui/ModLoader.template.qml"
cp "$HERE/../../../ui/OverlayHost.qml" "$PU/ui/OverlayHost.template.qml"
cp "$HERE/../../../ui/assets/prime_unlimited_mod_loader.svg" "$PU/ui/prime_unlimited_mod_loader.svg"
cp "$HERE/activate-jp24.sh" "$PU/bin/activate-jp24.sh"
cp "$RUNTIME" "$PU/bin/prime-unlimited-runtime-api"
cp "$RUNTIME_START" "$PU/bin/modloader-api-start.sh"
chmod 755 "$SSH_ACTIVATE_TARGET" "$PU/bin/runengine-hooked.sh" "$PU/bin/runengine.real" "$PU/bin/engine-hooked.sh" "$PU/bin/activate-jp24.sh" "$PU/bin/prime-unlimited-runtime-api" "$PU/bin/modloader-api-start.sh"
chmod 644 "$PU/lib/libprime-ui-hook.so" "$PU/ui/ModLoader.template.qml" "$PU/ui/OverlayHost.template.qml" "$PU/ui/prime_unlimited_mod_loader.svg" "$PU/ui/patches/ControlCenterTab.qml"

"$PU/bin/activate-jp24.sh" &
[ ! -d "$CACHE" ] || rm -rf "$CACHE"
echo 'installation staged; activation schedules one Engine restart'
echo 'run verify-device.sh after Engine returns'
echo 'SSH is started by a separate systemd unit and should remain reachable across the Engine restart'
