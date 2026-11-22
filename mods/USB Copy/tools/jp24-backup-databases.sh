#!/bin/sh
# Offline database-only backup; never restores over the live library.
set -eu
SRC='/media/Denon/Engine Library/Database2'
[ -d "$SRC" ]
if systemctl is-active --quiet engine.service; then
    echo 'Engine must be stopped' >&2; exit 1
fi
for p in /proc/[0-9]*/exe; do
    exe=$(readlink "$p" 2>/dev/null || true)
    [ "$exe" != /usr/Engine/Engine ] || exit 1
done
BACKUP="/data/prime-unlimited/backups/usb-copy/internal-$(date -u +%Y%m%dT%H%M%SZ)"
umask 077
mkdir -p "$BACKUP/files" "$BACKUP/restore-rehearsal"
: > "$BACKUP/SHA256SUMS"
count=0
for path in "$SRC"/*.db "$SRC"/*.db-journal "$SRC"/*.db-wal "$SRC"/*.db-shm; do
    [ -f "$path" ] || continue
    [ ! -L "$path" ] || exit 1
    name=${path##*/}
    cp -p "$path" "$BACKUP/files/$name"
    source_hash=$(sha256sum "$path"); source_hash=${source_hash%% *}
    copy_hash=$(sha256sum "$BACKUP/files/$name"); copy_hash=${copy_hash%% *}
    [ "$source_hash" = "$copy_hash" ] || exit 1
    printf '%s  %s\n' "$copy_hash" "$name" >> "$BACKUP/SHA256SUMS"
    cp -p "$BACKUP/files/$name" "$BACKUP/restore-rehearsal/$name"
    count=$((count + 1))
done
[ "$count" -ge 4 ]
(cd "$BACKUP/files" && sha256sum -c ../SHA256SUMS)
(cd "$BACKUP/restore-rehearsal" && sha256sum -c ../SHA256SUMS)
sync
printf 'VERIFIED_FILES=%s\nBACKUP=%s\n' "$count" "$BACKUP"
printf 'verified offline backup and restore-to-separate-directory\n' > "$BACKUP/COMPLETE"
