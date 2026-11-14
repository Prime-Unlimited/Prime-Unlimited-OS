"""Prime-Unlimited OS Qt entry point."""

from __future__ import annotations

import sys

from PySide6.QtCore import QCoreApplication, QSettings
from PySide6.QtWidgets import QApplication

from app.theme import STYLESHEET
from app.window import MainWindow


def build_application(argv: list[str] | None = None) -> QApplication:
    QCoreApplication.setOrganizationName("Prime-Unlimited")
    QCoreApplication.setApplicationName("Prime-Unlimited OS")
    app = QApplication(argv or sys.argv)
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
