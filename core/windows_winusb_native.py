"""ctypes bindings for SetupAPI and WinUSB.

Imported lazily by ``core.windows_fastboot`` only on Windows.
"""

from __future__ import annotations

import ctypes
import uuid
import winreg
from ctypes import wintypes

from core.windows_fastboot import (
    InterfaceDescriptor,
    InterfaceInfo,
    PipeInfo,
    WinUsbAccessError,
    WinUsbDisconnected,
    WinUsbError,
    WinUsbTimeout,
)

ERROR_ACCESS_DENIED = 5
ERROR_INSUFFICIENT_BUFFER = 122
ERROR_SEM_TIMEOUT = 121
ERROR_NO_MORE_ITEMS = 259
ERROR_OPERATION_ABORTED = 995
ERROR_DEVICE_NOT_CONNECTED = 1167

DIGCF_PRESENT = 0x00000002
DIGCF_DEVICEINTERFACE = 0x00000010
SPDRP_HARDWAREID = 0x00000001
SPDRP_SERVICE = 0x00000004

GENERIC_READ = 0x80000000
GENERIC_WRITE = 0x40000000
FILE_SHARE_READ = 0x00000001
FILE_SHARE_WRITE = 0x00000002
OPEN_EXISTING = 3
FILE_ATTRIBUTE_NORMAL = 0x00000080
PIPE_TRANSFER_TIMEOUT = 3


class GUID(ctypes.Structure):
    _fields_ = [
        ("Data1", wintypes.DWORD),
        ("Data2", wintypes.WORD),
        ("Data3", wintypes.WORD),
        ("Data4", ctypes.c_ubyte * 8),
    ]

    @classmethod
    def from_string(cls, value: str) -> GUID:
        parsed = uuid.UUID(value.strip("{}"))
        data = parsed.bytes_le
        return cls(
            int.from_bytes(data[0:4], "little"),
            int.from_bytes(data[4:6], "little"),
            int.from_bytes(data[6:8], "little"),
            (ctypes.c_ubyte * 8).from_buffer_copy(data[8:]),
        )


class SP_DEVICE_INTERFACE_DATA(ctypes.Structure):
    _fields_ = [
        ("cbSize", wintypes.DWORD),
        ("InterfaceClassGuid", GUID),
        ("Flags", wintypes.DWORD),
        ("Reserved", ctypes.c_size_t),
    ]


class SP_DEVINFO_DATA(ctypes.Structure):
    _fields_ = [
        ("cbSize", wintypes.DWORD),
        ("ClassGuid", GUID),
        ("DevInst", wintypes.DWORD),
        ("Reserved", ctypes.c_size_t),
    ]


class USB_INTERFACE_DESCRIPTOR(ctypes.Structure):
    _fields_ = [
        ("bLength", ctypes.c_ubyte),
        ("bDescriptorType", ctypes.c_ubyte),
        ("bInterfaceNumber", ctypes.c_ubyte),
        ("bAlternateSetting", ctypes.c_ubyte),
        ("bNumEndpoints", ctypes.c_ubyte),
        ("bInterfaceClass", ctypes.c_ubyte),
        ("bInterfaceSubClass", ctypes.c_ubyte),
        ("bInterfaceProtocol", ctypes.c_ubyte),
        ("iInterface", ctypes.c_ubyte),
    ]


class WINUSB_PIPE_INFORMATION(ctypes.Structure):
    _fields_ = [
        ("PipeType", ctypes.c_int),
        ("PipeId", ctypes.c_ubyte),
        ("MaximumPacketSize", wintypes.USHORT),
        ("Interval", ctypes.c_ubyte),
    ]


