"""Normalized Windows USB discovery backed by dynamic SetupAPI enumeration."""

from __future__ import annotations

import re
from typing import Protocol

from core.usb_discovery import DiscoveredUsbDevice, UsbInterface, VerificationState, identify_candidate
from core.windows_fastboot import InterfaceInfo


class InterfaceEnumerator(Protocol):
    def enumerate_interfaces(self) -> list[InterfaceInfo]: ...


class WindowsUsbDiscovery:
    def __init__(self, api: InterfaceEnumerator | None = None) -> None:
        if api is None:
            from core.windows_winusb_native import NativeWinUsbApi

            api = NativeWinUsbApi()
        self.api = api

    def discover(self) -> list[DiscoveredUsbDevice]:
        grouped: dict[str, list[InterfaceInfo]] = {}
        for interface in self.api.enumerate_interfaces():
            grouped.setdefault(interface.instance_id.casefold(), []).append(interface)
        return [self._normalize(items) for items in grouped.values()]

    @staticmethod
    def _normalize(interfaces: list[InterfaceInfo]) -> DiscoveredUsbDevice:
        first = interfaces[0]
        ids = " ".join(first.hardware_ids + (first.instance_id,))
        match = re.search(r"(?i)VID_([0-9A-F]{4})&PID_([0-9A-F]{4})", ids)
        vendor_id = int(match.group(1), 16) if match else None
        product_id = int(match.group(2), 16) if match else None
        families, transports, evidence = identify_candidate(
            vendor_id=vendor_id,
            product_id=product_id,
            manufacturer=first.manufacturer,
            product=first.product,
        )
        warnings: list[str] = []
        if families:
            warnings.append("Candidate family inferred; model and revision are not verified.")
        if vendor_id is None or product_id is None:
            warnings.append("USB vendor or product identifier is unavailable.")
        return DiscoveredUsbDevice(
            discovery_key=first.instance_id,
            display_name=first.product or "Unknown USB device",
            manufacturer=first.manufacturer,
            product=first.product,
            vendor_id=vendor_id,
            product_id=product_id,
            location=first.location,
            device_paths=tuple(item.path for item in interfaces),
            interfaces=tuple(UsbInterface(path=item.path, driver=item.service or None) for item in interfaces),
            driver=first.service or None,
            serial=first.serial,
            candidate_families=families,
            candidate_transports=transports,
            evidence=evidence,
            source="Windows SetupAPI",
            verification=VerificationState(candidate=bool(families)),
            warnings=tuple(warnings),
        )
