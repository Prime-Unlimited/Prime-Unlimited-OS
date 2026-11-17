#!/bin/sh
set -eu
P=""
for d in /proc/[0-9]*; do
    [ -r "$d/cmdline" ] || continue
    case "$(tr '\000' ' ' < "$d/cmdline")" in
        *'/usr/Engine/Engine'*) P=${d##*/}; break ;;
    esac
done
[ -n "$P" ] || { echo 'EMain process not found' >&2; exit 1; }
systemctl is-active --quiet engine.service
tr '\000' '\n' < "/proc/$P/environ" | grep -F 'LD_PRELOAD=/data/prime-unlimited/lib/libprime-ui-hook.so'
grep -F '/data/prime-unlimited/lib/libprime-ui-hook.so' "/proc/$P/maps"
grep -F 'swap applied ControlCenterTab.qml timestamped' /data/prime-unlimited/logs/ui-hook.log
printf 'verified Engine PID %s with active preload and QML swap\n' "$P"
