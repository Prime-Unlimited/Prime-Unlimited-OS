"""Best-effort device IP discovery. Read-only: never writes to anything.

Two strategies, tried in order:

1. mDNS hostname resolution for "prime4g2.local" - the device's hostname
   observed on real hardware (confirmed fixed for the model, not unique
   per unit). Fast and exact when it works; relies on the OS's own mDNS
   resolver (built into macOS/Linux's getaddrinfo, no extra dependency).
2. A concurrent TCP connect sweep of the local /24 subnet on the
   profile's SSH port. Not device-identity-verified - any host with that
   port open matches - so the caller must still show the result and let
   the user confirm before trusting it (the SSH test page's existing
   host-key inspection step already requires that regardless of how the
   IP was obtained).
"""

from __future__ import annotations

import socket
from concurrent.futures import ThreadPoolExecutor, as_completed
from dataclasses import dataclass

MDNS_HOSTNAME = "prime4g2.local"


class DiscoveryError(RuntimeError):
    pass


@dataclass(frozen=True)
class DiscoveryResult:
    host: str
    method: str  # "mdns" or "subnet-scan"


def _resolve_mdns(hostname: str, timeout: float) -> str | None:
    original_timeout = socket.getdefaulttimeout()
    socket.setdefaulttimeout(timeout)
    try:
        return socket.gethostbyname(hostname)
    except OSError:
        return None
    finally:
        socket.setdefaulttimeout(original_timeout)


def _local_subnet_hosts() -> list[str]:
    """The /24 containing this machine's primary outbound-route IP."""
    probe = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
    try:
        probe.connect(("8.8.8.8", 80))
        local_ip = probe.getsockname()[0]
    except OSError:
        return []
    finally:
        probe.close()
    prefix = ".".join(local_ip.split(".")[:3])
    return [f"{prefix}.{i}" for i in range(1, 255)]


def _port_open(host: str, port: int, timeout: float) -> bool:
    try:
        with socket.create_connection((host, port), timeout=timeout):
            return True
    except OSError:
        return False


def _scan_subnet(port: int, timeout: float) -> str | None:
    hosts = _local_subnet_hosts()
    if not hosts:
        return None
    with ThreadPoolExecutor(max_workers=64) as pool:
        futures = {pool.submit(_port_open, host, port, timeout): host for host in hosts}
        for future in as_completed(futures):
            if future.result():
                return futures[future]
    return None


def discover_device(port: int, mdns_timeout: float = 3.0, scan_timeout: float = 0.5) -> DiscoveryResult:
    host = _resolve_mdns(MDNS_HOSTNAME, mdns_timeout)
    if host:
        return DiscoveryResult(host, "mdns")
    host = _scan_subnet(port, scan_timeout)
    if host:
        return DiscoveryResult(host, "subnet-scan")
    raise DiscoveryError(
        f"Could not find the device via mDNS ({MDNS_HOSTNAME}) or by scanning the local "
        f"network for an open port {port}. Enter the IP address manually."
    )
