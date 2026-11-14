import sys
from pathlib import Path

from PySide6.QtWidgets import QApplication

from app.assets import asset_path
from app.logo import PrimeUnlimitedLogo


def application():
    return QApplication.instance() or QApplication([])


def test_source_asset_resolves_existing_repository_logo():
    path = asset_path("prime-unlimited-os-brand.png")
    assert path.name == "prime-unlimited-os-brand.png"
    assert path.is_file()


def test_packaged_asset_uses_pyinstaller_bundle_root(tmp_path: Path, monkeypatch):
    asset = tmp_path / "assets" / "prime-unlimited-os-brand.png"
    asset.parent.mkdir()
    asset.write_bytes(b"official-png-fixture")
    monkeypatch.setattr(sys, "_MEIPASS", str(tmp_path), raising=False)
    assert asset_path("prime-unlimited-os-brand.png") == asset


def test_logo_widget_uses_official_png_dimensions_and_missing_asset_falls_back(tmp_path: Path):
    app = application()
    logo = PrimeUnlimitedLogo()
    missing = PrimeUnlimitedLogo(tmp_path / "missing.png")
    logo.resize(600, 120)
    missing.resize(300, 80)
    logo.show()
    missing.show()
    app.processEvents()
    assert logo.has_logo
    assert (logo.pixmap.width(), logo.pixmap.height()) == (2560, 1280)
    assert not missing.has_logo
    assert not missing.grab().isNull()
    logo.close()
    missing.close()
