#!/bin/sh
# Explicitly authorized offline repair, not a production import operation.
set -eu
LIVE='/media/Denon/Engine Library/Database2/m.db'
STAGE='/media/Denon/Engine Library/Database2/m.db.usb-copy-rebuild.tmp'
[ ! -e "$STAGE" ]
systemctl is-active --quiet engine.service
systemctl is-active --quiet prime4-mod-sshd.service
RECOVERY="usb-copy-repair-recovery-$(date +%s)"
systemd-run --quiet --unit="$RECOVERY" --on-active=300s /bin/systemctl start engine.service
BACKUP=''
installed=0
success=0
restore() {
    if [ "$installed" = 1 ] && [ "$success" = 0 ]; then
        systemctl stop engine.service || return 1
        for suffix in -journal -wal -shm; do
            if [ -f "$LIVE$suffix" ]; then cp -p "$LIVE$suffix" "$BACKUP/failed-m.db$suffix"; fi
            if [ -f "$BACKUP/files/m.db$suffix" ]; then
                cp -p "$BACKUP/files/m.db$suffix" "$LIVE$suffix"
            else
                rm -f "$LIVE$suffix"
            fi
        done
        cp -p "$BACKUP/files/m.db" "$STAGE"
        sync
        mv -f "$STAGE" "$LIVE"
        sync
        echo 'ORIGINAL_DATABASE_RESTORED'
    fi
    systemctl start engine.service || true
}
trap restore EXIT
trap 'exit 1' HUP INT TERM
systemctl stop engine.service
sh /tmp/jp24-backup-databases.sh > /tmp/usb-copy-fresh-repair-backup.log
cat /tmp/usb-copy-fresh-repair-backup.log
BACKUP=$(sed -n 's/^BACKUP=//p' /tmp/usb-copy-fresh-repair-backup.log)
case "$BACKUP" in /data/prime-unlimited/backups/usb-copy/internal-*) ;; *) exit 1 ;; esac
/tmp/prime-usb-baseline-check --fresh-backup "$BACKUP"
CANDIDATE=$(cat "$BACKUP/VERIFIED_CANDIDATE")
case "$CANDIDATE" in "$BACKUP"/rebuild-check-*/m.db) ;; *) exit 1 ;; esac
for suffix in -journal -wal; do
    [ ! -s "$LIVE$suffix" ] || { echo 'NONEMPTY_JOURNAL_ABORT'; exit 1; }
done
expected=$(sha256sum "$BACKUP/files/m.db"); expected=${expected%% *}
actual=$(sha256sum "$LIVE"); actual=${actual%% *}
[ "$expected" = "$actual" ]
cp "$CANDIDATE" "$STAGE"
chmod "$(stat -c %a "$LIVE")" "$STAGE"
expected=$(sha256sum "$CANDIDATE"); expected=${expected%% *}
actual=$(sha256sum "$STAGE"); actual=${actual%% *}
[ "$expected" = "$actual" ]
sync
# Mark before rename so an interruption always restores the exact backup.
installed=1
mv -f "$STAGE" "$LIVE"
sync
echo 'VERIFIED_REBUILD_INSTALLED'
systemctl start engine.service
ready=0
for attempt in 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15 16 17 18 19 20; do
    for p in /proc/[0-9]*/exe; do
        exe=$(readlink "$p" 2>/dev/null || true)
        [ "$exe" != /usr/Engine/Engine ] || ready=1
    done
    [ "$ready" = 0 ] || break
    sleep 1
done
[ "$ready" = 1 ]
systemctl is-active --quiet engine.service
systemctl is-active --quiet prime4-mod-sshd.service
success=1
systemctl stop "$RECOVERY.timer"
trap - EXIT HUP INT TERM
echo 'ENGINE_RESTARTED_AFTER_VERIFIED_REBUILD'
