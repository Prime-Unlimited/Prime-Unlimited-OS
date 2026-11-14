#!/usr/bin/env bash
# Build the JP24 /data image. This script does not flash a device.
set -euo pipefail

MODULE_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
PUBLIC_KEY="${1:-}"
IMAGE="${2:-$MODULE_DIR/build/prime4-g2-ssh.img}"
SIZE_MB="${DATA_SIZE_MB:-256}"

if [[ -z "$PUBLIC_KEY" || ! -f "$PUBLIC_KEY" ]]; then
    echo "Usage: $0 /path/to/ed25519_key.pub [output-image]" >&2
    exit 2
fi
if ! awk '$1 == "ssh-ed25519" && NF >= 2 { found=1 } END { exit !found }' "$PUBLIC_KEY"; then
    echo "Only a valid-looking OpenSSH Ed25519 public key is accepted." >&2
    exit 2
fi
if [[ ! "$SIZE_MB" =~ ^(64|256)$ ]]; then
    echo "Supported JP24 image profiles are exactly 64 MiB and 256 MiB." >&2
    exit 2
fi
BLOCK_COUNT=$((SIZE_MB * 256)) # 4 KiB ext4 blocks, matching the documented JP24 image recipe

# Homebrew e2fsprogs is commonly keg-only on macOS.
for dir in /opt/homebrew/opt/e2fsprogs/sbin /opt/homebrew/opt/e2fsprogs/bin /usr/local/opt/e2fsprogs/sbin /usr/local/opt/e2fsprogs/bin; do
    [[ ! -d "$dir" ]] || PATH="$dir:$PATH"
done
export PATH

for cmd in mke2fs debugfs e2fsck dumpe2fs; do
    command -v "$cmd" >/dev/null 2>&1 || {
        echo "Missing $cmd; install e2fsprogs (macOS: brew install e2fsprogs)." >&2
        exit 1
    }
done

TMP_DIR="$(mktemp -d /tmp/prime4-jp24.XXXXXX)"
cleanup() { rm -rf "$TMP_DIR"; }
trap cleanup EXIT

HOOK_SO="$TMP_DIR/libalsa-boot-hook.so"
if command -v zig >/dev/null 2>&1; then
    zig cc -target aarch64-linux-musl -shared -fPIC -Os -o "$HOOK_SO" "$MODULE_DIR/payload/alsa-boot-hook.c"
elif command -v aarch64-linux-gnu-gcc >/dev/null 2>&1; then
    aarch64-linux-gnu-gcc -shared -fPIC -Os -o "$HOOK_SO" "$MODULE_DIR/payload/alsa-boot-hook.c"
else
    echo "Need Zig or aarch64-linux-gnu-gcc to cross-compile the JP24 hook." >&2
    exit 1
fi

cp "$MODULE_DIR/payload/99-prime4-mod.conf" "$TMP_DIR/99-prime4-mod.conf"
cp "$MODULE_DIR/payload/boot-activate.sh" "$TMP_DIR/boot-activate.sh"
cp "$PUBLIC_KEY" "$TMP_DIR/authorized_keys"
chmod 0644 "$TMP_DIR/99-prime4-mod.conf" "$TMP_DIR/libalsa-boot-hook.so"
chmod 0755 "$TMP_DIR/boot-activate.sh"
chmod 0600 "$TMP_DIR/authorized_keys"

mkdir -p "$(dirname -- "$IMAGE")"
IMAGE_TMP="${IMAGE}.tmp.$$"
cleanup_image() { rm -f "$IMAGE_TMP"; }
trap cleanup_image EXIT

dd if=/dev/zero of="$IMAGE_TMP" bs=4096 count="$BLOCK_COUNT"
mke2fs -t ext4 -O encrypt -L data -b 4096 -F "$IMAGE_TMP" "$BLOCK_COUNT" >/dev/null
debugfs -w "$IMAGE_TMP" <<'EOF' >/dev/null 2>&1
mkdir system
mkdir system/var-lib
mkdir system/var-lib/alsa
mkdir system/var-lib/alsa/conf.d
mkdir prime4-mod
mkdir prime4-mod/lib
EOF
debugfs -w "$IMAGE_TMP" <<EOF >/dev/null 2>&1
write $TMP_DIR/99-prime4-mod.conf system/var-lib/alsa/conf.d/99-prime4-mod.conf
write $HOOK_SO prime4-mod/lib/libalsa-boot-hook.so
write $TMP_DIR/boot-activate.sh prime4-mod/boot-activate.sh
write $TMP_DIR/authorized_keys prime4-mod/authorized_keys
set_inode_field prime4-mod/boot-activate.sh mode 0100755
set_inode_field prime4-mod/lib/libalsa-boot-hook.so mode 0100644
set_inode_field prime4-mod/authorized_keys mode 0100600
EOF

for image_path in \
    /system/var-lib/alsa/conf.d/99-prime4-mod.conf \
    /prime4-mod/lib/libalsa-boot-hook.so \
    /prime4-mod/boot-activate.sh \
    /prime4-mod/authorized_keys; do
    if ! debugfs -R "stat $image_path" "$IMAGE_TMP" 2>&1 | grep -q "Inode:"; then
        echo "Image validation failed: missing $image_path" >&2
        exit 1
    fi
done

e2fsck -fn "$IMAGE_TMP" >/dev/null
if ! dumpe2fs -h "$IMAGE_TMP" 2>/dev/null | awk -F: '/Filesystem features:/ { if ($2 ~ /encrypt/) ok=1 } END { exit !ok }'; then
    echo "Image validation failed: ext4 encrypt feature is missing." >&2
    exit 1
fi

if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$IMAGE_TMP"
else
    shasum -a 256 "$IMAGE_TMP"
fi
mv -f "$IMAGE_TMP" "$IMAGE"
echo "Built and checked JP24 image: $IMAGE ($SIZE_MB MiB). No device was written."
