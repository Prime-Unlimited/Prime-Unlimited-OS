"""Main window and functional, fail-closed guided installer."""

from __future__ import annotations

import tempfile
from pathlib import Path

from PySide6.QtCore import QObject, QRunnable, QSettings, Qt, QThreadPool, Signal, Slot
from PySide6.QtGui import QIcon, QKeySequence, QShortcut
from PySide6.QtSvgWidgets import QSvgWidget
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
from core.jp24_bootstrap import FlashError, FlashProgress, build_data_image
from core.keys import KeyPair, KeyValidationError, generate_keypair, validate_keypair
from core.profiles import PROFILES, DeviceProfile, ProfileId
from core.ssh_service import HostIdentity, SshService, SshVerificationError
from core.transports import ImbupdTransport

ASSETS_DIR = Path(__file__).resolve().parent.parent / "assets"
KNOWN_HOSTS_PATH = Path.home() / ".config" / "prime-unlimited-os" / "known_hosts"


class BrandHeader(QWidget):
    """Jogwheel mark + PRIME UNLIMITED / OPERATING SYSTEM wordmark."""

    def __init__(self) -> None:
        super().__init__()
        layout = QHBoxLayout(self)
        layout.setContentsMargins(0, 0, 0, 0)
        layout.setSpacing(14)
        icon = QSvgWidget(str(ASSETS_DIR / "jogwheel-icon.svg"))
        icon.setFixedSize(40, 40)
        layout.addWidget(icon)
        text_box = QVBoxLayout()
        text_box.setSpacing(0)
        text_box.addWidget(label("PRIME UNLIMITED", "brand-title"))
        text_box.addWidget(label("OPERATING SYSTEM", "brand-subtitle"))
        layout.addLayout(text_box)
        layout.addStretch()


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
        layout.addWidget(BrandHeader())
        layout.addSpacing(8)
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


class FlashWorkerSignals(QObject):
    progress = Signal(object)
    finished = Signal()
    failed = Signal(str)


class FlashWorker(QRunnable):
    """Builds the /data image and flashes it. Runs off the GUI thread since
    the write itself can take well over a minute."""

    def __init__(self, public_key: Path) -> None:
        super().__init__()
        self.public_key = public_key
        self.signals = FlashWorkerSignals()

    @Slot()
    def run(self) -> None:
        try:
            with tempfile.TemporaryDirectory(prefix="prime-unlimited-jp24-") as tmp:
                image = Path(tmp) / "prime4-data.img"
                self.signals.progress.emit(FlashProgress("build", "Building the /data image…"))
                build_data_image(self.public_key, image)
                ImbupdTransport().install(image, on_progress=self.signals.progress.emit)
            self.signals.finished.emit()
        except FlashError as exc:
            self.signals.failed.emit(redact(str(exc)))
        except Exception as exc:  # GUI boundary: never crash the app on an unexpected flash failure.
            self.signals.failed.emit(redact(str(exc)))


class SshWorkerSignals(QObject):
    finished = Signal(object)
    failed = Signal(str)


class SshInspectWorker(QRunnable):
    """Reads the device's SSH host key - unauthenticated, no trust decision
    made here. The caller must show the fingerprint and get explicit
    confirmation before any enrollment."""

    def __init__(self, service: SshService, host: str, port: int) -> None:
        super().__init__()
        self.service = service
        self.host = host
        self.port = port
        self.signals = SshWorkerSignals()

    @Slot()
    def run(self) -> None:
        try:
            self.signals.finished.emit(self.service.inspect_host_key(self.host, self.port))
        except SshVerificationError as exc:
            self.signals.failed.emit(redact(str(exc)))
        except Exception as exc:  # GUI boundary: never crash the app on an unexpected SSH failure.
            self.signals.failed.emit(redact(str(exc)))


