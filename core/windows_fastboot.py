"""JC11S native Windows WinUSB Fastboot transport.

This module contains no ctypes DLL loading. The native implementation is imported
only by ``create_windows_fastboot_transport`` when running on Windows.
"""

from __future__ import annotations

import platform
from dataclasses import dataclass
from typing import Protocol

from core.fastboot_protocol import FastbootProtocolError, FastbootSession
from core.transports import TransportError, UsbDevice, WriteBlockedError
from core.usb_discovery import UsbInterface

JC11S_VID = 0x15E4
JC11S_PID = 0xD007
FASTBOOT_CLASS = 0xFF
FASTBOOT_SUBCLASS = 0x42
FASTBOOT_PROTOCOL = 0x03
USB_ENDPOINT_IN = 0x80
USB_PIPE_TYPE_BULK = 2


class WinUsbError(TransportError):
    pass


class WinUsbAccessError(WinUsbError):
    pass


class WinUsbTimeout(WinUsbError):
    pass


class WinUsbDisconnected(WinUsbError):
    pass


class WinUsbAmbiguousDevice(WinUsbError):
    pass


class NotFastbootInterface(WinUsbError):
    """An accessible WinUSB interface does not expose Fastboot descriptors."""


@dataclass(frozen=True)
class InterfaceInfo:
    path: str
    instance_id: str
    hardware_ids: tuple[str, ...]
    service: str
    manufacturer: str | None = None
    product: str | None = None
    location: str | None = None
    serial: str | None = None


@dataclass(frozen=True)
class InterfaceDescriptor:
    interface_class: int
    interface_subclass: int
    interface_protocol: int
    endpoint_count: int


@dataclass(frozen=True)
class PipeInfo:
    pipe_type: int
    pipe_id: int
    maximum_packet_size: int


class WinUsbConnection(Protocol):
    def descriptor(self) -> InterfaceDescriptor: ...

    def pipes(self) -> tuple[PipeInfo, ...]: ...

    def set_timeout(self, pipe_id: int, timeout_ms: int) -> None: ...

    def write_pipe(self, pipe_id: int, data: bytes) -> int: ...

    def read_pipe(self, pipe_id: int, size: int) -> bytes: ...

    def close(self) -> None: ...


class WinUsbApi(Protocol):
    def enumerate_interfaces(self) -> list[InterfaceInfo]: ...

    def open_interface(self, path: str) -> WinUsbConnection: ...


class _FastbootPipeIo:
    def __init__(self, connection: WinUsbConnection, pipe_in: int, pipe_out: int) -> None:
        self.connection = connection
        self.pipe_in = pipe_in
        self.pipe_out = pipe_out

    def write(self, data: bytes) -> int:
        return self.connection.write_pipe(self.pipe_out, data)

    def read(self, size: int = 64) -> bytes:
        return self.connection.read_pipe(self.pipe_in, size)


@dataclass(frozen=True)
class VerifiedWindowsFastboot:
    interface: InterfaceInfo
    protocol_version: str
    pipe_in: int
    pipe_out: int


