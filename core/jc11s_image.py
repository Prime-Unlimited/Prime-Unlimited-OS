"""Offline JC11S research images. No device access, mounts or reference archive needed."""

from __future__ import annotations

import argparse
import hashlib
import json
import os
import re
import struct
import tempfile
from pathlib import Path

from cryptography.hazmat.primitives import serialization
from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PublicKey

from core.processes import run_process

EPOCH = "1700000000"
UUID = "914fa90d-f643-42d5-a91c-184916a58cd5"
FEATURES = "none,has_journal,ext_attr,filetype,extent,sparse_super,large_file"
SOURCE = Path(__file__).resolve().parents[1] / "bootstrap" / "jc11s"
TRUST_ROOT = Path(__file__).resolve().parents[1] / "modloader" / "catalog" / "trust" / "prime-unlimited-2026-1.json"


def run(
    args: list[str],
    *,
    tool_directory: Path | None = None,
    mke2fs_config: Path | None = None,
    **kwargs,
) -> str:
    env = dict(os.environ, E2FSPROGS_FAKE_TIME=EPOCH, SOURCE_DATE_EPOCH=EPOCH, LC_ALL="C")
    if tool_directory is not None:
        executable = tool_directory / (args[0] + (".exe" if os.name == "nt" else ""))
        if not executable.is_file():
            raise FileNotFoundError(f"Bundled filesystem tool is missing: {executable.name}")
        args = [str(executable), *args[1:]]
        env["PATH"] = str(tool_directory) + os.pathsep + env.get("PATH", "")
    if mke2fs_config is not None:
        env["MKE2FS_CONFIG"] = str(mke2fs_config)
    result = run_process(args, env=env, capture_output=True, text=True, check=True, **kwargs)
    return result.stdout


def public_key(path: Path) -> bytes:
    data = path.read_bytes().strip()
    if b"\n" in data or b"\r" in data or len(data) > 4096:
        raise ValueError("Select exactly one Ed25519 public key, without options.")
    if not data.startswith(b"ssh-ed25519 "):
        raise ValueError("Only Ed25519 public keys are accepted.")
    key = serialization.load_ssh_public_key(data)
    if not isinstance(key, Ed25519PublicKey):
        raise ValueError("Only Ed25519 public keys are accepted.")
    return key.public_bytes(serialization.Encoding.OpenSSH, serialization.PublicFormat.OpenSSH) + b"\n"


def digest(path: Path) -> str:
    h = hashlib.sha256()
    with path.open("rb") as stream:
        for chunk in iter(lambda: stream.read(1024 * 1024), b""):
            h.update(chunk)
    return h.hexdigest()


def validate_hook(path: Path) -> None:
    data = path.read_bytes()
    if len(data) < 52 or data[:7] != b"\x7fELF\x01\x01\x01":
        raise ValueError("Hook must be a little-endian ELF32 shared object.")
    kind, machine = struct.unpack_from("<HH", data, 16)
    if (kind, machine) != (3, 40):
        raise ValueError("Hook must be an ARM shared object.")


def bootstrap_files(authorized: bytes, hook: Path) -> dict[str, tuple[bytes, int]]:
    """Return the public, root-owned payload staged into the JC11S /data image."""
    return {
        "prime-ssh/authorized_keys": (authorized, 0o600),
        "prime-ssh/libprime-ssh.so": (hook.read_bytes(), 0o644),
        "prime-ssh/activate.sh": ((SOURCE / "activate.sh").read_bytes(), 0o700),
        "prime-ssh/sshd_config": ((SOURCE / "sshd_config").read_bytes(), 0o600),
        "system/var/overlay/lib/alsa/conf.d/99-prime-ssh.conf": ((SOURCE / "99-prime-ssh.conf").read_bytes(), 0o644),
        "system/var-lib/alsa/conf.d/99-prime-ssh.conf": ((SOURCE / "99-prime-ssh.conf").read_bytes(), 0o644),
        "prime-unlimited/trust/catalog-key.json": (TRUST_ROOT.read_bytes(), 0o644),
    }


