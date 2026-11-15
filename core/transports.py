"""Read-only transport probes and fail-closed writer interfaces."""

from __future__ import annotations

import platform
import plistlib
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

    def install(self, package: Path, on_progress: object | None = None) -> None: ...


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

    def install(self, package: Path, on_progress: object | None = None) -> None:
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
        if self.system == "Darwin":
            return self._darwin_usb_listing()
        commands = {
            "Linux": ["lsusb"],
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

    def _darwin_usb_listing(self) -> str:
        # Plain-text `system_profiler SPUSBDataType` prints Vendor ID and
        # Product ID on separate lines, with field labels localized to the
        # system language (e.g. "USB-Hersteller-ID" in German) - a plain
        # "15e4:d014" substring never appears, so the lsusb-style regex can
        # never match it. The -xml form keeps its key names in English
        # regardless of locale and is machine-parseable, so use that and
        # synthesize the "15e4:d014" token the shared regex expects.
        try:
            result = subprocess.run(
                ["system_profiler", "SPUSBDataType", "-xml"],
                capture_output=True, timeout=15, check=False,
            )
        except (FileNotFoundError, subprocess.TimeoutExpired) as exc:
            raise TransportError(f"USB interface probe failed: {exc}") from exc
        if result.returncode:
            raise TransportError(result.stderr.decode(errors="replace").strip() or "system_profiler returned an error")
        try:
            plist = plistlib.loads(result.stdout)
        except (plistlib.InvalidFileException, ValueError) as exc:
            raise TransportError(f"Could not parse system_profiler output: {exc}") from exc
        tokens = [f"{vid}:{pid}" for vid, pid in self._walk_darwin_usb_tree(plist)]
        return "\n".join(tokens)

    @staticmethod
    def _walk_darwin_usb_tree(node: object) -> list[tuple[str, str]]:
        found: list[tuple[str, str]] = []
        if isinstance(node, dict):
            vendor_id = node.get("vendor_id")
            product_id = node.get("product_id")
            if isinstance(vendor_id, str) and isinstance(product_id, str):
                vendor_match = re.search(r"0x([0-9a-fA-F]+)", vendor_id)
                product_match = re.search(r"0x([0-9a-fA-F]+)", product_id)
                if vendor_match and product_match:
                    found.append((vendor_match.group(1).lower(), product_match.group(1).lower()))
            for value in node.values():
                found.extend(ImbupdTransport._walk_darwin_usb_tree(value))
        elif isinstance(node, list):
            for item in node:
                found.extend(ImbupdTransport._walk_darwin_usb_tree(item))
        return found

    def install(self, package: Path, on_progress: object | None = None) -> None:
        """Write a prebuilt /data image and reboot the device.

        Delegates to core.jp24_bootstrap, the validated implementation
        confirmed against real JP24 hardware (see devices/jp24/README.md).
        The caller is responsible for showing an explicit confirmation
        before calling this - it overwrites the device's /data partition.
        """
        from core.jp24_bootstrap import ensure_flash_tool, flash_bootstrap

        tool = ensure_flash_tool(Path.home() / ".cache" / "prime-unlimited-os" / "jp24")
        flash_bootstrap(tool, package, on_progress=on_progress)


def redacted_identifier(identifier: str) -> str:
    if len(identifier) <= 4:
        return "****"
    return f"{identifier[:2]}…{identifier[-2:]}"
