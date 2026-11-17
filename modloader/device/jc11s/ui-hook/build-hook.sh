#!/bin/sh
set -eu
HERE=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
OUT=${1:-"$HERE/build"}
mkdir -p "$OUT"
clang --target=armv7a-linux-androideabi24 -shared -fPIC -Os -nostdlib \
    -Wl,-soname,libprime-ui-hook.so \
    -o "$OUT/libprime-ui-hook.so" "$HERE/prime_ui_hook.c"
readelf -h "$OUT/libprime-ui-hook.so" | grep -q 'Class:.*ELF32'
readelf -h "$OUT/libprime-ui-hook.so" | grep -q 'Machine:.*ARM'
printf '%s\n' "built $OUT/libprime-ui-hook.so"
