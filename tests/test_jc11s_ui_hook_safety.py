import json
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]


def test_activation_quarantines_only_the_legacy_ui_alsa_hook():
    script = (ROOT / "modloader/device/jc11s/ui-hook/activate.sh").read_text()
    assert "/data/system/var/overlay/lib/alsa/conf.d/99-prime-ui.conf" in script
    assert "/data/system/var-lib/alsa/conf.d/99-prime-ui.conf" in script
    assert "hook_func\\.prime_ui_hook" in script
    assert "/data/prime-unlimited/lib/libprime-ui-hook\\.so" in script
    assert "control-surface-recovery-v1" in script
    assert "99-prime-ssh.conf" not in script


def test_installer_does_not_create_a_ui_alsa_configuration():
    installer = (ROOT / "modloader/device/jc11s/ui-hook/install-device.sh").read_text()
    assert "99-prime-ui.conf" not in installer


def test_overlay_injection_is_fixed_length_and_avoids_division_helper():
    source = (ROOT / "modloader/device/jc11s/ui-hook/prime_ui_hook.c").read_text()
    old = "        PageCacheInfo {\\n            anchors {\\n                right: parent.right\\n                bottom: parent.bottom\\n                margins: 5\\n            }\\n        }"
    new = '        Loader{anchors.fill:parent;source:\\"file:/data/O\\"}'
    assert len(new) <= len(old)
    assert "ZSTD_decompress" in source
    assert "%2147483647" not in source


def test_modloader_tile_is_the_compact_engine_asset():
    asset = (ROOT / "modloader/ui/assets/prime_unlimited_mod_loader.svg").read_text()
    assert 'width="256" height="104" viewBox="0 0 256 104"' in asset
    assert "PRIME UNLIMITED" in asset and "MOD</tspan> LOADER" in asset
    installer = (ROOT / "modloader/device/jc11s/ui-hook/install-device.sh").read_text()
    assert "prime_unlimited_mod_loader.svg" in installer


def test_about_version_uses_separate_prime_unlimited_os_version():
    os_version = (ROOT / "VERSION").read_text().strip()
    modloader_version = json.loads((ROOT / "modloader/VERSION.json").read_text())
    qml = (ROOT / "modloader/device/jc11s/ui-hook/Version.qml").read_text()
    os_version_qml = (ROOT / "os/OSVersion.qml").read_text()

    assert os_version == "0.0.1"
    assert modloader_version["version"] == "0.2.0"
    assert isinstance(modloader_version["revision"], int) and modloader_version["revision"] >= 1
    assert "file:///data/prime-unlimited/os-version.qml" in qml
    assert 'property string version: "0.0.1"' in os_version_qml
    assert 'text: "Prime Unlimited OS " + primeUnlimitedVersion' in qml
    assert "Engine OS" not in qml
    assert "updaterController" not in qml


def test_both_device_installers_provision_os_version_without_touching_engine_version():
    for device in ("jc11s", "jp24"):
        installer = (ROOT / f"modloader/device/{device}/ui-hook/install-device.sh").read_text()
        hook = (ROOT / f"modloader/device/{device}/ui-hook/prime_ui_hook.c").read_text()
        verifier = (ROOT / f"modloader/device/{device}/ui-hook/verify-device.sh").read_text()

        assert 'cp "$OS_VERSION" "$PU/os-version"' in installer
        assert 'cp "$OS_VERSION_QML" "$PU/os-version.qml"' in installer
        assert 'Version.qml")" -le 930' in installer
        assert "ui/patches/Version.qml" in installer
        assert 'same(name,"Version.qml")' in hook
        assert "swap applied Version.qml timestamped" in hook
        assert "swap applied Version.qml timestamped" in verifier
        assert "/tmp/com.denondj.engine.version" not in installer
        assert "/tmp/com.denondj.engine.version" not in hook
