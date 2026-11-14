from core.device_detection import DetectionState
from core.diagnostic_service import UsbDiagnosticService
from core.profiles import ProfileId
from core.transports import TransportError, UsbDevice


class FakeProbe:
    def __init__(self, devices=None, error=None):
        self.devices = devices or []
        self.error = error

    def probe(self):
        if self.error:
            raise self.error
        return self.devices


def test_selected_transport_is_reported_unverified_without_positive_identity():
    service = UsbDiagnosticService(
        fastboot=FakeProbe([UsbDevice("fastboot", "serial-123")]),
        imbupd=FakeProbe(),
    )

    report = service.probe(ProfileId.JC11S)

    assert report.result.state == DetectionState.UNVERIFIED
    assert "se…23" in "\n".join(report.log_lines)


def test_opposite_transport_reports_wrong_profile_even_if_selected_probe_is_missing():
    service = UsbDiagnosticService(
        fastboot=FakeProbe(error=TransportError("fastboot is not installed")),
        imbupd=FakeProbe([UsbDevice("imbupd", "15e4:d014")]),
    )

    report = service.probe(ProfileId.JC11S)

    assert report.result.state == DetectionState.WRONG_PROFILE
    assert "unavailable" in "\n".join(report.log_lines)
