from pathlib import Path

import paramiko
from PySide6.QtCore import QSettings
from PySide6.QtWidgets import QApplication

from app.window import MainWindow, SshEnrollWorker, SshInspectWorker, SshVerifyWorker, WorkflowPage
from core.device_detection import DetectionResult, DetectionState
from core.diagnostic_service import DiagnosticReport
from core.keys import generate_keypair
from core.profiles import ProfileId
from core.ssh_service import HostIdentity, sha256_fingerprint


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
    # JC11S has no validated bootstrap - it stays behind the safety gate.
    assert wizard.page_stack.currentIndex() == WorkflowPage.BLOCKED
    assert not wizard.continue_button.isEnabled()

    wizard.go_back()
    assert wizard.page_stack.currentIndex() == WorkflowPage.SSH_KEY
    window.close()
    app.processEvents()


def test_jp24_navigation_reaches_the_validated_flash_page(tmp_path: Path):
    """JP24 has a validated bootstrap (see devices/jp24/README.md), so it
    reaches the real flash page instead of the safety-gate page."""
    app = application()
    window = MainWindow(QSettings(str(tmp_path / "settings.ini"), QSettings.Format.IniFormat))
    window.open_workflow(ProfileId.JP24)
    wizard = window.workflow

    wizard.handle_diagnostic_report(report(DetectionState.UNVERIFIED))
    wizard.go_forward()
    assert wizard.page_stack.currentIndex() == WorkflowPage.SSH_KEY

    wizard._accept_key_pair(generate_keypair(tmp_path / "keys", "test passphrase"))
    wizard.go_forward()
    assert wizard.page_stack.currentIndex() == WorkflowPage.FLASH
    assert wizard.continue_button.isEnabled()
    assert wizard.flash_button.isEnabled()

    wizard.go_back()
    assert wizard.page_stack.currentIndex() == WorkflowPage.SSH_KEY
    window.close()
    app.processEvents()


def test_flash_page_continue_reaches_ssh_test_page(tmp_path: Path):
    """Continuing past the flash page (not clicking the destructive 'Flash
    and enable SSH' button) moves on to the SSH test page, which stays
    behind the still-unbuilt mod manager step."""
    app = application()
    window = MainWindow(QSettings(str(tmp_path / "settings.ini"), QSettings.Format.IniFormat))
    window.open_workflow(ProfileId.JP24)
    wizard = window.workflow

    wizard.handle_diagnostic_report(report(DetectionState.UNVERIFIED))
    wizard.go_forward()
    wizard._accept_key_pair(generate_keypair(tmp_path / "keys", "test passphrase"))
    wizard.go_forward()
    assert wizard.page_stack.currentIndex() == WorkflowPage.FLASH

    wizard.go_forward()
    assert wizard.page_stack.currentIndex() == WorkflowPage.SSH_TEST
    assert not wizard.continue_button.isEnabled()
    assert wizard.continue_button.text() == "Mod manager (locked)"

    wizard.go_back()
    assert wizard.page_stack.currentIndex() == WorkflowPage.FLASH
    window.close()
    app.processEvents()


def test_ssh_host_key_inspect_trust_and_verify_flow(tmp_path: Path, monkeypatch):
    """Drives the SSH test page's three steps - inspect, trust, verify -
    by running each worker's run() synchronously (bypassing the real
    thread pool, which would make the test non-deterministic) against a
    fake SshService, and checking the resulting UI state after each."""
    app = application()
    window = MainWindow(QSettings(str(tmp_path / "settings.ini"), QSettings.Format.IniFormat))
    window.open_workflow(ProfileId.JP24)
    wizard = window.workflow
    wizard.ssh_host_input.setText("192.0.2.10")

    import app.window as window_module

    monkeypatch.setattr(window_module, "KNOWN_HOSTS_PATH", tmp_path / "known_hosts")

    key = paramiko.RSAKey.generate(1024)
    fingerprint = sha256_fingerprint(key)
    monkeypatch.setattr(wizard.ssh_service, "_remote_key", lambda host, port, timeout=8: key)
    monkeypatch.setattr(
        wizard.ssh_service,
        "verify_authenticated",
        lambda host, port, private_key, passphrase, known_hosts: "PRIME_UNLIMITED_SSH_OK",
    )

    inspect_worker = SshInspectWorker(wizard.ssh_service, "192.0.2.10", wizard.profile.ssh_port)
    inspect_worker.signals.finished.connect(wizard.handle_host_key_inspected)
    inspect_worker.run()
    assert wizard._pending_host_identity == HostIdentity(key.get_name(), fingerprint)
    assert wizard.trust_key_button.isEnabled()

    enroll_worker = SshEnrollWorker(wizard.ssh_service, "192.0.2.10", wizard.profile.ssh_port, fingerprint)
    enroll_worker.signals.finished.connect(wizard.handle_host_key_trusted)
    enroll_worker.run()
    assert wizard.verify_login_button.isEnabled()

    wizard.key_pair = generate_keypair(tmp_path / "keys", "test passphrase")
    wizard.ssh_test_passphrase.setText("test passphrase")
    verify_worker = SshVerifyWorker(
        wizard.ssh_service, "192.0.2.10", wizard.profile.ssh_port, wizard.key_pair.private_key, "test passphrase"
    )
    verify_worker.signals.finished.connect(wizard.handle_login_verified)
    verify_worker.run()
    assert "confirmed" in wizard.ssh_test_status.text()

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
