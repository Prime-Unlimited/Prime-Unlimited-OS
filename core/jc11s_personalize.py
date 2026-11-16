"""JC11S image personalization: preserve a proven bootable /data base and inject one client key.

This module never flashes hardware and never creates an empty /data filesystem.
"""
from __future__ import annotations

import hashlib
import re
import shutil
from pathlib import Path

from core.jc11s_image import public_key, run
from core.android_sparse import convert, verify_set

IMAGE_SIZE = 0x8FD7BE00
SSH_ROOT = "/sc5000-custom/ssh"


def sha256(path: Path) -> str:
    h = hashlib.sha256()
    with path.open("rb") as source:
        for block in iter(lambda: source.read(1024 * 1024), b""):
            h.update(block)
    return h.hexdigest()


def _debugfs(image: Path, command: str, *, tool_directory: Path | None = None) -> str:
    return run(["debugfs", "-R", command, str(image)], tool_directory=tool_directory)


def personalize(
    source: Path, output: Path, public_key_path: Path, *,
    expected_size: int = IMAGE_SIZE,
    tool_directory: Path | None = None,
) -> dict:
    """Create an independently verified copy; never change the reference image."""
    source, output = source.resolve(), output.resolve()
    if not source.is_file() or source.stat().st_size != expected_size:
        raise ValueError("JC11S reference image missing or size does not match the probed data partition.")
    checksum_file = source.with_suffix(source.suffix + ".sha256")
    if not checksum_file.is_file():
        raise ValueError("Reference image SHA256 sidecar is required.")
    expected_hash = checksum_file.read_text(encoding="ascii").split()[0].lower()
    if not re.fullmatch(r"[a-f0-9]{64}", expected_hash) or sha256(source) != expected_hash:
        raise ValueError("Reference image SHA256 mismatch.")
    key = public_key(public_key_path)
    if output.exists():
        raise FileExistsError("Refusing to overwrite existing image.")
    if output == source:
        raise ValueError("Reference and output image must differ.")
    # The base must already carry the complete boot activation and SSH config.
    config = _debugfs(source, f"cat {SSH_ROOT}/sshd_config", tool_directory=tool_directory)
    match = re.search(r"(?im)^\s*AuthorizedKeysFile\s+(\S+)", config)
    if not match:
        raise ValueError("Base SSH configuration has no explicit AuthorizedKeysFile.")
    auth_path = match.group(1)
    if auth_path.startswith("/data/"):
        auth_path = auth_path[5:]
    if not auth_path.startswith("/") or ".." in Path(auth_path).parts:
        raise ValueError("Unsafe authorized keys path in base SSH configuration.")
    # Do not accept an unrecognized service layout or accidentally inject elsewhere.
    if not auth_path.startswith("/sc5000-custom/"):
        raise ValueError("Unsupported SSH authorized keys path in reference image.")
    shutil.copyfile(source, output)
    try:
        # debugfs write does not overwrite existing files; remove then write.
        run(["debugfs", "-w", "-R", f"rm {auth_path}", str(output)], tool_directory=tool_directory)
        import tempfile
        with tempfile.TemporaryDirectory() as temporary:
            temp = Path(temporary)
            key_file = temp / "authorized_keys"
            key_file.write_bytes(key)
            run(["debugfs", "-w", "-R", f"write {key_file} {auth_path}", str(output)], tool_directory=tool_directory)
            run(["debugfs", "-w", "-R", f"set_inode_field {auth_path} mode 0100600", str(output)], tool_directory=tool_directory)
            run(["debugfs", "-R", f"dump {auth_path} {temp / 'readback'}", str(output)],
                tool_directory=tool_directory)
            if (temp / "readback").read_bytes() != key:
                raise ValueError("Public key readback failed.")
        run(["e2fsck", "-f", "-n", str(output)], tool_directory=tool_directory)
        if output.stat().st_size != expected_size:
            raise ValueError("Output image size mismatch.")
        return {"size": expected_size, "sha256": sha256(output), "authorized_keys": auth_path}
    except Exception:
        output.unlink(missing_ok=True)
        raise


def build_sparse(image: Path, directory: Path, max_download_size: int) -> tuple[Path, ...]:
    manifest = convert(image, directory, max_download_size)
    verified = verify_set(directory)
    if verified["expanded_sha256"] != sha256(image):
        raise ValueError("Sparse output does not match personalized image.")
    return tuple(directory / part["name"] for part in manifest["parts"])
