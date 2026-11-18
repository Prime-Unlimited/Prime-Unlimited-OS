#!/bin/sh
set -eu
# Run from repository root; arm-linux-gnueabihf-gcc and binutils must be provisioned.
[ "$#" = 1 ] || { echo 'usage: sh tools/build-jc11s-hook.sh OUTPUT.so' >&2; exit 2; }
export SOURCE_DATE_EPOCH=1700000000
"${CC:-arm-linux-gnueabihf-gcc}" -shared -fPIC -nostdlib -nodefaultlibs \
    -fno-stack-protector -fomit-frame-pointer -fno-unwind-tables -fno-asynchronous-unwind-tables \
    -march=armv7-a -mfpu=vfpv3-d16 -marm -Os -Wall -Wextra -Werror \
    -Wl,--build-id=none,-z,noexecstack,-z,relro,-z,now,--no-undefined \
    -o "$1" bootstrap/jc11s/hook.c