class WindowsWinUsbFastbootTransport:
    """Read-only native transport plus explicitly invoked reboot."""

    def __init__(self, api: WinUsbApi, timeout_ms: int = 2000) -> None:
        self.api = api
        self.timeout_ms = timeout_ms

    def probe(self, identifier: str | None = None) -> list[UsbDevice]:
        verified = self.discover(identifier)
        return [
            UsbDevice(
                transport="fastboot",
                identifier=item.interface.instance_id,
                protocol_version=item.protocol_version,
                statuses=(
                    "USB device detected",
                    "WinUSB interface accessible",
                    "Fastboot protocol verified",
                ),
                transport_verified=True,
                interface_details=UsbInterface(
                    path=item.interface.path,
                    interface_class=FASTBOOT_CLASS,
                    interface_subclass=FASTBOOT_SUBCLASS,
                    interface_protocol=FASTBOOT_PROTOCOL,
                    endpoints=(f"IN 0x{item.pipe_in:02X}", f"OUT 0x{item.pipe_out:02X}"),
                    driver=item.interface.service,
                ),
            )
            for item in verified
        ]

    def discover(self, identifier: str | None = None) -> list[VerifiedWindowsFastboot]:
        candidates = [item for item in self.api.enumerate_interfaces() if self._is_winusb_candidate(item)]
        grouped: dict[str, list[InterfaceInfo]] = {}
        for candidate in candidates:
            grouped.setdefault(candidate.instance_id.casefold(), []).append(candidate)
        if identifier is not None:
            grouped = {
                key: items for key, items in grouped.items() if items[0].instance_id.casefold() == identifier.casefold()
            }
        verified: list[VerifiedWindowsFastboot] = []
        for interfaces in grouped.values():
            item = self._verify_physical_device(
                interfaces,
                family_hint=any(self._has_jc11s_family_hint(interface) for interface in interfaces),
            )
            if item is not None:
                verified.append(item)
        if len(verified) > 1:
            raise WinUsbAmbiguousDevice("Multiple JC11S-compatible WinUSB devices were found.")
        return verified

    def getvar(self, name: str, identifier: str | None = None) -> str:
        interface = self._single_interface(identifier)
        connection, session, _, _ = self._open_session(interface)
        try:
            response = session.getvar(name)
            return response.payload
        finally:
            connection.close()

    def reboot(self, identifier: str | None = None) -> str:
        """Send reboot after UI confirmation; a resulting disconnect is expected."""
        interface = self._single_interface(identifier)
        connection, session, _, _ = self._open_session(interface)
        try:
            try:
                response = session.reboot()
                return response.payload
            except (WinUsbDisconnected, WinUsbTimeout):
                return "Device disconnected after the reboot command."
        finally:
            connection.close()

    def collect_partition_evidence(self, identifier: str | None = None):
        """Read-only PoC probes; no partition mapping or flash authorization inferred."""
        from core.jc11s_flash import probe

        interface = self._single_interface(identifier)
        connection, session, _, _ = self._open_session(interface)
        try:
            return probe(session)
        finally:
            connection.close()

    def install(self, package) -> None:
        raise WriteBlockedError("JC11S flashing is not implemented or enabled.")

    def _single_interface(self, identifier: str | None = None) -> InterfaceInfo:
        verified = self.discover(identifier)
        if not verified:
            raise WinUsbError("No accessible JC11S WinUSB Fastboot interface was found.")
        return verified[0].interface

    def _verify_physical_device(
        self,
        interfaces: list[InterfaceInfo],
        family_hint: bool,
    ) -> VerifiedWindowsFastboot | None:
        errors: list[Exception] = []
        transport_seen = False
        for interface in interfaces:
            connection = None
            try:
                connection, session, pipe_in, pipe_out = self._open_session(interface)
                transport_seen = True
                version = session.getvar("version")
                if not version.payload:
                    raise FastbootProtocolError("Fastboot returned an empty protocol version.")
                return VerifiedWindowsFastboot(interface, version.payload, pipe_in, pipe_out)
            except NotFastbootInterface as exc:
                if family_hint:
                    errors.append(exc)
                continue
            except (WinUsbError, FastbootProtocolError) as exc:
                errors.append(exc)
            finally:
                if connection is not None:
                    connection.close()
        if errors:
            if not family_hint and not transport_seen:
                return None
            last_error = errors[-1]
            if isinstance(last_error, WinUsbError):
                raise last_error
            raise WinUsbError(f"JC11S WinUSB interface validation failed: {last_error}")
        return None

    def _open_session(self, interface: InterfaceInfo) -> tuple[WinUsbConnection, FastbootSession, int, int]:
        connection = self.api.open_interface(interface.path)
        try:
            descriptor = connection.descriptor()
            if (
                descriptor.interface_class,
                descriptor.interface_subclass,
                descriptor.interface_protocol,
            ) != (FASTBOOT_CLASS, FASTBOOT_SUBCLASS, FASTBOOT_PROTOCOL):
                raise NotFastbootInterface("USB interface descriptors do not match Fastboot FF/42/03.")
            pipes = connection.pipes()
            bulk_in = [
                pipe for pipe in pipes if pipe.pipe_type == USB_PIPE_TYPE_BULK and pipe.pipe_id & USB_ENDPOINT_IN
            ]
            bulk_out = [
                pipe for pipe in pipes if pipe.pipe_type == USB_PIPE_TYPE_BULK and not pipe.pipe_id & USB_ENDPOINT_IN
            ]
            if len(bulk_in) != 1 or len(bulk_out) != 1:
                raise NotFastbootInterface("Fastboot requires exactly one bulk IN and one bulk OUT endpoint.")
            connection.set_timeout(bulk_in[0].pipe_id, self.timeout_ms)
            connection.set_timeout(bulk_out[0].pipe_id, self.timeout_ms)
            io = _FastbootPipeIo(connection, bulk_in[0].pipe_id, bulk_out[0].pipe_id)
            return connection, FastbootSession(io), bulk_in[0].pipe_id, bulk_out[0].pipe_id
        except Exception:
            connection.close()
            raise

    @staticmethod
    def _is_winusb_candidate(interface: InterfaceInfo) -> bool:
        return interface.service.casefold() == "winusb"

    @staticmethod
    def _has_jc11s_family_hint(interface: InterfaceInfo) -> bool:
        ids = " ".join(interface.hardware_ids).upper()
        names = " ".join(filter(None, (interface.manufacturer, interface.product))).casefold()
        return "VID_15E4&PID_D007" in ids or "prime 4 plus" in names or "prime 4+" in names


def create_windows_fastboot_transport() -> WindowsWinUsbFastbootTransport:
    if platform.system() != "Windows":
        raise RuntimeError("Native WinUSB Fastboot is available only on Windows.")
    from core.windows_winusb_native import NativeWinUsbApi

    return WindowsWinUsbFastbootTransport(NativeWinUsbApi())
