#!/bin/sh
set -eu
HERE=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
PU=/data/prime-unlimited
CACHE='/data/AppDataUser/AIR Music Technology/Engine/.cache/qml'
CATALOG="$HERE/../../../catalog/jc11s.json"
CATALOG_SIGNATURE="$HERE/../../../catalog/jc11s.sig.json"
CATALOG_TRUST="$HERE/../../../catalog/trust/prime-unlimited-2026-1.json"
UPDATER="$HERE/../../../updater/build/jc11s/prime-unlimited-self-updater"
UPDATER_START="$HERE/../updater/start.sh"
MODLOADER_VERSION="$HERE/../../../VERSION.json"
OS_VERSION="$HERE/../../../../VERSION"
OS_VERSION_QML="$HERE/../../../../os/OSVersion.qml"
SPLASH="$HERE/../../common/splash/SplashScreen.png"
[ "$(id -u)" = 0 ] || { echo 'run as root' >&2; exit 1; }
grep -q 'JC11S\|JC11' /proc/device-tree/model 2>/dev/null || {
    echo 'refusing non-JC11S target' >&2; exit 1;
}
[ -s "$HERE/build/libprime-ui-hook.so" ] || { echo 'run build-hook.sh first' >&2; exit 1; }
[ -s /data/prime-ssh/activate.sh ] || { echo 'existing SSH bootstrap not found' >&2; exit 1; }
[ -s /usr/Engine/Scripts/runengine ] || { echo 'firmware runengine missing' >&2; exit 1; }
[ -s "$CATALOG" ] && [ -s "$CATALOG_SIGNATURE" ] && [ -s "$CATALOG_TRUST" ] || { echo 'signed JC11S catalog bundle missing' >&2; exit 1; }
[ -s "$UPDATER" ] && [ -s "$UPDATER_START" ] && [ -s "$MODLOADER_VERSION" ] || { echo 'build the JC11S self-updater first' >&2; exit 1; }
[ "$(cat "$OS_VERSION")" = "0.0.1" ] || { echo 'Prime Unlimited OS version is missing or invalid' >&2; exit 1; }
grep -q 'version: "0.0.1"' "$OS_VERSION_QML" || { echo 'Prime Unlimited OS QML version is missing or invalid' >&2; exit 1; }
[ "$(wc -c < "$HERE/ControlCenterTab.qml")" -le 1156 ] || {
    echo 'ControlCenterTab.qml exceeds the verified qrc slot' >&2; exit 1;
}
[ "$(wc -c < "$HERE/Version.qml")" -le 930 ] || {
    echo 'Version.qml exceeds its guarded replacement size' >&2; exit 1;
}
[ -s "$SPLASH" ] || { echo 'second-stage splash PNG missing' >&2; exit 1; }
[ "$(wc -c < "$SPLASH")" -le 2097152 ] || { echo 'second-stage splash PNG exceeds 2 MiB' >&2; exit 1; }

mkdir -p "$PU/bin" "$PU/lib" "$PU/logs" "$PU/ui/originals" "$PU/ui/patches" "$PU/ui/splash"
mkdir -p "$PU/catalog" "$PU/trust"
chmod 700 "$PU/catalog" "$PU/trust"
cp "$CATALOG" "$PU/catalog/catalog.json"
cp "$CATALOG_SIGNATURE" "$PU/catalog/catalog.sig.json"
cp "$CATALOG_TRUST" "$PU/trust/catalog-key.json"
mkdir -p "$PU/updater"
chmod 700 "$PU/updater"
cp "$UPDATER" "$PU/bin/prime-unlimited-self-updater"
cp "$UPDATER_START" "$PU/bin/modloader-updater-start.sh"
cp "$MODLOADER_VERSION" "$PU/updater/version.json"
cp "$OS_VERSION" "$PU/os-version"
cp "$OS_VERSION_QML" "$PU/os-version.qml"
cp "$HERE/build/libprime-ui-hook.so" "$PU/lib/libprime-ui-hook.so"
cp "$HERE/runengine-hooked.sh" "$PU/bin/runengine-hooked.sh"
cp /usr/Engine/Scripts/runengine "$PU/bin/runengine.real"
"$HERE/make-engine-wrapper.sh" /usr/Engine/Scripts/engine "$PU/bin/engine-hooked.sh"
cp "$HERE/ControlCenterTab.qml" "$PU/ui/patches/ControlCenterTab.qml"
cp "$HERE/Version.qml" "$PU/ui/patches/Version.qml"
cp "$HERE/../../../ui/ModLoader.qml" "$PU/ui/ModLoader.template.qml"
cp "$HERE/../../../ui/OverlayHost.qml" "$PU/ui/OverlayHost.template.qml"
cp "$HERE/../../../ui/assets/prime_unlimited_mod_loader.svg" "$PU/ui/prime_unlimited_mod_loader.svg"
cp "$SPLASH" "$PU/ui/splash/SplashScreen.png"
cp /data/prime-ssh/activate.sh "/data/prime-ssh/activate.sh.bak.$(date +%Y%m%d%H%M%S)"
cp "$HERE/activate.sh" /data/prime-ssh/activate.sh
chmod 755 "$PU/bin/runengine-hooked.sh" "$PU/bin/runengine.real" "$PU/bin/engine-hooked.sh" /data/prime-ssh/activate.sh
chmod 644 "$PU/lib/libprime-ui-hook.so" "$PU/ui/ModLoader.template.qml" "$PU/ui/OverlayHost.template.qml" "$PU/ui/prime_unlimited_mod_loader.svg" "$PU/ui/patches/ControlCenterTab.qml" "$PU/ui/patches/Version.qml" "$PU/ui/splash/SplashScreen.png" "$PU/os-version" "$PU/os-version.qml"
chmod 600 "$PU/catalog/catalog.json" "$PU/catalog/catalog.sig.json" "$PU/trust/catalog-key.json"
chmod 755 "$PU/bin/prime-unlimited-self-updater" "$PU/bin/modloader-updater-start.sh"
chmod 600 "$PU/updater/version.json"

/data/prime-ssh/activate.sh &
[ ! -d "$CACHE" ] || rm -rf "$CACHE"
systemd-run --quiet --unit=prime-unlimited-engine-restart-install --on-active=2s \
    /bin/systemctl restart engine.service
echo 'installation staged; run verify-device.sh after Engine returns'