def _configure_functions(setupapi, kernel32, winusb) -> None:
    setupapi.SetupDiGetClassDevsW.argtypes = [
        ctypes.POINTER(GUID),
        wintypes.LPCWSTR,
        wintypes.HWND,
        wintypes.DWORD,
    ]
    setupapi.SetupDiGetClassDevsW.restype = wintypes.HANDLE
    setupapi.SetupDiEnumDeviceInterfaces.argtypes = [
        wintypes.HANDLE,
        ctypes.POINTER(SP_DEVINFO_DATA),
        ctypes.POINTER(GUID),
        wintypes.DWORD,
        ctypes.POINTER(SP_DEVICE_INTERFACE_DATA),
    ]
    setupapi.SetupDiEnumDeviceInterfaces.restype = wintypes.BOOL
    setupapi.SetupDiGetDeviceInterfaceDetailW.argtypes = [
        wintypes.HANDLE,
        ctypes.POINTER(SP_DEVICE_INTERFACE_DATA),
        ctypes.c_void_p,
        wintypes.DWORD,
        ctypes.POINTER(wintypes.DWORD),
        ctypes.POINTER(SP_DEVINFO_DATA),
    ]
    setupapi.SetupDiGetDeviceInterfaceDetailW.restype = wintypes.BOOL
    setupapi.SetupDiGetDeviceInstanceIdW.argtypes = [
        wintypes.HANDLE,
        ctypes.POINTER(SP_DEVINFO_DATA),
        wintypes.LPWSTR,
        wintypes.DWORD,
        ctypes.POINTER(wintypes.DWORD),
    ]
    setupapi.SetupDiGetDeviceInstanceIdW.restype = wintypes.BOOL
    setupapi.SetupDiGetDeviceRegistryPropertyW.argtypes = [
        wintypes.HANDLE,
        ctypes.POINTER(SP_DEVINFO_DATA),
        wintypes.DWORD,
        ctypes.POINTER(wintypes.DWORD),
        ctypes.POINTER(ctypes.c_ubyte),
        wintypes.DWORD,
        ctypes.POINTER(wintypes.DWORD),
    ]
    setupapi.SetupDiGetDeviceRegistryPropertyW.restype = wintypes.BOOL
    setupapi.SetupDiDestroyDeviceInfoList.argtypes = [wintypes.HANDLE]
    setupapi.SetupDiDestroyDeviceInfoList.restype = wintypes.BOOL

    kernel32.CreateFileW.argtypes = [
        wintypes.LPCWSTR,
        wintypes.DWORD,
        wintypes.DWORD,
        ctypes.c_void_p,
        wintypes.DWORD,
        wintypes.DWORD,
        wintypes.HANDLE,
    ]
    kernel32.CreateFileW.restype = wintypes.HANDLE
    kernel32.CloseHandle.argtypes = [wintypes.HANDLE]
    kernel32.CloseHandle.restype = wintypes.BOOL

    winusb.WinUsb_Initialize.argtypes = [wintypes.HANDLE, ctypes.POINTER(ctypes.c_void_p)]
    winusb.WinUsb_Initialize.restype = wintypes.BOOL
    winusb.WinUsb_Free.argtypes = [ctypes.c_void_p]
    winusb.WinUsb_Free.restype = wintypes.BOOL
    winusb.WinUsb_QueryInterfaceSettings.argtypes = [
        ctypes.c_void_p,
        ctypes.c_ubyte,
        ctypes.POINTER(USB_INTERFACE_DESCRIPTOR),
    ]
    winusb.WinUsb_QueryInterfaceSettings.restype = wintypes.BOOL
    winusb.WinUsb_QueryPipe.argtypes = [
        ctypes.c_void_p,
        ctypes.c_ubyte,
        ctypes.c_ubyte,
        ctypes.POINTER(WINUSB_PIPE_INFORMATION),
    ]
    winusb.WinUsb_QueryPipe.restype = wintypes.BOOL
    winusb.WinUsb_SetPipePolicy.argtypes = [
        ctypes.c_void_p,
        ctypes.c_ubyte,
        wintypes.ULONG,
        wintypes.ULONG,
        ctypes.c_void_p,
    ]
    winusb.WinUsb_SetPipePolicy.restype = wintypes.BOOL
    winusb.WinUsb_WritePipe.argtypes = [
        ctypes.c_void_p,
        ctypes.c_ubyte,
        ctypes.POINTER(ctypes.c_ubyte),
        wintypes.ULONG,
        ctypes.POINTER(wintypes.ULONG),
        ctypes.c_void_p,
    ]
    winusb.WinUsb_WritePipe.restype = wintypes.BOOL
    winusb.WinUsb_ReadPipe.argtypes = winusb.WinUsb_WritePipe.argtypes
    winusb.WinUsb_ReadPipe.restype = wintypes.BOOL


def _raise_windows_error(action: str) -> None:
    code = ctypes.get_last_error()
    message = f"{action} failed with Windows error {code}."
    if code == ERROR_ACCESS_DENIED:
        raise WinUsbAccessError(message)
    if code == ERROR_SEM_TIMEOUT:
        raise WinUsbTimeout(message)
    if code in {ERROR_DEVICE_NOT_CONNECTED, ERROR_OPERATION_ABORTED}:
        raise WinUsbDisconnected(message)
    raise WinUsbError(message)


