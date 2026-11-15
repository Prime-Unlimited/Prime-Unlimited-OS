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
# it directly from this script's own process tree is the key difference
# from earlier attempts that silently went nowhere: PID 1 forks/execs the
# new unit in its own context, not inheriting whatever constraints apply
# to a descendant of the Engine process.

MOD_DIR=/data/prime4-mod
LOG="$MOD_DIR/debug.log"
UNIT=/run/systemd/system/sshd-mod.service

log() { echo "$(date '+%H:%M:%S') $*" >> "$LOG" 2>/dev/null; }
beacon() { echo "$1" | /usr/bin/nc -w 2 192.168.178.132 9988 2>/dev/null; }

beacon "diag: boot-activate.sh started"
log "=== boot-activate.sh started ==="

if [ ! -s "$MOD_DIR/authorized_keys" ]; then
    beacon "diag: ABORT authorized_keys missing or empty"
    log "ABORT: authorized_keys missing or empty"
    exit 0
fi
beacon "diag: authorized_keys OK"

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

beacon "diag: unit written, reloading systemd"
log "unit written, reloading systemd"
systemctl daemon-reload 2>>"$LOG"
beacon "diag: daemon-reload result=$?"
systemctl start sshd-mod.service 2>>"$LOG"
RC=$?
beacon "diag: systemctl start result=$RC"
log "systemctl start requested, result=$RC"

sleep 1
systemctl is-active sshd-mod.service >/dev/null 2>&1
beacon "diag: sshd-mod is-active=$?"
STATUS="$(systemctl status sshd-mod.service 2>&1 | head -5)"
beacon "diag: status: $STATUS"

log "=== boot-activate.sh done ==="