def build(
    output: Path,
    key: Path,
    hook: Path,
    size: int,
    device_geometry: dict | None = None,
    *,
    tool_directory: Path | None = None,
    mke2fs_config: Path | None = None,
) -> dict:
    """Explicit size is experimental, never inferred from USB ID or mount path."""
    if size < 64 * 1024**2 or size > 0xFFFFFFFF * 512 or size % 512:
        raise ValueError("Image size must be at least 64 MiB, sparse-addressable and sector aligned.")
    authorized = public_key(key)
    validate_hook(hook)
    output = output.resolve()
    manifest_path = output.with_suffix(output.suffix + ".json")
    if output.exists() or manifest_path.exists():
        raise FileExistsError("Refusing to overwrite image or manifest.")
    output.parent.mkdir(parents=True, exist_ok=True)
    files = bootstrap_files(authorized, hook)
    # Windows checkouts may convert tracked Linux payload files to CRLF.
    # Normalize only text payloads; never alter SSH keys or the ARM ELF hook.
    for name in (
        "prime-ssh/activate.sh",
        "prime-ssh/sshd_config",
        "system/var/overlay/lib/alsa/conf.d/99-prime-ssh.conf",
        "system/var-lib/alsa/conf.d/99-prime-ssh.conf",
    ):
        content, mode = files[name]
        files[name] = (content.replace(b"\r\n", b"\n"), mode)
    directories = {"prime-ssh/hostkeys"}
    for name in files:
        directories.update(str(p) for p in Path(name).parents if str(p) != ".")
    with tempfile.TemporaryDirectory(prefix="prime-ssh-") as temporary:
        temp = Path(temporary)
        staging = temp / "root"
        staging.mkdir()
        for name in sorted(directories):
            (staging / name).mkdir(parents=True, exist_ok=True)
        for name, (content, _) in files.items():
            (staging / name).write_bytes(content)
        for p in [staging, *staging.rglob("*")]:
            os.utime(p, (int(EPOCH), int(EPOCH)))
        # Work in a fresh private directory: debugfs paths never contain user text.
        image = temp / "data.ext4"
        with image.open("xb") as stream:
            stream.truncate(size)
        run(
            [
                "mke2fs",
                "-q",
                "-F",
                "-t",
                "ext4",
                "-b",
                "4096",
                "-I",
                "256",
                "-m",
                "0",
                "-U",
                UUID,
                "-L",
                "prime-data",
                "-O",
                FEATURES,
                "-E",
                f"lazy_itable_init=0,lazy_journal_init=0,hash_seed={UUID},root_owner=0:0",
                "-d",
                "root",
                "data.ext4",
            ],
            cwd=temp,
            tool_directory=tool_directory,
            mke2fs_config=mke2fs_config,
        )
        commands = []
        modes = {"/": 0o40755, "lost+found": 0o40700}
        modes.update({name: 0o40700 if name.startswith("prime-ssh") else 0o40755 for name in directories})
        modes.update({name: 0o100000 | mode for name, (_, mode) in files.items()})
        for name, mode in sorted(modes.items()):
            path = "/" + name.lstrip("/")
            for field, value in {
                "uid": 0,
                "gid": 0,
                "mode": mode,
                "atime": EPOCH,
                "ctime": EPOCH,
                "mtime": EPOCH,
                "crtime": EPOCH,
            }.items():
                commands.append(f"set_inode_field {path} {field} {value}")
        (temp / "metadata").write_text("\n".join(commands) + "\n")
        run(["debugfs", "-w", "-f", "metadata", "data.ext4"], cwd=temp, tool_directory=tool_directory)
        run(["e2fsck", "-f", "-n", "data.ext4"], cwd=temp, tool_directory=tool_directory)
        # Copy sparsely in blocks without allocating all zero data on disk.
        with image.open("rb") as src, output.open("xb") as dst:
            for chunk in iter(lambda: src.read(1024 * 1024), b""):
                if any(chunk):
                    dst.write(chunk)
                else:
                    dst.seek(len(chunk), 1)
            dst.truncate(size)
    manifest = {
        "schema": 1,
        "model": "JC11S",
        "hardware_validated": False,
        "boot_validated": False,
        "size": size,
        "sha256": digest(output),
        "format": "raw-ext4",
        "device_geometry": device_geometry,
        "files": {name: hashlib.sha256(data).hexdigest() for name, (data, _) in files.items()},
    }
    manifest_path.write_text(json.dumps(manifest, indent=2) + "\n")
    verify(output, tool_directory=tool_directory, mke2fs_config=mke2fs_config)
    return manifest


def verify_integrity(image: Path) -> dict:
    """Pure-Python size, hash and ext4-superblock check suitable for Windows."""
    manifest = json.loads(image.with_suffix(image.suffix + ".json").read_text())
    if image.stat().st_size != manifest["size"] or digest(image) != manifest["sha256"]:
        raise ValueError("Image size or SHA256 mismatch.")
    with image.open("rb") as stream:
        stream.seek(1024)
        superblock = stream.read(1024)
    if superblock[56:58] != b"\x53\xef" or not struct.unpack_from("<I", superblock, 96)[0] & 0x40:
        raise ValueError("Not an ext4 extent filesystem.")
    return manifest


def verify(
    image: Path, *, tool_directory: Path | None = None, mke2fs_config: Path | None = None
) -> dict:
    manifest = verify_integrity(image)
    run(["e2fsck", "-f", "-n", str(image.resolve())], tool_directory=tool_directory)
    with tempfile.TemporaryDirectory() as temporary:
        for index, (name, expected) in enumerate(manifest["files"].items()):
            if not re.fullmatch(r"[a-zA-Z0-9_./-]+", name) or ".." in name:
                raise ValueError("Unsafe manifest path.")
            # cwd isolates output filename from debugfs command parsing.
            run(
                ["debugfs", "-R", f"dump /{name} file{index}", str(image.resolve())],
                cwd=temporary,
                tool_directory=tool_directory,
            )
            if digest(Path(temporary) / f"file{index}") != expected:
                raise ValueError(f"Filesystem payload mismatch: {name}")
    return manifest


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    sub = parser.add_subparsers(dest="command", required=True)
    create = sub.add_parser("build")
    create.add_argument("--key", type=Path, required=True)
    create.add_argument("--hook", type=Path, required=True)
    create.add_argument("--size-bytes", type=int, required=True)
    create.add_argument("--output", type=Path, required=True)
    inspect = sub.add_parser("verify")
    inspect.add_argument("image", type=Path)
    args = parser.parse_args()
    result = build(args.output, args.key, args.hook, args.size_bytes) if args.command == "build" else verify(args.image)
    print(json.dumps(result, indent=2))


if __name__ == "__main__":
    main()
