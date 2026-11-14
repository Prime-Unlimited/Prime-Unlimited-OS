"""Read-only transport probes and fail-closed writer interfaces."""

from __future__ import annotations

import platform
import re
import subprocess
from dataclasses import dataclass
from pathlib import Path
from typing import Protocol


class TransportError(RuntimeError):
    pass


class WriteBlockedError(TransportError):
    pass


@dataclass(frozen=True)
class UsbDevice:
    transport: str
    identifier: str
    verified_model: str | None = None


class Transport(Protocol):
    def probe(self) -> list[UsbDevice]: ...

    def install(self, package: Path) -> None: ...


class FastbootTransport:
    """Fastboot discovery. Writing remains disabled until release gates are met."""

    def __init__(self, executable: str = "fastboot") -> None:
        self.executable = executable

    def probe(self) -> list[UsbDevice]:
        try:
            result = subprocess.run(
                [self.executable, "devices"], capture_output=True, text=True, timeout=10, check=False
            )
        except (FileNotFoundError, subprocess.TimeoutExpired) as exc:
            raise TransportError(f"Fastboot probe failed: {exc}") from exc
        if result.returncode:
            raise TransportError(result.stderr.strip() or "Fastboot returned an error")
        return [
            UsbDevice("fastboot", line.split()[0])
            for line in result.stdout.splitlines()
            if line.strip() and len(line.split()) >= 2 and line.split()[1] == "fastboot"
        ]

    def install(self, package: Path) -> None:
        raise WriteBlockedError("JC11S writing is disabled by the repository safety gates.")


_USB_ID = re.compile(r"\b15e4:d014\b", re.IGNORECASE)


class ImbupdTransport:
    """Safe JP24 USB-interface diagnostic; no protocol writes are implemented."""

    def __init__(self, system: str | None = None, usb_listing: str | None = None) -> None:
        self.system = system or platform.system()
        self.usb_listing = usb_listing

    def probe(self) -> list[UsbDevice]:
        # VID:PID is transport evidence only and never verifies the product model.
        listing = self.usb_listing if self.usb_listing is not None else self._system_usb_listing()
        matches = _USB_ID.findall(listing)
        return [UsbDevice("imbupd", value.lower()) for value in matches]

    def _system_usb_listing(self) -> str:
        commands = {
            "Linux": ["lsusb"],
            "Darwin": ["system_profiler", "SPUSBDataType"],
            "Windows": [
                "powershell",
                "-NoProfile",
                "-Command",
                "Get-PnpDevice -PresentOnly | Select-Object -ExpandProperty InstanceId",
            ],
        }
        command = commands.get(self.system)
        if command is None:
            raise TransportError(f"USB diagnostics are not supported on {self.system}.")
        try:
            result = subprocess.run(command, capture_output=True, text=True, timeout=15, check=False)
        except (FileNotFoundError, subprocess.TimeoutExpired) as exc:
            raise TransportError(f"USB interface probe failed: {exc}") from exc
        if result.returncode:
            raise TransportError(result.stderr.strip() or "USB interface probe returned an error")
        # Windows uses USB\VID_15E4&PID_D014 rather than the lsusb form.
        return re.sub(r"(?i)VID_15E4&PID_D014", "15e4:d014", result.stdout)

    def install(self, package: Path) -> None:
        raise WriteBlockedError("JP24 writing is disabled: no validated imbupd implementation is present.")


def redacted_identifier(identifier: str) -> str:
    if len(identifier) <= 4:
        return "****"
    return f"{identifier[:2]}…{identifier[-2:]}"
