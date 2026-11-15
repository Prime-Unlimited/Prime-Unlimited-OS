from pathlib import Path

from PySide6.QtCore import QSettings
from PySide6.QtWidgets import QApplication, QMessageBox

from app.window import MainWindow, WorkflowPage
from core.device_detection import DetectionResult, DetectionState
from core.diagnostic_service import DiagnosticReport
from core.jc11s_flash import FlashPlan
from core.keys import generate_keypair
from core.profiles import ProfileId
from core.usb_discovery import (
    CandidateTransport,
    DeviceFamily,
    DiscoveredUsbDevice,
    VerificationState,
)
from core.windows_installer import HardwareReleasePolicy, InstallerArtifacts


def application():
    return QApplication.instance() or QApplication([])


def report(state: DetectionState) -> DiagnosticReport:
    return DiagnosticReport(
        DetectionResult(state, f"Result: {state.value}", state == DetectionState.MATCH),
        ("Read-only diagnostic fixture",),
    )


def test_wizard_navigation_requires_diagnostics_and_ssh_key(tmp_path: Path):
    app = application()
    window = MainWindow(QSettings(str(tmp_path / "settings.ini"), QSettings.Format.IniFormat))
    window.open_workflow(ProfileId.JC11S)
    wizard = window.workflow

    assert "SHIFT" in wizard.connection_instructions.text()
    assert not wizard.continue_button.isEnabled()

    wizard.handle_diagnostic_report(report(DetectionState.MATCH))
    assert wizard.continue_button.isEnabled()
    wizard.go_forward()
    assert wizard.page_stack.currentIndex() == WorkflowPage.SSH_KEY
    assert not wizard.continue_button.isEnabled()

    wizard._accept_key_pair(generate_keypair(tmp_path / "keys", "test passphrase"))
    assert wizard.continue_button.isEnabled()
    wizard.go_forward()
    assert wizard.page_stack.currentIndex() == WorkflowPage.INSTALLATION
    assert not wizard.continue_button.isEnabled()

    wizard.go_back()
    assert wizard.page_stack.currentIndex() == WorkflowPage.SSH_KEY
    window.close()
    app.processEvents()


def test_wrong_device_and_no_device_block_navigation(tmp_path: Path):
    app = application()
    window = MainWindow(QSettings(str(tmp_path / "settings.ini"), QSettings.Format.IniFormat))
    window.open_workflow(ProfileId.JP24)

    assert "USB-C" in window.workflow.connection_instructions.text()
    for state in (
        DetectionState.NO_DEVICE,
        DetectionState.WRONG_PROFILE,
        DetectionState.AMBIGUOUS,
        DetectionState.TRANSPORT_ERROR,
    ):
        window.workflow.handle_diagnostic_report(report(state))
        assert not window.workflow.continue_button.isEnabled()

    window.close()
    app.processEvents()


def test_verified_native_protocol_exposes_reboot_action(tmp_path: Path):
    app = application()
    window = MainWindow(QSettings(str(tmp_path / "settings.ini"), QSettings.Format.IniFormat))
    window.show()
    window.open_workflow(ProfileId.JC11S)
    native_report = DiagnosticReport(
        DetectionResult(DetectionState.UNVERIFIED, "Transport verified; model unverified.", False),
        ("USB device detected", "Fastboot protocol verified", "fastboot: protocol version 0.4"),
        fastboot_version="0.4",
        reboot_available=True,
    )

    window.workflow.handle_diagnostic_report(native_report)

    assert not window.workflow.reboot_button.isHidden()
    assert window.workflow.reboot_button.isEnabled()
    assert "version 0.4" in window.workflow.diagnostic_log.toPlainText()
    window.close()
    app.processEvents()


def test_reboot_requires_confirmation_and_runs_only_after_yes(tmp_path: Path, monkeypatch):
    class RebootService:
        def __init__(self):
            self.calls = 0

        def reboot_jc11s(self):
            self.calls += 1
            return "Device disconnected after the reboot command."

    class ImmediatePool:
        def start(self, worker):
            worker.run()

    app = application()
    service = RebootService()
    window = MainWindow(
        QSettings(str(tmp_path / "settings.ini"), QSettings.Format.IniFormat),
        diagnostic_service=service,
        thread_pool=ImmediatePool(),
    )
    window.show()
    window.open_workflow(ProfileId.JC11S)
    native_report = DiagnosticReport(
        DetectionResult(DetectionState.UNVERIFIED, "Protocol verified.", False),
        ("Fastboot protocol verified",),
        fastboot_version="0.4",
        reboot_available=True,
    )
    window.workflow.handle_diagnostic_report(native_report)

    monkeypatch.setattr(
        QMessageBox,
        "question",
        lambda *args, **kwargs: QMessageBox.StandardButton.No,
    )
    window.workflow.request_reboot()
    assert service.calls == 0

    monkeypatch.setattr(
        QMessageBox,
        "question",
        lambda *args, **kwargs: QMessageBox.StandardButton.Yes,
    )
    window.workflow.request_reboot()
    assert service.calls == 1
    assert window.workflow.diagnostic_report is None
    window.close()
    app.processEvents()


