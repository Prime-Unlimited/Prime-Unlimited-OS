#!/bin/sh
# JP24 Prime Unlimited UI hook activation - mod-loader wiring only.
#
# Unlike JC11S, where this logic lives inside a single activate.sh that
# JC11S's ALSA hook also uses to start sshd, JP24's SSH bootstrap is
# already proven and is triggered by compiled C
# (devices/jp24/ssh-bootstrap/alsa-boot-hook.c launches a dedicated
# systemd SSH service through boot-activate.sh). This script is called
# from a SECOND, independent
# fork in that same hook, guarded by its own once-per-boot stamp file
# separate from the SSH one, so a bug here can never affect SSH access -
# the two bootstraps share nothing but the ALSA trigger point.
set -eu
PU=/data/prime-unlimited
HERE=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
LOG="$PU/logs/activation.log"
SSH_ACTIVATE=/data/prime4-mod/boot-activate.sh
[ "$(id -u)" = 0 ] || exit 1

mkdir -p "$PU/logs"

# Ensure systemd owns SSH before restarting Engine. On a legacy installation
# an early sshd may still be direct; the dedicated unit retries that temporary
# port conflict and survives the restart.
if [ -x "$SSH_ACTIVATE" ]; then
    "$SSH_ACTIVATE" >>"$LOG" 2>&1 || printf "ui-hook(jp24): SSH service setup reported an error\n" >>"$LOG"
else
    printf "ui-hook(jp24): SSH service script missing; keeping existing SSH startup\n" >>"$LOG"
fi

# Install the preload path before engine.service executes on early ALSA
# activation - same two-bind-mount approach verified on JC11S.
if [ -x "$PU/bin/runengine-hooked.sh" ] && [ -s "$PU/bin/runengine.real" ]; then
    if ! awk '$2 == "/usr/Engine/Scripts/runengine" { found=1 } END { exit !found }' /proc/mounts; then
        mount --bind "$PU/bin/runengine-hooked.sh" /usr/Engine/Scripts/runengine
        printf "ui-hook(jp24): runengine bind-mounted\n" >>"$LOG"
    fi
fi

if [ -x "$PU/bin/engine-hooked.sh" ]; then
    if ! awk '$2 == "/usr/Engine/Scripts/engine" { found=1 } END { exit !found }' /proc/mounts; then
        mount --bind "$PU/bin/engine-hooked.sh" /usr/Engine/Scripts/engine
        printf "ui-hook(jp24): engine launcher bind-mounted\n" >>"$LOG"
    fi
fi

# Keep a systemd environment drop-in as a second path for later service
# starts - this is exactly the drop-in we placed by hand over SSH during
# manual testing, now written automatically every boot instead.
if [ -x "$PU/lib/libprime-ui-hook.so" ]; then
    mkdir -p /run/systemd/system/engine.service.d
    printf "[Service]\nEnvironment=LD_PRELOAD=%s/lib/libprime-ui-hook.so\n" "$PU" \
        > /run/systemd/system/engine.service.d/99-prime-ui.conf
    systemctl daemon-reload || true
    printf "ui-hook(jp24): drop-in written and daemon reloaded\n" >>"$LOG"
fi

# ALSA activation happens inside the first EMain start, before the mounts
# above exist yet from systemd's point of view. Schedule exactly one
# systemd-managed restart so they apply within this boot - same approach
# verified on JC11S. The former directly forked sshd was killed by this
# restart. The dedicated prime4-mod-sshd.service now remains outside the
# Engine cgroup and was verified after both an Engine restart and a reboot.
RESTART_FLAG=/run/prime-unlimited-jp24-engine-restart-scheduled
if [ ! -e "$RESTART_FLAG" ]; then
    : > "$RESTART_FLAG"
    systemd-run --quiet --unit=prime-unlimited-jp24-engine-restart --on-active=5s \
        /bin/systemctl restart engine.service || true
    printf "ui-hook(jp24): one engine restart scheduled\n" >>"$LOG"
fi

# Start the aarch64 loopback API once per boot. The transient unit is outside
# engine.service and survives the scheduled Engine restart above.
if [ -x "$PU/bin/modloader-api-start.sh" ] && [ -x "$PU/bin/prime-unlimited-runtime-api" ]; then
    if ! systemctl is-active --quiet prime-unlimited-modloader-api.service; then
        systemd-run --quiet --unit=prime-unlimited-modloader-api \
            --property=Restart=on-failure --property=RestartSec=2s \
            "$PU/bin/modloader-api-start.sh" || true
        printf "modloader(jp24): runtime API start requested\n" >>"$LOG"
    fi
fi

if [ -x "$PU/bin/modloader-updater-start.sh" ] && [ -x "$PU/bin/prime-unlimited-self-updater" ]; then
    if ! systemctl is-active --quiet prime-unlimited-modloader-updater.service; then
        systemd-run --quiet --unit=prime-unlimited-modloader-updater \
            --property=Restart=on-failure --property=RestartSec=2s \
            "$PU/bin/modloader-updater-start.sh" || true
        printf "modloader(jp24): independent updater start requested\n" >>"$LOG"
    fi
fi
