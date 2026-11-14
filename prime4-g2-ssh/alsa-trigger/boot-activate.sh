#!/bin/sh
# Start the custom key-only SSH service once ALSA loads the /data hook.
set -eu

MOD_DIR=/data/prime4-mod
UNIT=/run/systemd/system/prime4-mod-sshd.service

[ -s "$MOD_DIR/authorized_keys" ] || exit 0
[ -r /etc/ssh/ssh_host_ed25519_key ] || {
    logger -t prime4-mod "SSH host key unavailable; not starting sshd"
    exit 1
}

mkdir -p /run/systemd/system
cat > "$UNIT" <<'EOF'
[Unit]
Description=Prime 4 G2 mod SSH (key only, port 2222)
After=network.target

[Service]
Type=simple
ExecStart=/usr/sbin/sshd -D -f /dev/null -o HostKey=/etc/ssh/ssh_host_ed25519_key -o AuthorizedKeysFile=/data/prime4-mod/authorized_keys -o PermitRootLogin=yes -o PubkeyAuthentication=yes -o PasswordAuthentication=no -o KbdInteractiveAuthentication=no -o StrictModes=no -o Port=2222 -o PidFile=/run/prime4-mod-sshd.pid
Restart=on-failure
RestartSec=2

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
systemctl restart prime4-mod-sshd.service
logger -t prime4-mod "Key-only SSH service started on port 2222"
