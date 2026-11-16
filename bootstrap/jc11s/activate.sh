#!/bin/sh
# JC11S boot activation: independent SSH unit, with persistent diagnostics.
set -eu
umask 077
base=/data/prime-ssh
log="$base/activation.log"
[ "$(id -u)" = 0 ]
[ "$(cat /proc/1/comm)" = systemd ]
[ -d "$base" ] || exit 1
exec </dev/null >>"$log" 2>&1
echo "=== prime-ssh activation $(date -u '+%Y-%m-%dT%H:%M:%SZ') ==="
# A lock directory prevents multiple concurrent ALSA configuration hooks.
mkdir /run/prime-ssh.lock 2>/dev/null || exit 0
trap 'rmdir /run/prime-ssh.lock 2>/dev/null || :' EXIT
[ ! -f /run/prime-ssh.ready ] || exit 0
[ -s "$base/authorized_keys" ] || { echo "ERROR: authorized_keys missing"; exit 1; }
mkdir -p "$base/hostkeys" /run/sshd /run/systemd/system
chmod 700 "$base" "$base/hostkeys"
chmod 600 "$base/authorized_keys"
chown -R 0:0 "$base"
if [ ! -s "$base/hostkeys/ssh_host_ed25519_key" ]; then
    /usr/bin/ssh-keygen -q -t ed25519 -N '' -f "$base/hostkeys/ssh_host_ed25519_key"
fi
/usr/sbin/sshd -t -f "$base/sshd_config"
# The stock firmware need not provide an sshd.service unit.
# Match the known-working EngineX approach: create an independent unit in /run.
cat > /run/systemd/system/prime-sshd.service <<'UNIT'
[Unit]
Description=Prime Unlimited JC11S key-only SSH
After=network.target
[Service]
Type=simple
ExecStartPre=/usr/sbin/sshd -t -f /data/prime-ssh/sshd_config
ExecStart=/usr/sbin/sshd -D -e -f /data/prime-ssh/sshd_config
Restart=on-failure
RestartSec=5
UNIT
systemctl daemon-reload
systemctl start --no-block prime-sshd.service
echo "prime-sshd.service start requested"
touch /run/prime-ssh.ready
