#!/bin/sh
# Start the JP24 key-only SSH daemon in its own systemd unit.
#
# The ALSA hook calls this script as soon as it fires. The Mod Loader
# activation calls it again before restarting Engine. A short lock makes
# those concurrent calls idempotent. Keeping sshd in its own unit places it
# outside engine.service's cgroup, so the UI activation restart cannot kill
# SSH.
set -eu

MOD_DIR=/data/prime4-mod
UNIT=/run/systemd/system/prime4-mod-sshd.service
AUTHORIZED_KEYS="$MOD_DIR/authorized_keys"
HOST_KEY=/etc/ssh/ssh_host_ed25519_key
LOCK=/run/prime4-mod-ssh-activation.lock

if ! mkdir "$LOCK" 2>/dev/null; then
    exit 0
fi
trap 'rmdir "$LOCK" 2>/dev/null || true' EXIT HUP INT TERM

[ -s "$AUTHORIZED_KEYS" ] || {
    logger -t prime4-mod "authorized_keys missing; not starting sshd"
    exit 1
}
[ -r "$HOST_KEY" ] || {
    logger -t prime4-mod "SSH host key unavailable; not starting sshd"
    exit 1
}

mkdir -p /var/run/sshd /run/systemd/system

UNIT_TMP="$UNIT.tmp"
cat > "$UNIT_TMP" <<'EOF'
[Unit]
Description=Prime 4 G2 mod SSH (key only, port 2222)
After=network.target
StartLimitIntervalSec=0

[Service]
Type=simple
ExecStart=/usr/sbin/sshd -D -f /dev/null -o HostKey=/etc/ssh/ssh_host_ed25519_key -o AuthorizedKeysFile=/data/prime4-mod/authorized_keys -o PermitRootLogin=yes -o PubkeyAuthentication=yes -o PasswordAuthentication=no -o KbdInteractiveAuthentication=no -o StrictModes=no -o Port=2222 -o PidFile=/run/prime4-mod-sshd.pid
Restart=on-failure
RestartSec=2s

[Install]
WantedBy=multi-user.target
EOF
chmod 0644 "$UNIT_TMP"
mv -f "$UNIT_TMP" "$UNIT"

systemctl daemon-reload
# On an upgrade, the old hook may still have a directly forked sshd holding
# port 2222. Let systemd retry after Engine's scheduled restart stops that
# old process; do not let this expected bind conflict abort Mod Loader setup.
systemctl restart prime4-mod-sshd.service || true
logger -t prime4-mod "systemd-managed SSH service start requested on port 2222"
