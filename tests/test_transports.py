from pathlib import Path

import pytest

from core.transports import ImbupdTransport, WriteBlockedError
from core.usb_discovery import (
    CandidateTransport,
    DeviceFamily,
    DiscoveredUsbDevice,
    VerificationState,
)


def test_jp24_usb_id_is_only_unverified_transport_evidence():
    devices = ImbupdTransport("linux", "Bus 001 Device 002: ID 15e4:d014 vendor").probe()
    assert len(devices) == 1
    assert devices[0].transport == "imbupd"
    assert devices[0].verified_model is None


def test_multiple_jp24_interfaces_are_preserved_as_ambiguous():
    assert len(ImbupdTransport("linux", "ID 15e4:d014 first\nID 15E4:D014 second").probe()) == 2


def test_imbupd_writer_fails_closed():
    with pytest.raises(WriteBlockedError):
        ImbupdTransport("linux").install(Path("payload.img"))


class Discovery:
    def __init__(self, devices):
        self.devices = devices

    def discover(self):
        return self.devices


def test_imbupd_uses_normalized_macos_candidate_without_claiming_protocol_support():
    candidate = DiscoveredUsbDevice(
        discovery_key="ioreg:port-1",
        display_name="PRIME 4 G2",
        vendor_id=0x15E4,
        product_id=0xD014,
        candidate_families=(DeviceFamily.JP24,),
        candidate_transports=(CandidateTransport.IMBUPD,),
        verification=VerificationState(candidate=True),
        source="macOS IORegistry",
    )
    devices = ImbupdTransport(discovery=Discovery([candidate])).probe()
    assert len(devices) == 1
    assert devices[0].identifier == "ioreg:port-1"
    assert not devices[0].transport_verified
    assert "imbupd protocol remains unverified" in devices[0].statuses


def test_imbupd_can_select_one_candidate_from_multiple_revisions():
    candidates = [
        DiscoveredUsbDevice(
            discovery_key=f"ioreg:port-{number}",
            display_name="PRIME 4 G2",
            candidate_families=(DeviceFamily.JP24,),
            candidate_transports=(CandidateTransport.IMBUPD,),
            verification=VerificationState(candidate=True),
        )
        for number in (1, 2)
    ]
    devices = ImbupdTransport(discovery=Discovery(candidates)).probe("ioreg:port-2")
    assert [device.identifier for device in devices] == ["ioreg:port-2"]
