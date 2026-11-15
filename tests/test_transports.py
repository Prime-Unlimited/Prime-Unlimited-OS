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


def test_darwin_xml_probe_finds_nested_device(monkeypatch):
    """Regression test: `system_profiler SPUSBDataType` (plain text) prints
    Vendor ID and Product ID on separate lines, with field labels localized
    to the system language (e.g. German "USB-Hersteller-ID") - a combined
    "15e4:d014" substring never appears in that output, so the probe must
    use the -xml form and parse vendor_id/product_id pairs structurally.
    This also covers devices nested under a USB hub, as real hardware is."""
    plist = [
        {
            "_items": [
                {
                    "_name": "USB3 Gen2 Hub",
                    "vendor_id": "0x05ac",
                    "product_id": "0x800c",
                    "_items": [
                        {"_name": "PRIME 4 G2", "vendor_id": "0x15e4", "product_id": "0xd014"},
                    ],
                },
            ],
        },
    ]

    class FakeResult:
        returncode = 0
        stdout = plistlib.dumps(plist)
        stderr = b""

    def fake_run(command, **kwargs):
        assert "-xml" in command
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
