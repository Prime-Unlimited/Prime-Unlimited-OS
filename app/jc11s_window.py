"""Main window and functional, fail-closed guided installer."""

from __future__ import annotations

import sys
from datetime import datetime
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
from core.diagnostics import redact
from core.jc11s_device_detection import DetectionState
from core.jc11s_diagnostic_service import DiagnosticReport, UsbDiagnosticService
from core.keys import KeyPair, KeyValidationError, generate_keypair_paths, validate_keypair
from core.profiles import PROFILES, DeviceProfile, ProfileId
from core.windows_installer import InstallerArtifacts, Jc11sInstallerService


def label(text: str, object_name: str = "") -> QLabel:
    item = QLabel(text)
    item.setWordWrap(True)
    if object_name:
        item.setObjectName(object_name)
    return item


def executable_directory() -> Path:
    """Directory containing the packaged EXE, or the launch directory in source runs."""
    if getattr(sys, "frozen", False):
        return Path(sys.executable).resolve().parent
    return Path.cwd().resolve()


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
    progress = Signal(object)


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
    def __init__(self, function, with_progress: bool = False) -> None:
        super().__init__()
        self.function = function
        self.with_progress = with_progress
        self.signals = WorkerSignals()

    @Slot()
    def run(self) -> None:
        try:
            result = self.function(self.signals.progress.emit) if self.with_progress else self.function()
            self.signals.finished.emit(result)
        except Exception as exc:
            self.signals.failed.emit(redact(str(exc)))


