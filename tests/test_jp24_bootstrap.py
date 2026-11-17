from pathlib import Path

import core.jp24_bootstrap as jp24_bootstrap
from core.jp24_bootstrap import build_data_image


class FakeResult:
    def __init__(self, returncode: int = 0, stderr: str = "", stdout: str = "") -> None:
        self.returncode = returncode
        self.stderr = stderr
        self.stdout = stdout


class FakeStdout:
    """Feeds `_write_with_live_progress` one character at a time, as its
    real-time `proc.stdout.read(1)` loop expects, then signals EOF."""

    def __init__(self, text: str) -> None:
        self._chars = list(text)

    def read(self, _n: int = 1) -> str:
        if self._chars:
            return self._chars.pop(0)
        return ""


class FakePopen:
    def __init__(self, output: str, returncode: int) -> None:
        self.stdout = FakeStdout(output)
        self._returncode = returncode

    def poll(self):
        return None if self.stdout._chars else self._returncode

    def wait(self) -> int:
        return self._returncode


def test_build_data_image_passes_size_override_via_env(monkeypatch, tmp_path: Path):
    """size_mb must reach the build script as DATA_SIZE_MB, and the
    timeout must scale with it - a multi-gigabyte image genuinely takes
    longer to dd/mke2fs than the script's small default image."""
    captured: dict[str, object] = {}

    def fake_run(command, *, timeout, use_admin=False, env=None):
        captured["timeout"] = timeout
        captured["env"] = env
        return FakeResult(returncode=0)

    monkeypatch.setattr(jp24_bootstrap, "_run", fake_run)

    build_data_image(tmp_path / "key.pub", tmp_path / "out.img", size_mb=20000)

    assert captured["env"]["DATA_SIZE_MB"] == "20000"
    assert captured["timeout"] >= 60.0
    assert captured["timeout"] > 60.0  # scaled up for a large image


def test_build_data_image_uses_script_default_without_size_mb(monkeypatch, tmp_path: Path):
    captured: dict[str, object] = {}

    def fake_run(command, *, timeout, use_admin=False, env=None):
        captured["timeout"] = timeout
        captured["env"] = env
        return FakeResult(returncode=0)

    monkeypatch.setattr(jp24_bootstrap, "_run", fake_run)

    build_data_image(tmp_path / "key.pub", tmp_path / "out.img")

    assert captured["env"] is None
    assert captured["timeout"] == 60.0


def test_write_with_live_progress_reports_percent_and_eta(monkeypatch):
    """The imbupd tool's own `written / total bytes (pct%)` line must reach
    the caller as it's written, not only once the whole write finishes -
    the whole point of streaming instead of a single buffered subprocess
    call for a write that can take minutes on a multi-gigabyte image."""
    output = "5000000 / 10000000 bytes (50%)\n10000000 / 10000000 bytes (100%)\nWrite complete.\n"
    monkeypatch.setattr(jp24_bootstrap.subprocess, "Popen", lambda *a, **k: FakePopen(output, 0))

    reported: list[str] = []
    result = jp24_bootstrap._write_with_live_progress(
        ["imbupd_write_mac", "write", "data", "img.bin"], timeout=30, report=reported.append
    )

    assert result is not None
    assert result.returncode == 0
    assert any("50%" in message for message in reported)
    assert any("100%" in message for message in reported)


def test_write_with_live_progress_falls_back_when_sudo_needs_a_password(monkeypatch):
    """sudo -n fails immediately (no cached ticket) before any progress is
    ever seen - this must signal the caller to use the buffered GUI
    admin-prompt path instead of raising, since nothing failed yet."""
    output = "sudo: a password is required\n"
    monkeypatch.setattr(jp24_bootstrap.subprocess, "Popen", lambda *a, **k: FakePopen(output, 1))

    result = jp24_bootstrap._write_with_live_progress(
        ["imbupd_write_mac", "write", "data", "img.bin"], timeout=30, report=None
    )

    assert result is None
