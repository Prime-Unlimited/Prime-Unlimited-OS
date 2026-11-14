from pathlib import Path

from PySide6.QtCore import QSettings
from PySide6.QtWidgets import QApplication

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
    for state in (DetectionState.NO_DEVICE, DetectionState.WRONG_PROFILE, DetectionState.AMBIGUOUS):
        window.workflow.handle_diagnostic_report(report(state))
        assert not window.workflow.continue_button.isEnabled()

    window.close()
    app.processEvents()
