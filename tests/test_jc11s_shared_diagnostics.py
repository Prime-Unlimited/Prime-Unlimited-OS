"""Regression tests for native JC11S discovery in the shared wizard."""

from core.device_detection import DetectionState
from core.diagnostic_service import UsbDiagnosticService
from core.profiles import ProfileId
from core.transports import UsbDevice


class FakeFastboot:
    def __init__(self, product):
        self.product = product

    def probe(self):
        return [UsbDevice("fastboot", "USB\\VID_15E4&PID_D007", transport_verified=True)]

    def getvar(self, name, identifier=None):
        assert name == "product"
        return self.product


class FakeImbupd:
    def probe(self):
        return []


def test_verified_jc11s_product_unlocks_key_setup():
    report = UsbDiagnosticService(
        fastboot=FakeFastboot("rk3288-az01-jc11s"), imbupd=FakeImbupd()
    ).probe(ProfileId.JC11S)
    assert report.result.state == DetectionState.MATCH
    assert report.result.may_continue


def test_unrecognized_jc11s_product_cannot_unlock_key_setup():
    report = UsbDiagnosticService(
        fastboot=FakeFastboot("other-product"), imbupd=FakeImbupd()
    ).probe(ProfileId.JC11S)
    assert report.result.state == DetectionState.UNVERIFIED
    assert not report.result.may_continue


def test_native_winusb_selected_by_default_on_windows(monkeypatch):
    from core import diagnostic_service
    from core import windows_fastboot

    marker = object()
    monkeypatch.setattr(diagnostic_service.platform, "system", lambda: "Windows")
    monkeypatch.setattr(windows_fastboot, "create_windows_fastboot_transport", lambda: marker)
    service = UsbDiagnosticService(imbupd=FakeImbupd())
    assert service.fastboot is marker
