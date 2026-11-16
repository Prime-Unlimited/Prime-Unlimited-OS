"""Cross-platform subprocess execution without visible Windows consoles."""

from __future__ import annotations

import os
import subprocess
from collections.abc import Sequence
from typing import Any


def hidden_process_options(platform: str | None = None) -> dict[str, Any]:
    """Return Windows-only flags that suppress child console windows."""
    if (platform or os.name) != "nt":
        return {}
    startupinfo = subprocess.STARTUPINFO()
    startupinfo.dwFlags |= subprocess.STARTF_USESHOWWINDOW
    startupinfo.wShowWindow = subprocess.SW_HIDE
    return {
        "creationflags": subprocess.CREATE_NO_WINDOW,
        "startupinfo": startupinfo,
    }


def run_process(command: Sequence[str | os.PathLike[str]], **kwargs) -> subprocess.CompletedProcess:
    """Run a non-interactive child while preserving output, exit status and timeouts."""
    options = hidden_process_options()
    options.update(kwargs)
    return subprocess.run([str(item) for item in command], **options)
