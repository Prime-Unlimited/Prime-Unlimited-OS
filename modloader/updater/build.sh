#!/bin/sh
set -eu
HERE=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
for TARGET in jc11s jp24; do
    OUT="$HERE/build/$TARGET"
    mkdir -p "$OUT"
    case "$TARGET" in jc11s) GOARCH=arm GOARM=7 ;; jp24) GOARCH=arm64 GOARM= ;; esac
    export GOOS=linux GOARCH GOARM CGO_ENABLED=0
    (cd "$HERE" && go build -trimpath -ldflags='-s -w -buildid=' -o "$OUT/prime-unlimited-self-updater" .)
done
echo "built JC11S and JP24 independent updater binaries"
