"""Minimal, non-destructive Fastboot protocol framing."""

from __future__ import annotations

from dataclasses import dataclass
from enum import StrEnum
from pathlib import Path
from typing import Protocol


class FastbootProtocolError(RuntimeError):
    pass


class FastbootCommandError(FastbootProtocolError):
    pass


class FastbootMalformedResponse(FastbootProtocolError):
    pass


class ResponseKind(StrEnum):
    OKAY = "OKAY"
    FAIL = "FAIL"
    INFO = "INFO"
    DATA = "DATA"


@dataclass(frozen=True)
class FastbootResponse:
    kind: ResponseKind
    payload: str
    info: tuple[str, ...] = ()


class FastbootIo(Protocol):
    def write(self, data: bytes) -> int: ...

    def read(self, size: int = 64) -> bytes: ...


def parse_response(packet: bytes) -> FastbootResponse:
    if len(packet) < 4:
        raise FastbootMalformedResponse("Fastboot response is shorter than its four-byte prefix.")
    try:
        prefix = packet[:4].decode("ascii")
        payload = packet[4:].decode("ascii")
    except UnicodeDecodeError as exc:
        raise FastbootMalformedResponse("Fastboot response is not ASCII.") from exc
    try:
        kind = ResponseKind(prefix)
    except ValueError as exc:
        raise FastbootMalformedResponse(f"Unknown Fastboot response prefix: {prefix!r}") from exc
    return FastbootResponse(kind, payload)


class FastbootSession:
    """Fastboot 0.4 framing. Callers own all destructive-operation policy gates."""

    def __init__(self, io: FastbootIo) -> None:
        self.io = io

    def getvar(self, name: str) -> FastbootResponse:
        if not name or any(character not in "abcdefghijklmnopqrstuvwxyz0123456789-_.:" for character in name.lower()):
            raise ValueError("Fastboot variable name contains unsupported characters.")
        return self._exchange(f"getvar:{name}")

    def reboot(self) -> FastbootResponse:
        return self._exchange("reboot")

    def download_file(self, path: Path, maximum_size: int, progress=None) -> FastbootResponse:
        size = path.stat().st_size
        if size <= 0 or size >= maximum_size or size > 0xFFFFFFFF:
            raise ValueError("Fastboot download must be non-empty and below the measured device limit.")
        response = self._exchange_until_data(f"download:{size:08x}")
        try:
            accepted = int(response.payload, 16)
        except ValueError as exc:
            raise FastbootMalformedResponse("Device returned a non-hexadecimal DATA size.") from exc
        if accepted != size:
            raise FastbootProtocolError(f"Device accepted {accepted} bytes, expected {size}.")
        sent = 0
        with path.open("rb") as stream:
            # Smaller writes avoid long single WinUSB requests while the
            # bootloader drains its receive buffer on USB 2 hardware.
            for chunk in iter(lambda: stream.read(64 * 1024), b""):
                written = self.io.write(chunk)
                if written != len(chunk):
                    raise FastbootProtocolError(f"Partial Fastboot payload transfer at byte {sent}.")
                sent += written
                if progress is not None:
                    progress(sent, size)
        return self._read_terminal()

    def flash(self, partition: str) -> FastbootResponse:
        if partition != "data":
            raise ValueError("This JC11S installer only permits the verified bootloader partition name 'data'.")
        return self._exchange(f"flash:{partition}")

    def _exchange(self, command: str) -> FastbootResponse:
        encoded = command.encode("ascii")
        written = self.io.write(encoded)
        if written != len(encoded):
            raise FastbootProtocolError(f"Partial Fastboot command transfer: wrote {written} of {len(encoded)} bytes.")
        info: list[str] = []
        for _ in range(128):
            response = parse_response(self.io.read(64))
            if response.kind == ResponseKind.INFO:
                info.append(response.payload)
                continue
            response = FastbootResponse(response.kind, response.payload, tuple(info))
            if response.kind == ResponseKind.FAIL:
                raise FastbootCommandError(response.payload or "Device rejected the Fastboot command.")
            return response
        raise FastbootMalformedResponse("Fastboot sent too many INFO responses without completion.")

    def _exchange_until_data(self, command: str) -> FastbootResponse:
        encoded = command.encode("ascii")
        if self.io.write(encoded) != len(encoded):
            raise FastbootProtocolError("Partial Fastboot download command transfer.")
        info = []
        for _ in range(128):
            response = parse_response(self.io.read(64))
            if response.kind == ResponseKind.INFO:
                info.append(response.payload)
                continue
            if response.kind == ResponseKind.FAIL:
                raise FastbootCommandError(response.payload or "Device rejected the download command.")
            if response.kind != ResponseKind.DATA:
                raise FastbootMalformedResponse(f"Expected DATA, received {response.kind}.")
            return FastbootResponse(response.kind, response.payload, tuple(info))
        raise FastbootMalformedResponse("Fastboot sent too many INFO responses before DATA.")

    def _read_terminal(self) -> FastbootResponse:
        info = []
        for _ in range(128):
            response = parse_response(self.io.read(64))
            if response.kind == ResponseKind.INFO:
                info.append(response.payload)
                continue
            if response.kind == ResponseKind.FAIL:
                raise FastbootCommandError(response.payload or "Device rejected the transferred payload.")
            if response.kind != ResponseKind.OKAY:
                raise FastbootMalformedResponse(f"Expected OKAY after payload, received {response.kind}.")
            return FastbootResponse(response.kind, response.payload, tuple(info))
        raise FastbootMalformedResponse("Fastboot sent too many INFO responses after the payload.")
