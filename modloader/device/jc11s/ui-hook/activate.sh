#!/bin/sh
# JC11S SSH + Prime Unlimited UI hook
set -eu
BASE=/data/prime-ssh
PU=/data/prime-unlimited
LOG="$BASE/activation.log"
UI_READY="$PU/bootstrap/ready"
[ "$(id -u)" = 0 ] || exit 1
[ -s "$BASE/authorized_keys" ] || exit 1

mkdir -p "$BASE/hostkeys" /run/sshd "$PU/logs"
chmod 700 "$BASE/hostkeys"

# Some Windows e2fsprogs builds preserve the host account (for example UID
# 1002 and mode 0770) on directories imported with mke2fs -d. The runtime
# intentionally refuses to write authorized QML into such a directory.
# Normalize only the Modloader-owned /data trees before either service starts.
mkdir -p "$PU/ui/patches" "$PU/ui/splash" "$PU/catalog" "$PU/trust"
chown -R root:root "$PU/ui" "$PU/catalog" "$PU/trust"
chmod 755 "$PU/ui" "$PU/ui/patches" "$PU/ui/splash"
chmod 700 "$PU/catalog" "$PU/trust"

# Early development builds could leave a persistent ALSA configuration that
# incorrectly loads the Qt resource interposer as an ALSA hook. That prevents
# Engine from opening its MIDI sequencer and disables the physical controls.
# Quarantine only the exact legacy configuration; the independent SSH ALSA
# hook remains untouched.
LEGACY_BACKUP="$PU/backups/control-surface-recovery-v1"
for LEGACY_PATH in \
    /data/system/var/overlay/lib/alsa/conf.d/99-prime-ui.conf \
    /data/system/var-lib/alsa/conf.d/99-prime-ui.conf
do
    if [ -f "$LEGACY_PATH" ] &&
       grep -q 'hook_func\.prime_ui_hook' "$LEGACY_PATH" &&
       grep -q '/data/prime-unlimited/lib/libprime-ui-hook\.so' "$LEGACY_PATH"
    then
        mkdir -p "$LEGACY_BACKUP"
        chmod 700 "$LEGACY_BACKUP"
        case "$LEGACY_PATH" in
            */var/overlay/*) LEGACY_NAME=99-prime-ui.conf.overlay.disabled ;;
            *) LEGACY_NAME=99-prime-ui.conf.var-lib.disabled ;;
        esac
        mv "$LEGACY_PATH" "$LEGACY_BACKUP/$LEGACY_NAME"
        chmod 600 "$LEGACY_BACKUP/$LEGACY_NAME"
        printf "ui-hook: quarantined legacy ALSA configuration %s\n" "$LEGACY_PATH" >>"$LOG"
    fi
done

if [ ! -s "$BASE/hostkeys/ssh_host_ed25519_key" ]; then
    /usr/bin/ssh-keygen -q -t ed25519 -N "" -f "$BASE/hostkeys/ssh_host_ed25519_key" >>"$LOG" 2>&1 || exit 1
fi
chmod 600 "$BASE/hostkeys/ssh_host_ed25519_key"
/usr/sbin/sshd -t -f "$BASE/sshd_config" >>"$LOG" 2>&1 || exit 1
printf "direct sshd start requested\n" >>"$LOG"

# A factory image cannot copy launchers from the protected firmware while it is
# built. Prepare the wrappers once, on the device, without changing the OFW.
if [ -x "$PU/bin/bootstrap-first-start.sh" ]; then
    "$PU/bin/bootstrap-first-start.sh" >>"$LOG" 2>&1 || \
        printf "ui-hook: first-start preparation failed; stock Engine kept\n" >>"$LOG"
fi

# Install the preload path before engine.service executes on early ALSA activation.
if [ -x "$PU/bin/runengine-hooked.sh" ] && [ -s "$PU/bin/runengine.real" ]; then
    if ! awk '$2 == "/usr/Engine/Scripts/runengine" { found=1 } END { exit !found }' /proc/mounts; then
        mount --bind "$PU/bin/runengine-hooked.sh" /usr/Engine/Scripts/runengine
        printf "ui-hook: runengine bind-mounted\n" >>"$LOG"
    fi
fi

# The hook removes LD_PRELOAD from its constructor environment. Inject it again
# only for the final Engine binary so the library remains mapped in EMain.
if [ -x "$PU/bin/engine-hooked.sh" ]; then
    if ! awk '$2 == "/usr/Engine/Scripts/engine" { found=1 } END { exit !found }' /proc/mounts; then
        mount --bind "$PU/bin/engine-hooked.sh" /usr/Engine/Scripts/engine
        printf "ui-hook: engine launcher bind-mounted\n" >>"$LOG"
    fi
fi

# Keep a systemd environment drop-in as a second path for later service starts.
if [ -e "$UI_READY" ] && [ -x "$PU/lib/libprime-ui-hook.so" ]; then
    mkdir -p /run/systemd/system/engine.service.d
    printf "[Service]\nEnvironment=LD_PRELOAD=$PU/lib/libprime-ui-hook.so\n" > /run/systemd/system/engine.service.d/99-prime-ui.conf
    systemctl daemon-reload || true
    printf "ui-hook: drop-in written and daemon reloaded\n" >>"$LOG"
fi

# ALSA activation happens inside the first EMain start. Schedule exactly one
# systemd-managed restart so the prepared mounts/drop-in apply in this boot.
RESTART_FLAG=/run/prime-unlimited-engine-restart-scheduled
if [ -e "$UI_READY" ] && [ ! -e "$RESTART_FLAG" ]; then
    : > "$RESTART_FLAG"
    systemd-run --quiet --unit=prime-unlimited-engine-restart --on-active=5s \
        /bin/systemctl restart engine.service || true
    printf "ui-hook: one engine restart scheduled\n" >>"$LOG"
fi

# Start the dependency-free loopback API once per boot. It remains read-only
# until the authenticated package transaction engine is provisioned.
if [ -x "$PU/bin/modloader-api-start.sh" ] && [ -x "$PU/bin/prime-unlimited-runtime-api" ]; then
    if ! systemctl is-active --quiet prime-unlimited-modloader-api.service; then
        systemd-run --quiet --unit=prime-unlimited-modloader-api \
            --property=Restart=on-failure --property=RestartSec=2s \
            "$PU/bin/modloader-api-start.sh" || true
        printf "modloader: runtime API start requested\n" >>"$LOG"
    fi
fi

if [ -x "$PU/bin/modloader-updater-start.sh" ] && [ -x "$PU/bin/prime-unlimited-self-updater" ]; then
    if ! systemctl is-active --quiet prime-unlimited-modloader-updater.service; then
        systemd-run --quiet --unit=prime-unlimited-modloader-updater \
            --property=Restart=on-failure --property=RestartSec=2s \
            "$PU/bin/modloader-updater-start.sh" || true
        printf "modloader: independent updater start requested\n" >>"$LOG"
    fi
fi

exec /usr/sbin/sshd -D -e -f "$BASE/sshd_config" >>"$LOG" 2>&1
