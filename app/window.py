"""Main window and functional, fail-closed guided installer."""

from __future__ import annotations

from pathlib import Path

from PySide6.QtCore import QObject, QRunnable, QSettings, Qt, QThreadPool, Signal, Slot
from PySide6.QtGui import QKeySequence, QShortcut
from PySide6.QtWidgets import (
    QFileDialog,
    QFrame,
    QHBoxLayout,
    QLabel,
    QLineEdit,
    QListWidget,
    QMainWindow,
    QMessageBox,
    QPlainTextEdit,
    QPushButton,
    QStackedWidget,
    QVBoxLayout,
    QWidget,
)

from core.device_detection import DetectionState
from core.diagnostic_service import DiagnosticReport, UsbDiagnosticService
from core.diagnostics import redact
from core.keys import KeyPair, KeyValidationError, generate_keypair, validate_keypair
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
            label(
                "Choose the exact model printed on your controller. Detection will validate your choice.",
                "subtitle",
            )
        )
        cards = QHBoxLayout()
        cards.setSpacing(18)
        for profile in PROFILES.values():
            card = DeviceCard(profile)
            card.selected.connect(self.selected)
            cards.addWidget(card)
        layout.addLayout(cards, 1)
        layout.addWidget(
            label(
                "No device is selected automatically. USB discovery never writes to the controller.",
                "muted",
            )
        )


class WorkerSignals(QObject):
    finished = Signal(object)
    failed = Signal(str)


class DiagnosticWorker(QRunnable):
    def __init__(self, service: UsbDiagnosticService, profile_id: ProfileId) -> None:
        super().__init__()
        self.service = service
        self.profile_id = profile_id
        self.signals = WorkerSignals()

    @Slot()
    def run(self) -> None:
        try:
            self.signals.finished.emit(self.service.probe(self.profile_id))
        except Exception as exc:  # GUI boundary: turn unexpected probe failures into diagnostics.
            self.signals.failed.emit(redact(str(exc)))


class RebootWorker(QRunnable):
    def __init__(self, service: UsbDiagnosticService) -> None:
        super().__init__()
        self.service = service
        self.signals = WorkerSignals()

    @Slot()
    def run(self) -> None:
        try:
            self.signals.finished.emit(self.service.reboot_jc11s())
        except Exception as exc:
            self.signals.failed.emit(redact(str(exc)))


