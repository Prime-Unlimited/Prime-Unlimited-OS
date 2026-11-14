"""Official PNG branding widget with high-quality scaling and a text fallback."""

from __future__ import annotations

from pathlib import Path

from PySide6.QtCore import QRectF, QSize, Qt
from PySide6.QtGui import QColor, QFont, QPainter, QPixmap
from PySide6.QtWidgets import QSizePolicy, QWidget

from app.assets import asset_path


class PrimeUnlimitedLogo(QWidget):
    def __init__(self, logo_path: Path | None = None, compact: bool = False) -> None:
        super().__init__()
        self.compact = compact
        self.logo_path = logo_path or asset_path("prime-unlimited-os-brand.png")
        self.pixmap = QPixmap(str(self.logo_path))
        self.has_logo = self.logo_path.is_file() and not self.pixmap.isNull()
        self.setSizePolicy(QSizePolicy.Policy.Expanding, QSizePolicy.Policy.Preferred)
        self.setMinimumHeight(82 if compact else 150)
        self.setMaximumHeight(110 if compact else 220)
        self.setAccessibleName("Prime-Unlimited OS logo")

    def sizeHint(self) -> QSize:  # noqa: N802
        return QSize(220, 110) if self.compact else QSize(440, 220)

    def paintEvent(self, event) -> None:  # noqa: N802
        painter = QPainter(self)
        painter.setRenderHint(QPainter.RenderHint.Antialiasing)
        painter.setRenderHint(QPainter.RenderHint.SmoothPixmapTransform)
        if self.has_logo and self.pixmap.width() > 0 and self.pixmap.height() > 0:
            scale = min(self.width() / self.pixmap.width(), self.height() / self.pixmap.height())
            width = self.pixmap.width() * scale
            height = self.pixmap.height() * scale
            target = QRectF((self.width() - width) / 2, (self.height() - height) / 2, width, height)
            painter.drawPixmap(target, self.pixmap, QRectF(self.pixmap.rect()))
            return
        painter.setPen(QColor("#29d3ff"))
        font = QFont("Segoe UI", 11 if self.compact else 20, QFont.Weight.Bold)
        painter.setFont(font)
        painter.drawText(self.rect(), Qt.AlignmentFlag.AlignCenter, "PRIME-UNLIMITED OS")
