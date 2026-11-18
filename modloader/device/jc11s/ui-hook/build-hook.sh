#!/bin/sh
set -eu
HERE=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
OUT=${1:-"$HERE/build"}
mkdir -p "$OUT"
CC=${CC:-arm-linux-gnueabihf-gcc}
READELF=${READELF:-arm-linux-gnueabihf-readelf}
CFLAGS=${CFLAGS:-}
# CFLAGS is intentionally word-split so CI can select Clang's target triple.
# shellcheck disable=SC2086
"$CC" $CFLAGS -shared -fPIC -Os -nostdlib \
    -Wl,-soname,libprime-ui-hook.so \
    -o "$OUT/libprime-ui-hook.so" "$HERE/prime_ui_hook.c" "$HERE/../../common/splash_resource.c"
"$READELF" -h "$OUT/libprime-ui-hook.so" | grep -q 'Class:.*ELF32'
"$READELF" -h "$OUT/libprime-ui-hook.so" | grep -q 'Machine:.*ARM'
"$READELF" -Ws "$OUT/libprime-ui-hook.so" | grep -q ' ZSTD_decompress$'
if "$READELF" -Ws "$OUT/libprime-ui-hook.so" | grep -q '__aeabi_'; then
    printf '%s\n' 'unexpected ARM runtime helper dependency' >&2
    exit 1
fi
printf '%s\n' "built $OUT/libprime-ui-hook.so"
