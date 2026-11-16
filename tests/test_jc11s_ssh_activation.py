"""Guard against regressions in the JC11S SSH activation recipe.

The historical EngineX 5.3.0.2 hardware report confirmed an independent
systemd unit; stock sshd.service is not guaranteed to exist.
"""
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]


def test_jc11s_ssh_starts_independent_systemd_unit():
    script = (ROOT / "bootstrap/jc11s/activate.sh").read_text()
    assert "prime-sshd.service" in script
    assert "systemctl start --no-block prime-sshd.service" in script
    assert "systemctl restart sshd.service" not in script
    assert 'activation.log' in script
    assert "ExecStartPre=/usr/sbin/sshd -t" in script


def test_jc11s_ssh_config_avoids_unsupported_pam_option():
    config = (ROOT / "bootstrap/jc11s/sshd_config").read_text()
    assert "UsePAM" not in config
    assert "PubkeyAuthentication yes" in config
    assert "PasswordAuthentication no" in config
    assert "PermitRootLogin prohibit-password" in config
