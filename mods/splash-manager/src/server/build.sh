#!/bin/sh
set -eu
cd "$(dirname "$0")"
mkdir -p ../../src/bin

echo "Building armhf (JC11S)..."
CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 go build -trimpath -ldflags='-s -w' -o ../../src/bin/splash-server-armhf .

echo "Building aarch64 (JP24)..."
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags='-s -w' -o ../../src/bin/splash-server-aarch64 .

echo "Done."
ls -lh ../../src/bin/splash-server-*
