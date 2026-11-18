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
