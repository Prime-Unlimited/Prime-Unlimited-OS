"""Immutable device profiles; support does not imply permission to write."""

from __future__ import annotations

from dataclasses import dataclass
from enum import StrEnum


class ProfileId(StrEnum):
    JC11S = "jc11s"
    JP24 = "jp24"


@dataclass(frozen=True)
class DeviceProfile:
    id: ProfileId
    display_name: str
    hardware_profile: str
    transport: str
    architecture: str
    ssh_port: int
    setup_mode: str
    flash_enabled: bool = False
    blocker: str = ""


PROFILES = {
    ProfileId.JC11S: DeviceProfile(
        id=ProfileId.JC11S,
        display_name="Denon DJ Prime 4+",
        hardware_profile="JC11S",
        transport="Fastboot",
        architecture="ARMv7 / ARM32",
        ssh_port=22,
        setup_mode="Hold SHIFT + right-deck EJECT while powering on.",
        blocker=(
            "Writing is disabled: positive Fastboot model identity, a validated bootstrap package, "
            "and a tested backup/recovery procedure are not available."
        ),
    ),
    ProfileId.JP24: DeviceProfile(
        id=ProfileId.JP24,
        display_name="Denon DJ Prime 4 G2",
        hardware_profile="JP24",
        transport="imbupd",
        architecture="AArch64 / ARM64",
        ssh_port=2222,
        setup_mode="Open Firmware Update Mode and connect the rear USB-C port.",
        flash_enabled=True,
        blocker=(
            "The imbupd implementation is validated against real hardware (ALSA boot hook + "
            "sshd on port 2222 - see devices/jp24/README.md). Writing /data is still "
            "irreversible if the device is disconnected mid-flash: back up anything you care "
            "about and keep the device powered and connected throughout."
        ),
    ),
}
