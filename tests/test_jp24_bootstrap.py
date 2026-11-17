from pathlib import Path

import core.jp24_bootstrap as jp24_bootstrap
from core.jp24_bootstrap import build_data_image


class FakeResult:
    def __init__(self, returncode: int = 0, stderr: str = "", stdout: str = "") -> None:
        self.returncode = returncode
        self.stderr = stderr
        self.stdout = stdout


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