class WorkflowPage(QWidget):
    exit_requested = Signal()

    CONNECTION = 0
    SSH_KEY = 1
    INSTALLATION = 2

    def __init__(
        self,
        diagnostic_service: UsbDiagnosticService | None = None,
        thread_pool: QThreadPool | None = None,
    ) -> None:
        super().__init__()
        self.diagnostic_service = diagnostic_service or UsbDiagnosticService()
        self.thread_pool = thread_pool or QThreadPool.globalInstance()
        self.profile: DeviceProfile | None = None
        self.profile_id: ProfileId | None = None
        self.diagnostic_report: DiagnosticReport | None = None
        self.key_pair: KeyPair | None = None
        self._worker: DiagnosticWorker | None = None

        outer = QHBoxLayout(self)
        outer.setContentsMargins(30, 30, 30, 30)
        self.steps = QListWidget()
        self.steps.setFixedWidth(240)
        self.steps.setFocusPolicy(Qt.FocusPolicy.NoFocus)
        outer.addWidget(self.steps)

        panel = QFrame()
        panel.setObjectName("card")
        body = QVBoxLayout(panel)
        body.setContentsMargins(32, 28, 32, 28)
        body.setSpacing(12)
        body.addWidget(label("GUIDED SETUP", "eyebrow"))
        self.page_stack = QStackedWidget()
        self.page_stack.addWidget(self._build_connection_page())
        self.page_stack.addWidget(self._build_key_page())
        self.page_stack.addWidget(self._build_blocked_page())
        body.addWidget(self.page_stack, 1)

        navigation = QHBoxLayout()
        self.back_button = QPushButton("Back")
        self.back_button.clicked.connect(self.go_back)
        self.continue_button = QPushButton("Continue")
        self.continue_button.setObjectName("primary")
        self.continue_button.clicked.connect(self.go_forward)
        navigation.addWidget(self.back_button)
        navigation.addStretch()
        navigation.addWidget(self.continue_button)
        body.addLayout(navigation)
        outer.addWidget(panel, 1)
        self._update_navigation()

    def _build_connection_page(self) -> QWidget:
        page = QWidget()
        layout = QVBoxLayout(page)
        layout.setContentsMargins(0, 0, 0, 0)
        self.connection_title = label("Connect your controller", "title")
        self.connection_instructions = label("", "subtitle")
        layout.addWidget(self.connection_title)
        layout.addWidget(self.connection_instructions)
        self.run_diagnostics_button = QPushButton("Run read-only diagnostics")
        self.run_diagnostics_button.setObjectName("primary")
        self.run_diagnostics_button.clicked.connect(self.run_diagnostics)
        layout.addWidget(self.run_diagnostics_button, 0, Qt.AlignmentFlag.AlignLeft)
        self.reboot_button = QPushButton("Reboot device")
        self.reboot_button.clicked.connect(self.request_reboot)
        self.reboot_button.setVisible(False)
        layout.addWidget(self.reboot_button, 0, Qt.AlignmentFlag.AlignLeft)
        self.diagnostic_status = label("Diagnostics have not run.")
        layout.addWidget(self.diagnostic_status)
        self.diagnostic_log = QPlainTextEdit()
        self.diagnostic_log.setReadOnly(True)
        self.diagnostic_log.setAccessibleName("USB diagnostic log")
        layout.addWidget(self.diagnostic_log, 1)
        layout.addWidget(
            label(
                "Detection is read-only. A matching USB transport is not positive model identity "
                "and never authorizes a write.",
                "muted",
            )
        )
        return page

    def _build_key_page(self) -> QWidget:
        page = QWidget()
        layout = QVBoxLayout(page)
        layout.setContentsMargins(0, 0, 0, 0)
        layout.addWidget(label("Set up mandatory SSH access", "title"))
        layout.addWidget(
            label(
                "Generate a protected Ed25519 key or validate an existing pair. The private key "
                "remains on this computer; only the public key may be placed in a future "
                "validated bootstrap.",
                "subtitle",
            )
        )

        self.private_key_path = QLineEdit()
        self.private_key_path.setPlaceholderText("Private key path")
        self.private_key_path.setAccessibleName("Private key path")
        self.public_key_path = QLineEdit()
        self.public_key_path.setPlaceholderText("Public key path")
        self.public_key_path.setAccessibleName("Public key path")
        layout.addWidget(self.private_key_path)
        layout.addWidget(self.public_key_path)

        browse = QHBoxLayout()
        browse_private = QPushButton("Choose private key")
        browse_private.clicked.connect(self.choose_private_key)
        browse_public = QPushButton("Choose public key")
        browse_public.clicked.connect(self.choose_public_key)
        browse.addWidget(browse_private)
        browse.addWidget(browse_public)
        layout.addLayout(browse)

        self.passphrase = QLineEdit()
        self.passphrase.setEchoMode(QLineEdit.EchoMode.Password)
        self.passphrase.setPlaceholderText("Key passphrase")
        self.passphrase.setAccessibleName("SSH key passphrase")
        self.confirm_passphrase = QLineEdit()
        self.confirm_passphrase.setEchoMode(QLineEdit.EchoMode.Password)
        self.confirm_passphrase.setPlaceholderText("Confirm passphrase for a new key")
        layout.addWidget(self.passphrase)
        layout.addWidget(self.confirm_passphrase)

        actions = QHBoxLayout()
        generate = QPushButton("Generate new key")
        generate.clicked.connect(self.generate_key)
        validate = QPushButton("Validate imported pair")
        validate.clicked.connect(self.validate_imported_key)
        actions.addWidget(generate)
        actions.addWidget(validate)
        layout.addLayout(actions)
        self.key_status = label("SSH key setup is required. There is no skip option.")
        layout.addWidget(self.key_status)
        layout.addStretch()
        return page

    def _build_blocked_page(self) -> QWidget:
        page = QWidget()
        layout = QVBoxLayout(page)
        layout.setContentsMargins(0, 0, 0, 0)
        layout.addWidget(label("Installation safety gate", "title"))
        self.blocker_text = label("")
        self.blocker_text.setStyleSheet("background:#2b2114; color:#ffd18a; border-radius:9px; padding:14px;")
        layout.addWidget(self.blocker_text)
        layout.addWidget(
            label(
                "Your SSH key is ready, but no image preparation or USB write is permitted. "
                "Mod management remains locked until a future validated bootstrap finishes and "
                "an authenticated, host-key-verified SSH session succeeds.",
                "subtitle",
            )
        )
        layout.addWidget(label("No destructive command is implemented or available from this application.", "muted"))
        layout.addStretch()
        return page

    def set_profile(self, profile_id: ProfileId) -> None:
        self.profile_id = profile_id
        self.profile = PROFILES[profile_id]
        self.diagnostic_report = None
        self.key_pair = None
        self.steps.clear()
        for step in (
            "1  USB connection & diagnostics",
            "2  Mandatory SSH key",
            "3  Installation safety gate",
            "4  Authenticated SSH test (locked)",
            "5  Mod manager (locked)",
        ):
            self.steps.addItem(step)
        self.connection_title.setText(f"Connect {self.profile.display_name}")
        self.connection_instructions.setText(
            f"Selected profile: {self.profile.hardware_profile} · {self.profile.architecture}\n"
            f"Expected USB transport: {self.profile.transport}\n\n{self.profile.setup_mode}"
        )
        self.blocker_text.setText(self.profile.blocker)
        self.diagnostic_status.setText("Diagnostics have not run.")
        self.diagnostic_status.setStyleSheet("")
        self.diagnostic_log.clear()
        self.reboot_button.setVisible(False)
        self.private_key_path.clear()
        self.public_key_path.clear()
        self.passphrase.clear()
        self.confirm_passphrase.clear()
        self.key_status.setText("SSH key setup is required. There is no skip option.")
        self.show_page(self.CONNECTION)

    def show_page(self, page: int) -> None:
        self.page_stack.setCurrentIndex(page)
        self.steps.setCurrentRow(page)
        self._update_navigation()

    def run_diagnostics(self) -> None:
        if self.profile_id is None:
            return
        self.run_diagnostics_button.setEnabled(False)
        self.continue_button.setEnabled(False)
        self.diagnostic_status.setText("Running read-only USB diagnostics…")
        self.diagnostic_log.setPlainText("Waiting for USB probes. No write command will run.")
        worker = DiagnosticWorker(self.diagnostic_service, self.profile_id)
        worker.signals.finished.connect(self.handle_diagnostic_report)
        worker.signals.failed.connect(self.handle_diagnostic_failure)
        self._worker = worker
        self.thread_pool.start(worker)

    @Slot(object)
    def handle_diagnostic_report(self, report: DiagnosticReport) -> None:
        self.diagnostic_report = report
        self.run_diagnostics_button.setEnabled(True)
        self.diagnostic_log.setPlainText("\n".join(redact(line) for line in report.log_lines))
        self.diagnostic_status.setText(report.result.message)
        can_reboot = (
            self.profile_id == ProfileId.JC11S and report.reboot_available and report.fastboot_version is not None
        )
        self.reboot_button.setVisible(can_reboot)
        self.reboot_button.setEnabled(can_reboot)
        if report.result.state in {DetectionState.MATCH, DetectionState.UNVERIFIED}:
            self.diagnostic_status.setStyleSheet("color:#ffd18a;")
        else:
            self.diagnostic_status.setStyleSheet("color:#ff7b7b;")
        self._update_navigation()

    @Slot(str)
    def handle_diagnostic_failure(self, message: str) -> None:
        self.diagnostic_report = None
        self.run_diagnostics_button.setEnabled(True)
        self.diagnostic_status.setText("Diagnostics failed. Review the log and try again.")
        self.diagnostic_status.setStyleSheet("color:#ff7b7b;")
        self.diagnostic_log.setPlainText(redact(message))
        self.reboot_button.setVisible(False)
        self._update_navigation()

    def request_reboot(self) -> None:
        if not self.reboot_button.isVisible() or self.diagnostic_report is None:
            return
        answer = QMessageBox.question(
            self,
            "Reboot device",
            "Send the non-flashing Fastboot reboot command now? The USB device is expected to disconnect.",
            QMessageBox.StandardButton.Yes | QMessageBox.StandardButton.No,
            QMessageBox.StandardButton.No,
        )
        if answer != QMessageBox.StandardButton.Yes:
            return
        self.reboot_button.setEnabled(False)
        self.run_diagnostics_button.setEnabled(False)
        self.diagnostic_status.setText("Sending the explicitly confirmed reboot command…")
        worker = RebootWorker(self.diagnostic_service)
        worker.signals.finished.connect(self.handle_reboot_complete)
        worker.signals.failed.connect(self.handle_reboot_failure)
        self._worker = worker
        self.thread_pool.start(worker)

    @Slot(object)
    def handle_reboot_complete(self, message: str) -> None:
        self.run_diagnostics_button.setEnabled(True)
        self.reboot_button.setVisible(False)
        self.diagnostic_report = None
        self.diagnostic_status.setText("Reboot command sent. Reconnect and run diagnostics again.")
        self.diagnostic_log.appendPlainText(redact(str(message)))
        self._update_navigation()

    @Slot(str)
    def handle_reboot_failure(self, message: str) -> None:
        self.run_diagnostics_button.setEnabled(True)
        self.reboot_button.setEnabled(True)
        self.diagnostic_status.setText("The reboot command failed. The device was not modified.")
        self.diagnostic_status.setStyleSheet("color:#ff7b7b;")
        self.diagnostic_log.appendPlainText(redact(message))

    def choose_private_key(self) -> None:
        path, _ = QFileDialog.getOpenFileName(self, "Choose an Ed25519 private key")
        if path:
            self.private_key_path.setText(path)

    def choose_public_key(self) -> None:
        path, _ = QFileDialog.getOpenFileName(self, "Choose an Ed25519 public key")
        if path:
            self.public_key_path.setText(path)

    def generate_key(self) -> None:
        passphrase = self.passphrase.text()
        if passphrase != self.confirm_passphrase.text():
            self._key_error("The passphrase confirmation does not match.")
            return
        directory = QFileDialog.getExistingDirectory(self, "Choose a secure key folder")
        if not directory:
            return
        try:
            pair = generate_keypair(Path(directory), passphrase)
        except (KeyValidationError, FileExistsError, OSError) as exc:
            self._key_error(str(exc))
            return
        self._accept_key_pair(pair)

    def validate_imported_key(self) -> None:
        private_path = Path(self.private_key_path.text())
        public_path = Path(self.public_key_path.text())
        try:
            validate_keypair(private_path, public_path, self.passphrase.text())
        except KeyValidationError as exc:
            self._key_error(str(exc))
            return
        self._accept_key_pair(KeyPair(private_path, public_path))

    def _accept_key_pair(self, pair: KeyPair) -> None:
        self.key_pair = pair
        self.private_key_path.setText(str(pair.private_key))
        self.public_key_path.setText(str(pair.public_key))
        self.passphrase.clear()
        self.confirm_passphrase.clear()
        self.key_status.setText("Ed25519 key pair validated. The private key remains on this computer.")
        self.key_status.setStyleSheet("color:#63e6a6;")
        self._update_navigation()

    def _key_error(self, message: str) -> None:
        self.key_pair = None
        self.key_status.setText(redact(message))
        self.key_status.setStyleSheet("color:#ff7b7b;")
        self._update_navigation()

    def go_back(self) -> None:
        page = self.page_stack.currentIndex()
        if page == self.CONNECTION:
            self.exit_requested.emit()
        else:
            self.show_page(page - 1)

    def go_forward(self) -> None:
        page = self.page_stack.currentIndex()
        if page == self.CONNECTION and self._diagnostics_allow_key_setup():
            self.show_page(self.SSH_KEY)
        elif page == self.SSH_KEY and self.key_pair is not None:
            self.show_page(self.INSTALLATION)
        elif page == self.INSTALLATION:
            QMessageBox.information(
                self,
                "Installation blocked",
                "Production installation remains disabled until the hardware and recovery requirements are validated.",
            )

    def _diagnostics_allow_key_setup(self) -> bool:
        if self.diagnostic_report is None:
            return False
        return self.diagnostic_report.result.state in {
            DetectionState.MATCH,
            DetectionState.UNVERIFIED,
        }

    def _update_navigation(self) -> None:
        page = self.page_stack.currentIndex()
        self.back_button.setText("Choose another device" if page == self.CONNECTION else "Back")
        if page == self.CONNECTION:
            self.continue_button.setText("Continue to SSH key")
            self.continue_button.setEnabled(self._diagnostics_allow_key_setup())
        elif page == self.SSH_KEY:
            self.continue_button.setText("Continue")
            self.continue_button.setEnabled(self.key_pair is not None)
        else:
            self.continue_button.setText("Installation blocked")
            self.continue_button.setEnabled(False)


class MainWindow(QMainWindow):
    def __init__(
        self,
        settings: QSettings,
        diagnostic_service: UsbDiagnosticService | None = None,
        thread_pool: QThreadPool | None = None,
    ) -> None:
        super().__init__()
        self.settings = settings
        self.setWindowTitle("Prime-Unlimited OS")
        self.setMinimumSize(980, 660)
        self.resize(settings.value("window/size", self.size()))
        self.stack = QStackedWidget()
        self.selection = SelectionPage()
        self.workflow = WorkflowPage(diagnostic_service, thread_pool)
        self.selection.selected.connect(self.open_workflow)
        self.workflow.exit_requested.connect(lambda: self.stack.setCurrentWidget(self.selection))
        self.stack.addWidget(self.selection)
        self.stack.addWidget(self.workflow)
        self.setCentralWidget(self.stack)
        QShortcut(
            QKeySequence("Alt+Left"),
            self,
            activated=self.workflow.go_back,
        )

    def open_workflow(self, profile_id: ProfileId) -> None:
        self.workflow.set_profile(profile_id)
        self.stack.setCurrentWidget(self.workflow)

    def closeEvent(self, event) -> None:  # noqa: N802
        self.settings.setValue("window/size", self.size())
        super().closeEvent(event)
