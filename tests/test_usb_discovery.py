import json
import plistlib
from dataclasses import dataclass

import pytest

from core.usb_discovery import (
    CandidateTransport,
    DeviceFamily,
    MacOsUsbDiscovery,
    VerificationState,
    identify_candidate,
)


@dataclass
class Result:
    stdout: str = ""
    returncode: int = 0
    stderr: str = ""


class Runner:
    def __init__(self, responses):
        self.responses = responses
        self.commands = []

    def __call__(self, command):
        self.commands.append(command)
        return self.responses.get(tuple(command), Result(returncode=1))


def ioreg_result(*nodes):
    return Result(plistlib.dumps(list(nodes), fmt=plistlib.FMT_XML).decode("utf-8"))


@pytest.mark.parametrize(
    "vendor,product_id,name,family,transport",
    [
        (0x15E4, 0xD007, None, DeviceFamily.JC11S, CandidateTransport.FASTBOOT),
        (0x9999, 0x0001, "Denon DJ PRIME 4 Plus Rev B", DeviceFamily.JC11S, CandidateTransport.FASTBOOT),
        (0x15E4, 0xD014, None, DeviceFamily.JP24, CandidateTransport.IMBUPD),
        (0x9999, 0x0002, "PRIME 4 G2 Rev B", DeviceFamily.JP24, CandidateTransport.IMBUPD),
    ],
)
def test_known_and_changed_identifiers_are_discoverable_candidates(vendor, product_id, name, family, transport):
    families, transports, _ = identify_candidate(
        vendor_id=vendor,
        product_id=product_id,
        manufacturer="Denon DJ",
        product=name,
    )
    assert family in families
    assert transport in transports


def test_product_name_can_be_ambiguous_but_never_verifies_model():
    families, _, evidence = identify_candidate(
        vendor_id=0x9999,
        product_id=0x9999,
        manufacturer=None,
        product="PRIME 4 Plus / PRIME 4 G2 engineering sample",
    )
    assert set(families) == {DeviceFamily.JC11S, DeviceFamily.JP24}
    assert len(evidence) == 2


def test_unknown_hardware_with_missing_strings_remains_only_discovered():
    families, transports, evidence = identify_candidate(
        vendor_id=0x1234,
        product_id=0x5678,
        manufacturer=None,
        product=None,
    )
    assert families == transports == evidence == ()


def test_installation_authorization_requires_independent_verification():
    state = VerificationState(candidate=True, model_verified=True)
    assert not state.revision_verified
    assert not state.installation_authorized
    with pytest.raises(ValueError, match="requires"):
        VerificationState(installation_authorized=True)


def test_macos_ioreg_finds_jp24_when_system_profiler_would_be_empty():
    ioreg = ioreg_result(
        {
            "idVendor": 0x15E4,
            "idProduct": 0xD014,
            "USB Product Name": "PRIME 4 G2",
            "USB Vendor Name": "Denon DJ",
            "locationID": 0x14100000,
        }
    )
    runner = Runner(
        {
            ("ioreg", "-a", "-r", "-c", "IOUSBHostDevice"): ioreg,
            ("system_profiler", "-json", "SPUSBDataType"): Result(""),
        }
    )
    devices = MacOsUsbDiscovery(runner).discover()
    assert len(devices) == 1
    assert devices[0].vid_pid == "15E4:D014"
    assert DeviceFamily.JP24 in devices[0].candidate_families
    assert devices[0].source == "macOS IORegistry"
    assert not devices[0].verification.model_verified
    assert not any(command[0] == "system_profiler" for command in runner.commands)


def test_macos_changed_jp24_identifiers_remain_unverified_candidate():
    runner = Runner(
        {
            ("ioreg", "-a", "-r", "-c", "IOUSBHostDevice"): ioreg_result(
                {
                    "idVendor": 0x9999,
                    "idProduct": 0x1234,
                    "USB Product Name": "PRIME 4 G2 Future Revision",
                }
            )
        }
    )
    device = MacOsUsbDiscovery(runner).discover()[0]
    assert device.vid_pid == "9999:1234"
    assert DeviceFamily.JP24 in device.candidate_families
    assert device.verification.candidate
    assert not device.verification.revision_verified
    assert not device.verification.installation_authorized


def test_macos_handles_multiple_devices_and_missing_manufacturer():
    runner = Runner(
        {
            ("ioreg", "-a", "-r", "-c", "IOUSBHostDevice"): ioreg_result(
                {"idVendor": 0x15E4, "idProduct": 0xD014, "USB Product Name": "PRIME 4 G2"},
                {"idVendor": 0x1234, "idProduct": 0x5678},
            )
        }
    )
    devices = MacOsUsbDiscovery(runner).discover()
    assert len(devices) == 2
    assert devices[0].manufacturer is None
    assert devices[1].display_name == "Unknown USB device"


def test_macos_uses_structured_profiler_fallback_when_ioreg_is_empty():
    payload = {
        "SPUSBDataType": [
            {
                "_items": [
                    {
                        "_name": "PRIME 4 G2",
                        "vendor_id": "0x15e4",
                        "product_id": "0xd014",
                        "manufacturer": "Denon DJ",
                    }
                ]
            }
        ]
    }
    runner = Runner(
        {
            ("ioreg", "-a", "-r", "-c", "IOUSBHostDevice"): Result(""),
            ("ioreg", "-a", "-p", "IOUSB", "-l", "-w", "0"): Result(""),
            ("system_profiler", "-json", "SPUSBDataType"): Result(json.dumps(payload)),
        }
    )
    device = MacOsUsbDiscovery(runner).discover()[0]
    assert device.source == "macOS system_profiler"
    assert device.vid_pid == "15E4:D014"
