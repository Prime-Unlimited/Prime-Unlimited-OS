from core.device_detection import DetectionState
from core.diagnostic_service import UsbDiagnosticService
from core.profiles import ProfileId
from core.transports import TransportError, UsbDevice
from core.usb_discovery import (
    CandidateTransport,
    DeviceFamily,
    DiscoveredUsbDevice,
    VerificationState,
)


class FakeProbe:
    def __init__(self, devices=None, error=None):
        self.devices = devices or []
        self.error = error
        self.last_identifier = None

    def probe(self, identifier=None):
        self.last_identifier = identifier
        if self.error:
            raise self.error
        return [device for device in self.devices if identifier is None or device.identifier == identifier]


class Discovery:
    def __init__(self, devices):
        self.devices = devices

    def discover(self):
        return self.devices


def jp24_candidate(key):
    return DiscoveredUsbDevice(
        discovery_key=key,
        display_name="PRIME 4 G2 — Possible new hardware revision",
        vendor_id=0x9999,
        product_id=0x1234,
        candidate_families=(DeviceFamily.JP24,),
        candidate_transports=(CandidateTransport.IMBUPD,),
        verification=VerificationState(candidate=True),
        source="macOS IORegistry",
    )


def test_selected_transport_is_reported_unverified_without_positive_identity():
    service = UsbDiagnosticService(
        fastboot=FakeProbe(
            [
                UsbDevice(
                    "fastboot",
                    "serial-123",
                    protocol_version="0.4",
                    statuses=("WinUSB interface accessible", "Fastboot protocol verified"),
                    transport_verified=True,
                )
            ]
        ),
        imbupd=FakeProbe(),
    )

    report = service.probe(ProfileId.JC11S)

    assert report.result.state == DetectionState.UNVERIFIED
    assert "se…23" in "\n".join(report.log_lines)
    assert report.fastboot_version == "0.4"
    assert report.reboot_available is False  # The generic fixture deliberately has no reboot method.
    assert "Fastboot protocol verified" in "\n".join(report.log_lines)


def test_opposite_transport_reports_wrong_profile_even_if_selected_probe_is_missing():
    service = UsbDiagnosticService(
        fastboot=FakeProbe(error=TransportError("fastboot is not installed")),
        imbupd=FakeProbe([UsbDevice("imbupd", "15e4:d014")]),
    )

    report = service.probe(ProfileId.JC11S)

    assert report.result.state == DetectionState.WRONG_PROFILE
    assert "unavailable" in "\n".join(report.log_lines)


def test_selected_transport_access_error_is_not_misreported_as_no_device():
    service = UsbDiagnosticService(
        fastboot=FakeProbe(error=TransportError("access denied by Windows")),
        imbupd=FakeProbe(),
    )

    report = service.probe(ProfileId.JC11S)

    assert report.result.state == DetectionState.TRANSPORT_ERROR
    assert "access denied" in report.result.message


def test_multiple_revision_candidates_require_explicit_selection():
    candidates = [jp24_candidate("port-1"), jp24_candidate("port-2")]
    imbupd = FakeProbe(
        [UsbDevice("imbupd", candidate.discovery_key, transport_verified=False) for candidate in candidates]
    )
    service = UsbDiagnosticService(
        fastboot=FakeProbe(),
        imbupd=imbupd,
        discovery=Discovery(candidates),
    )

    ambiguous = service.probe(ProfileId.JP24)
    selected = service.probe(ProfileId.JP24, "port-2")

    assert ambiguous.result.state == DetectionState.AMBIGUOUS
    assert len(ambiguous.candidates) == 2
    assert selected.result.state == DetectionState.CANDIDATE
    assert imbupd.last_identifier == "port-2"
    assert not selected.result.may_continue
