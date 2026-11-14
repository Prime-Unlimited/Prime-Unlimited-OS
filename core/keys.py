"""Ed25519 client key creation and pair validation."""

from __future__ import annotations

import os
from dataclasses import dataclass
from pathlib import Path

from cryptography.hazmat.primitives import serialization
from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PrivateKey, Ed25519PublicKey


class KeyValidationError(ValueError):
    pass


@dataclass(frozen=True)
class KeyPair:
    private_key: Path
    public_key: Path


def generate_keypair(directory: Path, passphrase: str, name: str = "prime_unlimited_ed25519") -> KeyPair:
    if not passphrase:
        raise KeyValidationError("A passphrase is required for generated keys.")
    directory.mkdir(parents=True, exist_ok=True, mode=0o700)
    private_path = directory / name
    public_path = directory / f"{name}.pub"
    if private_path.exists() or public_path.exists():
        raise FileExistsError("The requested key files already exist.")
    private = Ed25519PrivateKey.generate()
    private_bytes = private.private_bytes(
        serialization.Encoding.PEM,
        serialization.PrivateFormat.OpenSSH,
        serialization.BestAvailableEncryption(passphrase.encode("utf-8")),
    )
    public_bytes = private.public_key().public_bytes(serialization.Encoding.OpenSSH, serialization.PublicFormat.OpenSSH)
    private_path.write_bytes(private_bytes)
    os.chmod(private_path, 0o600)
    public_path.write_bytes(public_bytes + b" prime-unlimited-os\n")
    os.chmod(public_path, 0o644)
    return KeyPair(private_path, public_path)


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
