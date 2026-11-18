#!/bin/sh
# JP24 is a 64-bit aarch64 glibc/systemd target (confirmed live: `uname -m`
# -> aarch64, Engine is an ELF64 aarch64 dynamically-linked executable) -
# unlike JC11S's 32-bit Android (Bionic) target, so this builds for
# aarch64-linux-gnu rather than armv7a-linux-androideabi24.
#
# Builds DIAGNOSTIC_ONLY (logging only, no byte patch) unless
# PRIME_UI_HOOK_PATCH=1 is set - the patch path has not yet been verified
# against a real 64-bit Engine process on JP24 hardware.
set -eu
HERE=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
OUT=${1:-"$HERE/build"}
mkdir -p "$OUT"

DEFS=""
if [ "${PRIME_UI_HOOK_PATCH:-0}" != "1" ]; then
    DEFS="-DDIAGNOSTIC_ONLY"
fi

if command -v zig >/dev/null 2>&1; then
    ZIG_GLOBAL_CACHE_DIR=${ZIG_GLOBAL_CACHE_DIR:-$OUT/.zig-global-cache} \
    ZIG_LOCAL_CACHE_DIR=${ZIG_LOCAL_CACHE_DIR:-$OUT/.zig-local-cache} \
    zig cc -target aarch64-linux-gnu.2.17 -shared -fPIC -Os -nostdlib $DEFS \
        -Wl,-soname,libprime-ui-hook.so \
        -o "$OUT/libprime-ui-hook.so" "$HERE/prime_ui_hook.c" "$HERE/../../common/splash_resource.c"
elif command -v clang >/dev/null 2>&1; then
    clang --target=aarch64-linux-gnu -shared -fPIC -Os -nostdlib $DEFS \
        -Wl,-soname,libprime-ui-hook.so \
        -o "$OUT/libprime-ui-hook.so" "$HERE/prime_ui_hook.c" "$HERE/../../common/splash_resource.c"
elif command -v aarch64-linux-gnu-gcc >/dev/null 2>&1; then
    aarch64-linux-gnu-gcc -shared -fPIC -Os -nostdlib $DEFS \
        -Wl,-soname,libprime-ui-hook.so \
        -o "$OUT/libprime-ui-hook.so" "$HERE/prime_ui_hook.c" "$HERE/../../common/splash_resource.c"
else
    echo "Need zig, clang, or aarch64-linux-gnu-gcc to build the aarch64 UI hook" >&2
    exit 1
fi

file "$OUT/libprime-ui-hook.so" | grep -q 'ELF 64-bit'
file "$OUT/libprime-ui-hook.so" | grep -qi 'aarch64'
printf '%s\n' "built $OUT/libprime-ui-hook.so"
