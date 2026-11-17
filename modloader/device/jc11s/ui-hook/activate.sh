#!/bin/sh
# JC11S SSH + Prime Unlimited UI hook
set -eu
BASE=/data/prime-ssh
PU=/data/prime-unlimited
LOG="$BASE/activation.log"
[ "$(id -u)" = 0 ] || exit 1
[ -s "$BASE/authorized_keys" ] || exit 1

mkdir -p "$BASE/hostkeys" /run/sshd "$PU/logs"
chmod 700 "$BASE/hostkeys"
if [ ! -s "$BASE/hostkeys/ssh_host_ed25519_key" ]; then
    /usr/bin/ssh-keygen -q -t ed25519 -N "" -f "$BASE/hostkeys/ssh_host_ed25519_key" >>"$LOG" 2>&1 || exit 1
fi
chmod 600 "$BASE/hostkeys/ssh_host_ed25519_key"
/usr/sbin/sshd -t -f "$BASE/sshd_config" >>"$LOG" 2>&1 || exit 1
printf "direct sshd start requested\n" >>"$LOG"

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
if [ -x "$PU/lib/libprime-ui-hook.so" ]; then
    mkdir -p /run/systemd/system/engine.service.d
    printf "[Service]\nEnvironment=LD_PRELOAD=$PU/lib/libprime-ui-hook.so\n" > /run/systemd/system/engine.service.d/99-prime-ui.conf
    systemctl daemon-reload || true
    printf "ui-hook: drop-in written and daemon reloaded\n" >>"$LOG"
fi

# ALSA activation happens inside the first EMain start. Schedule exactly one
# systemd-managed restart so the prepared mounts/drop-in apply in this boot.
RESTART_FLAG=/run/prime-unlimited-engine-restart-scheduled
if [ ! -e "$RESTART_FLAG" ]; then
    : > "$RESTART_FLAG"
    systemd-run --quiet --unit=prime-unlimited-engine-restart --on-active=5s \
        /bin/systemctl restart engine.service || true
    printf "ui-hook: one engine restart scheduled\n" >>"$LOG"
fi

exec /usr/sbin/sshd -D -e -f "$BASE/sshd_config" >>"$LOG" 2>&1