class SshEnrollWorker(QRunnable):
    """Pins the host key the user just confirmed into known_hosts."""

    def __init__(self, service: SshService, host: str, port: int, expected_fingerprint: str) -> None:
        super().__init__()
        self.service = service
        self.host = host
        self.port = port
        self.expected_fingerprint = expected_fingerprint
        self.signals = SshWorkerSignals()

    @Slot()
    def run(self) -> None:
        try:
            identity = self.service.enroll_host_key(
                self.host, self.port, self.expected_fingerprint, KNOWN_HOSTS_PATH
            )
            self.signals.finished.emit(identity)
        except SshVerificationError as exc:
            self.signals.failed.emit(redact(str(exc)))
        except Exception as exc:
            self.signals.failed.emit(redact(str(exc)))


class SshVerifyWorker(QRunnable):
    """Logs in with the enrolled key against the pinned host key and runs
    one harmless command to prove the connection is real."""

    def __init__(self, service: SshService, host: str, port: int, private_key: Path, passphrase: str) -> None:
        super().__init__()
        self.service = service
        self.host = host
        self.port = port
        self.private_key = private_key
        self.passphrase = passphrase
        self.signals = SshWorkerSignals()

    @Slot()
    def run(self) -> None:
        try:
            marker = self.service.verify_authenticated(
                self.host, self.port, self.private_key, self.passphrase, KNOWN_HOSTS_PATH
            )
            self.signals.finished.emit(marker)
        except SshVerificationError as exc:
            self.signals.failed.emit(redact(str(exc)))
        except Exception as exc:
            self.signals.failed.emit(redact(str(exc)))