def test_wizard_displays_candidates_and_passes_explicit_selection(tmp_path: Path):
    candidates = tuple(
        DiscoveredUsbDevice(
            discovery_key=f"port-{number}",
            display_name="PRIME 4 G2 — Possible new hardware revision",
            vendor_id=0x9999,
            product_id=0x1200 + number,
            candidate_families=(DeviceFamily.JP24,),
            candidate_transports=(CandidateTransport.IMBUPD,),
            verification=VerificationState(candidate=True),
            source="macOS IORegistry",
        )
        for number in (1, 2)
    )
    diagnostic_report = DiagnosticReport(
        DetectionResult(DetectionState.AMBIGUOUS, "Select one candidate.", False),
        ("Two candidates",),
        candidates=candidates,
    )

    class Service:
        def __init__(self):
            self.selected = None

        def probe(self, profile, selected_device=None):
            self.selected = selected_device
            return diagnostic_report

    class ImmediatePool:
        def start(self, worker):
            worker.run()

    app = application()
    service = Service()
    window = MainWindow(
        QSettings(str(tmp_path / "settings.ini"), QSettings.Format.IniFormat),
        diagnostic_service=service,
        thread_pool=ImmediatePool(),
    )
    window.show()
    window.open_workflow(ProfileId.JP24)
    window.workflow.handle_diagnostic_report(diagnostic_report)
    assert window.workflow.candidate_selector.count() == 2
    assert "9999:1201" in window.workflow.candidate_details.text()
    assert "Installation: Blocked" in window.workflow.candidate_details.text()
    window.workflow.candidate_selector.setCurrentIndex(1)
    window.workflow.run_diagnostics()
    assert service.selected == "port-2"
    assert not window.workflow.continue_button.isEnabled()
    window.close()
    app.processEvents()


def test_wizard_displays_verified_product_and_blocks_unknown_identity(tmp_path: Path):
    app = application()
    window = MainWindow(QSettings(str(tmp_path / "settings.ini"), QSettings.Format.IniFormat))
    window.show()
    window.open_workflow(ProfileId.JC11S)
    candidate = DiscoveredUsbDevice(
        discovery_key="port-1",
        display_name="Prime 4+",
        vendor_id=0x15E4,
        product_id=0xD007,
        candidate_families=(DeviceFamily.JC11S,),
        candidate_transports=(CandidateTransport.FASTBOOT,),
        verification=VerificationState(
            candidate=True,
            transport_detected=True,
            protocol_verified=True,
            model_verified=True,
        ),
        source="Windows SetupAPI",
        bootloader="0.4",
    )
    verified = DiagnosticReport(
        DetectionResult(DetectionState.MATCH, "Device identity verified.", True),
        ("fastboot: product rk3288-az01-jc11s",),
        fastboot_version="0.4",
        fastboot_product="rk3288-az01-jc11s",
        candidates=(candidate,),
    )
    window.workflow.handle_diagnostic_report(verified)
    assert "Model identity: Verified (rk3288-az01-jc11s)" in window.workflow.candidate_details.text()
    assert window.workflow.continue_button.isEnabled()

    unknown = DiagnosticReport(
        DetectionResult(DetectionState.UNVERIFIED, "Identity unavailable.", False),
        ("product unavailable",),
        fastboot_version="0.4",
        candidates=(candidate,),
    )
    window.workflow.handle_diagnostic_report(unknown)
    assert "Model identity: Unverified" in window.workflow.candidate_details.text()
    assert not window.workflow.continue_button.isEnabled()
    window.close()
    app.processEvents()


def test_wizard_offers_safe_view_for_unknown_usb_hardware(tmp_path: Path):
    app = application()
    window = MainWindow(QSettings(str(tmp_path / "settings.ini"), QSettings.Format.IniFormat))
    window.show()
    window.open_workflow(ProfileId.JP24)
    unknown = DiscoveredUsbDevice(
        discovery_key="unknown-port",
        display_name="Unknown USB device",
        vendor_id=0x1234,
        product_id=0x5678,
        source="macOS IORegistry",
    )
    unknown_report = DiagnosticReport(
        DetectionResult(DetectionState.NO_DEVICE, "No supported USB device was found.", False),
        ("One unknown USB device",),
        discovered_devices=(unknown,),
    )
    window.workflow.handle_diagnostic_report(unknown_report)
    assert window.workflow.candidate_selector.count() == 1
    assert "unknown family" in window.workflow.candidate_selector.currentText()
    assert "1234:5678" in window.workflow.candidate_details.text()
    assert "Installation: Blocked" in window.workflow.candidate_details.text()
    assert not window.workflow.continue_button.isEnabled()
    window.close()
    app.processEvents()


