import plistlib
from pathlib import Path

import pytest

import core.jp24_bootstrap as jp24_bootstrap
from core.transports import FastbootTransport, ImbupdTransport, WriteBlockedError


def test_jp24_usb_id_is_only_unverified_transport_evidence():
    devices = ImbupdTransport("linux", "Bus 001 Device 002: ID 15e4:d014 vendor").probe()
    assert len(devices) == 1
    assert devices[0].transport == "imbupd"
    assert devices[0].verified_model is None


def test_multiple_jp24_interfaces_are_preserved_as_ambiguous():
    assert len(ImbupdTransport("linux", "ID 15e4:d014 first\nID 15E4:D014 second").probe()) == 2


def test_darwin_probe_finds_device_nested_two_hubs_deep(monkeypatch):
    """Regression test: `system_profiler SPUSBDataType` goes through a
    separate reporting daemon that was observed returning a stale, empty USB
    list on real hardware (both plain-text and -xml forms) while the device
    was visibly connected and selected in Apple's own System Information
    app at the same moment - so the probe uses `ioreg` instead, which reads
    the IOKit registry directly. A root-restricted `ioreg -r -c
    IOUSBHostDevice` query was tried first, but on real hardware it only
    ever returned the top-level hubs and never a device sitting behind two
    of them (hub -> hub -> PRIME 4 G2) - ioreg stops descending a branch
    once something on it matches the class filter, and the intermediate
    hubs match too. The fix dumps the full, unrestricted IOUSB-plane
    archive and walks its IORegistryEntryChildren tree in Python instead,
    which this test models with exactly that two-hub nesting depth."""
    root = {
        "IORegistryEntryChildren": [
            {
                "IORegistryEntryName": "USB2 Hub",
                "idVendor": 0x05AC,
                "idProduct": 0x800B,
                "IORegistryEntryChildren": [
                    {
                        "IORegistryEntryName": "USB3 Gen2 Hub",
                        "idVendor": 0x05AC,
                        "idProduct": 0x800C,
                        "IORegistryEntryChildren": [
                            {
                                "IORegistryEntryName": "PRIME 4 G2",
                                "idVendor": 0x15E4,
                                "idProduct": 0xD014,
                            },
                        ],
                    },
                ],
            },
        ],
    }

    class FakeResult:
        returncode = 0
        stdout = plistlib.dumps(root)
        stderr = b""

    def fake_run(command, **kwargs):
        assert command[0] == "ioreg"
        assert "system_profiler" not in command
        assert "-c" not in command  # no class filter - it was the bug
        return FakeResult()

    monkeypatch.setattr("core.transports.subprocess.run", fake_run)
    devices = ImbupdTransport("Darwin").probe()
    assert len(devices) == 1
    assert devices[0].transport == "imbupd"
    assert devices[0].identifier == "15e4:d014"


def test_jc11s_writer_fails_closed():
    """JC11S (Prime 4+) writing remains blocked - no validated implementation exists."""
    with pytest.raises(WriteBlockedError):
        FastbootTransport().install(Path("payload.img"))


def test_jp24_writer_delegates_to_validated_bootstrap(monkeypatch, tmp_path):
    """JP24 (Prime 4 G2) writing is validated against real hardware (see
    devices/jp24/README.md) and delegates to core.jp24_bootstrap rather than
    failing closed. The actual subprocess/USB/admin-prompt calls are faked
    here - this test must never touch real hardware or prompt for a password."""
    calls: dict[str, object] = {}

    def fake_ensure_flash_tool(build_dir: Path) -> Path:
        calls["build_dir"] = build_dir
        return tmp_path / "imbupd_write_mac"

    def fake_flash_bootstrap(tool: Path, image: Path, *, on_progress=None) -> None:
        calls["tool"] = tool
        calls["image"] = image
        calls["on_progress"] = on_progress

    monkeypatch.setattr(jp24_bootstrap, "ensure_flash_tool", fake_ensure_flash_tool)
    monkeypatch.setattr(jp24_bootstrap, "flash_bootstrap", fake_flash_bootstrap)

    image = tmp_path / "prime4-data.img"
    ImbupdTransport("linux").install(image)

    assert calls["image"] == image
    assert calls["tool"] == tmp_path / "imbupd_write_mac"


def test_jp24_writer_without_device_raises_flash_error(monkeypatch, tmp_path):
    """No device present must raise FlashError, not crash or hang - and must
    never shell out to a real tool or admin prompt from a unit test."""

    def fake_ensure_flash_tool(build_dir: Path) -> Path:
        return tmp_path / "imbupd_write_mac"

    def fake_flash_bootstrap(tool: Path, image: Path, *, on_progress=None) -> None:
        raise jp24_bootstrap.FlashError("No JP24 device was found in Firmware Update Mode.")

    monkeypatch.setattr(jp24_bootstrap, "ensure_flash_tool", fake_ensure_flash_tool)
    monkeypatch.setattr(jp24_bootstrap, "flash_bootstrap", fake_flash_bootstrap)

    with pytest.raises(jp24_bootstrap.FlashError):
        ImbupdTransport("linux").install(tmp_path / "prime4-data.img")