class NativeWinUsbConnection:
    def __init__(self, path: str, kernel32, winusb) -> None:
        self.kernel32 = kernel32
        self.winusb = winusb
        self.file_handle = kernel32.CreateFileW(
            path,
            GENERIC_READ | GENERIC_WRITE,
            FILE_SHARE_READ | FILE_SHARE_WRITE,
            None,
            OPEN_EXISTING,
            FILE_ATTRIBUTE_NORMAL,
            None,
        )
        invalid_handle = ctypes.c_void_p(-1).value
        if self.file_handle in (None, invalid_handle):
            _raise_windows_error("Opening the WinUSB interface")
        self.interface_handle = ctypes.c_void_p()
        if not winusb.WinUsb_Initialize(self.file_handle, ctypes.byref(self.interface_handle)):
            kernel32.CloseHandle(self.file_handle)
            self.file_handle = None
            _raise_windows_error("WinUsb_Initialize")

    def descriptor(self) -> InterfaceDescriptor:
        value = USB_INTERFACE_DESCRIPTOR()
        if not self.winusb.WinUsb_QueryInterfaceSettings(self.interface_handle, 0, ctypes.byref(value)):
            _raise_windows_error("WinUsb_QueryInterfaceSettings")
        return InterfaceDescriptor(
            value.bInterfaceClass,
            value.bInterfaceSubClass,
            value.bInterfaceProtocol,
            value.bNumEndpoints,
        )

    def pipes(self) -> tuple[PipeInfo, ...]:
        descriptor = self.descriptor()
        result: list[PipeInfo] = []
        for index in range(descriptor.endpoint_count):
            value = WINUSB_PIPE_INFORMATION()
            if not self.winusb.WinUsb_QueryPipe(self.interface_handle, 0, index, ctypes.byref(value)):
                _raise_windows_error("WinUsb_QueryPipe")
            result.append(PipeInfo(value.PipeType, value.PipeId, value.MaximumPacketSize))
        return tuple(result)

    def set_timeout(self, pipe_id: int, timeout_ms: int) -> None:
        value = wintypes.ULONG(timeout_ms)
        if not self.winusb.WinUsb_SetPipePolicy(
            self.interface_handle,
            pipe_id,
            PIPE_TRANSFER_TIMEOUT,
            ctypes.sizeof(value),
            ctypes.byref(value),
        ):
            _raise_windows_error("Setting the WinUSB pipe timeout")

    def write_pipe(self, pipe_id: int, data: bytes) -> int:
        buffer = (ctypes.c_ubyte * len(data)).from_buffer_copy(data)
        transferred = wintypes.ULONG()
        if not self.winusb.WinUsb_WritePipe(
            self.interface_handle,
            pipe_id,
            buffer,
            len(data),
            ctypes.byref(transferred),
            None,
        ):
            _raise_windows_error("WinUsb_WritePipe")
        return transferred.value

    def read_pipe(self, pipe_id: int, size: int) -> bytes:
        buffer = (ctypes.c_ubyte * size)()
        transferred = wintypes.ULONG()
        if not self.winusb.WinUsb_ReadPipe(
            self.interface_handle,
            pipe_id,
            buffer,
            size,
            ctypes.byref(transferred),
            None,
        ):
            _raise_windows_error("WinUsb_ReadPipe")
        return bytes(buffer[: transferred.value])

    def close(self) -> None:
        if self.interface_handle:
            self.winusb.WinUsb_Free(self.interface_handle)
            self.interface_handle = ctypes.c_void_p()
        if self.file_handle:
            self.kernel32.CloseHandle(self.file_handle)
            self.file_handle = None


