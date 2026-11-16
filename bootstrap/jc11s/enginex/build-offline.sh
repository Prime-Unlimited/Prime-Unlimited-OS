#!/usr/bin/env bash
set -Eeuo pipefail
# JC11S EngineX 5.3.0.2 /data builder. No flash operation.
# PUBKEY must point to the public half of the installer-generated Ed25519 pair.
BASE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
WORK="${WORK:-$HOME/enginex-prime4plus-offline}"
BUNDLE='PRIME4PLUS-5.3.0.2-EngineX-OS-Data-Bundle.tar.gz'
VERSION='v5.3.0.2'
IMAGE="$WORK/userdata-enginex-ssh.img"
PUBKEY="${PUBKEY:?Set PUBKEY to the installer-generated .pub file}"
SIZE=2413280768
mkdir -p "$WORK"
for tool in curl python3 tar mke2fs debugfs e2fsck sha256sum stat cmp; do
    command -v "$tool" >/dev/null || { echo "Missing tool: $tool" >&2; exit 1; }
done
[[ -f "$PUBKEY" ]] || { echo "Missing public key: $PUBKEY" >&2; exit 1; }
python3 - "$PUBKEY" <<'PY'
from pathlib import Path
import base64,sys
lines=Path(sys.argv[1]).read_text().strip().splitlines()
if len(lines)!=1: raise SystemExit('Expected exactly one public key')
parts=lines[0].split()
if len(parts)<2 or parts[0]!='ssh-ed25519': raise SystemExit('Expected Ed25519 public key')
base64.b64decode(parts[1],validate=True)
PY
cd "$WORK"
[[ ! -e "$IMAGE" ]] || { echo "Image already exists: $IMAGE (not overwriting)" >&2; exit 1; }
url="https://github.com/Cd1s/EngineX-OS/releases/download/$VERSION"
[[ -f "$BUNDLE" ]] || curl -fL --retry 3 -o "$BUNDLE" "$url/$BUNDLE"
[[ -f SHA256SUMS.txt ]] || curl -fL --retry 3 -o SHA256SUMS.txt "$url/SHA256SUMS.txt"
python3 - "$BUNDLE" <<'PY'
from pathlib import Path
import hashlib,sys
name=sys.argv[1]; entries=[]
for line in Path('SHA256SUMS.txt').read_text().splitlines():
    parts=line.split(maxsplit=1)
    if len(parts)==2 and parts[1].lstrip('*')==name: entries.append(parts[0].lower())
if len(entries)!=1 or hashlib.sha256(Path(name).read_bytes()).hexdigest()!=entries[0]:
    raise SystemExit('Release bundle checksum verification FAILED')
print('Release bundle SHA256: OK')
PY
if [[ ! -d "${BUNDLE%.tar.gz}" ]]; then tar -xzf "$BUNDLE"; fi
BUNDLEDIR="${BUNDLE%.tar.gz}"
( cd "$BUNDLEDIR" && sha256sum -c SHA256SUMS )
truncate -s "$SIZE" "$IMAGE"
mke2fs -t ext4 -O encrypt -L az01-data -F "$IMAGE"
python3 "$BUNDLEDIR/host-install-userdata.py" "$IMAGE" --bundle "$BUNDLE"
debugfs -R 'stat /sc5000-custom/bundle/boot-activate.sh' "$IMAGE" 2>&1 | grep -q 'Inode:' || { echo 'Original boot script missing' >&2; exit 1; }
debugfs -R 'stat /sc5000-custom/bundle/lib/libengine-ui-hook.so' "$IMAGE" 2>&1 | grep -q 'Inode:' || { echo 'EngineX hook missing' >&2; exit 1; }
debugfs -w -R "write $PUBKEY /sc5000-custom/authorized_keys" "$IMAGE"
debugfs -w -R 'set_inode_field /sc5000-custom/authorized_keys mode 0100600' "$IMAGE"
debugfs -w -R 'rm /sc5000-custom/bundle/boot-activate.sh' "$IMAGE"
debugfs -w -R "write $BASE/boot-activate.sh /sc5000-custom/bundle/boot-activate.sh" "$IMAGE"
debugfs -w -R 'set_inode_field /sc5000-custom/bundle/boot-activate.sh mode 0100755' "$IMAGE"
TEMP="$(mktemp -d)"
trap 'rm -rf "$TEMP"' EXIT
debugfs -R "dump /sc5000-custom/authorized_keys $TEMP/authorized_keys" "$IMAGE" >/dev/null 2>&1
debugfs -R "dump /sc5000-custom/bundle/boot-activate.sh $TEMP/boot-activate.sh" "$IMAGE" >/dev/null 2>&1
cmp "$PUBKEY" "$TEMP/authorized_keys"
cmp "$BASE/boot-activate.sh" "$TEMP/boot-activate.sh"
debugfs -R 'stat /sc5000-custom/authorized_keys' "$IMAGE" 2>&1 | grep -Eq 'Mode: +0600'
debugfs -R 'stat /sc5000-custom/bundle/boot-activate.sh' "$IMAGE" 2>&1 | grep -Eq 'Mode: +0755'
[[ "$(stat -c %s "$IMAGE")" == "$SIZE" ]] || { echo 'Image size mismatch' >&2; exit 1; }
set +e
e2fsck -fn "$IMAGE" | tee "$WORK/e2fsck.txt"
fsck_rc=${PIPESTATUS[0]}
set -e
[[ "$fsck_rc" == 0 ]] || { echo "e2fsck exit=$fsck_rc; do not flash" >&2; exit 1; }
sha256sum "$IMAGE" > "$IMAGE.sha256"
echo "BUILD VERIFIED: $IMAGE"
echo 'NOT FLASHED. Verify JC11S, Engine OS 5.3.0 and partition-size:data first.'
