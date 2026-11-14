from pathlib import Path

import pytest

from core.keys import KeyValidationError, generate_keypair, validate_keypair


def test_generate_and_validate_ed25519_pair(tmp_path: Path):
    pair = generate_keypair(tmp_path, "correct horse battery staple")
    public = validate_keypair(pair.private_key, pair.public_key, "correct horse battery staple")
    assert public.startswith("ssh-ed25519 ")
    assert pair.private_key.stat().st_mode & 0o777 == 0o600
    assert b"PRIVATE" not in pair.public_key.read_bytes()


def test_generation_requires_passphrase(tmp_path: Path):
    with pytest.raises(KeyValidationError):
        generate_keypair(tmp_path, "")


def test_wrong_passphrase_is_rejected(tmp_path: Path):
    pair = generate_keypair(tmp_path, "right")
    with pytest.raises(KeyValidationError):
        validate_keypair(pair.private_key, pair.public_key, "wrong")


def test_mismatched_public_key_is_rejected(tmp_path: Path):
    first = generate_keypair(tmp_path / "a", "secret")
    second = generate_keypair(tmp_path / "b", "secret")
    with pytest.raises(KeyValidationError, match="do not match"):
        validate_keypair(first.private_key, second.public_key, "secret")
