"""Ed25519 client key creation and pair validation."""

from __future__ import annotations

import getpass
import os
import subprocess
from dataclasses import dataclass
from pathlib import Path

from cryptography.hazmat.primitives import serialization
from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PrivateKey, Ed25519PublicKey

from core.processes import run_process


class KeyValidationError(ValueError):
    pass


@dataclass(frozen=True)
class KeyPair:
    private_key: Path
    public_key: Path


def _is_windows() -> bool:
    return os.name == "nt"


def restrict_private_key_access(private_path: Path) -> None:
    """Apply an owner-only Windows ACL accepted by Windows OpenSSH."""
    if not _is_windows():
        os.chmod(private_path, 0o600)
        return
    user = getpass.getuser()
    domain = os.environ.get("USERDOMAIN", "").strip()
    account = f"{domain}\\{user}" if domain else user
    try:
        result = run_process(
            ["icacls.exe", str(private_path), "/inheritance:r", "/grant:r", f"{account}:(F)"],
            capture_output=True,
            text=True,
            timeout=15,
            check=False,
        )
    except (OSError, subprocess.TimeoutExpired) as exc:
        raise KeyValidationError(f"Could not secure the private key with Windows ACLs: {exc}") from exc
    if result.returncode:
        detail = (result.stderr or result.stdout).strip()
        raise KeyValidationError(f"Could not secure the private key with Windows ACLs: {detail}")


def generate_keypair_paths(private_path: Path, public_path: Path, passphrase: str) -> KeyPair:
    if not passphrase:
        raise KeyValidationError("A passphrase is required for generated keys.")
    private_path = private_path.expanduser().resolve()
    public_path = public_path.expanduser().resolve()
    if private_path.exists() or public_path.exists():
        raise FileExistsError("The requested key files already exist.")
    private_path.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
    public_path.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
    private = Ed25519PrivateKey.generate()
    private_bytes = private.private_bytes(
        serialization.Encoding.PEM,
        serialization.PrivateFormat.OpenSSH,
        serialization.BestAvailableEncryption(passphrase.encode("utf-8")),
    )
    public_bytes = private.public_key().public_bytes(serialization.Encoding.OpenSSH, serialization.PublicFormat.OpenSSH)
    try:
        private_path.write_bytes(private_bytes)
        restrict_private_key_access(private_path)
        public_path.write_bytes(public_bytes + b" prime-unlimited-os\n")
        os.chmod(public_path, 0o644)
    except Exception:
        private_path.unlink(missing_ok=True)
        public_path.unlink(missing_ok=True)
        raise
    return KeyPair(private_path, public_path)


def generate_keypair(directory: Path, passphrase: str, name: str = "prime_unlimited_ed25519") -> KeyPair:
    return generate_keypair_paths(directory / name, directory / f"{name}.pub", passphrase)


def validate_keypair(private_path: Path, public_path: Path, passphrase: str = "") -> str:
    try:
        private = serialization.load_ssh_private_key(
            private_path.read_bytes(), password=passphrase.encode() if passphrase else None
        )
        public_parts = public_path.read_bytes().split(None, 2)
        public = serialization.load_ssh_public_key(b" ".join(public_parts[:2]))
    except (OSError, ValueError, TypeError, IndexError) as exc:
        raise KeyValidationError(f"Could not read the SSH key pair: {exc}") from exc
    if not isinstance(private, Ed25519PrivateKey) or not isinstance(public, Ed25519PublicKey):
        raise KeyValidationError("Only Ed25519 keys are supported.")
    private_public = private.public_key().public_bytes(serialization.Encoding.Raw, serialization.PublicFormat.Raw)
    public_raw = public.public_bytes(serialization.Encoding.Raw, serialization.PublicFormat.Raw)
    if private_public != public_raw:
        raise KeyValidationError("The public and private keys do not match.")
    return public_path.read_text(encoding="utf-8").strip()
