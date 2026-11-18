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
systemctl is-active --quiet prime4-mod-sshd.service
systemctl is-active --quiet prime-unlimited-modloader-api.service
grep -F 'ExecStart=/usr/sbin/sshd' /run/systemd/system/prime4-mod-sshd.service
tr '\000' '\n' < "/proc/$P/environ" | grep -F 'LD_PRELOAD=/data/prime-unlimited/lib/libprime-ui-hook.so'
grep -F '/data/prime-unlimited/lib/libprime-ui-hook.so' "/proc/$P/maps"
grep -F 'swap applied ControlCenterTab.qml timestamped' /data/prime-unlimited/logs/ui-hook.log
grep -F 'swap applied Version.qml timestamped' /data/prime-unlimited/logs/ui-hook.log
grep -F 'overlay host injected into EmbeddedMain.qml' /data/prime-unlimited/logs/ui-hook.log
[ -s /data/prime-unlimited/ui/ModLoader.qml ]
[ -s /data/O ]
! grep -Fq '__PRIME_UNLIMITED_AUTH_TOKEN__' /data/prime-unlimited/ui/ModLoader.qml
[ "$(cat /data/prime-unlimited/os-version)" = '0.0.1' ]
grep -F 'version: "0.0.1"' /data/prime-unlimited/os-version.qml
grep -F 'Prime Unlimited OS ' /data/prime-unlimited/ui/patches/Version.qml
! grep -Fq 'Engine OS' /data/prime-unlimited/ui/patches/Version.qml
printf 'verified Engine PID %s with active preload, QML/overlay/About injection, systemd-managed SSH, and runtime API\n' "$P"