class WorkflowPage(QWidget):
    exit_requested = Signal()

    CONNECTION = 0
    SSH_KEY = 1
    BLOCKED = 2
    FLASH = 3
    SSH_TEST = 4

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
        self._flash_worker: FlashWorker | None = None
        self.installation_page = self.BLOCKED
        self.ssh_service = SshService()
        self._ssh_worker: QRunnable | None = None
        self._pending_host_identity: HostIdentity | None = None

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
        self.page_stack.addWidget(self._build_flash_page())
        self.page_stack.addWidget(self._build_ssh_test_page())
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

    def _build_flash_page(self) -> QWidget:
        page = QWidget()
        layout = QVBoxLayout(page)
        layout.setContentsMargins(0, 0, 0, 0)
        layout.addWidget(label("Write /data and enable SSH", "title"))
        self.flash_warning = label("")
        self.flash_warning.setStyleSheet("background:#2b2114; color:#ffd18a; border-radius:9px; padding:14px;")
        layout.addWidget(self.flash_warning)
        layout.addWidget(
            label(
                "This overwrites the device's /data partition with an image containing the "
                "public key from the previous step, then reboots the device. It is irreversible "
                "if the device loses power or connection mid-write.",
                "subtitle",
            )
        )
        self.flash_button = QPushButton("Flash and enable SSH")
        self.flash_button.setObjectName("primary")
        self.flash_button.clicked.connect(self.confirm_and_flash)
        layout.addWidget(self.flash_button, 0, Qt.AlignmentFlag.AlignLeft)
        self.flash_status = label("Ready to flash. Keep the device in Firmware Update Mode and connected.")
        layout.addWidget(self.flash_status)
        self.flash_log = QPlainTextEdit()
        self.flash_log.setReadOnly(True)
        self.flash_log.setAccessibleName("Flash progress log")
        layout.addWidget(self.flash_log, 1)
        layout.addWidget(
            label(
                "After the device reboots and reconnects to Wi-Fi, connect with:\n"
                "ssh -i <your private key> -p 2222 -o StrictHostKeyChecking=accept-new root@<device-ip>",
                "muted",
            )
        )
        return page

    def _build_ssh_test_page(self) -> QWidget:
        page = QWidget()
        layout = QVBoxLayout(page)
        layout.setContentsMargins(0, 0, 0, 0)
        layout.addWidget(label("Confirm SSH access", "title"))
        layout.addWidget(
            label(
                "Enter the device's IP address, inspect its SSH host key, and confirm the "
                "fingerprint before it is trusted. This is trust-on-first-use with an explicit "
                "confirmation step, not automatic trust.",
                "subtitle",
            )
        )

        self.ssh_host_input = QLineEdit()
        self.ssh_host_input.setPlaceholderText("Device IP address (see the device's Wi-Fi settings)")
        self.ssh_host_input.setAccessibleName("Device IP address")
        self.ssh_host_input.textChanged.connect(self._update_navigation)
        layout.addWidget(self.ssh_host_input)

        inspect_row = QHBoxLayout()
        self.inspect_key_button = QPushButton("Inspect host key")
        self.inspect_key_button.clicked.connect(self.inspect_host_key)
        inspect_row.addWidget(self.inspect_key_button)
        self.trust_key_button = QPushButton("Trust and pin this key")
        self.trust_key_button.setObjectName("primary")
        self.trust_key_button.setEnabled(False)
        self.trust_key_button.clicked.connect(self.trust_host_key)
        inspect_row.addWidget(self.trust_key_button)
        layout.addLayout(inspect_row)

        self.ssh_test_passphrase = QLineEdit()
        self.ssh_test_passphrase.setEchoMode(QLineEdit.EchoMode.Password)
        self.ssh_test_passphrase.setPlaceholderText("Private key passphrase")
        self.ssh_test_passphrase.setAccessibleName("Private key passphrase for SSH test")
        layout.addWidget(self.ssh_test_passphrase)

        self.verify_login_button = QPushButton("Test authenticated login")
        self.verify_login_button.setObjectName("primary")
        self.verify_login_button.setEnabled(False)
        self.verify_login_button.clicked.connect(self.verify_login)
        layout.addWidget(self.verify_login_button, 0, Qt.AlignmentFlag.AlignLeft)

        self.ssh_test_status = label("Enter the device's IP address to begin.")
        layout.addWidget(self.ssh_test_status)
        self.ssh_test_log = QPlainTextEdit()
        self.ssh_test_log.setReadOnly(True)
        self.ssh_test_log.setAccessibleName("SSH test log")
        layout.addWidget(self.ssh_test_log, 1)
        return page

    def _ssh_host(self) -> str:
        return self.ssh_host_input.text().strip()

    def inspect_host_key(self) -> None:
        if self.profile is None or not self._ssh_host():
            return
        self.inspect_key_button.setEnabled(False)
        self.trust_key_button.setEnabled(False)
        self._pending_host_identity = None
        self.ssh_test_status.setText("Connecting to inspect the host key…")
        self.ssh_test_status.setStyleSheet("")
        worker = SshInspectWorker(self.ssh_service, self._ssh_host(), self.profile.ssh_port)
        worker.signals.finished.connect(self.handle_host_key_inspected)
        worker.signals.failed.connect(self.handle_ssh_failure)
        self._ssh_worker = worker
        self.thread_pool.start(worker)

    @Slot(object)
    def handle_host_key_inspected(self, identity: HostIdentity) -> None:
        self.inspect_key_button.setEnabled(True)
        self._pending_host_identity = identity
        self.ssh_test_status.setText(
            f"Host key fingerprint: {identity.algorithm} {identity.fingerprint}\n"
            "Compare this against the device if you have another way to check it, then trust it."
        )
        self.ssh_test_status.setStyleSheet("color:#ffd18a;")
        self.ssh_test_log.appendPlainText(f"[inspect] {identity.algorithm} {identity.fingerprint}")
        self.trust_key_button.setEnabled(True)

    def trust_host_key(self) -> None:
        if self.profile is None or self._pending_host_identity is None:
            return
        self.trust_key_button.setEnabled(False)
        self.ssh_test_status.setText("Pinning the confirmed host key…")
        worker = SshEnrollWorker(
            self.ssh_service, self._ssh_host(), self.profile.ssh_port, self._pending_host_identity.fingerprint
        )
        worker.signals.finished.connect(self.handle_host_key_trusted)
        worker.signals.failed.connect(self.handle_ssh_failure)
        self._ssh_worker = worker
        self.thread_pool.start(worker)

    @Slot(object)
    def handle_host_key_trusted(self, identity: HostIdentity) -> None:
        self.ssh_test_status.setText("Host key pinned. Enter the key passphrase and test the login.")
        self.ssh_test_status.setStyleSheet("color:#63e6a6;")
        self.ssh_test_log.appendPlainText(f"[trust] pinned {identity.algorithm} {identity.fingerprint}")
        self.verify_login_button.setEnabled(True)

    def verify_login(self) -> None:
        if self.profile is None or self.key_pair is None:
            return
        self.verify_login_button.setEnabled(False)
        self.ssh_test_status.setText("Connecting and authenticating…")
        self.ssh_test_status.setStyleSheet("")
        worker = SshVerifyWorker(
            self.ssh_service,
            self._ssh_host(),
            self.profile.ssh_port,
            self.key_pair.private_key,
            self.ssh_test_passphrase.text(),
        )
        worker.signals.finished.connect(self.handle_login_verified)
        worker.signals.failed.connect(self.handle_ssh_failure)
        self._ssh_worker = worker
        self.thread_pool.start(worker)

    @Slot(object)
    def handle_login_verified(self, marker: str) -> None:
        self.verify_login_button.setEnabled(True)
        self.ssh_test_passphrase.clear()
        self.ssh_test_status.setText("SSH access confirmed with your enrolled key.")
        self.ssh_test_status.setStyleSheet("color:#63e6a6;")
        self.ssh_test_log.appendPlainText(f"[verify] authenticated command returned: {marker}")
        self._update_navigation()

    @Slot(str)
    def handle_ssh_failure(self, message: str) -> None:
        self.inspect_key_button.setEnabled(True)
        self.verify_login_button.setEnabled(bool(self._pending_host_identity))
        self.ssh_test_status.setText("That step failed. Review the log and try again.")
        self.ssh_test_status.setStyleSheet("color:#ff7b7b;")
        self.ssh_test_log.appendPlainText(f"[error] {message}")

    def confirm_and_flash(self) -> None:
        if self.key_pair is None or self.profile is None:
            return
        choice = QMessageBox.warning(
            self,
            "Confirm destructive write",
            f"This will overwrite the /data partition on your {self.profile.display_name} and "
            "reboot it. Do not disconnect the device during this process.\n\n"
            "Continue?",
            QMessageBox.StandardButton.Yes | QMessageBox.StandardButton.Cancel,
            QMessageBox.StandardButton.Cancel,
        )
        if choice != QMessageBox.StandardButton.Yes:
            return
        self.flash_button.setEnabled(False)
        self.continue_button.setEnabled(False)
        self.flash_log.clear()
        self.flash_status.setText("Flashing…")
        self.flash_status.setStyleSheet("color:#ffd18a;")
        worker = FlashWorker(self.key_pair.public_key)
        worker.signals.progress.connect(self.handle_flash_progress)
        worker.signals.finished.connect(self.handle_flash_finished)
        worker.signals.failed.connect(self.handle_flash_failed)
        self._flash_worker = worker
        self.thread_pool.start(worker)

    @Slot(object)
    def handle_flash_progress(self, progress: FlashProgress) -> None:
        self.flash_log.appendPlainText(f"[{progress.step}] {progress.message}")
        self.flash_status.setText(progress.message)

    @Slot()
    def handle_flash_finished(self) -> None:
        self.flash_button.setEnabled(True)
        self.flash_status.setText("Done. Waiting for the device to reboot and reconnect to Wi-Fi.")
        self.flash_status.setStyleSheet("color:#63e6a6;")
        self._update_navigation()

    @Slot(str)
    def handle_flash_failed(self, message: str) -> None:
        self.flash_button.setEnabled(True)
        self.flash_status.setText("Flashing failed. Review the log and try again.")
        self.flash_status.setStyleSheet("color:#ff7b7b;")
        self.flash_log.appendPlainText(f"[error] {message}")
        self._update_navigation()

    def set_profile(self, profile_id: ProfileId) -> None:
        self.profile_id = profile_id
        self.profile = PROFILES[profile_id]
        self.diagnostic_report = None
        self.key_pair = None
        self.installation_page = self.FLASH if self.profile.flash_enabled else self.BLOCKED
        self.steps.clear()
        step_three = "3  Write /data & enable SSH" if self.profile.flash_enabled else "3  Installation safety gate"
        step_four = "4  Authenticated SSH test" if self.profile.flash_enabled else "4  Authenticated SSH test (locked)"
        for step in (
            "1  USB connection & diagnostics",
            "2  Mandatory SSH key",
            step_three,
            step_four,
            "5  Mod manager (locked)",
        ):
            self.steps.addItem(step)
        self.connection_title.setText(f"Connect {self.profile.display_name}")
        self.connection_instructions.setText(
            f"Selected profile: {self.profile.hardware_profile} · {self.profile.architecture}\n"
            f"Expected USB transport: {self.profile.transport}\n\n{self.profile.setup_mode}"
        )
        self.blocker_text.setText(self.profile.blocker)
        self.flash_warning.setText(self.profile.blocker)
        self.flash_button.setEnabled(True)
        self.flash_status.setText("Ready to flash. Keep the device in Firmware Update Mode and connected.")
        self.flash_status.setStyleSheet("")
        self.flash_log.clear()
        self.diagnostic_status.setText("Diagnostics have not run.")
        self.diagnostic_status.setStyleSheet("")
        self.diagnostic_log.clear()
        self.private_key_path.clear()
        self.public_key_path.clear()
        self.passphrase.clear()
        self.confirm_passphrase.clear()
        self.key_status.setText("SSH key setup is required. There is no skip option.")
        self.ssh_host_input.clear()
        self.ssh_test_passphrase.clear()
        self.ssh_test_log.clear()
        self.ssh_test_status.setText("Enter the device's IP address to begin.")
        self.ssh_test_status.setStyleSheet("")
        self.inspect_key_button.setEnabled(True)
        self.trust_key_button.setEnabled(False)
        self.verify_login_button.setEnabled(False)
        self._pending_host_identity = None
        self.show_page(self.CONNECTION)

    _STEP_ROW_BY_PAGE = {BLOCKED: 2, FLASH: 2, SSH_TEST: 3}

    def show_page(self, page: int) -> None:
        self.page_stack.setCurrentIndex(page)
        # BLOCKED and FLASH are alternate pages for the same "step 3" row;
        # SSH_TEST is step 4 but sits one page index later than its row.
        self.steps.setCurrentRow(self._STEP_ROW_BY_PAGE.get(page, page))
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
        self._update_navigation()

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
        elif page in (self.BLOCKED, self.FLASH):
            self.show_page(self.SSH_KEY)
        else:
            self.show_page(page - 1)

    def go_forward(self) -> None:
        page = self.page_stack.currentIndex()
        if page == self.CONNECTION and self._diagnostics_allow_key_setup():
            self.show_page(self.SSH_KEY)
        elif page == self.SSH_KEY and self.key_pair is not None:
            self.show_page(self.installation_page)
        elif page == self.BLOCKED:
            QMessageBox.information(
                self,
                "Installation blocked",
                "Production installation remains disabled until the hardware and recovery requirements are validated.",
            )
        elif page == self.FLASH:
            # The destructive action only runs via the dedicated "Flash and
            # enable SSH" button with its own confirmation, never from this
            # Continue button - which just moves on to the SSH test page.
            self.show_page(self.SSH_TEST)
        elif page == self.SSH_TEST:
            # Mod management (step 5) is not built yet.
            pass

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
        elif page == self.FLASH:
            self.continue_button.setText("Continue")
            self.continue_button.setEnabled(True)
        elif page == self.SSH_TEST:
            self.continue_button.setText("Mod manager (locked)")
            self.continue_button.setEnabled(False)
            self.inspect_key_button.setEnabled(bool(self._ssh_host()))
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
        self.setWindowIcon(QIcon(str(ASSETS_DIR / "jogwheel-icon.svg")))
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
