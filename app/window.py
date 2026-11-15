"""Main window and functional, fail-closed guided installer."""

from __future__ import annotations

from pathlib import Path

from PySide6.QtCore import QObject, QRunnable, QSettings, QStandardPaths, Qt, QThreadPool, Signal, Slot
from PySide6.QtGui import QKeySequence, QShortcut
from PySide6.QtWidgets import (
    QCheckBox,
    QComboBox,
    QFileDialog,
    QFrame,
    QHBoxLayout,
    QLabel,
    QLineEdit,
    QListWidget,
    QMainWindow,
    QMessageBox,
    QPlainTextEdit,
    QProgressBar,
    QPushButton,
    QStackedWidget,
    QVBoxLayout,
    QWidget,
)

from app.logo import PrimeUnlimitedLogo
from core.device_detection import DetectionState
from core.diagnostic_service import DiagnosticReport, UsbDiagnosticService
from core.diagnostics import redact
from core.keys import KeyPair, KeyValidationError, generate_keypair, validate_keypair
from core.profiles import PROFILES, DeviceProfile, ProfileId
from core.windows_installer import InstallerArtifacts, Jc11sInstallerService


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
        layout.addWidget(label(profile.display_name, "cardTitle"))
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
        layout.setContentsMargins(58, 34, 58, 38)
        layout.setSpacing(14)
        self.logo = PrimeUnlimitedLogo()
        layout.addWidget(self.logo)
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
    def __init__(
        self,
        service: UsbDiagnosticService,
        profile_id: ProfileId,
        selected_device: str | None = None,
    ) -> None:
        super().__init__()
        self.service = service
        self.profile_id = profile_id
        self.selected_device = selected_device
        self.signals = WorkerSignals()

    @Slot()
    def run(self) -> None:
        try:
            self.signals.finished.emit(self.service.probe(self.profile_id, self.selected_device))
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


class TaskWorker(QRunnable):
    def __init__(self, function) -> None:
        super().__init__()
        self.function = function
        self.signals = WorkerSignals()

    @Slot()
    def run(self) -> None:
        try:
            self.signals.finished.emit(self.function())
        except Exception as exc:
            self.signals.failed.emit(redact(str(exc)))


