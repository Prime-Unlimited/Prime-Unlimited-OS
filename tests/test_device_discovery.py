import pytest

import core.device_discovery as discovery
from core.device_discovery import DiscoveryError, DiscoveryResult, discover_device


def test_discover_device_prefers_mdns(monkeypatch):
    monkeypatch.setattr(discovery, "_resolve_mdns", lambda hostname, timeout: "192.0.2.42")
    monkeypatch.setattr(
        discovery, "_scan_subnet", lambda port, timeout: pytest.fail("subnet scan should not run")
    )

    result = discover_device(port=2222)

    assert result == DiscoveryResult("192.0.2.42", "mdns")


def test_discover_device_falls_back_to_subnet_scan_when_mdns_fails(monkeypatch):
    monkeypatch.setattr(discovery, "_resolve_mdns", lambda hostname, timeout: None)
    monkeypatch.setattr(discovery, "_scan_subnet", lambda port, timeout: "192.0.2.77")

    result = discover_device(port=2222)

    assert result == DiscoveryResult("192.0.2.77", "subnet-scan")


def test_discover_device_raises_when_both_strategies_fail(monkeypatch):
    monkeypatch.setattr(discovery, "_resolve_mdns", lambda hostname, timeout: None)
    monkeypatch.setattr(discovery, "_scan_subnet", lambda port, timeout: None)

    with pytest.raises(DiscoveryError, match="mDNS"):
        discover_device(port=2222)


def test_port_open_detects_a_listening_socket():
    import socket

    server = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
    server.bind(("127.0.0.1", 0))
    server.listen(1)
    port = server.getsockname()[1]
    try:
        assert discovery._port_open("127.0.0.1", port, timeout=1.0) is True
    finally:
        server.close()
    assert discovery._port_open("127.0.0.1", port, timeout=0.2) is False
