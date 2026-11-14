"""Guarded JP24 image transfer and post-install SSH verification."""

from __future__ import annotations

import hashlib
import subprocess
from dataclasses import dataclass
from pathlib import Path
from typing import Callable

from bootstrap.jp24.identity import Jp24IdentityProbe, Jp24Inspection
from core.ssh_service import SshService


class WriteBlockedError(RuntimeError):
    pass


@dataclass(frozen=True)
class FlashPlan:
    inspection: Jp24Inspection
    image: Path
    expected_sha256: str
    data_loss_acknowledged: bool


class Jp24Bootstrap:
    """Composable API for installer integration; no work runs at import time."""

    CONFIRMATION_TEXT = "FLASH JP24 /data"

    def __init__(
        self,
        native_tool: Path,
        identity_probe: Jp24IdentityProbe | None = None,
        ssh_service: SshService | None = None,
    ) -> None:
        self.native_tool = native_tool
        self.identity_probe = identity_probe or Jp24IdentityProbe(native_tool)
        self.ssh_service = ssh_service or SshService()

    def flash(
        self,
        plan: FlashPlan,
        *,
        confirm: Callable[[str], str],
    ) -> None:
        """Write /data only after all gates pass and the caller explicitly confirms.

        The caller should show the exact message to the user in a modal dialog.
        The native writer additionally requires the literal confirmation phrase.
        """
        current = self.identity_probe.probe()
        if not current.may_write:
            raise WriteBlockedError(f"Device identity/power gate failed: {current.detail}")
        if current != plan.inspection:
            raise WriteBlockedError("The device evidence changed after inspection. Run the setup flow again.")
        if not plan.data_loss_acknowledged:
            raise WriteBlockedError("Explicit acknowledgement that the complete /data partition will be replaced is required.")
        allowed_image_sizes = {64 * 1024 * 1024, 256 * 1024 * 1024}
        if not plan.image.is_file() or plan.image.stat().st_size not in allowed_image_sizes:
            raise WriteBlockedError("The validated JP24 image is missing or has an unsupported size.")
        if not plan.expected_sha256 or self._sha256(plan.image) != plan.expected_sha256.lower():
            raise WriteBlockedError("The image checksum does not match the validated build artifact.")
        message = (
            "This will erase and replace the complete Prime 4 G2 /data partition. "
            "No backup exists. Type to authorize this irreversible data loss: "
            + self.CONFIRMATION_TEXT
        )
        if confirm(message) != self.CONFIRMATION_TEXT:
            raise WriteBlockedError("Flash cancelled by the user.")

        # Repeat identity immediately before invoking the native writer.
        current = self.identity_probe.probe()
        if current != plan.inspection or not current.may_write:
            raise WriteBlockedError("The connected device changed after confirmation; no write was started.")

        subprocess.run(
            [str(self.native_tool), "write", "data", str(plan.image), "--confirm", self.CONFIRMATION_TEXT],
            check=True,
            timeout=600,
        )
        subprocess.run([str(self.native_tool), "reboot"], check=True, timeout=30)

    def verify_ssh(
        self,
        host: str,
        private_key: Path,
        passphrase: str,
        known_hosts: Path,
        expected_host_fingerprint: str,
    ) -> str:
        """Pin a user-verified host fingerprint, then prove key-authenticated SSH."""
        self.ssh_service.enroll_host_key(
            host,
            2222,
            expected_host_fingerprint,
            known_hosts,
        )
        return self.ssh_service.verify_authenticated(host, 2222, private_key, passphrase, known_hosts)

    @staticmethod
    def _sha256(path: Path) -> str:
        digest = hashlib.sha256()
        with path.open("rb") as stream:
            for chunk in iter(lambda: stream.read(1024 * 1024), b""):
                digest.update(chunk)
        return digest.hexdigest()
