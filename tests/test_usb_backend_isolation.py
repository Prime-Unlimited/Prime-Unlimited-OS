"""USB backend selection regression checks; no hardware access or flashing."""
from unittest.mock import Mock, patch

import pytest

from core.profiles import ProfileId
from core.diagnostic_service import UsbDiagnosticService as Jp24Diagnostics
from core.jc11s_diagnostic_service import UsbDiagnosticService as Jc11sDiagnostics


def test_jp24_service_does_not_construct_backends_at_startup():
    with patch("core.diagnostic_service.ImbupdTransport") as jp, patch.object(
        Jp24Diagnostics, "_default_fastboot"
    ) as fastboot:
        service = Jp24Diagnostics()
        jp.assert_not_called()
        fastboot.assert_not_called()
        service.probe(ProfileId.JP24)
        jp.assert_called_once()
        fastboot.assert_not_called()


def test_jp24_probe_does_not_touch_fastboot():
    fastboot, imbupd = Mock(), Mock()
    imbupd.probe.return_value = []
    report = Jp24Diagnostics(fastboot=fastboot, imbupd=imbupd).probe(ProfileId.JP24)
    fastboot.probe.assert_not_called()
    imbupd.probe.assert_called_once()
    assert not report.result.may_continue


def test_jc11s_probe_does_not_touch_imbupd():
    fastboot, imbupd, discovery = Mock(), Mock(), Mock()
    fastboot.probe.return_value = []
    discovery.discover.return_value = []
    report = Jc11sDiagnostics(fastboot=fastboot, imbupd=imbupd, discovery=discovery).probe(
        ProfileId.JC11S
    )
    imbupd.probe.assert_not_called()
    fastboot.probe.assert_called_once()
    assert not report.result.may_continue


def test_jc11s_diagnostics_are_lazy():
    with patch("core.jc11s_diagnostic_service.create_usb_discovery") as discovery, patch.object(
        Jc11sDiagnostics, "_default_fastboot"
    ) as fastboot:
        Jc11sDiagnostics()
        discovery.assert_not_called()
        fastboot.assert_not_called()


def test_wrong_family_cannot_enter_jc11s_diagnostics():
    with pytest.raises(ValueError):
        Jc11sDiagnostics().probe(ProfileId.JP24)


def test_jp24_uses_historical_ioreg_transport():
    from core.transports import ImbupdTransport
    with patch("core.transports.subprocess.run") as run:
        import plistlib
        run.return_value.returncode = 0
        run.return_value.stdout = plistlib.dumps([
            {"IORegistryEntryChildren": [
                {"IORegistryEntryChildren": [
                    {"idVendor": 0x15E4, "idProduct": 0xD014}
                ]}
            ]}
        ])
        devices = ImbupdTransport(system="Darwin").probe()
        assert len(devices) == 1
        assert devices[0].identifier == "15e4:d014"
        assert run.call_args.args[0] == ["ioreg", "-p", "IOUSB", "-a", "-l", "-w0"]


def test_gui_workflows_created_only_after_selection():
    from PySide6.QtCore import QSettings
    from PySide6.QtWidgets import QApplication
    from app.window import MainWindow
    app = QApplication.instance() or QApplication([])
    window = MainWindow(QSettings("Prime-Unlimited", "USB-Isolation-Test"))
    assert window.workflow is None
    assert window.jc11s_workflow is None
    window.open_workflow(ProfileId.JP24)
    assert window.workflow is not None
    assert window.jc11s_workflow is None
    window.close()
    app.processEvents()
