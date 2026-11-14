from collections import deque

import pytest

from core.fastboot_protocol import (
    FastbootCommandError,
    FastbootMalformedResponse,
    FastbootProtocolError,
    FastbootSession,
    ResponseKind,
    parse_response,
)


class FakeIo:
    def __init__(self, responses, written=None):
        self.responses = deque(responses)
        self.commands = []
        self.written = written

    def write(self, data):
        self.commands.append(data)
        return len(data) if self.written is None else self.written

    def read(self, size=64):
        return self.responses.popleft()


def test_hardware_observation_getvar_version_okay_0_4():
    io = FakeIo([b"OKAY0.4"])
    response = FastbootSession(io).getvar("version")
    assert response.kind == ResponseKind.OKAY
    assert response.payload == "0.4"
    assert io.commands == [b"getvar:version"]


def test_info_packets_are_retained_until_final_response():
    response = FastbootSession(FakeIo([b"INFOone", b"INFOtwo", b"OKAYdone"])).getvar("product")
    assert response.info == ("one", "two")
    assert response.payload == "done"


def test_fail_response_raises_command_error():
    with pytest.raises(FastbootCommandError, match="unsupported"):
        FastbootSession(FakeIo([b"FAILunsupported"])).getvar("product")


def test_data_response_is_parsed_without_enabling_downloads():
    response = parse_response(b"DATA00001000")
    assert response.kind == ResponseKind.DATA
    assert response.payload == "00001000"


@pytest.mark.parametrize("packet", [b"", b"BAD", b"WHATvalue", b"OKAY\xff"])
def test_malformed_response_is_rejected(packet):
    with pytest.raises(FastbootMalformedResponse):
        parse_response(packet)


def test_partial_command_write_is_rejected():
    with pytest.raises(FastbootProtocolError, match="Partial"):
        FastbootSession(FakeIo([], written=3)).getvar("version")
