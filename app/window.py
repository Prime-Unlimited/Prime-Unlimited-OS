"""Main window and guided installer pages."""

from __future__ import annotations

from PySide6.QtCore import QSettings, Qt, Signal
from PySide6.QtGui import QKeySequence, QShortcut
from PySide6.QtWidgets import (
    QCheckBox,
    QFrame,
    QHBoxLayout,
    QLabel,
    QListWidget,
    QMainWindow,
    QPlainTextEdit,
    QPushButton,
    QStackedWidget,
    QVBoxLayout,
    QWidget,
)

from core.profiles import PROFILES, DeviceProfile, ProfileId


def label(text: str, object_name: str = "") -> QLabel:
    item = QLabel(text)
    item.setWordWrap(True)
    if object_name:
        item.setObjectName(object_name)
    return item


class DeviceCard(QFrame):
    selected = Signal(object)

    def __init__(self, profile: DeviceProfile) -> None:
        super().__init__()
        self.profile = profile
        self.setObjectName("card")
        self.setCursor(Qt.CursorShape.PointingHandCursor)
        self.setAccessibleName(f"Select {profile.display_name}")
        layout = QVBoxLayout(self)
        layout.setContentsMargins(24, 24, 24, 24)
        layout.setSpacing(10)
        layout.addWidget(label(profile.display_name, "title"))
        layout.addWidget(label(f"Hardware profile  {profile.hardware_profile}", "muted"))
        layout.addWidget(label(f"USB transport  {profile.transport}", "muted"))
        layout.addStretch()
        button = QPushButton("Select device")
        button.setObjectName("primary")
        button.clicked.connect(lambda: self.selected.emit(profile.id))
        layout.addWidget(button)


class SelectionPage(QWidget):
    selected = Signal(object)

    def __init__(self) -> None:
        super().__init__()
        layout = QVBoxLayout(self)
        layout.setContentsMargins(54, 42, 54, 42)
        layout.setSpacing(16)
        layout.addWidget(label("PRIME-UNLIMITED OS", "eyebrow"))
        layout.addWidget(label("Select your device", "title"))
        layout.addWidget(
            label("Choose the exact model printed on your controller. Detection will validate your choice.", "subtitle")
        )
        cards = QHBoxLayout()
        cards.setSpacing(18)
        for profile in PROFILES.values():
            card = DeviceCard(profile)
            card.selected.connect(self.selected)
            cards.addWidget(card)
        layout.addLayout(cards, 1)
        layout.addWidget(
            label("No device is selected automatically. USB discovery never writes to the controller.", "muted")
        )


class WorkflowPage(QWidget):
    back_requested = Signal()

    def __init__(self) -> None:
        super().__init__()
        self.profile: DeviceProfile | None = None
        outer = QHBoxLayout(self)
        outer.setContentsMargins(30, 30, 30, 30)
        self.steps = QListWidget()
        self.steps.setFixedWidth(230)
        self.steps.setFocusPolicy(Qt.FocusPolicy.NoFocus)
        outer.addWidget(self.steps)
        panel = QFrame()
        panel.setObjectName("card")
        body = QVBoxLayout(panel)
        body.setContentsMargins(32, 28, 32, 28)
        self.eyebrow = label("GUIDED SETUP", "eyebrow")
        self.title = label("", "title")
        self.description = label("", "subtitle")
        body.addWidget(self.eyebrow)
        body.addWidget(self.title)
        body.addWidget(self.description)
        body.addSpacing(14)
        self.warning = label("")
        self.warning.setStyleSheet("background:#2b2114; color:#ffd18a; border-radius:9px; padding:14px;")
        body.addWidget(self.warning)
        body.addSpacing(12)
        body.addWidget(label("Installation status", "eyebrow"))
        self.status = label("")
        body.addWidget(self.status)
        self.log = QPlainTextEdit()
        self.log.setReadOnly(True)
        self.log.setMaximumHeight(130)
        self.log.setAccessibleName("Diagnostic log")
        body.addWidget(self.log)
        self.recovery = QCheckBox("I have reviewed the backup and recovery requirements")
        self.recovery.setEnabled(False)
        body.addWidget(self.recovery)
        body.addStretch()
        buttons = QHBoxLayout()
        back = QPushButton("Back")
        back.clicked.connect(self.back_requested)
        self.continue_button = QPushButton("Continue")
        self.continue_button.setObjectName("primary")
        self.continue_button.setEnabled(False)
        buttons.addWidget(back)
        buttons.addStretch()
        buttons.addWidget(self.continue_button)
        body.addLayout(buttons)
        outer.addWidget(panel, 1)

    def set_profile(self, profile_id: ProfileId) -> None:
        self.profile = PROFILES[profile_id]
        profile = self.profile
        self.steps.clear()
        for step in (
            "1  USB connection",
            "2  Device verification",
            "3  Mandatory SSH key",
            "4  Image validation",
            "5  Backup & recovery",
            f"6  {profile.transport} installation",
            "7  Authenticated SSH test",
            "8  Mod manager",
        ):
            self.steps.addItem(step)
        self.steps.setCurrentRow(0)
        self.title.setText(profile.display_name)
        self.description.setText(
            f"{profile.hardware_profile} · {profile.architecture} · {profile.transport}\n\n"
            f"Connection instructions: {profile.setup_mode}"
        )
        self.warning.setText(profile.blocker)
        self.status.setText(
            "Diagnostic mode is available. Production installation is blocked by unverified hardware prerequisites."
        )
        self.log.setPlainText(
            "Ready for read-only diagnostics.\n"
            "No device commands have run.\n"
            "No progress is estimated until an operation reports real progress."
        )


class MainWindow(QMainWindow):
    def __init__(self, settings: QSettings) -> None:
        super().__init__()
        self.settings = settings
        self.setWindowTitle("Prime-Unlimited OS")
        self.setMinimumSize(940, 620)
        self.resize(settings.value("window/size", self.size()))
        self.stack = QStackedWidget()
        self.selection = SelectionPage()
        self.workflow = WorkflowPage()
        self.selection.selected.connect(self.open_workflow)
        self.workflow.back_requested.connect(lambda: self.stack.setCurrentWidget(self.selection))
        self.stack.addWidget(self.selection)
        self.stack.addWidget(self.workflow)
        self.setCentralWidget(self.stack)
        QShortcut(QKeySequence("Alt+Left"), self, activated=lambda: self.stack.setCurrentWidget(self.selection))

    def open_workflow(self, profile_id: ProfileId) -> None:
        self.workflow.set_profile(profile_id)
        self.stack.setCurrentWidget(self.workflow)

    def closeEvent(self, event) -> None:  # noqa: N802
        self.settings.setValue("window/size", self.size())
        super().closeEvent(event)
