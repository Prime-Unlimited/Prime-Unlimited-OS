from pathlib import Path

import paramiko
import pytest

from core.ssh_service import SshService, SshVerificationError, sha256_fingerprint


def test_host_key_enrollment_pins_confirmed_fingerprint(tmp_path: Path, monkeypatch):
    key = paramiko.RSAKey.generate(1024)
    service = SshService()
    monkeypatch.setattr(service, "_remote_key", lambda host, port, timeout: key)
    known_hosts = tmp_path / "ssh" / "known_hosts"

    identity = service.enroll_host_key("192.0.2.10", 2222, sha256_fingerprint(key), known_hosts)

    loaded = paramiko.HostKeys(str(known_hosts))
    assert identity.fingerprint.startswith("SHA256:")
    assert loaded.lookup("[192.0.2.10]:2222") is not None


def test_host_key_enrollment_rejects_changed_key(tmp_path: Path, monkeypatch):
    key = paramiko.RSAKey.generate(1024)
    service = SshService()
    monkeypatch.setattr(service, "_remote_key", lambda host, port, timeout: key)

    with pytest.raises(SshVerificationError, match="changed"):
        service.enroll_host_key("192.0.2.10", 22, "SHA256:not-the-key", tmp_path / "known_hosts")
