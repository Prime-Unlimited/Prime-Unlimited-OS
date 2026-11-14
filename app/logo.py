"""High-DPI vector logo widget with a text fallback."""

from __future__ import annotations

from pathlib import Path

from PySide6.QtCore import QRectF, QSize, Qt
from PySide6.QtGui import QColor, QFont, QPainter
from PySide6.QtSvg import QSvgRenderer
from PySide6.QtWidgets import QSizePolicy, QWidget

from app.assets import asset_path


class PrimeUnlimitedLogo(QWidget):
    def __init__(self, logo_path: Path | None = None, compact: bool = False) -> None:
        super().__init__()
        self.compact = compact
        self.logo_path = logo_path or asset_path("prime-unlimited-logo.svg")
        self.renderer = QSvgRenderer(str(self.logo_path))
        self.has_logo = self.logo_path.is_file() and self.renderer.isValid()
        self.setSizePolicy(QSizePolicy.Policy.Expanding, QSizePolicy.Policy.Fixed)
        self.setMinimumHeight(48 if compact else 88)
        self.setAccessibleName("Prime-Unlimited OS logo")

    def sizeHint(self) -> QSize:  # noqa: N802
        return QSize(230, 56) if self.compact else QSize(620, 118)

    def paintEvent(self, event) -> None:  # noqa: N802
        painter = QPainter(self)
        painter.setRenderHint(QPainter.RenderHint.Antialiasing)
        if self.has_logo:
            source = self.renderer.defaultSize()
            if source.width() > 0 and source.height() > 0:
                scale = min(self.width() / source.width(), self.height() / source.height())
                width = source.width() * scale
                height = source.height() * scale
                target = QRectF((self.width() - width) / 2, (self.height() - height) / 2, width, height)
                self.renderer.render(painter, target)
                return
        painter.setPen(QColor("#29d3ff"))
        font = QFont("Segoe UI", 11 if self.compact else 20, QFont.Weight.Bold)
        painter.setFont(font)
        painter.drawText(self.rect(), Qt.AlignmentFlag.AlignCenter, "PRIME-UNLIMITED OS")
