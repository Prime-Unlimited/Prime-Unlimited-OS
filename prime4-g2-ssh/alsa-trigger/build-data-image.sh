#!/usr/bin/env bash
# Build a /data ext4 image with the corrected ALSA hook and a caller-supplied
# SSH public key. This script only builds an image; it does not flash a device.
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd -- "$SCRIPT_DIR/.." && pwd)"
PUBLIC_KEY="${1:-}"
IMAGE="${2:-$SCRIPT_DIR/prime4-data.img}"
SIZE_MB="${DATA_SIZE_MB:-256}"

if [[ -z "$PUBLIC_KEY" || ! -f "$PUBLIC_KEY" ]]; then
    echo "Usage: $0 /path/to/authorized_key.pub [output-image]" >&2
    exit 2
fi
if [[ ! "$SIZE_MB" =~ ^[0-9]+$ ]] || (( SIZE_MB < 32 )); then
    echo "DATA_SIZE_MB must be an integer of at least 32 MiB" >&2
    exit 2
fi

for cmd in mke2fs debugfs; do
    command -v "$cmd" >/dev/null 2>&1 || {
        for dir in /opt/homebrew/opt/e2fsprogs/sbin /opt/homebrew/opt/e2fsprogs/bin /usr/local/opt/e2fsprogs/sbin /usr/local/opt/e2fsprogs/bin; do
            [[ -d "$dir" ]] && PATH="$dir:$PATH"
        done
        export PATH
        command -v "$cmd" >/dev/null 2>&1 || {
            echo "Missing $cmd. On macOS install e2fsprogs: brew install e2fsprogs" >&2
            exit 1
        }
    }
done

for file in "$SCRIPT_DIR/alsa-boot-hook.c" "$SCRIPT_DIR/99-prime4-mod.conf" "$SCRIPT_DIR/boot-activate.sh" "$ROOT_DIR/flash-tool/imbupd_write_mac.c"; do
    [[ -f "$file" ]] || { echo "Missing source file: $file" >&2; exit 1; }
done

TMP_DIR="$(mktemp -d)"
cleanup() { rm -rf "$TMP_DIR"; }
trap cleanup EXIT

HOOK_SO="$TMP_DIR/libalsa-boot-hook.so"
if command -v zig >/dev/null 2>&1; then
    zig cc -target aarch64-linux-musl -shared -fPIC -Os -o "$HOOK_SO" "$SCRIPT_DIR/alsa-boot-hook.c"
elif command -v aarch64-linux-gnu-gcc >/dev/null 2>&1; then
    aarch64-linux-gnu-gcc -shared -fPIC -Os -o "$HOOK_SO" "$SCRIPT_DIR/alsa-boot-hook.c"
else
    echo "Need zig or aarch64-linux-gnu-gcc to build the AArch64 ALSA hook" >&2
    exit 1
fi

cp "$SCRIPT_DIR/99-prime4-mod.conf" "$TMP_DIR/99-prime4-mod.conf"
cp "$SCRIPT_DIR/boot-activate.sh" "$TMP_DIR/boot-activate.sh"
cp "$PUBLIC_KEY" "$TMP_DIR/authorized_keys"
chmod 0644 "$TMP_DIR/99-prime4-mod.conf" "$TMP_DIR/libalsa-boot-hook.so"
chmod 0755 "$TMP_DIR/boot-activate.sh"
chmod 0600 "$TMP_DIR/authorized_keys"

mkdir -p "$(dirname -- "$IMAGE")"
dd if=/dev/zero of="$IMAGE" bs=1m count="$SIZE_MB"
mke2fs -t ext4 -O encrypt -L data -F "$IMAGE" >/dev/null

debugfs -w "$IMAGE" <<'EOF' >/dev/null 2>&1
mkdir system
mkdir system/var-lib
mkdir system/var-lib/alsa
mkdir system/var-lib/alsa/conf.d
mkdir prime4-mod
mkdir prime4-mod/lib
EOF

debugfs -w "$IMAGE" <<EOF >/dev/null 2>&1
write $TMP_DIR/99-prime4-mod.conf system/var-lib/alsa/conf.d/99-prime4-mod.conf
write $HOOK_SO prime4-mod/lib/libalsa-boot-hook.so
write $TMP_DIR/boot-activate.sh prime4-mod/boot-activate.sh
write $TMP_DIR/authorized_keys prime4-mod/authorized_keys
set_inode_field prime4-mod/boot-activate.sh mode 0100755
set_inode_field prime4-mod/lib/libalsa-boot-hook.so mode 0100755
set_inode_field prime4-mod/authorized_keys mode 0100600
EOF

echo "Built image: $IMAGE (${SIZE_MB} MiB)"
echo "This script did not flash the device. The image replaces the /data partition if flashed."
