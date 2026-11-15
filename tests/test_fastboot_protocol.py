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


def test_data_response_is_parsed():
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


def test_download_stream_and_flash_data_only(tmp_path):
    payload = tmp_path / "part.sparse"
    payload.write_bytes(b"sparse payload")
    io = FakeIo([b"DATA0000000e", b"INFOwriting", b"OKAY", b"OKAYflashed"])
    session = FastbootSession(io)
    progress = []
    assert (
        session.download_file(payload, 1024, lambda sent, total: progress.append((sent, total))).kind
        == ResponseKind.OKAY
    )
    assert session.flash("data").payload == "flashed"
    assert io.commands == [b"download:0000000e", b"sparse payload", b"flash:data"]
    assert progress == [(14, 14)]


def test_download_fail_and_wrong_data_size_stop_before_payload(tmp_path):
    payload = tmp_path / "part.sparse"
    payload.write_bytes(b"payload")
    with pytest.raises(FastbootCommandError):
        FastbootSession(FakeIo([b"FAILbuffer full"])).download_file(payload, 1024)
    io = FakeIo([b"DATA00000008"])
    with pytest.raises(FastbootProtocolError, match="accepted"):
        FastbootSession(io).download_file(payload, 1024)
    assert io.commands == [b"download:00000007"]


def test_download_limit_and_partition_allowlist(tmp_path):
    payload = tmp_path / "part.sparse"
    payload.write_bytes(b"payload")
    with pytest.raises(ValueError, match="below"):
        FastbootSession(FakeIo([])).download_file(payload, len(b"payload"))
    with pytest.raises(ValueError, match="only permits"):
        FastbootSession(FakeIo([])).flash("userdata")
