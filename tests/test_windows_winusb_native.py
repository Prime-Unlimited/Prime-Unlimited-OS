import ctypes
from collections import deque
from ctypes import wintypes

import pytest

import core.windows_winusb_native as native
from core.windows_fastboot import WinUsbDisconnected, WinUsbError, WinUsbTimeout


class FakeKernel32:
    def __init__(self, waits=None, result=True, transferred=14):
        self.waits = deque(waits or [native.WAIT_OBJECT_0])
        self.result = result
        self.transferred = transferred
        self.create_flags = None
        self.closed = []
        self.cancelled = []
        self.next_event = 200

    def CreateFileW(self, path, access, sharing, security, creation, flags, template):
        self.create_flags = flags
        return 100

    def CloseHandle(self, handle):
        self.closed.append(handle)
        return True

    def CreateEventW(self, security, manual_reset, initial_state, name):
        self.next_event += 1
        return self.next_event

    def WaitForSingleObject(self, handle, timeout):
        return self.waits.popleft()

    def GetOverlappedResult(self, file_handle, overlapped, transferred, wait):
        ctypes.cast(transferred, ctypes.POINTER(wintypes.DWORD)).contents.value = self.transferred
        return self.result

    def CancelIoEx(self, file_handle, overlapped):
        self.cancelled.append(file_handle)
        return True


class FakeWinUsb:
    def __init__(self, initialize=True, transfer_result=False):
        self.initialize = initialize
        self.transfer_result = transfer_result
        self.freed = []
        self.writes = []

    def WinUsb_Initialize(self, file_handle, interface_handle):
        if self.initialize:
            ctypes.cast(interface_handle, ctypes.POINTER(ctypes.c_void_p)).contents.value = 300
        return self.initialize

    def WinUsb_Free(self, interface_handle):
        self.freed.append(interface_handle)
        return True

    def WinUsb_WritePipe(self, interface, pipe_id, buffer, size, transferred, overlapped):
        self.writes.append((pipe_id, bytes(buffer[:size]), bool(overlapped)))
        return self.transfer_result

    def WinUsb_ReadPipe(self, interface, pipe_id, buffer, size, transferred, overlapped):
        return self.transfer_result


def error_sequence(*values):
    sequence = iter(values)
    return lambda: next(sequence)


def test_create_file_uses_hardware_required_overlapped_flag(monkeypatch):
    kernel = FakeKernel32()
    winusb = FakeWinUsb()
    connection = native.NativeWinUsbConnection("device-path", kernel, winusb)
    assert kernel.create_flags == native.FILE_FLAG_OVERLAPPED
    connection.close()


def test_winusb_initialization_failure_closes_file_handle(monkeypatch):
    kernel = FakeKernel32()
    winusb = FakeWinUsb(initialize=False)
    monkeypatch.setattr(native, "_last_error", lambda: 6)
    with pytest.raises(WinUsbError, match="error 6"):
        native.NativeWinUsbConnection("device-path", kernel, winusb)
    assert 100 in kernel.closed


def test_overlapped_write_waits_for_completion_and_keeps_buffer_alive(monkeypatch):
    kernel = FakeKernel32(transferred=14)
    winusb = FakeWinUsb(transfer_result=False)
    monkeypatch.setattr(native, "_last_error", lambda: native.ERROR_IO_PENDING)
    connection = native.NativeWinUsbConnection("device-path", kernel, winusb)
    connection.set_timeout = lambda pipe, timeout: connection.pipe_timeouts.update({pipe: timeout})
    connection.set_timeout(0x02, 2000)
    assert connection.write_pipe(0x02, b"getvar:version") == 14
    assert winusb.writes == [(0x02, b"getvar:version", True)]
    assert 201 in kernel.closed
    connection.close()


def test_overlapped_timeout_cancels_pending_transfer_without_blocking(monkeypatch):
    kernel = FakeKernel32(waits=[native.WAIT_TIMEOUT, native.WAIT_OBJECT_0])
    winusb = FakeWinUsb(transfer_result=False)
    monkeypatch.setattr(native, "_last_error", lambda: native.ERROR_IO_PENDING)
    connection = native.NativeWinUsbConnection("device-path", kernel, winusb)
    connection.pipe_timeouts[0x81] = 25
    with pytest.raises(WinUsbTimeout, match="bounded wait"):
        connection.read_pipe(0x81, 64)
    assert kernel.cancelled == [100]
    assert connection.pending == []
    connection.close()


def test_overlapped_disconnect_is_reported_and_event_is_closed(monkeypatch):
    kernel = FakeKernel32(result=False)
    winusb = FakeWinUsb(transfer_result=False)
    monkeypatch.setattr(
        native,
        "_last_error",
        error_sequence(native.ERROR_IO_PENDING, native.ERROR_DEVICE_NOT_CONNECTED),
    )
    connection = native.NativeWinUsbConnection("device-path", kernel, winusb)
    with pytest.raises(WinUsbDisconnected):
        connection.read_pipe(0x81, 64)
    assert 201 in kernel.closed
    connection.close()
