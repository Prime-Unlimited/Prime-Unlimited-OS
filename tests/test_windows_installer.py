from pathlib import Path
from subprocess import CompletedProcess

import pytest

from core.windows_installer import InstallerError, WslImageBuilder


def test_wsl_prerequisites_report_missing_windows(monkeypatch):
    monkeypatch.setattr("core.windows_installer.platform.system", lambda: "Linux")
    result = WslImageBuilder().prerequisites()
    assert not result.ready
    assert result.missing == ("Windows host",)


def test_wsl_prerequisites_list_missing_tool(monkeypatch):
    monkeypatch.setattr("core.windows_installer.platform.system", lambda: "Windows")
    monkeypatch.setattr("core.windows_installer.shutil.which", lambda name: "C:/Windows/wsl.exe")

    def runner(arguments, **kwargs):
        if arguments[-2:] == ["--list", "--quiet"]:
            return CompletedProcess(arguments, 0, "Debian\n", "")
        command = arguments[-1]
        return CompletedProcess(arguments, 1 if command.endswith("debugfs") else 0, "", "missing")

    result = WslImageBuilder(runner=runner).prerequisites()
    assert not result.ready
    assert result.missing == ("debugfs in WSL distribution Debian",)


def test_wsl_builder_stops_before_temp_build_when_prerequisites_missing(monkeypatch, tmp_path: Path):
    monkeypatch.setattr("core.windows_installer.platform.system", lambda: "Linux")
    builder = WslImageBuilder()
    with pytest.raises(InstallerError, match="Windows host"):
        builder.build(None, tmp_path / "key.pub")
