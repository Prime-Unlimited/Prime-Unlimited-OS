from core.usb_discovery import DeviceFamily
from core.windows_fastboot import InterfaceInfo
from core.windows_usb_discovery import WindowsUsbDiscovery


class Api:
    def __init__(self, interfaces):
        self.interfaces = interfaces

    def enumerate_interfaces(self):
        return self.interfaces


def interface(path, instance, hardware_id, product=None, manufacturer="Denon DJ"):
    return InterfaceInfo(
        path=path,
        instance_id=instance,
        hardware_ids=(hardware_id,),
        service="WINUSB",
        manufacturer=manufacturer,
        product=product,
        location="Port 1",
        serial="redacted",
    )


def test_windows_groups_different_interface_paths_for_one_physical_device():
    interfaces = [
        interface("guid-a-path", "USB\\ONE", "USB\\VID_15E4&PID_D007", "Denon DJ PRIME 4 Plus"),
        interface("guid-b-path", "USB\\ONE", "USB\\VID_15E4&PID_D007", "Denon DJ PRIME 4 Plus"),
        interface("guid-c-path", "USB\\ONE", "USB\\VID_15E4&PID_D007", "Denon DJ PRIME 4 Plus"),
    ]
    devices = WindowsUsbDiscovery(Api(interfaces)).discover()
    assert len(devices) == 1
    assert devices[0].device_paths == ("guid-a-path", "guid-b-path", "guid-c-path")
    assert devices[0].vid_pid == "15E4:D007"


def test_windows_changed_identifier_uses_name_only_as_candidate_hint():
    device = WindowsUsbDiscovery(
        Api([interface("future-path", "USB\\FUTURE", "USB\\VID_9999&PID_0001", "PRIME 4 Plus Rev B")])
    ).discover()[0]
    assert DeviceFamily.JC11S in device.candidate_families
    assert device.vid_pid == "9999:0001"
    assert not device.verification.model_verified
    assert not device.verification.revision_verified
    assert not device.verification.installation_authorized


def test_windows_unknown_usb_hardware_is_visible_but_not_candidate():
    device = WindowsUsbDiscovery(
        Api([interface("other", "USB\\OTHER", "USB\\VID_1234&PID_5678", None, None)])
    ).discover()[0]
    assert device.display_name == "Unknown USB device"
    assert device.candidate_families == ()
    assert device.verification.discovered
    assert not device.verification.candidate
