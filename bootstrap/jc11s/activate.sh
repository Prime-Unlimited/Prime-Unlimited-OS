#!/bin/sh
# JC11S hardware-tested SSH bootstrap: direct sshd, no systemd service.
set -eu
BASE=/data/prime-ssh
LOG="$BASE/activation.log"
[ "$(id -u)" = 0 ] || exit 1
[ -s "$BASE/authorized_keys" ] || exit 1
mkdir -p "$BASE/hostkeys" /run/sshd
chmod 700 "$BASE/hostkeys"
if [ ! -s "$BASE/hostkeys/ssh_host_ed25519_key" ]; then
    /usr/bin/ssh-keygen -q -t ed25519 -N '' -f "$BASE/hostkeys/ssh_host_ed25519_key" >>"$LOG" 2>&1 || exit 1
fi
chmod 600 "$BASE/hostkeys/ssh_host_ed25519_key"
/usr/sbin/sshd -t -f "$BASE/sshd_config" >>"$LOG" 2>&1 || exit 1
printf 'direct sshd start requested\n' >>"$LOG"
exec /usr/sbin/sshd -D -e -f "$BASE/sshd_config" >>"$LOG" 2>&1
