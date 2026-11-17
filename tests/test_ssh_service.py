from pathlib import Path

import paramiko
import pytest

from core.ssh_service import SshService, SshVerificationError, sha256_fingerprint


class FakeStream:
    def __init__(self, text: str) -> None:
        self._data = text.encode("utf-8")

    def read(self) -> bytes:
        return self._data


class FakeSshClient:
    """Stands in for paramiko.SSHClient in tests that exercise a command
    run over an already-connected client, without a real network call."""

    def __init__(self, stdout: str, stderr: str = "") -> None:
        self._stdout = stdout
        self._stderr = stderr
        self.closed = False
        self.commands: list[str] = []

    def exec_command(self, command: str, timeout: float | None = None):
        self.commands.append(command)
        return None, FakeStream(self._stdout), FakeStream(self._stderr)

    def close(self) -> None:
        self.closed = True


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


def test_query_data_partition_size_looks_up_the_device_live(tmp_path: Path, monkeypatch):
    """Must look up /data's backing device via /proc/mounts and its size
    via /proc/partitions rather than assume a fixed name/size - the whole
    point of querying this live over SSH instead of hardcoding a
    partition name or size that could differ on another hardware
    revision. Confirmed live against real hardware: a 21812719 KB
    (~20.8 GB) /data partition reported as 229.9M by df (the filesystem
    was only ever formatted that small; the underlying partition is much
    bigger) - resize2fs isn't available on this device to grow it in
    place, so the real size is used to build and reflash a bigger image."""
    fake = FakeSshClient("SIZE_KB=21812719\n")
    service = SshService()
    monkeypatch.setattr(service, "_connect", lambda *a, **k: fake)

    size_bytes = service.query_data_partition_size(
        "192.0.2.10", 2222, tmp_path / "key", "pw", tmp_path / "known_hosts"
    )

    assert size_bytes == 21812719 * 1024
    assert fake.closed is True
    (command,) = fake.commands
    assert "/proc/mounts" in command
    assert "/proc/partitions" in command


def test_query_data_partition_size_raises_when_data_is_not_mounted(tmp_path: Path, monkeypatch):
    fake = FakeSshClient("NO_DATA_MOUNT\n")
    service = SshService()
    monkeypatch.setattr(service, "_connect", lambda *a, **k: fake)

    with pytest.raises(SshVerificationError, match="block device"):
        service.query_data_partition_size("192.0.2.10", 2222, tmp_path / "key", "pw", tmp_path / "known_hosts")


def test_query_data_partition_size_raises_when_output_is_unparseable(tmp_path: Path, monkeypatch):
    fake = FakeSshClient("something unexpected\n")
    service = SshService()
    monkeypatch.setattr(service, "_connect", lambda *a, **k: fake)

    with pytest.raises(SshVerificationError, match="parse"):
        service.query_data_partition_size("192.0.2.10", 2222, tmp_path / "key", "pw", tmp_path / "known_hosts")
