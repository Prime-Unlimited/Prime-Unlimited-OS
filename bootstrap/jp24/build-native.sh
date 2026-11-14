#!/usr/bin/env bash
# Compile the native imbupd probe/writer. Build only; this never opens USB.
set -euo pipefail

MODULE_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
command -v cc >/dev/null 2>&1 || { echo "A C compiler is required." >&2; exit 1; }
command -v pkg-config >/dev/null 2>&1 || { echo "pkg-config is required." >&2; exit 1; }
pkg-config --exists libusb-1.0 || {
    echo "libusb-1.0 development files are required (macOS: brew install libusb)." >&2
    exit 1
}

mkdir -p "$MODULE_DIR/build"
cc -std=c11 -O2 -Wall -Wextra \
    $(pkg-config --cflags libusb-1.0) \
    -o "$MODULE_DIR/build/imbupd_write_mac" \
    "$MODULE_DIR/native/imbupd_write_mac.c" \
    $(pkg-config --libs libusb-1.0)
echo "Built $MODULE_DIR/build/imbupd_write_mac. No device was opened."
