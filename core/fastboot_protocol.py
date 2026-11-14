"""Minimal, non-destructive Fastboot protocol framing."""

from __future__ import annotations

from dataclasses import dataclass
from enum import StrEnum
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
    """Allows diagnostic variables and reboot; download/flash/erase are unreachable."""

    def __init__(self, io: FastbootIo) -> None:
        self.io = io

    def getvar(self, name: str) -> FastbootResponse:
        if not name or any(character not in "abcdefghijklmnopqrstuvwxyz0123456789-_.:" for character in name.lower()):
            raise ValueError("Fastboot variable name contains unsupported characters.")
        return self._exchange(f"getvar:{name}")

    def reboot(self) -> FastbootResponse:
        return self._exchange("reboot")

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
