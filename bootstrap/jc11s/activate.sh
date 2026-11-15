#!/bin/sh
set -eu
exec </dev/null >/dev/null 2>&1
umask 077
base=/data/prime-ssh
[ "$(id -u)" = 0 ]
[ "$(cat /proc/1/comm)" = systemd ]
# One activation per boot, also serializes concurrent ALSA configuration loads.
mkdir /run/prime-ssh.lock 2>/dev/null || exit 0
trap 'rmdir /run/prime-ssh.lock 2>/dev/null || :' EXIT
[ ! -f /run/prime-ssh.ready ] || exit 0
[ -s "$base/authorized_keys" ]
mkdir -p "$base/hostkeys" /run/sshd /run/systemd/system/sshd.service.d
chmod 700 "$base" "$base/hostkeys"
chmod 600 "$base/authorized_keys"
chown -R 0:0 "$base"
if [ ! -s "$base/hostkeys/ssh_host_ed25519_key" ]; then
    /usr/bin/ssh-keygen -q -t ed25519 -N '' -f "$base/hostkeys/ssh_host_ed25519_key"
fi
# Validate the complete isolated config before changing the stock service.
/usr/sbin/sshd -t -f "$base/sshd_config"
cat > /run/systemd/system/sshd.service.d/99-prime-ssh.conf <<'UNIT'
[Service]
Type=simple
PIDFile=
ExecStartPre=
ExecStart=
ExecStart=/usr/sbin/sshd -D -e -f /data/prime-ssh/sshd_config
ExecReload=
UNIT
systemctl daemon-reload
systemctl restart sshd.service
touch /run/prime-ssh.ready
