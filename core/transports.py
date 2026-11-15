"""Read-only transport probes and fail-closed writer interfaces."""

from __future__ import annotations

import re
import subprocess
from dataclasses import dataclass
from pathlib import Path
from typing import Protocol

from core.usb_discovery import DeviceFamily, UsbDiscovery, UsbInterface, create_usb_discovery


class TransportError(RuntimeError):
    pass


class WriteBlockedError(TransportError):
    pass


@dataclass(frozen=True)
class UsbDevice:
    transport: str
    identifier: str
    verified_model: str | None = None
    protocol_version: str | None = None
    statuses: tuple[str, ...] = ()
    transport_verified: bool = False
    interface_details: UsbInterface | None = None


class Transport(Protocol):
    def probe(self, identifier: str | None = None) -> list[UsbDevice]: ...

    def install(self, package: Path) -> None: ...


class FastbootTransport:
    """Fastboot discovery. Writing remains disabled until release gates are met."""

    def __init__(self, executable: str = "fastboot") -> None:
        self.executable = executable

    def probe(self, identifier: str | None = None) -> list[UsbDevice]:
        try:
            result = subprocess.run(
                [self.executable, "devices"], capture_output=True, text=True, timeout=10, check=False
            )
        except (FileNotFoundError, subprocess.TimeoutExpired) as exc:
            raise TransportError(f"Fastboot probe failed: {exc}") from exc
        if result.returncode:
            raise TransportError(result.stderr.strip() or "Fastboot returned an error")
        devices = [
            UsbDevice("fastboot", line.split()[0], transport_verified=True)
            for line in result.stdout.splitlines()
            if line.strip() and len(line.split()) >= 2 and line.split()[1] == "fastboot"
        ]
        return [device for device in devices if identifier is None or device.identifier == identifier]

    def install(self, package: Path) -> None:
        raise WriteBlockedError("JC11S writing is disabled by the repository safety gates.")


_USB_ID = re.compile(r"\b15e4:d014\b", re.IGNORECASE)


class ImbupdTransport:
    """Safe JP24 USB-interface diagnostic; no protocol writes are implemented."""

    def __init__(
        self,
        system: str | None = None,
        usb_listing: str | None = None,
        discovery: UsbDiscovery | None = None,
    ) -> None:
        self.system = system
        self.usb_listing = usb_listing
        self.discovery = discovery or create_usb_discovery(system)

    def probe(self, identifier: str | None = None) -> list[UsbDevice]:
        if self.usb_listing is not None:  # Backward-compatible fixture path.
            matches = _USB_ID.findall(self.usb_listing)
            return [UsbDevice("imbupd", value.lower()) for value in matches]
        candidates = [
            device
            for device in self.discovery.discover()
            if DeviceFamily.JP24 in device.candidate_families
            and (identifier is None or device.discovery_key == identifier)
        ]
        return [
            UsbDevice(
                "imbupd",
                device.discovery_key,
                statuses=(
                    "USB device discovered",
                    f"VID/PID: {device.vid_pid}",
                    "Prime 4 G2 family candidate (inferred)",
                    "imbupd protocol remains unverified",
                ),
                transport_verified=False,
            )
            for device in candidates
        ]

    def install(self, package: Path) -> None:
        raise WriteBlockedError("JP24 writing is disabled: no validated imbupd implementation is present.")


def redacted_identifier(identifier: str) -> str:
    if len(identifier) <= 4:
        return "****"
    return f"{identifier[:2]}…{identifier[-2:]}"
