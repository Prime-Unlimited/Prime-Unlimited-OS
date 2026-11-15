"""Cross-platform, revision-aware USB discovery.

Discovery is intentionally broader than compatibility authorization. This module
never sends USB protocol commands and never marks installation as authorized.
"""

from __future__ import annotations

import json
import platform
import plistlib
import re
import subprocess
from dataclasses import dataclass, field
from enum import StrEnum
from typing import Any, Protocol


class DeviceFamily(StrEnum):
    JC11S = "jc11s"
    JP24 = "jp24"


class CandidateTransport(StrEnum):
    FASTBOOT = "fastboot"
    IMBUPD = "imbupd"


@dataclass(frozen=True)
class VerificationState:
    discovered: bool = True
    candidate: bool = False
    transport_detected: bool = False
    protocol_verified: bool = False
    model_verified: bool = False
    revision_verified: bool = False
    installation_authorized: bool = False

    def __post_init__(self) -> None:
        if self.installation_authorized and not (
            self.protocol_verified and self.model_verified and self.revision_verified
        ):
            raise ValueError("Installation authorization requires protocol, model, and revision verification.")


@dataclass(frozen=True)
class UsbInterface:
    path: str | None = None
    interface_class: int | None = None
    interface_subclass: int | None = None
    interface_protocol: int | None = None
    endpoints: tuple[str, ...] = ()
    driver: str | None = None


@dataclass(frozen=True)
class DiscoveredUsbDevice:
    discovery_key: str
    display_name: str
    manufacturer: str | None = None
    product: str | None = None
    vendor_id: int | None = None
    product_id: int | None = None
    location: str | None = None
    device_paths: tuple[str, ...] = ()
    interfaces: tuple[UsbInterface, ...] = ()
    driver: str | None = None
    serial: str | None = None
    candidate_families: tuple[DeviceFamily, ...] = ()
    candidate_transports: tuple[CandidateTransport, ...] = ()
    bootloader: str | None = None
    firmware: str | None = None
    hardware_revision: str | None = None
    evidence: tuple[str, ...] = ()
    source: str = "unknown"
    verification: VerificationState = field(default_factory=VerificationState)
    warnings: tuple[str, ...] = ()

    @property
    def vid_pid(self) -> str:
        vendor = f"{self.vendor_id:04X}" if self.vendor_id is not None else "????"
        product = f"{self.product_id:04X}" if self.product_id is not None else "????"
        return f"{vendor}:{product}"


class UsbDiscovery(Protocol):
    def discover(self) -> list[DiscoveredUsbDevice]: ...


_JC11S_NAMES = ("prime 4 plus", "prime 4+")
_JP24_NAMES = ("prime 4 g2",)


def identify_candidate(
    *,
    vendor_id: int | None,
    product_id: int | None,
    manufacturer: str | None,
    product: str | None,
) -> tuple[tuple[DeviceFamily, ...], tuple[CandidateTransport, ...], tuple[str, ...]]:
    """Infer candidate families from observed hints without verifying identity."""
    text = " ".join(filter(None, (manufacturer, product))).casefold()
    families: list[DeviceFamily] = []
    evidence: list[str] = []
    if (vendor_id, product_id) == (0x15E4, 0xD007):
        families.append(DeviceFamily.JC11S)
        evidence.append("Observed JC11S VID/PID hint")
    if (vendor_id, product_id) == (0x15E4, 0xD014):
        families.append(DeviceFamily.JP24)
        evidence.append("Observed JP24 VID/PID hint")
    if any(name in text for name in _JC11S_NAMES) and DeviceFamily.JC11S not in families:
        families.append(DeviceFamily.JC11S)
        evidence.append("Product/manufacturer text suggests Prime 4+")
    if any(name in text for name in _JP24_NAMES) and DeviceFamily.JP24 not in families:
        families.append(DeviceFamily.JP24)
        evidence.append("Product/manufacturer text suggests Prime 4 G2")
    transports: list[CandidateTransport] = []
    if DeviceFamily.JC11S in families:
        transports.append(CandidateTransport.FASTBOOT)
    if DeviceFamily.JP24 in families:
        transports.append(CandidateTransport.IMBUPD)
    return tuple(families), tuple(transports), tuple(evidence)


def _integer(value: Any) -> int | None:
    if isinstance(value, bool):
        return None
    if isinstance(value, int):
        return value
    if isinstance(value, bytes):
        return int.from_bytes(value, "little")
    if isinstance(value, str):
        match = re.search(r"(?:0x)?([0-9a-fA-F]{4})", value)
        if match:
            return int(match.group(1), 16)
    return None


def _text(value: Any) -> str | None:
    if value is None:
        return None
    if isinstance(value, bytes):
        return value.decode("utf-8", "replace").strip("\x00") or None
    rendered = str(value).strip()
    return rendered or None


class CommandRunner(Protocol):
    def __call__(self, command: list[str]) -> subprocess.CompletedProcess[str]: ...


