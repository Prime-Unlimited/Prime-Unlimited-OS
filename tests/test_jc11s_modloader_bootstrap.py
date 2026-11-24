import json
from pathlib import Path

import pytest

from core.jc11s_bootstrap import ARCHIVE_ENTRIES, payload

ROOT = Path(__file__).resolve().parents[1]


def test_pinned_bootstrap_is_complete_and_exact():
    files, pin = payload()
    assert pin["device"] == "JC11S"
    assert pin["updatePolicy"] == "installer-bootstrap-major-only"
    assert json.loads(files["prime-unlimited/updater/version.json"][0]) == {
        "version": pin["version"], "revision": pin["revision"]
    }
    assert len(ARCHIVE_ENTRIES) == 8
    assert files["prime-ssh/activate.sh"][1] == 0o700
    assert files["prime-unlimited/ui/splash/SplashScreen.png"][0].startswith(b"\x89PNG\r\n\x1a\n")
    assert files["prime-unlimited/lib/libprime-ui-hook.so"][0].startswith(b"\x7fELF")


def test_tampered_bootstrap_is_rejected(tmp_path):
    archive = ROOT / "bootstrap/jc11s/modloader-bootstrap.zip"
    changed = tmp_path / archive.name
    data = bytearray(archive.read_bytes())
    data[-1] ^= 1
    changed.write_bytes(data)
    with pytest.raises(ValueError, match="SHA-256"):
        payload(changed)


def test_first_start_is_data_only_and_idempotent():
    script = (ROOT / "modloader/device/jc11s/ui-hook/bootstrap-first-start.sh").read_text()
    assert '/data/prime-unlimited' in script
    assert 'bootstrap/ready' in script
    assert 'rm -rf' in script and '.cache/qml' in script
    assert '/dev/mmc' not in script
    assert 'mount -o remount' not in script


def test_activation_fails_closed_before_ui_bootstrap_is_ready():
    script = (ROOT / "modloader/device/jc11s/ui-hook/activate.sh").read_text()
    assert 'UI_READY="$PU/bootstrap/ready"' in script
    assert 'if [ -e "$UI_READY" ] && [ -x "$PU/lib/libprime-ui-hook.so" ]' in script
    assert 'if [ -e "$UI_READY" ] && [ ! -e "$RESTART_FLAG" ]' in script


def test_activation_repairs_cross_platform_ext4_directory_metadata():
    script = (ROOT / "modloader/device/jc11s/ui-hook/activate.sh").read_text()
    assert 'chown -R root:root "$PU/ui" "$PU/catalog" "$PU/trust"' in script
    assert 'chmod 755 "$PU/ui" "$PU/ui/patches" "$PU/ui/splash"' in script
    assert 'chmod 700 "$PU/catalog" "$PU/trust"' in script
