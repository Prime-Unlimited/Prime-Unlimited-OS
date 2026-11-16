"""Regression checks for the JC11S hardware-tested direct SSH bootstrap."""
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]


def test_jc11s_direct_sshd_activation():
    script = (ROOT / "bootstrap/jc11s/activate.sh").read_text()
    assert 'exec /usr/sbin/sshd -D -e -f "$BASE/sshd_config"' in script
    assert "direct sshd start requested" in script
    assert "ssh-keygen -q -t ed25519" in script
    assert "mkdir -p \"$BASE/hostkeys\" /run/sshd" in script
    assert "systemctl" not in script
    assert "prime-sshd.service" not in script


def test_jc11s_alsa_compatibility_symbols():
    source = (ROOT / "bootstrap/jc11s/hook.c").read_text()
    assert "snd_config_hook_prime_ssh_hook(" in source
    assert "_snd_config_hook_prime_ssh_hook_dlsym_config_hook_001" in source
    assert "_prime_ssh_hook_dlsym_config_hook_001" in source


def test_jc11s_image_contains_both_alsa_paths():
    source = (ROOT / "core/jc11s_image.py").read_text()
    assert "system/var/overlay/lib/alsa/conf.d/99-prime-ssh.conf" in source
    assert "system/var-lib/alsa/conf.d/99-prime-ssh.conf" in source


def test_jc11s_ssh_config_avoids_unsupported_pam_option():
    config = (ROOT / "bootstrap/jc11s/sshd_config").read_text()
    assert "UsePAM" not in config
    assert "PubkeyAuthentication yes" in config
    assert "PasswordAuthentication no" in config
    assert "PermitRootLogin prohibit-password" in config
