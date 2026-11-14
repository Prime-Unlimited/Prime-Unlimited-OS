from pathlib import Path

from PySide6.QtCore import QSettings
from PySide6.QtWidgets import QApplication, QMessageBox

from app.window import MainWindow, WorkflowPage
from core.device_detection import DetectionResult, DetectionState
from core.diagnostic_service import DiagnosticReport
from core.keys import generate_keypair
from core.profiles import ProfileId


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

    wizard.handle_diagnostic_report(report(DetectionState.UNVERIFIED))
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
