import json
from pathlib import Path

from core.jc11s_image import TRUST_ROOT, bootstrap_files

ROOT = Path(__file__).resolve().parents[1]


def test_committed_trust_root_contains_public_material_only():
    document = json.loads(TRUST_ROOT.read_text())
    assert set(document) == {"algorithm", "keyId", "publicKey"}
    assert document["algorithm"] == "ed25519"
    assert document["keyId"] == "prime-unlimited-2026-1"
    assert "private" not in TRUST_ROOT.read_text().lower()


def test_jc11s_image_provisions_exact_committed_public_trust_root(tmp_path):
    hook = tmp_path / "hook.so"
    hook.write_bytes(b"test hook")
    payload, mode = bootstrap_files(b"ssh-ed25519 test\n", hook)["prime-unlimited/trust/catalog-key.json"]
    assert payload == TRUST_ROOT.read_bytes()
    assert mode == 0o644


def test_jp24_image_provisions_exact_committed_public_trust_root_as_root():
    script = (ROOT / "devices/jp24/ssh-bootstrap/build-data-image.sh").read_text()
    assert 'TRUST_ROOT="$SCRIPT_DIR/../../../modloader/catalog/trust/prime-unlimited-2026-1.json"' in script
    assert 'cp "$TRUST_ROOT" "$TMP_DIR/catalog-key.json"' in script
    assert "write $TMP_DIR/catalog-key.json prime-unlimited/trust/catalog-key.json" in script
    assert "set_inode_field prime-unlimited/trust/catalog-key.json uid 0" in script
    assert "set_inode_field prime-unlimited/trust/catalog-key.json gid 0" in script
    assert "set_inode_field prime-unlimited/trust/catalog-key.json mode 0100644" in script


def test_both_device_installers_copy_only_the_committed_public_trust_root():
    for device in ("jc11s", "jp24"):
        installer = (ROOT / f"modloader/device/{device}/ui-hook/install-device.sh").read_text()
        assert 'CATALOG_TRUST="$HERE/../../../catalog/trust/prime-unlimited-2026-1.json"' in installer
        assert 'cp "$CATALOG_TRUST" "$PU/trust/catalog-key.json"' in installer
        assert 'chmod 600 "$PU/catalog/catalog.json" "$PU/catalog/catalog.sig.json" "$PU/trust/catalog-key.json"' in installer


def test_frozen_installer_bundles_the_public_trust_root():
    spec = (ROOT / "packaging/prime-unlimited-os.spec").read_text()
    assert '"modloader/catalog/trust"' in spec
