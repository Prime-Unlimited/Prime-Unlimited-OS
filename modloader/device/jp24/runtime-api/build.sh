#!/bin/sh
# Cross-build the shared Mod Loader runtime API for JP24 (aarch64 Linux).
set -eu
HERE=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
SOURCE="$HERE/../../jc11s/runtime-api"
OUTPUT="$HERE/build/prime-unlimited-runtime-api"

command -v go >/dev/null 2>&1 || {
    echo 'Go is required to build the JP24 runtime API' >&2
    exit 1
}
[ -s "$SOURCE/main.go" ] || {
    echo 'shared runtime API source is missing' >&2
    exit 1
}

mkdir -p "$HERE/build"
(
    cd "$SOURCE"
    GOCACHE=${GOCACHE:-$HERE/build/.go-cache} \
    CGO_ENABLED=0 GOOS=linux GOARCH=arm64 GO111MODULE=off GOENV=off \
        go build -trimpath -ldflags='-s -w' -o "$OUTPUT" .
)

file "$OUTPUT"
echo "built $OUTPUT"
