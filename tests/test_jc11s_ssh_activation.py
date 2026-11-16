"""Guard against regressions in the hardware-verified JC11S SSH bootstrap."""

from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]


def test_jc11s_ssh_starts_directly_without_systemd_unit():
    script = (ROOT / "bootstrap/jc11s/activate.sh").read_text()
    assert 'exec /usr/sbin/sshd -D -e -f "$BASE/sshd_config"' in script
    assert '/usr/sbin/sshd -t -f "$BASE/sshd_config"' in script
    assert 'activation.log' in script
    assert "systemctl" not in script
    assert "/run/systemd/system" not in script


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
