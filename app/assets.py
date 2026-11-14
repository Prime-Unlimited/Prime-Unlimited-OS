"""Asset resolution for source checkouts and PyInstaller bundles."""

from __future__ import annotations

import sys
from pathlib import Path


def asset_path(name: str) -> Path:
    bundle_root = getattr(sys, "_MEIPASS", None)
    root = Path(bundle_root) if bundle_root else Path(__file__).resolve().parents[1]
    return root / "assets" / name
