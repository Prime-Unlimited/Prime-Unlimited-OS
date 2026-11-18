"""SSH/SFTP service with explicit host-key trust enrollment."""

from __future__ import annotations

import base64
import hashlib
import socket
from dataclasses import dataclass
from pathlib import Path

import paramiko


class SshVerificationError(RuntimeError):
    pass


@dataclass(frozen=True)
class HostIdentity:
    algorithm: str
    fingerprint: str


def sha256_fingerprint(key: paramiko.PKey) -> str:
    digest = hashlib.sha256(key.asbytes()).digest()
    return "SHA256:" + base64.b64encode(digest).decode("ascii").rstrip("=")


class SshService:
    @staticmethod
    def _known_host_name(host: str, port: int) -> str:
        return host if port == 22 else f"[{host}]:{port}"

    def has_pinned_host(self, host: str, port: int, known_hosts: Path) -> bool:
        if not known_hosts.exists():
            return False
        host_keys = paramiko.HostKeys()
        host_keys.load(str(known_hosts))
        return host_keys.lookup(self._known_host_name(host, port)) is not None

    def _remote_key(self, host: str, port: int, timeout: float) -> paramiko.PKey:
        sock = socket.create_connection((host, port), timeout=timeout)
        transport = paramiko.Transport(sock)
        try:
            transport.start_client(timeout=timeout)
            return transport.get_remote_server_key()
        except Exception as exc:
            raise SshVerificationError(f"Could not inspect the SSH host key: {exc}") from exc
        finally:
            transport.close()

    def inspect_host_key(self, host: str, port: int, timeout: float = 8) -> HostIdentity:
        key = self._remote_key(host, port, timeout)
        return HostIdentity(key.get_name(), sha256_fingerprint(key))

    def is_host_key_trusted(
        self, host: str, port: int, identity: HostIdentity, known_hosts: Path
    ) -> bool:
        """Return whether the inspected identity is already pinned locally."""
        if not known_hosts.exists():
            return False
        host_keys = paramiko.HostKeys()
        host_keys.load(str(known_hosts))
        hostname = self._known_host_name(host, port)
        stored = host_keys.lookup(hostname)
        if not stored or identity.algorithm not in stored:
            return False
        return sha256_fingerprint(stored[identity.algorithm]) == identity.fingerprint

    def enroll_host_key(
        self,
        host: str,
        port: int,
        expected_fingerprint: str,
        known_hosts: Path,
        timeout: float = 8,
    ) -> HostIdentity:
        """Pin a key only after the caller confirms the displayed fingerprint."""
        key = self._remote_key(host, port, timeout)
        identity = HostIdentity(key.get_name(), sha256_fingerprint(key))
        if identity.fingerprint != expected_fingerprint:
            raise SshVerificationError("The SSH host-key fingerprint changed before enrollment.")
        host_keys = paramiko.HostKeys()
        if known_hosts.exists():
            host_keys.load(str(known_hosts))
        hostname = self._known_host_name(host, port)
        host_keys.add(hostname, identity.algorithm, key)
        known_hosts.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
        host_keys.save(str(known_hosts))
        return identity

    def verify_authenticated(
        self,
        host: str,
        port: int,
        private_key: Path,
        passphrase: str,
        known_hosts: Path,
    ) -> str:
        client = paramiko.SSHClient()
        client.load_host_keys(str(known_hosts))
        client.set_missing_host_key_policy(paramiko.RejectPolicy())
        try:
            client.connect(
                host,
                port=port,
                username="root",
                key_filename=str(private_key),
                passphrase=passphrase,
                allow_agent=False,
                look_for_keys=False,
                timeout=10,
            )
            _, stdout, _ = client.exec_command("printf PRIME_UNLIMITED_SSH_OK", timeout=10)
            marker = stdout.read().decode("utf-8", "replace")
            if marker != "PRIME_UNLIMITED_SSH_OK":
                raise SshVerificationError("The authenticated command returned an unexpected result.")
            return marker
        except Exception as exc:
            if isinstance(exc, SshVerificationError):
                raise
            raise SshVerificationError(f"Authenticated SSH verification failed: {exc}") from exc
        finally:
            client.close()