class WorkflowPage(QWidget):
    exit_requested = Signal()

    CONNECTION = 0
    SSH_KEY = 1
    PREPARE = 2
    INSTALLATION = PREPARE
    REVIEW = 3
    FLASH = 4
    SSH_TEST = 5

    def __init__(
        self,
        diagnostic_service: UsbDiagnosticService | None = None,
        thread_pool: QThreadPool | None = None,
        installer_service: Jc11sInstallerService | None = None,
    ) -> None:
        super().__init__()
        self.diagnostic_service = diagnostic_service or UsbDiagnosticService()
        self.thread_pool = thread_pool or QThreadPool.globalInstance()
        self.installer_service = installer_service
        self.profile: DeviceProfile | None = None
        self.profile_id: ProfileId | None = None
        self.diagnostic_report: DiagnosticReport | None = None
        self.key_pair: KeyPair | None = None
        self.artifacts: InstallerArtifacts | None = None
        self._flash_succeeded = False
        self._worker: DiagnosticWorker | None = None

        outer = QHBoxLayout(self)
        outer.setContentsMargins(30, 30, 30, 30)
        sidebar = QWidget()
        sidebar_layout = QVBoxLayout(sidebar)
        sidebar_layout.setContentsMargins(0, 0, 10, 0)
        sidebar_layout.setSpacing(18)
        self.logo = PrimeUnlimitedLogo(compact=True)
        sidebar_layout.addWidget(self.logo)
        self.steps = QListWidget()
        self.steps.setFixedWidth(240)
        self.steps.setFocusPolicy(Qt.FocusPolicy.NoFocus)
        sidebar_layout.addWidget(self.steps, 1)
        outer.addWidget(sidebar)

        panel = QFrame()
        panel.setObjectName("card")
        body = QVBoxLayout(panel)
        body.setContentsMargins(32, 28, 32, 28)
        body.setSpacing(12)
        body.addWidget(label("GUIDED SETUP", "eyebrow"))
        self.page_stack = QStackedWidget()
        self.page_stack.addWidget(self._build_connection_page())
        self.page_stack.addWidget(self._build_key_page())
        self.page_stack.addWidget(self._build_prepare_page())
        self.page_stack.addWidget(self._build_review_page())
        self.page_stack.addWidget(self._build_flash_page())
        self.page_stack.addWidget(self._build_ssh_page())
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
        self.candidate_selector = QComboBox()
        self.candidate_selector.setAccessibleName("Discovered USB candidate")
        self.candidate_selector.setVisible(False)
        self.candidate_selector.currentIndexChanged.connect(self._show_selected_candidate)
        layout.addWidget(self.candidate_selector)
        self.candidate_details = label("", "muted")
        self.candidate_details.setVisible(False)
        layout.addWidget(self.candidate_details)
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

    def _build_prepare_page(self) -> QWidget:
        page = QWidget()
        layout = QVBoxLayout(page)
        layout.setContentsMargins(0, 0, 0, 0)
        layout.addWidget(label("Build this device's SSH image", "title"))
        layout.addWidget(
            label(
                "The installer re-reads the connected device geometry, checks its bundled native build tools, "
                "creates an exact-size ext4 image, converts it to Android sparse format and verifies every sector.",
                "subtitle",
            )
        )
        self.prepare_button = QPushButton("Check prerequisites and build image")
        self.prepare_button.setObjectName("primary")
        self.prepare_button.clicked.connect(self.prepare_image)
        layout.addWidget(self.prepare_button, 0, Qt.AlignmentFlag.AlignLeft)
        self.build_progress = QProgressBar()
        self.build_progress.setRange(0, 100)
        self.build_progress.setValue(0)
        layout.addWidget(self.build_progress)
        self.build_status = label("Image has not been built.")
        layout.addWidget(self.build_status)
        layout.addStretch()
        return page

    def _build_review_page(self) -> QWidget:
        page = QWidget()
        layout = QVBoxLayout(page)
        layout.addWidget(label("Review destructive installation", "title"))
        self.plan_summary = label("")
        layout.addWidget(self.plan_summary)
        warning = label(
            "This operation replaces the complete /data filesystem. Device settings, Wi-Fi configuration, "
            "local library metadata and every other file on that partition will be lost."
        )
        warning.setStyleSheet("background:#3a1717; color:#ffb4b4; border-radius:9px; padding:14px;")
        layout.addWidget(warning)
        self.recovery_confirmation = QCheckBox(
            "I understand that an OFW file alone is not a tested recovery path and recovery must be demonstrated."
        )
        self.data_loss_confirmation = QCheckBox("I understand that the complete /data partition will be overwritten.")
        self.erase_phrase = QLineEdit()
        self.erase_phrase.setPlaceholderText("Type ERASE DATA")
        for widget in (self.recovery_confirmation, self.data_loss_confirmation):
            widget.stateChanged.connect(self._update_navigation)
            layout.addWidget(widget)
        self.erase_phrase.textChanged.connect(self._update_navigation)
        layout.addWidget(self.erase_phrase)
        layout.addStretch()
        return page

    def _build_flash_page(self) -> QWidget:
        page = QWidget()
        layout = QVBoxLayout(page)
        layout.addWidget(label("Hardware flash", "title"))
        self.blocker_text = label("")
        self.blocker_text.setStyleSheet("background:#2b2114; color:#ffd18a; border-radius:9px; padding:14px;")
        layout.addWidget(self.blocker_text)
        self.flash_button = QPushButton("Start confirmed hardware flash")
        self.flash_button.setObjectName("primary")
        self.flash_button.clicked.connect(self.start_flash)
        layout.addWidget(self.flash_button, 0, Qt.AlignmentFlag.AlignLeft)
        self.flash_progress = QProgressBar()
        layout.addWidget(self.flash_progress)
        self.flash_status = label("No write has started.")
        layout.addWidget(self.flash_status)
        layout.addStretch()
        return page

    def _build_ssh_page(self) -> QWidget:
        page = QWidget()
        layout = QVBoxLayout(page)
        layout.addWidget(label("Verify installed SSH access", "title"))
        self.ssh_host = QLineEdit()
        self.ssh_host.setPlaceholderText("Device IP address or hostname")
        self.ssh_passphrase = QLineEdit()
        self.ssh_passphrase.setEchoMode(QLineEdit.EchoMode.Password)
        self.ssh_passphrase.setPlaceholderText("Private-key passphrase")
        self.ssh_fingerprint = QLineEdit()
        self.ssh_fingerprint.setPlaceholderText("Trusted host-key fingerprint (SHA256:…)")
        self.ssh_test_button = QPushButton("Test authenticated SSH")
        self.ssh_test_button.clicked.connect(self.test_ssh)
        self.ssh_status = label("SSH has not been tested.")
        layout.addWidget(self.ssh_host)
        layout.addWidget(self.ssh_passphrase)
        layout.addWidget(self.ssh_fingerprint)
        layout.addWidget(self.ssh_test_button, 0, Qt.AlignmentFlag.AlignLeft)
        layout.addWidget(self.ssh_status)
        layout.addStretch()
        return page

    def set_profile(self, profile_id: ProfileId) -> None:
        self.profile_id = profile_id
        self.profile = PROFILES[profile_id]
        self.diagnostic_report = None
        self.key_pair = None
        if self.artifacts is not None:
            self.artifacts.cleanup()
        self.artifacts = None
        self._flash_succeeded = False
        if profile_id == ProfileId.JC11S and self.installer_service is None:
            fastboot = getattr(self.diagnostic_service, "fastboot", None)
            if fastboot is not None and hasattr(fastboot, "collect_partition_evidence"):
                self.installer_service = Jc11sInstallerService(fastboot)
        self.steps.clear()
        for step in (
            "1  USB connection & diagnostics",
            "2  Mandatory SSH key",
            "3  Device-specific image build",
            "4  Data-loss and recovery review",
            "5  Guarded hardware flash",
            "6  Authenticated SSH test",
        ):
            self.steps.addItem(step)
        self.connection_title.setText(f"Connect {self.profile.display_name}")
        self.connection_instructions.setText(
            f"Selected profile: {self.profile.hardware_profile} · {self.profile.architecture}\n"
            f"Expected USB transport: {self.profile.transport}\n\n{self.profile.setup_mode}"
        )
        self._refresh_flash_gate()
        self.diagnostic_status.setText("Diagnostics have not run.")
        self.diagnostic_status.setStyleSheet("")
        self.diagnostic_log.clear()
        self.candidate_selector.clear()
        self.candidate_selector.setVisible(False)
        self.candidate_details.clear()
        self.candidate_details.setVisible(False)
        self.reboot_button.setVisible(False)
        self.private_key_path.clear()
        self.public_key_path.clear()
        self.passphrase.clear()
        self.confirm_passphrase.clear()
        self.key_status.setText("SSH key setup is required. There is no skip option.")
        self.build_progress.setValue(0)
        self.build_status.setText("Image has not been built.")
        self.plan_summary.clear()
        self.recovery_confirmation.setChecked(False)
        self.data_loss_confirmation.setChecked(False)
        self.erase_phrase.clear()
        self.flash_progress.setValue(0)
        self.flash_status.setText("No write has started.")
        self.ssh_status.setText("SSH has not been tested.")
        self.show_page(self.CONNECTION)

    def _refresh_flash_gate(self) -> None:
        if self.profile_id != ProfileId.JC11S or self.installer_service is None:
            blockers = (self.profile.blocker if self.profile else "Installer service unavailable.",)
        else:
            blockers = self.installer_service.release_policy.blockers
            if (
                self.artifacts is not None
                and len(self.artifacts.plan.transfers) > 1
                and not self.installer_service.release_policy.multipart_sparse_verified
            ):
                blockers += ("Multipart sparse flashing is not verified on the InMusic bootloader",)
        self.blocker_text.setText(
            "Hardware write blocked:\n• " + "\n• ".join(blockers) if blockers else "All release gates satisfied."
        )
        self.flash_button.setEnabled(not blockers and self._review_confirmed())

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
        selected_device = self.candidate_selector.currentData() if self.candidate_selector.isVisible() else None
        worker = DiagnosticWorker(self.diagnostic_service, self.profile_id, selected_device)
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
        self._populate_candidates(report)
        can_reboot = (
            self.profile_id == ProfileId.JC11S and report.reboot_available and report.fastboot_version is not None
        )
        self.reboot_button.setVisible(can_reboot)
        self.reboot_button.setEnabled(can_reboot)
        if report.result.state in {
            DetectionState.MATCH,
            DetectionState.UNVERIFIED,
            DetectionState.CANDIDATE,
        }:
            self.diagnostic_status.setStyleSheet("color:#ffd18a;")
        else:
            self.diagnostic_status.setStyleSheet("color:#ff7b7b;")
        self._update_navigation()

    def _populate_candidates(self, report: DiagnosticReport) -> None:
        previous = self.candidate_selector.currentData()
        displayed_devices = report.candidates or report.discovered_devices
        self.candidate_selector.blockSignals(True)
        self.candidate_selector.clear()
        for candidate in displayed_devices:
            family = ", ".join(item.value.upper() for item in candidate.candidate_families)
            self.candidate_selector.addItem(
                f"{candidate.display_name} — {candidate.vid_pid} — {family or 'unknown family'}",
                candidate.discovery_key,
            )
        if previous is not None:
            index = self.candidate_selector.findData(previous)
            if index >= 0:
                self.candidate_selector.setCurrentIndex(index)
        self.candidate_selector.blockSignals(False)
        visible = bool(displayed_devices)
        self.candidate_selector.setVisible(visible)
        self.candidate_details.setVisible(visible)
        self._show_selected_candidate()

    def _show_selected_candidate(self) -> None:
        if self.diagnostic_report is None:
            return
        key = self.candidate_selector.currentData()
        displayed_devices = self.diagnostic_report.candidates or self.diagnostic_report.discovered_devices
        candidate = next(
            (item for item in displayed_devices if item.discovery_key == key),
            None,
        )
        if candidate is None:
            self.candidate_details.clear()
            return
        transports = ", ".join(item.value for item in candidate.candidate_transports) or "Unverified"
        if candidate.verification.protocol_verified:
            transport_status = f"Protocol verified ({candidate.bootloader or 'version unknown'})"
        elif candidate.verification.transport_detected:
            transport_status = "Transport detected; protocol unverified"
        else:
            transport_status = f"{transports} (inferred; unverified)"
        interface = next(
            (item for item in candidate.interfaces if item.interface_class is not None),
            None,
        )
        interface_text = ""
        if interface is not None:
            descriptor = (
                f"{interface.interface_class:02X}/{interface.interface_subclass:02X}/{interface.interface_protocol:02X}"
            )
            interface_text = f"\nInterface: {descriptor}  ·  Endpoints: {', '.join(interface.endpoints)}"
        product = self.diagnostic_report.fastboot_product
        model_verified = product == "rk3288-az01-jc11s"
        model_text = (
            f"Verified ({product})" if model_verified else (f"Rejected ({product})" if product else "Unverified")
        )
        installation = "Ready for guarded preparation" if model_verified else "Blocked"
        self.candidate_details.setText(
            f"USB detected: Yes  ·  VID/PID: {candidate.vid_pid}  ·  Source: {candidate.source}\n"
            f"Transport: {transport_status}  ·  Model identity: {model_text}{interface_text}\n"
            f"Hardware revision: Unknown  ·  Installation: {installation}"
        )

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

    def prepare_image(self) -> None:
        if self.installer_service is None or self.key_pair is None:
            self.build_status.setText("JC11S installer service and a validated SSH key are required.")
            return
        self.prepare_button.setEnabled(False)
        self.build_progress.setRange(0, 0)
        self.build_status.setText("Re-reading device geometry and building the image natively…")
        identifier = self.candidate_selector.currentData() if self.candidate_selector.isVisible() else None
        worker = TaskWorker(lambda: self.installer_service.prepare(self.key_pair.public_key, identifier))
        worker.signals.finished.connect(self._image_ready)
        worker.signals.failed.connect(self._image_failed)
        self._worker = worker
        self.thread_pool.start(worker)

    @Slot(object)
    def _image_ready(self, artifacts: InstallerArtifacts) -> None:
        if self.artifacts is not None:
            self.artifacts.cleanup()
        self.artifacts = artifacts
        self.build_progress.setRange(0, 100)
        self.build_progress.setValue(100)
        self.prepare_button.setEnabled(True)
        plan = artifacts.plan
        self.build_status.setText("Exact-size ext4 image and sparse transfer set verified.")
        self.plan_summary.setText(
            f"Bootloader partition: data\nRaw image: {plan.size:,} bytes\n"
            f"Measured download limit: {plan.max_download:,} bytes\n"
            f"Sparse transfers: {len(plan.transfers)} · "
            f"{sum(path.stat().st_size for path in plan.transfers):,} transmitted bytes"
        )
        self._refresh_flash_gate()
        self._update_navigation()

    @Slot(str)
    def _image_failed(self, message: str) -> None:
        self.artifacts = None
        self.build_progress.setRange(0, 100)
        self.build_progress.setValue(0)
        self.prepare_button.setEnabled(True)
        self.build_status.setText("Image preparation failed: " + redact(message))
        self._update_navigation()

    def _review_confirmed(self) -> bool:
        return (
            self.recovery_confirmation.isChecked()
            and self.data_loss_confirmation.isChecked()
            and self.erase_phrase.text().strip() == "ERASE DATA"
        )

    def start_flash(self) -> None:
        if self.installer_service is None or self.artifacts is None or not self._review_confirmed():
            return
        self._refresh_flash_gate()
        if not self.flash_button.isEnabled():
            return
        answer = QMessageBox.warning(
            self,
            "Final /data erase confirmation",
            "This is the final confirmation. The complete data partition will be overwritten now. Continue?",
            QMessageBox.StandardButton.Yes | QMessageBox.StandardButton.No,
            QMessageBox.StandardButton.No,
        )
        if answer != QMessageBox.StandardButton.Yes:
            self.flash_status.setText("Flash cancelled by the user. No write command was sent.")
            return
        self.flash_button.setEnabled(False)
        self.flash_progress.setRange(0, 0)
        self.flash_status.setText("Revalidating the connected device before guarded flash…")
        identifier = self.candidate_selector.currentData() if self.candidate_selector.isVisible() else None
        worker = TaskWorker(lambda: self.installer_service.flash(self.artifacts, True, identifier))
        worker.signals.finished.connect(self._flash_complete)
        worker.signals.failed.connect(self._flash_failed)
        self._worker = worker
        self.thread_pool.start(worker)

    @Slot(object)
    def _flash_complete(self, message) -> None:
        self._flash_succeeded = True
        self.flash_progress.setRange(0, 100)
        self.flash_progress.setValue(100)
        self.flash_status.setText("Flash completed and reboot requested. " + redact(str(message)))
        self._update_navigation()

    @Slot(str)
    def _flash_failed(self, message: str) -> None:
        self._flash_succeeded = False
        self.flash_progress.setRange(0, 100)
        self.flash_progress.setValue(0)
        self.flash_status.setText("Flash stopped; no reboot was sent: " + redact(message))
        self._refresh_flash_gate()
        self._update_navigation()

    def test_ssh(self) -> None:
        if not self._flash_succeeded or self.installer_service is None or self.key_pair is None:
            return
        host = self.ssh_host.text().strip()
        fingerprint = self.ssh_fingerprint.text().strip()
        if not host or not fingerprint.startswith("SHA256:"):
            self.ssh_status.setText("Enter the device address and a trusted SHA256 host-key fingerprint.")
            return
        app_data = Path(QStandardPaths.writableLocation(QStandardPaths.StandardLocation.AppDataLocation))
        known_hosts = app_data / "known_hosts"

        def verify_connection():
            self.installer_service.ssh.enroll_host_key(host, 22, fingerprint, known_hosts)
            return self.installer_service.verify_ssh(
                host, self.key_pair.private_key, self.ssh_passphrase.text(), known_hosts
            )

        self.ssh_test_button.setEnabled(False)
        self.ssh_status.setText("Enrolling the trusted host key and testing authenticated SSH…")
        worker = TaskWorker(verify_connection)
        worker.signals.finished.connect(self._ssh_complete)
        worker.signals.failed.connect(self._ssh_failed)
        self._worker = worker
        self.thread_pool.start(worker)

    @Slot(object)
    def _ssh_complete(self, marker) -> None:
        self.ssh_test_button.setEnabled(True)
        self.ssh_status.setText("Authenticated SSH succeeded: " + redact(str(marker)))

    @Slot(str)
    def _ssh_failed(self, message: str) -> None:
        self.ssh_test_button.setEnabled(True)
        self.ssh_status.setText("SSH verification failed: " + redact(message))

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
            self.show_page(self.PREPARE)
        elif page == self.PREPARE and self.artifacts is not None:
            self.show_page(self.REVIEW)
        elif page == self.REVIEW and self._review_confirmed():
            self.show_page(self.FLASH)
        elif page == self.FLASH and self._flash_succeeded:
            self.show_page(self.SSH_TEST)

    def _diagnostics_allow_key_setup(self) -> bool:
        if self.diagnostic_report is None:
            return False
        return self.diagnostic_report.result.state == DetectionState.MATCH

    def _update_navigation(self) -> None:
        page = self.page_stack.currentIndex()
        self.back_button.setText("Choose another device" if page == self.CONNECTION else "Back")
        if page == self.CONNECTION:
            self.continue_button.setText("Continue to SSH key")
            self.continue_button.setEnabled(self._diagnostics_allow_key_setup())
        elif page == self.SSH_KEY:
            self.continue_button.setText("Continue")
            self.continue_button.setEnabled(self.key_pair is not None)
        elif page == self.PREPARE:
            self.continue_button.setText("Review installation")
            self.continue_button.setEnabled(self.artifacts is not None)
        elif page == self.REVIEW:
            self.continue_button.setText("Continue to guarded flash")
            self.continue_button.setEnabled(self._review_confirmed())
        elif page == self.FLASH:
            self._refresh_flash_gate()
            self.continue_button.setText("Continue to SSH test")
            self.continue_button.setEnabled(self._flash_succeeded)
        else:
            self.continue_button.setText("Finish after successful SSH test")
            self.continue_button.setEnabled(False)


class MainWindow(QMainWindow):
    def __init__(
        self,
        settings: QSettings,
        diagnostic_service: UsbDiagnosticService | None = None,
        thread_pool: QThreadPool | None = None,
        installer_service: Jc11sInstallerService | None = None,
    ) -> None:
        super().__init__()
        self.settings = settings
        self.setWindowTitle("Prime-Unlimited OS")
        self.setMinimumSize(980, 660)
        self.resize(settings.value("window/size", self.size()))
        self.stack = QStackedWidget()
        self.selection = SelectionPage()
        self.workflow = WorkflowPage(diagnostic_service, thread_pool, installer_service)
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
        if self.workflow.artifacts is not None:
            self.workflow.artifacts.cleanup()
            self.workflow.artifacts = None
        super().closeEvent(event)