class WorkflowPage(QWidget):
    exit_requested = Signal()

    CONNECTION = 0
    SSH_KEY = 1
    PREPARE = 2
    INSTALLATION = PREPARE
    FLASH = 3
    SSH_TEST = 4

    def __init__(
        self,
        diagnostic_service: UsbDiagnosticService | None = None,
        thread_pool: QThreadPool | None = None,
        installer_service: Jc11sInstallerService | None = None,
    ) -> None:
        super().__init__()
        self.diagnostic_service = diagnostic_service or UsbDiagnosticService()
        self._diagnostic_epoch = 0
        self._diagnostic_busy = False
        self.thread_pool = thread_pool or QThreadPool.globalInstance()
        self.installer_service = installer_service
        self.profile: DeviceProfile | None = None
        self.profile_id: ProfileId | None = None
        self.diagnostic_report: DiagnosticReport | None = None
        self.key_pair: KeyPair | None = None
        self._key_passphrase = ""
        self._pending_ssh_host = ""
        self._ssh_verified = False
        self.artifacts: InstallerArtifacts | None = None
        self._flash_succeeded = False
        self._bench_flash_active = False
        self._critical_write_active = False
        self._write_status_unknown = False
        self._flash_phase = "idle"
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
        self.generate_key_button = generate
        generate.clicked.connect(self.generate_key)
        validate = QPushButton("Validate imported pair")
        self.validate_key_button = validate
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

    def _build_flash_page(self) -> QWidget:
        page = QWidget()
        layout = QVBoxLayout(page)
        layout.addWidget(label("Hardware flash", "title"))
        self.plan_summary = label("")
        layout.addWidget(self.plan_summary)
        warning = label(
            "All settings and data on /data will be erased. "
            "The original firmware partitions are not targeted by this flash operation."
        )
        warning.setStyleSheet("background:#3a1717; color:#ffb4b4; border-radius:9px; padding:14px;")
        layout.addWidget(warning)
        self.data_loss_confirmation = QCheckBox("I understand that my data will be erased")
        self.data_loss_confirmation.stateChanged.connect(self._refresh_flash_gate)
        layout.addWidget(self.data_loss_confirmation)
        self.bench_mode = QCheckBox("Authorize experimental JC11S first flash for this device")
        self.bench_mode.stateChanged.connect(self._refresh_flash_gate)
        layout.addWidget(self.bench_mode)
        self.blocker_text = label("")
        self.blocker_text.setStyleSheet("background:#2b2114; color:#ffd18a; border-radius:9px; padding:14px;")
        layout.addWidget(self.blocker_text)
        self.flash_button = QPushButton("Start confirmed hardware flash")
        self.flash_button.setObjectName("primary")
        self.flash_button.clicked.connect(self.start_flash)
        layout.addWidget(self.flash_button, 0, Qt.AlignmentFlag.AlignLeft)
        self.critical_write_warning = label("DO NOT DISCONNECT USB – DO NOT POWER OFF THE DEVICE")
        self.critical_write_warning.setStyleSheet(
            "background:#5a1010; color:#ffffff; font-weight:700; border-radius:9px; padding:14px;"
        )
        self.critical_write_warning.setVisible(False)
        layout.addWidget(self.critical_write_warning)
        layout.addWidget(label("Sparse download only", "muted"))
        self.download_progress = QProgressBar()
        self.download_progress.setRange(0, 100)
        self.download_progress.setValue(0)
        layout.addWidget(self.download_progress)
        # Compatibility alias for existing integrations; this is never overall installation progress.
        self.flash_progress = self.download_progress
        self.flash_activity = QProgressBar()
        self.flash_activity.setRange(0, 0)
        self.flash_activity.setVisible(False)
        layout.addWidget(self.flash_activity)
        self.flash_phase_status = label("State: waiting to start.")
        layout.addWidget(self.flash_phase_status)
        self.flash_phase_log = QPlainTextEdit()
        self.flash_phase_log.setReadOnly(True)
        self.flash_phase_log.setMaximumHeight(150)
        layout.addWidget(self.flash_phase_log)
        self.flash_status = label("No write has started.")
        layout.addWidget(self.flash_status)
        layout.addStretch()
        return page

    def _build_ssh_page(self) -> QWidget:
        page = QWidget()
        layout = QVBoxLayout(page)
        layout.addWidget(label("Verify installed SSH access", "title"))
        self.ssh_host = QLineEdit()
        self.ssh_host.setPlaceholderText("Device IP address")
        self.ssh_host.setAccessibleName("Device IP address")
        self.ssh_test_button = QPushButton("TEST SSH")
        self.ssh_test_button.setObjectName("primary")
        self.ssh_test_button.clicked.connect(self.test_ssh)
        self.first_boot_failed_button = QPushButton("Record failed first boot / SSH activation")
        self.first_boot_failed_button.clicked.connect(self.record_failed_first_boot)
        self.ssh_status = label("SSH has not been tested.")
        layout.addWidget(self.ssh_host)
        layout.addWidget(self.ssh_test_button, 0, Qt.AlignmentFlag.AlignLeft)
        layout.addWidget(self.first_boot_failed_button, 0, Qt.AlignmentFlag.AlignLeft)
        layout.addWidget(self.ssh_status)
        layout.addStretch()
        return page

    def set_profile(self, profile_id: ProfileId) -> None:
        self._diagnostic_epoch += 1
        self.profile_id = profile_id
        self.profile = PROFILES[profile_id]
        self.diagnostic_report = None
        self.key_pair = None
        self._key_passphrase = ""
        self._validated_public_key = None
        self.generate_key_button.setEnabled(True)
        self.validate_key_button.setEnabled(True)
        self.private_key_path.setReadOnly(False)
        self.public_key_path.setReadOnly(False)
        self._pending_ssh_host = ""
        self._ssh_verified = False
        if self.artifacts is not None:
            self.artifacts.cleanup()
        self.artifacts = None
        self._flash_succeeded = False
        self._bench_flash_active = False
        self._critical_write_active = False
        self._write_status_unknown = False
        self._flash_phase = "idle"
        if profile_id == ProfileId.JC11S and self.installer_service is None:
            fastboot = getattr(self.diagnostic_service, "fastboot", None)
            if fastboot is not None and hasattr(fastboot, "collect_partition_evidence"):
                self.installer_service = Jc11sInstallerService(fastboot)
        self.steps.clear()
        for step in (
            "1  USB connection & diagnostics",
            "2  Mandatory SSH key",
            "3  Device-specific image build",
            "4  Guarded hardware flash",
            "5  Authenticated SSH test",
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
        key_directory = executable_directory()
        self.private_key_path.setText(str(key_directory / "prime_unlimited_ed25519"))
        self.public_key_path.setText(str(key_directory / "prime_unlimited_ed25519.pub"))
        self.passphrase.clear()
        self.confirm_passphrase.clear()
        self.key_status.setText("SSH key setup is required. There is no skip option.")
        self.build_progress.setValue(0)
        self.build_status.setText("Image has not been built.")
        self.plan_summary.clear()
        self.data_loss_confirmation.setChecked(False)
        self.bench_mode.setChecked(False)
        self.flash_progress.setValue(0)
        self.flash_button.setVisible(True)
        self.prepare_button.setEnabled(True)
        self.flash_activity.setVisible(False)
        self.critical_write_warning.setVisible(False)
        self.flash_phase_status.setText("State: waiting to start.")
        self.flash_phase_log.clear()
        self.flash_status.setText("No write has started.")
        self.ssh_status.setText("SSH has not been tested.")
        self.ssh_host.clear()
        self.first_boot_failed_button.setVisible(True)
        self.show_page(self.CONNECTION)

    def _refresh_flash_gate(self) -> None:
        if self.profile_id != ProfileId.JC11S or self.installer_service is None:
            blockers = (self.profile.blocker if self.profile else "Installer service unavailable.",)
        elif self.bench_mode.isChecked():
            blockers = () if self._bench_confirmed() else ("Experimental bench confirmations are incomplete",)
        else:
            blockers = self.installer_service.release_policy.blockers
            if (
                self.artifacts is not None
                and len(self.artifacts.plan.transfers) > 1
                and not self.installer_service.release_policy.multipart_sparse_verified
            ):
                blockers += ("Multipart sparse flashing is not verified on the InMusic bootloader",)
        self.blocker_text.setText(
            "Hardware write blocked:\n• " + "\n• ".join(blockers)
            if blockers
            else (
                "EXPERIMENTAL FIRST-FLASH BENCH authorization ready for data only."
                if self.bench_mode.isChecked()
                else "All normal release gates satisfied."
            )
        )
        self.flash_button.setEnabled(
            not blockers
            and self._review_confirmed()
            and not self._critical_write_active
            and not self._write_status_unknown
            and not self._flash_succeeded
        )

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
        self._diagnostic_epoch += 1
        epoch = self._diagnostic_epoch
        self._diagnostic_busy = True
        worker.signals.finished.connect(lambda report, token=epoch: self._accept_report(token, report))
        worker.signals.failed.connect(lambda message, token=epoch: self._accept_failure(token, message))
        self._worker = worker
        self.thread_pool.start(worker)

    def _accept_report(self, token: int, report: DiagnosticReport) -> None:
        self._diagnostic_busy = False
        if token == self._diagnostic_epoch:
            self.handle_diagnostic_report(report)
        elif getattr(self, "_return_pending", False):
            self._return_pending = False
            self.exit_requested.emit()

    def _accept_failure(self, token: int, message: str) -> None:
        self._diagnostic_busy = False
        if token == self._diagnostic_epoch:
            self.handle_diagnostic_failure(message)
        elif getattr(self, "_return_pending", False):
            self._return_pending = False
            self.exit_requested.emit()

    @Slot(object)
    def handle_diagnostic_report(self, report: DiagnosticReport) -> None:
        self.diagnostic_report = report
        if (
            self.profile_id == ProfileId.JC11S
            and report.result.may_continue
            and self.installer_service is None
        ):
            fastboot = getattr(self.diagnostic_service, "fastboot", None)
            if fastboot is not None and hasattr(fastboot, "collect_partition_evidence"):
                self.installer_service = Jc11sInstallerService(fastboot)
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
        model_verified = candidate.verification.model_verified and product == "rk3288-az01-jc11s"
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
        if self.key_pair is not None:
            return
        passphrase = self.passphrase.text()
        if passphrase != self.confirm_passphrase.text():
            self._key_error("The passphrase confirmation does not match.")
            return
        private_path = Path(self.private_key_path.text().strip())
        public_path = Path(self.public_key_path.text().strip())
        if not self.private_key_path.text().strip() or not self.public_key_path.text().strip():
            self._key_error("Choose paths for both the private and public SSH key files.")
            return
        if private_path.exists() or public_path.exists():
            QMessageBox.warning(
                self,
                "SSH key files already exist",
                "The selected key files will not be overwritten. Choose different filenames, or use "
                "Validate imported pair to reuse the existing files.",
            )
            self._key_error("Existing SSH key files were left unchanged.")
            return
        try:
            pair = generate_keypair_paths(private_path, public_path, passphrase)
        except PermissionError:
            fallback = Path(
                QStandardPaths.writableLocation(QStandardPaths.StandardLocation.DocumentsLocation)
            ) / "Prime-Unlimited-OS"
            self.private_key_path.setText(str(fallback / "prime_unlimited_ed25519"))
            self.public_key_path.setText(str(fallback / "prime_unlimited_ed25519.pub"))
            self._key_error(
                "The selected folder is not writable. A writable Documents folder has been suggested; "
                "review it and select Generate new key again."
            )
            return
        except (KeyValidationError, FileExistsError, OSError) as exc:
            self._key_error(str(exc))
            return
        self._accept_key_pair(pair, passphrase)

    def validate_imported_key(self) -> None:
        if self.key_pair is not None:
            return
        private_path = Path(self.private_key_path.text())
        public_path = Path(self.public_key_path.text())
        try:
            validate_keypair(private_path, public_path, self.passphrase.text())
        except KeyValidationError as exc:
            self._key_error(str(exc))
            return
        self._accept_key_pair(KeyPair(private_path, public_path), self.passphrase.text())

    def _accept_key_pair(self, pair: KeyPair, passphrase: str = "") -> None:
        self.key_pair = pair
        self._key_passphrase = passphrase
        self._validated_public_key = pair.public_key.read_bytes()
        self.generate_key_button.setEnabled(False)
        self.validate_key_button.setEnabled(False)
        self.private_key_path.setReadOnly(True)
        self.public_key_path.setReadOnly(True)
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
        if self.artifacts is not None or not self.prepare_button.isEnabled():
            return
        if self.key_pair is not None and self.key_pair.public_key.read_bytes() != self._validated_public_key:
            self.build_status.setText("Validated public key changed. Image build blocked.")
            return
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
        self.prepare_button.setEnabled(False)
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
        return self.data_loss_confirmation.isChecked()

    def _bench_confirmed(self) -> bool:
        return self.bench_mode.isChecked() and self.data_loss_confirmation.isChecked()

    def _bench_log_path(self) -> Path:
        app_data = Path(QStandardPaths.writableLocation(QStandardPaths.StandardLocation.AppDataLocation))
        return app_data / "jc11s-first-flash-bench.jsonl"

    def _set_flash_phase(self, phase: str, text: str) -> None:
        if phase == self._flash_phase:
            return
        self._flash_phase = phase
        timestamp = datetime.now().astimezone().isoformat(timespec="seconds")
        self.flash_phase_status.setText("State: " + text)
        self.flash_phase_log.appendPlainText(f"{timestamp}  {text}")
        waiting = phase == "flash_wait"
        self.flash_activity.setVisible(waiting)
        if waiting:
            self.flash_activity.setRange(0, 0)

    def start_flash(self) -> None:
        if self._flash_succeeded or self._critical_write_active or self._write_status_unknown:
            return
        if self.installer_service is None or self.artifacts is None or not self._review_confirmed():
            return
        self._refresh_flash_gate()
        if not self.flash_button.isEnabled():
            return
        answer = QMessageBox.warning(
            self,
            "Final /data erase confirmation",
            (
                "EXPERIMENTAL JC11S FIRST FLASH. Only Fastboot partition data will be overwritten. "
                "The complete filesystem and all settings will be lost. Continue?"
                if self.bench_mode.isChecked()
                else "This is the final confirmation. The complete data partition will be overwritten now. Continue?"
            ),
            QMessageBox.StandardButton.Yes | QMessageBox.StandardButton.No,
            QMessageBox.StandardButton.No,
        )
        if answer != QMessageBox.StandardButton.Yes:
            self.flash_status.setText("Flash cancelled by the user. No write command was sent.")
            return
        self._critical_write_active = True
        self._write_status_unknown = False
        self._flash_succeeded = False
        self.flash_button.setEnabled(False)
        self.flash_progress.setRange(0, 100)
        self.flash_progress.setValue(0)
        self.flash_phase_log.clear()
        self.critical_write_warning.setText("DO NOT DISCONNECT USB – DO NOT POWER OFF THE DEVICE")
        self.critical_write_warning.setVisible(True)
        self._set_flash_phase("verification", "Verifying device and image")
        self.flash_status.setText("Revalidating the connected device and image before guarded flash…")
        self._update_navigation()
        identifier = self.candidate_selector.currentData() if self.candidate_selector.isVisible() else None
        if self.bench_mode.isChecked():
            self._bench_flash_active = True

            def task(progress):
                def report(*event):
                    progress(event)

                return self.installer_service.flash_bench(
                    self.artifacts,
                    True,
                    True,
                    identifier,
                    report,
                )
        else:
            self._bench_flash_active = False

            def task(progress):
                def report(*event):
                    progress(event)

                return self.installer_service.flash(self.artifacts, True, identifier, report)
        worker = TaskWorker(task, with_progress=True)
        worker.signals.progress.connect(self._flash_progress_update)
        worker.signals.finished.connect(self._flash_complete)
        worker.signals.failed.connect(self._flash_failed)
        self._worker = worker
        self.thread_pool.start(worker)

    @Slot(object)
    def _flash_progress_update(self, event) -> None:
        if event[0] == "phase":
            phase = event[1]
            index = event[2] if len(event) > 2 else None
            count = event[3] if len(event) > 3 else None
            suffix = f" ({index}/{count})" if index is not None and count is not None else ""
            labels = {
                "verification_started": "Verifying device and image",
                "verification_complete": "Device and image verification successful",
                "flash_command_sent": f"Fastboot flash:data sent{suffix}",
                "flash_wait": f"Waiting for bootloader flash completion{suffix}",
                "flash_okay": f"Fastboot OKAY confirmed{suffix}",
                "reboot_requested": "Reboot requested",
            }
            self._set_flash_phase(phase, labels[phase])
            if phase == "verification_complete":
                self.flash_status.setText("Verification complete. Sparse download will start next.")
            elif phase == "flash_wait":
                self.flash_status.setText("Bootloader is writing /data. No completion percentage is available.")
            elif phase == "flash_okay":
                self.flash_status.setText("Bootloader confirmed flash:data with Fastboot OKAY.")
            return

        stage, index, count, sent, total = event
        fraction = 0.0 if total <= 0 else min(sent / total, 1.0)
        percent = round(((index - 1 + fraction) / count) * 100) if count else 0
        self._set_flash_phase("download", f"Sparse download in progress ({index}/{count})")
        self.flash_progress.setValue(percent)
        if percent == 100:
            self.flash_status.setText(
                "Sparse download: 100%. Download complete; flash:data is NOT confirmed yet."
            )
        else:
            self.flash_status.setText(
                f"Sparse download {index}/{count}: {sent:,} of {total:,} bytes ({percent}%)."
            )

    @Slot(object)
    def _flash_complete(self, message) -> None:
        self._critical_write_active = False
        self._flash_succeeded = True
        self.flash_button.setEnabled(False)
        self.flash_button.setVisible(False)
        self.flash_progress.setRange(0, 100)
        self.flash_progress.setValue(100)
        self.flash_activity.setVisible(False)
        self.critical_write_warning.setVisible(False)
        self._set_flash_phase("reboot_requested", "Reboot requested; SSH verification pending")
        self.flash_status.setText(
            "Flash confirmed and reboot requested. Installation awaits SSH verification. " + redact(str(message))
        )
        if self._bench_flash_active and hasattr(self.installer_service, "record_bench_result"):
            self.installer_service.record_bench_result(
                self.artifacts, self._bench_log_path(), "flash_succeeded", str(message)
            )
        self._update_navigation()

    @Slot(str)
    def _flash_failed(self, message: str) -> None:
        failure = message.casefold()
        connection_uncertain = any(
            marker in failure for marker in ("timed out", "timeout", "disconnected", "not connected")
        )
        failed_during_commit = self._flash_phase == "flash_wait" and connection_uncertain
        self._critical_write_active = False
        self._flash_succeeded = False
        self.flash_progress.setRange(0, 100)
        self.flash_progress.setValue(0)
        self.flash_activity.setVisible(False)
        if failed_during_commit:
            self._write_status_unknown = True
            warning = "Write status unknown – do not reboot or flash again automatically."
            self.critical_write_warning.setText(
                "DO NOT DISCONNECT USB – DO NOT POWER OFF THE DEVICE\n" + warning
            )
            self.critical_write_warning.setVisible(True)
            self._set_flash_phase("unknown", warning)
            self.flash_status.setText(warning + " " + redact(message))
        else:
            self.critical_write_warning.setVisible(False)
            self._set_flash_phase("failed", "Flash stopped before confirmed completion")
            self.flash_status.setText("Flash stopped; no reboot was sent: " + redact(message))
        self._refresh_flash_gate()
        self._update_navigation()

    def _check_installation_key(self) -> bool:
        """Reject changed client keys; use the passphrase validated before image creation."""
        if self.key_pair is None or self._validated_public_key is None:
            self.ssh_status.setText("No validated installation key is available.")
            return False
        try:
            validate_keypair(
                self.key_pair.private_key, self.key_pair.public_key, self._key_passphrase
            )
            if self.key_pair.public_key.read_bytes() != self._validated_public_key:
                raise KeyValidationError("The installation public key has changed.")
        except (KeyValidationError, OSError) as exc:
            self.ssh_status.setText("Installation key verification failed: " + redact(str(exc)))
            return False
        return True

    def test_ssh(self) -> None:
        if not self._flash_succeeded or self.installer_service is None or not self._check_installation_key():
            return
        host = self.ssh_host.text().strip()
        if not host:
            self.ssh_status.setText("Enter the device IP address.")
            return
        self._pending_ssh_host = host
        self.ssh_test_button.setEnabled(False)
        self._set_flash_phase("ssh_verification", "SSH verification in progress")
        self.ssh_status.setText("Reading the device SSH host key…")
        worker = TaskWorker(lambda: self.installer_service.ssh.inspect_host_key(host, 22))
        worker.signals.finished.connect(self._ssh_identity_ready)
        worker.signals.failed.connect(self._ssh_failed)
        self._worker = worker
        self.thread_pool.start(worker)

    def _known_hosts_path(self) -> Path:
        app_data = Path(QStandardPaths.writableLocation(QStandardPaths.StandardLocation.AppDataLocation))
        return app_data / "known_hosts"

    @Slot(object)
    def _ssh_identity_ready(self, identity) -> None:
        host = self._pending_ssh_host
        known_hosts = self._known_hosts_path()
        if self.installer_service.ssh.is_host_key_trusted(host, 22, identity, known_hosts):
            self._start_authenticated_ssh_test(host, known_hosts)
            return
        changed = self.installer_service.ssh.has_pinned_host(host, 22, known_hosts)
        warning = (
            "WARNING: The SSH host key has changed. This can happen after reflashing, "
            "but may also indicate a different device or a security attack. "
            "Verify the device and fingerprint independently before proceeding."
            if changed else
            "First SSH connection. Verify that this is your intended Prime 4+ device."
        )
        answer = QMessageBox.warning(
            self,
            "SSH host key changed" if changed else "Trust this device's SSH host key?",
            f"Device: {host}\\nHost key type: {identity.algorithm}\\n"
            f"New SHA256 fingerprint: {identity.fingerprint}\\n\\n"
            + warning
            + "\\n\\nReplace the stored host key for this device and test SSH?",
            QMessageBox.StandardButton.Yes | QMessageBox.StandardButton.No,
            QMessageBox.StandardButton.No,
        )
        if answer != QMessageBox.StandardButton.Yes:
            self.ssh_test_button.setEnabled(True)
            self.ssh_status.setText("SSH host key not approved. No authentication attempted.")
            return
        expected = identity.fingerprint

        def enroll_and_verify():
            self.installer_service.ssh.enroll_host_key(host, 22, expected, known_hosts)
            return self.installer_service.verify_ssh(
                host, self.key_pair.private_key, self._key_passphrase, known_hosts
            )

        self.ssh_status.setText("Saving the confirmed host key and testing authenticated SSH…")
        worker = TaskWorker(enroll_and_verify)
        worker.signals.finished.connect(self._ssh_complete)
        worker.signals.failed.connect(self._ssh_failed)
        self._worker = worker
        self.thread_pool.start(worker)

    def _start_authenticated_ssh_test(self, host: str, known_hosts: Path) -> None:
        self.ssh_status.setText("Known host key matched. Testing authenticated SSH…")
        worker = TaskWorker(
            lambda: self.installer_service.verify_ssh(
                host, self.key_pair.private_key, self._key_passphrase, known_hosts
            )
        )
        worker.signals.finished.connect(self._ssh_complete)
        worker.signals.failed.connect(self._ssh_failed)
        self._worker = worker
        self.thread_pool.start(worker)

    @Slot(object)
    def _ssh_complete(self, marker) -> None:
        self.ssh_test_button.setEnabled(False)
        self._ssh_verified = True
        self.ssh_status.setText("SSH connection successful")
        self.first_boot_failed_button.setVisible(False)
        self._set_flash_phase("complete", "Installation complete; SSH verification successful")
        if self._bench_flash_active and hasattr(self.installer_service, "record_bench_result"):
            self.installer_service.record_bench_result(
                self.artifacts, self._bench_log_path(), "first_boot_ssh_succeeded", str(marker)
            )
        self._update_navigation()

    @Slot(str)
    def _ssh_failed(self, message: str) -> None:
        self.ssh_test_button.setEnabled(True)
        self._ssh_verified = False
        self.first_boot_failed_button.setVisible(True)
        self.ssh_status.setText("SSH verification failed: " + redact(message))
        self._set_flash_phase("ssh_failed", "SSH verification failed; retry available")
        if self._bench_flash_active and hasattr(self.installer_service, "record_bench_result"):
            self.installer_service.record_bench_result(
                self.artifacts, self._bench_log_path(), "first_boot_or_ssh_failed", message
            )

    def record_failed_first_boot(self) -> None:
        if not self._bench_flash_active or self.artifacts is None or not self._flash_succeeded:
            return
        self.installer_service.record_bench_result(
            self.artifacts,
            self._bench_log_path(),
            "first_boot_or_ssh_failed",
            "Tester explicitly reported that first boot or SSH activation did not succeed.",
        )
        self.ssh_status.setText("Failed first boot / SSH activation recorded as a bench result.")

    def go_back(self) -> None:
        if self._critical_write_active:
            return
        page = self.page_stack.currentIndex()
        if page == self.CONNECTION:
            self._diagnostic_epoch += 1
            self.diagnostic_report = None
            self.exit_requested.emit()
        else:
            self.show_page(page - 1)

    def go_forward(self) -> None:
        if self._critical_write_active:
            return
        page = self.page_stack.currentIndex()
        if page == self.CONNECTION and self._diagnostics_allow_key_setup():
            self.show_page(self.SSH_KEY)
        elif page == self.SSH_KEY and self.key_pair is not None:
            self.show_page(self.PREPARE)
        elif page == self.PREPARE and self.artifacts is not None:
            self.show_page(self.FLASH)
        elif page == self.FLASH and self._flash_succeeded:
            self.show_page(self.SSH_TEST)
        elif page == self.SSH_TEST and self._ssh_verified:
            self.exit_requested.emit()

    def _diagnostics_allow_key_setup(self) -> bool:
        if self.diagnostic_report is None:
            return False
        return self.diagnostic_report.result.state == DetectionState.MATCH

    def _update_navigation(self) -> None:
        page = self.page_stack.currentIndex()
        self.back_button.setText("Choose another device" if page == self.CONNECTION else "Back")
        if self._critical_write_active:
            self.back_button.setEnabled(False)
            self.continue_button.setEnabled(False)
            return
        self.back_button.setEnabled(True)
        if page == self.CONNECTION:
            self.continue_button.setText("Continue to SSH key")
            self.continue_button.setEnabled(self._diagnostics_allow_key_setup())
        elif page == self.SSH_KEY:
            self.continue_button.setText("Continue")
            self.continue_button.setEnabled(self.key_pair is not None)
        elif page == self.PREPARE:
            self.continue_button.setText("Review installation")
            self.continue_button.setEnabled(self.artifacts is not None)
        elif page == self.FLASH:
            self._refresh_flash_gate()
            self.continue_button.setText("Continue to SSH test")
            self.continue_button.setEnabled(self._flash_succeeded)
        else:
            self.continue_button.setText("Finish after successful SSH test")
            self.continue_button.setEnabled(self._ssh_verified)


class Jc11sMainWindow(QMainWindow):
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
        if self.workflow._critical_write_active:
            QMessageBox.critical(
                self,
                "Flash in progress",
                "DO NOT DISCONNECT USB – DO NOT POWER OFF THE DEVICE\n\n"
                "The window cannot be closed while a critical write is in progress.",
            )
            event.ignore()
            return
        self.settings.setValue("window/size", self.size())
        if self.workflow.artifacts is not None:
            self.workflow.artifacts.cleanup()
            self.workflow.artifacts = None
        super().closeEvent(event)
