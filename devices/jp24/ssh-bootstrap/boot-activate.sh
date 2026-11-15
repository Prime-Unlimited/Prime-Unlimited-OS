#!/bin/sh
# Prime 4 G2 mod: start key-only sshd on port 2222 via a transient systemd
# unit. Called by the ALSA boot hook (forked, detached).
#
# Port 2222 because inMusic's own sshd already owns port 22 and only
# accepts their own CA-signed certs. HostKey uses the stock rootfs key at
# /etc/ssh/ssh_host_ed25519_key (read-only rootfs, always present) rather
# than generating our own - one less moving part.
#
# Starting sshd through systemctl (talking to PID 1) rather than exec'ing
# it directly from this script's own process tree matches the
# independently confirmed-working setup: PID 1 forks/execs the new unit
# in its own context.

MOD_DIR=/data/prime4-mod
LOG="$MOD_DIR/debug.log"
UNIT=/run/systemd/system/sshd-mod.service

log() { echo "$(date '+%H:%M:%S') $*" >> "$LOG" 2>/dev/null; }

log "=== boot-activate.sh started ==="

if [ ! -s "$MOD_DIR/authorized_keys" ]; then
    log "ABORT: authorized_keys missing or empty"
    exit 0
fi

mkdir -p /run/systemd/system
cat > "$UNIT" <<EOF
[Unit]
Description=Prime 4 G2 mod SSH (key only, port 2222)
After=network.target

[Service]
ExecStart=/usr/sbin/sshd -D -f /dev/null \\
    -o "HostKey /etc/ssh/ssh_host_ed25519_key" \\
    -o "AuthorizedKeysFile $MOD_DIR/authorized_keys" \\
    -o "PermitRootLogin yes" \\
    -o "PubkeyAuthentication yes" \\
    -o "PasswordAuthentication no" \\
    -o "KbdInteractiveAuthentication no" \\
    -o "StrictModes no" \\
    -o "Port 2222"
Restart=on-failure
RestartSec=2
EOF

log "unit written, reloading systemd"
systemctl daemon-reload 2>>"$LOG"
systemctl start sshd-mod.service 2>>"$LOG"
log "systemctl start requested, result=$?"

log "=== boot-activate.sh done ==="