def _run(command: list[str]) -> subprocess.CompletedProcess[str]:
    return subprocess.run(command, capture_output=True, text=True, timeout=20, check=False)


class MacOsUsbDiscovery:
    """Structured IORegistry discovery with structured system_profiler fallback."""

    def __init__(self, runner: CommandRunner | None = None) -> None:
        self.runner = runner or _run

    def discover(self) -> list[DiscoveredUsbDevice]:
        ioreg_devices = self._ioreg_devices()
        return ioreg_devices if ioreg_devices else self._profiler_devices()

    def _ioreg_devices(self) -> list[DiscoveredUsbDevice]:
        commands = (
            ["ioreg", "-a", "-r", "-c", "IOUSBHostDevice"],
            ["ioreg", "-a", "-p", "IOUSB", "-l", "-w", "0"],
        )
        for command in commands:
            result = self.runner(command)
            if result.returncode or not result.stdout.strip():
                continue
            try:
                root = plistlib.loads(result.stdout.encode("utf-8"))
            except (plistlib.InvalidFileException, ValueError):
                continue
            devices = [self._from_ioreg(node) for node in self._walk(root)]
            filtered = [device for device in devices if device is not None]
            if filtered:
                return filtered
        return []

    def _profiler_devices(self) -> list[DiscoveredUsbDevice]:
        result = self.runner(["system_profiler", "-json", "SPUSBDataType"])
        if result.returncode or not result.stdout.strip():
            return []
        try:
            root = json.loads(result.stdout)
        except json.JSONDecodeError:
            return []
        devices = [self._from_profiler(node) for node in self._walk(root)]
        return [device for device in devices if device is not None]

    @staticmethod
    def _walk(value: Any):
        if isinstance(value, dict):
            yield value
            for child in value.values():
                yield from MacOsUsbDiscovery._walk(child)
        elif isinstance(value, list):
            for child in value:
                yield from MacOsUsbDiscovery._walk(child)

    def _from_ioreg(self, node: dict[str, Any]) -> DiscoveredUsbDevice | None:
        vendor_id = _integer(node.get("idVendor"))
        product_id = _integer(node.get("idProduct"))
        if vendor_id is None and product_id is None:
            return None
        product = _text(node.get("USB Product Name") or node.get("kUSBProductString"))
        manufacturer = _text(node.get("USB Vendor Name") or node.get("kUSBVendorString"))
        serial = _text(node.get("USB Serial Number") or node.get("kUSBSerialNumberString"))
        location = _text(node.get("locationID") or node.get("LocationID"))
        families, transports, evidence = identify_candidate(
            vendor_id=vendor_id,
            product_id=product_id,
            manufacturer=manufacturer,
            product=product,
        )
        key = f"ioreg:{location or serial or f'{vendor_id}:{product_id}'}"
        return DiscoveredUsbDevice(
            discovery_key=key,
            display_name=product or "Unknown USB device",
            manufacturer=manufacturer,
            product=product,
            vendor_id=vendor_id,
            product_id=product_id,
            location=location,
            serial=serial,
            candidate_families=families,
            candidate_transports=transports,
            evidence=evidence,
            source="macOS IORegistry",
            verification=VerificationState(candidate=bool(families)),
            warnings=("Model and hardware revision remain unverified.",) if families else (),
        )

    def _from_profiler(self, node: dict[str, Any]) -> DiscoveredUsbDevice | None:
        vendor_id = _integer(node.get("vendor_id"))
        product_id = _integer(node.get("product_id"))
        if vendor_id is None and product_id is None:
            return None
        product = _text(node.get("_name"))
        manufacturer = _text(node.get("manufacturer"))
        serial = _text(node.get("serial_num"))
        location = _text(node.get("location_id"))
        families, transports, evidence = identify_candidate(
            vendor_id=vendor_id,
            product_id=product_id,
            manufacturer=manufacturer,
            product=product,
        )
        key = f"profiler:{location or serial or f'{vendor_id}:{product_id}'}"
        return DiscoveredUsbDevice(
            discovery_key=key,
            display_name=product or "Unknown USB device",
            manufacturer=manufacturer,
            product=product,
            vendor_id=vendor_id,
            product_id=product_id,
            location=location,
            serial=serial,
            candidate_families=families,
            candidate_transports=transports,
            evidence=evidence,
            source="macOS system_profiler",
            verification=VerificationState(candidate=bool(families)),
            warnings=("Model and hardware revision remain unverified.",) if families else (),
        )


class EmptyUsbDiscovery:
    def discover(self) -> list[DiscoveredUsbDevice]:
        return []


def create_usb_discovery(system: str | None = None) -> UsbDiscovery:
    system = system or platform.system()
    if system == "Darwin":
        return MacOsUsbDiscovery()
    if system == "Windows":
        from core.windows_usb_discovery import WindowsUsbDiscovery

        return WindowsUsbDiscovery()
    return EmptyUsbDiscovery()
