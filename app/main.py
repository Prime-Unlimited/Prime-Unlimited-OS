"""Prime-Unlimited OS Qt entry point."""

from __future__ import annotations

import sys
from pathlib import Path

from PySide6.QtCore import QCoreApplication, QSettings
from PySide6.QtGui import QFontDatabase
from PySide6.QtWidgets import QApplication

from app.theme import STYLESHEET
from app.window import MainWindow

FONTS_DIR = Path(__file__).resolve().parent.parent / "assets" / "fonts"


def _load_bundled_fonts() -> None:
    for font_file in ("ArchivoNarrow.ttf", "IBMPlexMono-Regular.ttf", "IBMPlexMono-Medium.ttf"):
        QFontDatabase.addApplicationFont(str(FONTS_DIR / font_file))


def build_application(argv: list[str] | None = None) -> QApplication:
    QCoreApplication.setOrganizationName("Prime-Unlimited")
    QCoreApplication.setApplicationName("Prime-Unlimited OS")
    app = QApplication(argv or sys.argv)
    _load_bundled_fonts()
    app.setStyle("Fusion")
    app.setStyleSheet(STYLESHEET)
    return app


def main() -> int:
    app = build_application()
    window = MainWindow(QSettings())
    window.show()
    return app.exec()


if __name__ == "__main__":
    raise SystemExit(main())