class NativeWinUsbApi:
    def __init__(self) -> None:
        self.setupapi = ctypes.WinDLL("setupapi", use_last_error=True)
        self.kernel32 = ctypes.WinDLL("kernel32", use_last_error=True)
        self.winusb = ctypes.WinDLL("winusb", use_last_error=True)
        _configure_functions(self.setupapi, self.kernel32, self.winusb)

    def enumerate_interfaces(self) -> list[InterfaceInfo]:
        """Enumerate all registered interface classes; no tested GUID is hardcoded."""
        results: dict[str, InterfaceInfo] = {}
        for class_guid in self._interface_class_guids():
            for item in self._enumerate_class(class_guid):
                results[item.path.casefold()] = item
        return list(results.values())

    def open_interface(self, path: str) -> NativeWinUsbConnection:
        return NativeWinUsbConnection(path, self.kernel32, self.winusb)

    @staticmethod
    def _interface_class_guids() -> list[GUID]:
        path = r"SYSTEM\CurrentControlSet\Control\DeviceClasses"
        result: list[GUID] = []
        with winreg.OpenKey(winreg.HKEY_LOCAL_MACHINE, path) as key:
            index = 0
            while True:
                try:
                    name = winreg.EnumKey(key, index)
                except OSError:
                    break
                index += 1
                try:
                    result.append(GUID.from_string(name))
                except (ValueError, AttributeError):
                    continue
        return result

    def _enumerate_class(self, class_guid: GUID) -> list[InterfaceInfo]:
        handle = self.setupapi.SetupDiGetClassDevsW(
            ctypes.byref(class_guid), None, None, DIGCF_PRESENT | DIGCF_DEVICEINTERFACE
        )
        invalid_handle = ctypes.c_void_p(-1).value
        if handle in (None, invalid_handle):
            return []
        results: list[InterfaceInfo] = []
        try:
            index = 0
            while True:
                interface_data = SP_DEVICE_INTERFACE_DATA()
                interface_data.cbSize = ctypes.sizeof(interface_data)
                if not self.setupapi.SetupDiEnumDeviceInterfaces(
                    handle,
                    None,
                    ctypes.byref(class_guid),
                    index,
                    ctypes.byref(interface_data),
                ):
                    if ctypes.get_last_error() == ERROR_NO_MORE_ITEMS:
                        break
                    index += 1
                    continue
                index += 1
                item = self._interface_detail(handle, interface_data)
                if item is not None:
                    results.append(item)
        finally:
            self.setupapi.SetupDiDestroyDeviceInfoList(handle)
        return results

    def _interface_detail(self, handle, interface_data: SP_DEVICE_INTERFACE_DATA) -> InterfaceInfo | None:
        required = wintypes.DWORD()
        self.setupapi.SetupDiGetDeviceInterfaceDetailW(
            handle, ctypes.byref(interface_data), None, 0, ctypes.byref(required), None
        )
        if required.value == 0 or ctypes.get_last_error() != ERROR_INSUFFICIENT_BUFFER:
            return None
        buffer = ctypes.create_string_buffer(required.value)
        ctypes.cast(buffer, ctypes.POINTER(wintypes.DWORD)).contents.value = (
            8 if ctypes.sizeof(ctypes.c_void_p) == 8 else 6
        )
        device_info = SP_DEVINFO_DATA()
        device_info.cbSize = ctypes.sizeof(device_info)
        if not self.setupapi.SetupDiGetDeviceInterfaceDetailW(
            handle,
            ctypes.byref(interface_data),
            buffer,
            required.value,
            None,
            ctypes.byref(device_info),
        ):
            return None
        path = ctypes.wstring_at(ctypes.addressof(buffer) + ctypes.sizeof(wintypes.DWORD))
        instance_id = self._instance_id(handle, device_info)
        hardware_ids = self._registry_property(handle, device_info, SPDRP_HARDWAREID)
        service = self._registry_property(handle, device_info, SPDRP_SERVICE)
        if not instance_id:
            return None
        return InterfaceInfo(path, instance_id, tuple(hardware_ids), service[0] if service else "")

    def _instance_id(self, handle, device_info: SP_DEVINFO_DATA) -> str:
        buffer = ctypes.create_unicode_buffer(1024)
        if not self.setupapi.SetupDiGetDeviceInstanceIdW(handle, ctypes.byref(device_info), buffer, len(buffer), None):
            return ""
        return buffer.value

    def _registry_property(self, handle, device_info: SP_DEVINFO_DATA, property_id: int) -> list[str]:
        buffer = (ctypes.c_ubyte * 8192)()
        if not self.setupapi.SetupDiGetDeviceRegistryPropertyW(
            handle,
            ctypes.byref(device_info),
            property_id,
            None,
            buffer,
            len(buffer),
            None,
        ):
            return []
        text = ctypes.wstring_at(ctypes.addressof(buffer), len(buffer) // ctypes.sizeof(wintypes.WCHAR))
        return [part for part in text.split("\x00") if part]
