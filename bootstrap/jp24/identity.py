"""Read-only JP24 transport probe and strict model identity assessment."""

from __future__ import annotations

import re
import subprocess
from dataclasses import dataclass
from enum import StrEnum
from pathlib import Path

import usb.core


class IdentityState(StrEnum):
    ABSENT = "absent"
    AMBIGUOUS = "ambiguous"
    TRANSPORT_ONLY = "transport_only"
    VERIFIED_JP24 = "verified_jp24"
    UNSUPPORTED = "unsupported"


@dataclass(frozen=True)
class Jp24Inspection:
    state: IdentityState
    device_count: int
    interface_verified: bool
    protocol_version: str | None
    power_source: str | None
    product_code: str | None
    detail: str

    @property
    def may_write(self) -> bool:
        return self.state is IdentityState.VERIFIED_JP24 and self.power_source == "mains"


class Jp24IdentityProbe:
    """Probe the imbupd USB interface and its read-only properties.

    A matching VID:PID identifies a transport only. JP24 is verified only if
    imbupd returns the exact JP24 product code as a property.
    """

    VID = 0x15E4
    PID = 0xD014
    INTERFACE_CLASS = 0xFF
    INTERFACE_SUBCLASS = 0x61
    INTERFACE_PROTOCOL = 0x7A
    EXPECTED_PROTOCOL_VERSION = "1.4.2"

    def __init__(self, native_tool: Path) -> None:
        self.native_tool = native_tool

    def probe(self) -> Jp24Inspection:
        try:
            devices = list(usb.core.find(find_all=True, idVendor=self.VID, idProduct=self.PID) or [])
        except (usb.core.NoBackendError, usb.core.USBError) as exc:
            return self._result(
                IdentityState.UNSUPPORTED, 0, False, None, None, None,
                f"USB inspection backend unavailable: {exc}",
            )
        if not devices:
            return self._result(IdentityState.ABSENT, 0, False, None, None, None, "No imbupd USB device found.")
        if len(devices) != 1:
            return self._result(
                IdentityState.AMBIGUOUS, len(devices), False, None, None, None,
                "Multiple matching USB devices are connected; disconnect all but the target.",
            )

        device = devices[0]
        interface_verified = self._has_imbupd_interface(device)
        if not interface_verified:
            return self._result(
                IdentityState.UNSUPPORTED, 1, False, None, None, None,
                "VID:PID matched, but the vendor-specific imbupd interface descriptor did not.",
            )

        try:
            completed = subprocess.run(
                [str(self.native_tool), "query"], capture_output=True, text=True, timeout=20, check=False
            )
        except (OSError, subprocess.TimeoutExpired) as exc:
            return self._result(
                IdentityState.TRANSPORT_ONLY, 1, True, None, None, None,
                f"USB descriptors match imbupd, but the read-only protocol query failed: {exc}",
            )

        output = completed.stdout + "\n" + completed.stderr
        properties = dict(re.findall(r"^\s*(version|power-source|product):\s*(.*?)\s*$", output, re.MULTILINE))
        version = properties.get("version")
        power = properties.get("power-source")
        product = properties.get("product")
        if completed.returncode != 0 or version is None:
            return self._result(
                IdentityState.TRANSPORT_ONLY, 1, True, version, power, product,
                "The imbupd interface is present, but a valid protocol response was not confirmed.",
            )
        if version != self.EXPECTED_PROTOCOL_VERSION:
            return self._result(
                IdentityState.UNSUPPORTED, 1, True, version, power, product,
                f"Unsupported imbupd protocol version {version!r}.",
            )
        if product is None:
            return self._result(
                IdentityState.TRANSPORT_ONLY, 1, True, version, power, product,
                "The protocol responds, but it does not expose a positive JP24 product identity. No write is allowed.",
            )
        if product != "JP24":
            return self._result(
                IdentityState.UNSUPPORTED, 1, True, version, power, product,
                f"The device reports product code {product!r}, not JP24.",
            )
        state = IdentityState.VERIFIED_JP24
        detail = "JP24 product identity verified through the imbupd product property."
        if power != "mains":
            detail += " Connect the device to mains power before any write."
        return self._result(state, 1, True, version, power, product, detail)

    @classmethod
    def _has_imbupd_interface(cls, device: usb.core.Device) -> bool:
        try:
            for configuration in device:
                for interface in configuration:
                    if (
                        interface.bInterfaceClass == cls.INTERFACE_CLASS
                        and interface.bInterfaceSubClass == cls.INTERFACE_SUBCLASS
                        and interface.bInterfaceProtocol == cls.INTERFACE_PROTOCOL
                    ):
                        return True
        except (usb.core.USBError, NotImplementedError):
            return False
        return False

    @staticmethod
    def _result(state, count, interface, version, power, product, detail):
        return Jp24Inspection(state, count, interface, version, power, product, detail)
