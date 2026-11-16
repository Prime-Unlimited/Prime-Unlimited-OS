#!/bin/sh
# Hardware-observed JC11S EngineX 5.3.0.2 boot activation.
# SSH listening on port 22 was confirmed on JC11S after flashing this payload.
set -eu
dest="${SC5000_CUSTOM:-/data/sc5000-custom}"
bundle_dir="$dest/bundle"
bin_dir="$bundle_dir/bin"
lib_hook="$bundle_dir/lib/libengine-ui-hook.so"
mkdir -p /run/systemd/system/engine.service.d
cat >/run/systemd/system/engine.service.d/99-sc5000.conf <<UNIT
[Service]
Environment="LD_PRELOAD=$lib_hook"
UNIT
if [ -f "$bundle_dir/version.txt" ]; then
    mkdir -p /run/sc5000
    if [ ! -f /etc/sc5000/version.txt ] || ! cmp -s "$bundle_dir/version.txt" /etc/sc5000/version.txt; then
        cp "$bundle_dir/version.txt" /run/sc5000/version.txt
        if [ -f /etc/sc5000/version.txt ]; then
            mount --bind /run/sc5000/version.txt /etc/sc5000/version.txt 2>/dev/null || :
        fi
    fi
fi
for applet in stream-sync rb-bridge sc5000-deemixd; do
    args=""
    [ "$applet" != rb-bridge ] || args="--prolink"
    cat >"/run/systemd/system/enginex-$applet.service" <<UNIT
[Unit]
Description=EngineX $applet
[Service]
Type=simple
ExecStart=$bin_dir/$applet $args
Restart=on-failure
RestartSec=5
UNIT
done
ssh_dir="$dest/ssh"
mkdir -p "$ssh_dir" /run/sshd
chmod 700 "$ssh_dir"
if [ ! -f "$ssh_dir/ssh_host_ed25519_key" ]; then
    ssh-keygen -t ed25519 -N '' -f "$ssh_dir/ssh_host_ed25519_key"
fi
cat >"$ssh_dir/sshd_config" <<SSHCONF
Port 22
HostKey $ssh_dir/ssh_host_ed25519_key
AuthorizedKeysFile $dest/authorized_keys
PermitRootLogin prohibit-password
PubkeyAuthentication yes
PasswordAuthentication no
KbdInteractiveAuthentication no
PermitEmptyPasswords no
StrictModes yes
PidFile /run/enginex-sshd.pid
Subsystem sftp internal-sftp
SSHCONF
cat >/run/systemd/system/enginex-sshd.service <<UNIT
[Unit]
Description=EngineX key-only SSH
[Service]
Type=simple
ExecStartPre=/usr/sbin/sshd -t -f $ssh_dir/sshd_config
ExecStart=/usr/sbin/sshd -D -e -f $ssh_dir/sshd_config
Restart=on-failure
RestartSec=5
UNIT
systemctl daemon-reload
systemctl start --no-block \
    enginex-stream-sync.service \
    enginex-rb-bridge.service \
    enginex-sc5000-deemixd.service \
    enginex-sshd.service
systemctl restart engine.service || :
