from pathlib import Path

import pytest

from core.transports import ImbupdTransport, WriteBlockedError


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
