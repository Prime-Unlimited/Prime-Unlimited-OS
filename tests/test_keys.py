from pathlib import Path

import pytest

from core.keys import (
    KeyValidationError,
    generate_keypair,
    generate_keypair_paths,
    restrict_private_key_access,
    validate_keypair,
)


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


def test_generate_pair_at_user_selected_paths(tmp_path: Path):
    private = tmp_path / "custom" / "controller_key"
    public = tmp_path / "public" / "controller_key.pub"
    pair = generate_keypair_paths(private, public, "secret")
    assert pair.private_key == private.resolve()
    assert pair.public_key == public.resolve()
    assert validate_keypair(private, public, "secret").startswith("ssh-ed25519 ")


def test_generation_never_overwrites_existing_key(tmp_path: Path):
    private = tmp_path / "existing"
    private.write_text("keep me", encoding="utf-8")
    with pytest.raises(FileExistsError):
        generate_keypair_paths(private, tmp_path / "existing.pub", "secret")
    assert private.read_text(encoding="utf-8") == "keep me"


def test_windows_private_key_acl_uses_hidden_checked_process(tmp_path: Path, monkeypatch):
    private = tmp_path / "key"
    private.write_text("private", encoding="utf-8")
    calls = []

    class Result:
        returncode = 0
        stdout = "processed"
        stderr = ""

    monkeypatch.setattr("core.keys._is_windows", lambda: True)
    monkeypatch.setattr("core.keys.getpass.getuser", lambda: "Tester")
    monkeypatch.setenv("USERDOMAIN", "LAB")
    monkeypatch.setattr("core.keys.run_process", lambda command, **kwargs: calls.append((command, kwargs)) or Result())
    restrict_private_key_access(private)
    assert calls[0][0][-1] == "LAB\\Tester:(F)"
    assert calls[0][1]["capture_output"] is True
