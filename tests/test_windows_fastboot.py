from collections import deque

import pytest

from core.windows_fastboot import (
    InterfaceDescriptor,
    InterfaceInfo,
    PipeInfo,
    WindowsWinUsbFastbootTransport,
    WinUsbAccessError,
    WinUsbAmbiguousDevice,
    WinUsbDisconnected,
    WinUsbError,
    WinUsbTimeout,
)

INTERFACE = InterfaceInfo(
    path=r"\\?\usb#vid_15e4&pid_d007#redacted",
    instance_id=r"USB\VID_15E4&PID_D007\REDACTED",
    hardware_ids=(r"USB\VID_15E4&PID_D007",),
    service="WINUSB",
)


class FakeConnection:
    def __init__(self, responses=None, descriptor=None, write_size=None):
        self.responses = deque(responses or [])
        self.interface_descriptor = descriptor or InterfaceDescriptor(0xFF, 0x42, 0x03, 2)
        self.pipe_values = (PipeInfo(2, 0x81, 64), PipeInfo(2, 0x02, 64))
        self.write_size = write_size
        self.writes = []
        self.timeouts = []
        self.closed = False

    def descriptor(self):
        return self.interface_descriptor

    def pipes(self):
        return self.pipe_values

    def set_timeout(self, pipe_id, timeout_ms):
        self.timeouts.append((pipe_id, timeout_ms))

    def write_pipe(self, pipe_id, data):
        self.writes.append((pipe_id, data))
        return len(data) if self.write_size is None else self.write_size

    def read_pipe(self, pipe_id, size):
        response = self.responses.popleft()
        if isinstance(response, Exception):
            raise response
        return response

    def close(self):
        self.closed = True


class FakeApi:
    def __init__(self, interfaces=None, connections=None):
        self.interfaces = interfaces or []
        self.connections = deque(connections or [])
        self.opened = []

    def enumerate_interfaces(self):
        return self.interfaces

    def open_interface(self, path):
        self.opened.append(path)
        connection = self.connections.popleft()
        if isinstance(connection, Exception):
            raise connection
        return connection


def test_successful_discovery_verifies_protocol_and_cleans_up():
    connection = FakeConnection([b"OKAY0.4"])
    transport = WindowsWinUsbFastbootTransport(FakeApi([INTERFACE], [connection]))
    devices = transport.probe()
    assert devices[0].protocol_version == "0.4"
    assert "Fastboot protocol verified" in devices[0].statuses
    assert connection.writes == [(0x02, b"getvar:version")]
    assert connection.timeouts == [(0x81, 2000), (0x02, 2000)]
    assert connection.closed


def test_missing_device_returns_empty_list():
    assert WindowsWinUsbFastbootTransport(FakeApi()).probe() == []


def test_multiple_physical_devices_are_ambiguous():
    second = InterfaceInfo(
        path="second", instance_id="USB\\SECOND", hardware_ids=INTERFACE.hardware_ids, service="winusb"
    )
    first_connection = FakeConnection([b"OKAY0.4"])
    second_connection = FakeConnection([b"OKAY0.4"])
    api = FakeApi([INTERFACE, second], [first_connection, second_connection])
    with pytest.raises(WinUsbAmbiguousDevice):
        WindowsWinUsbFastbootTransport(api).probe()
    assert first_connection.closed and second_connection.closed


def test_same_physical_device_with_multiple_paths_is_not_ambiguous():
    alias = InterfaceInfo("alias", INTERFACE.instance_id, INTERFACE.hardware_ids, "WINUSB")
    devices = WindowsWinUsbFastbootTransport(FakeApi([INTERFACE, alias], [FakeConnection([b"OKAY0.4"])])).probe()
    assert len(devices) == 1


@pytest.mark.parametrize(
    "response,error",
    [
        (b"FAILunsupported", WinUsbError),
        (b"WHATbad", WinUsbError),
        (WinUsbTimeout("timeout"), WinUsbError),
        (WinUsbDisconnected("removed"), WinUsbError),
    ],
)
def test_protocol_errors_timeouts_and_disconnects_close_resources(response, error):
    connection = FakeConnection([response])
    with pytest.raises(error):
        WindowsWinUsbFastbootTransport(FakeApi([INTERFACE], [connection])).probe()
    assert connection.closed