class ImmediatePool:
    def start(self, worker):
        worker.run()


class FakeInstaller:
    def __init__(self, artifacts, policy=None, ssh_error=None):
        self.artifacts = artifacts
        self.release_policy = policy or HardwareReleasePolicy()
        self.flash_calls = 0
        self.ssh_error = ssh_error
        self.ssh = self

    def prepare(self, public_key, identifier=None):
        return self.artifacts

    def flash(self, artifacts, confirmed, identifier=None):
        self.flash_calls += 1
        return "rebooted"

    def enroll_host_key(self, *args):
        if self.ssh_error:
            raise RuntimeError(self.ssh_error)

    def verify_ssh(self, *args):
        if self.ssh_error:
            raise RuntimeError(self.ssh_error)
        return "PRIME_UNLIMITED_SSH_OK"


def installer_artifacts(tmp_path):
    root = tmp_path / "artifacts"
    root.mkdir()
    image = root / "data.ext4"
    image.write_bytes(b"raw")
    sparse = root / "sparse"
    sparse.mkdir()
    part = sparse / "data-001-of-001.sparse"
    part.write_bytes(b"sparse")
    plan = FlashPlan("data", image, 123456, "a" * 64, 512 * 1024**2, sparse, (part,))
    return InstallerArtifacts(root, image, sparse, plan)


def configured_installer_window(tmp_path, policy=None, ssh_error=None):
    app = application()
    installer = FakeInstaller(installer_artifacts(tmp_path), policy, ssh_error)
    window = MainWindow(
        QSettings(str(tmp_path / "settings.ini"), QSettings.Format.IniFormat),
        thread_pool=ImmediatePool(),
        installer_service=installer,
    )
    window.open_workflow(ProfileId.JC11S)
    wizard = window.workflow
    wizard.handle_diagnostic_report(report(DetectionState.MATCH))
    wizard.go_forward()
    wizard._accept_key_pair(generate_keypair(tmp_path / "keys", "test passphrase"))
    wizard.go_forward()
    return app, window, wizard, installer


def test_full_gui_build_review_and_recovery_gate(tmp_path):
    app, window, wizard, installer = configured_installer_window(tmp_path)
    assert wizard.page_stack.currentIndex() == WorkflowPage.PREPARE
    wizard.prepare_image()
    assert wizard.artifacts is installer.artifacts
    assert "123,456" in wizard.plan_summary.text()
    wizard.go_forward()
    assert wizard.page_stack.currentIndex() == WorkflowPage.REVIEW
    assert not wizard.continue_button.isEnabled()
    wizard.recovery_confirmation.setChecked(True)
    wizard.data_loss_confirmation.setChecked(True)
    wizard.erase_phrase.setText("ERASE DATA")
    assert wizard.continue_button.isEnabled()
    wizard.go_forward()
    assert wizard.page_stack.currentIndex() == WorkflowPage.FLASH
    assert not wizard.flash_button.isEnabled()
    assert "recovery path" in wizard.blocker_text.text()
    assert installer.flash_calls == 0
    window.close()
    app.processEvents()


def test_final_user_cancel_sends_no_flash(tmp_path, monkeypatch):
    policy = HardwareReleasePolicy(True, True, True, True)
    app, window, wizard, installer = configured_installer_window(tmp_path, policy)
    wizard.prepare_image()
    wizard.go_forward()
    wizard.recovery_confirmation.setChecked(True)
    wizard.data_loss_confirmation.setChecked(True)
    wizard.erase_phrase.setText("ERASE DATA")
    wizard.go_forward()
    assert wizard.flash_button.isEnabled()
    monkeypatch.setattr(QMessageBox, "warning", lambda *args, **kwargs: QMessageBox.StandardButton.No)
    wizard.start_flash()
    assert installer.flash_calls == 0
    assert "cancelled" in wizard.flash_status.text().lower()
    window.close()
    app.processEvents()


def test_ssh_connection_failure_is_shown(tmp_path):
    policy = HardwareReleasePolicy(True, True, True, True)
    app, window, wizard, _ = configured_installer_window(tmp_path, policy, "connection refused")
    wizard._flash_succeeded = True
    wizard.show_page(WorkflowPage.SSH_TEST)
    wizard.ssh_host.setText("192.0.2.1")
    wizard.ssh_fingerprint.setText("SHA256:trusted")
    wizard.test_ssh()
    assert "connection refused" in wizard.ssh_status.text()
    window.close()
    app.processEvents()
