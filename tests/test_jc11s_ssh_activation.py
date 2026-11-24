"""Guard against regressions in the hardware-verified JC11S SSH bootstrap."""

from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]


def test_jc11s_ssh_starts_directly_without_systemd_unit():
    script = (ROOT / "modloader/device/jc11s/ui-hook/activate.sh").read_text()
    image_builder = (ROOT / "core/jc11s_image.py").read_text()
    assert 'exec /usr/sbin/sshd -D -e -f "$BASE/sshd_config"' in script
    assert '/usr/sbin/sshd -t -f "$BASE/sshd_config"' in script
    assert 'activation.log' in script
    assert 'bootstrap-first-start.sh' in script
    assert 'prime-unlimited-modloader-api' in script
    assert 'modloader_payload' in image_builder
    assert 'chown -R root:root "$PU/ui" "$PU/catalog" "$PU/trust"' in script
    assert 'chmod 755 "$PU/ui" "$PU/ui/patches" "$PU/ui/splash"' in script
    assert 'chmod 700 "$PU/catalog" "$PU/trust"' in script


def test_jc11s_image_installs_both_alsa_hook_paths():
    from core import jc11s_image

    source = Path(jc11s_image.__file__).read_text()
    assert '"system/var/overlay/lib/alsa/conf.d/99-prime-ssh.conf"' in source
    assert '"system/var-lib/alsa/conf.d/99-prime-ssh.conf"' in source


def test_jc11s_ssh_config_avoids_unsupported_pam_option():
    config = (ROOT / "bootstrap/jc11s/sshd_config").read_text()
    assert "UsePAM" not in config
    assert "PubkeyAuthentication yes" in config
    assert "PasswordAuthentication no" in config
    assert "PermitRootLogin prohibit-password" in config


def test_installer_guards_completed_steps_and_uses_validated_client_key():
    source = (ROOT / "app/jc11s_window.py").read_text()
    assert "and not self._flash_succeeded" in source
    assert "if self._flash_succeeded or self._critical_write_active or self._write_status_unknown:" in source
    assert "self.flash_button.setVisible(False)" in source
    assert "self.prepare_button.setEnabled(False)" in source
    assert "if self.artifacts is not None or not self.prepare_button.isEnabled():" in source
    assert "self.generate_key_button.setEnabled(False)" in source
    assert "self.validate_key_button.setEnabled(False)" in source
    assert "self._validated_public_key = pair.public_key.read_bytes()" in source
    assert "self._key_passphrase" in source
    assert "validate_keypair(" in source


def test_changed_host_key_requires_explicit_approval():
    source = (ROOT / "app/jc11s_window.py").read_text()
    assert "SSH host key changed" in source
    assert "Verify the device and fingerprint independently" in source
    assert "QMessageBox.StandardButton.No" in source
    assert "enroll_host_key(host, 22, expected, known_hosts)" in source


def test_jc11s_image_precreates_connman_storage_on_data():
    source = (ROOT / "core/jc11s_image.py").read_text()
    assert "system/var-lib/connman" in source
    assert "system/var/overlay/lib/connman" in source
    assert "mkdir -p /var/lib/connman" not in source