def test_wrong_interface_descriptor_is_rejected_and_closed():
    connection = FakeConnection(descriptor=InterfaceDescriptor(0xFF, 0x00, 0x00, 2))
    with pytest.raises(WinUsbError, match="FF/42/03"):
        WindowsWinUsbFastbootTransport(FakeApi([INTERFACE], [connection])).probe()
    assert connection.closed


def test_access_error_is_reported():
    api = FakeApi([INTERFACE], [WinUsbAccessError("access denied")])
    with pytest.raises(WinUsbAccessError, match="access denied"):
        WindowsWinUsbFastbootTransport(api).probe()


def test_partial_transfer_is_rejected_and_closed():
    connection = FakeConnection(write_size=3)
    with pytest.raises(WinUsbError, match="Partial"):
        WindowsWinUsbFastbootTransport(FakeApi([INTERFACE], [connection])).probe()
    assert connection.closed


def test_confirmed_reboot_treats_expected_disconnect_as_success():
    discovery = FakeConnection([b"OKAY0.4"])
    reboot = FakeConnection([WinUsbDisconnected("device removed")])
    transport = WindowsWinUsbFastbootTransport(FakeApi([INTERFACE], [discovery, reboot]))
    message = transport.reboot()
    assert "disconnected" in message.lower()
    assert reboot.writes == [(0x02, b"reboot")]
    assert discovery.closed and reboot.closed


def test_unrelated_winusb_device_is_descriptor_checked_without_protocol_command():
    unrelated = InterfaceInfo("other", "USB\\OTHER", (r"USB\VID_1234&PID_5678",), "WINUSB")
    connection = FakeConnection(descriptor=InterfaceDescriptor(0xFF, 0x00, 0x00, 2))
    api = FakeApi([unrelated], [connection])
    assert WindowsWinUsbFastbootTransport(api).probe() == []
    assert api.opened == ["other"]
    assert connection.writes == []
    assert connection.closed


def test_changed_identifiers_with_prime4plus_name_are_safely_protocol_probed():
    future = InterfaceInfo(
        path="future-path",
        instance_id="USB\\FUTURE",
        hardware_ids=(r"USB\VID_9999&PID_0001",),
        service="WINUSB",
        manufacturer="Denon DJ",
        product="PRIME 4 Plus future revision",
    )
    connection = FakeConnection([b"OKAY0.4"])
    devices = WindowsWinUsbFastbootTransport(FakeApi([future], [connection])).probe()
    assert len(devices) == 1
    assert devices[0].protocol_version == "0.4"
    assert devices[0].verified_model is None


def test_unknown_identifiers_with_fastboot_descriptor_are_transport_verified_only():
    future = InterfaceInfo(
        path="future-fastboot",
        instance_id="USB\\UNKNOWN-REVISION",
        hardware_ids=(r"USB\VID_9999&PID_9999",),
        service="WINUSB",
    )
    connection = FakeConnection([b"OKAY0.4"])
    devices = WindowsWinUsbFastbootTransport(FakeApi([future], [connection])).probe()
    assert len(devices) == 1
    assert devices[0].transport_verified
    assert devices[0].verified_model is None


def test_partition_evidence_uses_native_session_and_closes():
    # Discovery and the subsequent read-only probe open separate handles.
    discovery = FakeConnection([b"OKAY0.4"])
    diagnostics = FakeConnection([b"OKAY0.4"] + [b"FAILunsupported"] * 12)
    transport = WindowsWinUsbFastbootTransport(FakeApi([INTERFACE], [discovery, diagnostics]))
    evidence = transport.collect_partition_evidence()
    assert evidence.values == {"version": "0.4"}
    assert "partition-size:data" in evidence.unsupported
    assert "partition-size:userdata" in evidence.unsupported
    assert all(data.startswith(b"getvar:") for _, data in diagnostics.writes)
    assert discovery.closed and diagnostics.closed
