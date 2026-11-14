import sys
from pathlib import Path

from PySide6.QtWidgets import QApplication

from app.assets import asset_path
from app.logo import PrimeUnlimitedLogo


def application():
    return QApplication.instance() or QApplication([])


def test_source_asset_resolves_existing_repository_logo():
    path = asset_path("prime-unlimited-logo.svg")
    assert path.name == "prime-unlimited-logo.svg"
    assert path.is_file()


def test_packaged_asset_uses_pyinstaller_bundle_root(tmp_path: Path, monkeypatch):
    asset = tmp_path / "assets" / "prime-unlimited-logo.svg"
    asset.parent.mkdir()
    asset.write_text("<svg xmlns='http://www.w3.org/2000/svg'/>", encoding="utf-8")
    monkeypatch.setattr(sys, "_MEIPASS", str(tmp_path), raising=False)
    assert asset_path("prime-unlimited-logo.svg") == asset


def test_logo_widget_uses_svg_and_missing_asset_falls_back(tmp_path: Path):
    app = application()
    logo = PrimeUnlimitedLogo()
    missing = PrimeUnlimitedLogo(tmp_path / "missing.svg")
    logo.resize(600, 120)
    missing.resize(300, 80)
    logo.show()
    missing.show()
    app.processEvents()
    assert logo.has_logo
    assert not missing.has_logo
    assert not missing.grab().isNull()
    logo.close()
    missing.close()
