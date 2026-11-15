import subprocess

import pytest

from core import processes


def test_hidden_windows_process_options(monkeypatch):
    class StartupInfo:
        dwFlags = 0
        wShowWindow = None

    monkeypatch.setattr(processes.subprocess, "STARTUPINFO", StartupInfo, raising=False)
    monkeypatch.setattr(processes.subprocess, "STARTF_USESHOWWINDOW", 1, raising=False)
    monkeypatch.setattr(processes.subprocess, "SW_HIDE", 0, raising=False)
    monkeypatch.setattr(processes.subprocess, "CREATE_NO_WINDOW", 0x08000000, raising=False)
    options = processes.hidden_process_options("nt")
    assert options["creationflags"] == 0x08000000
    assert options["startupinfo"].dwFlags & 1
    assert options["startupinfo"].wShowWindow == 0


def test_run_process_preserves_output_and_hidden_options(monkeypatch):
    captured = {}

    def fake_run(command, **kwargs):
        captured.update(command=command, kwargs=kwargs)
        return subprocess.CompletedProcess(command, 7, "out", "error")

    monkeypatch.setattr(processes, "hidden_process_options", lambda: {"creationflags": 123})
    monkeypatch.setattr(processes.subprocess, "run", fake_run)
    result = processes.run_process(["tool", "arg"], capture_output=True, text=True, timeout=4)
    assert result.returncode == 7
    assert result.stderr == "error"
    assert captured["kwargs"]["creationflags"] == 123
    assert captured["kwargs"]["timeout"] == 4


def test_run_process_propagates_timeout(monkeypatch):
    def timed_out(command, **kwargs):
        raise subprocess.TimeoutExpired(command, kwargs["timeout"], stderr="still running")

    monkeypatch.setattr(processes.subprocess, "run", timed_out)
    with pytest.raises(subprocess.TimeoutExpired) as error:
        processes.run_process(["slow-tool"], timeout=1, capture_output=True)
    assert error.value.stderr == "still running"
