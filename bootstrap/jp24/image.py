"""Public-key-only JP24 ext4 image builder wrapper."""

from __future__ import annotations

import hashlib
import os
import subprocess
from dataclasses import dataclass
from pathlib import Path


@dataclass(frozen=True)
class ImageArtifact:
    path: Path
    size_bytes: int
    sha256: str
    profile: str = "JP24"


SUPPORTED_IMAGE_SIZES_MIB = (64, 256)


def build_image(
    public_key: Path,
    output: Path,
    builder: Path | None = None,
    *,
    size_mib: int = 256,
) -> ImageArtifact:
    if size_mib not in SUPPORTED_IMAGE_SIZES_MIB:
        raise ValueError("JP24 image size must be exactly 64 or 256 MiB.")
    if not public_key.is_file():
        raise FileNotFoundError(f"SSH public key not found: {public_key}")
    lines = public_key.read_text(encoding="utf-8").splitlines()
    first_line = lines[0].split() if lines else []
    if len(first_line) < 2 or first_line[0] != "ssh-ed25519":
        raise ValueError("The installer must provide an OpenSSH Ed25519 public key.")

    module_dir = Path(__file__).resolve().parent
    build_script = builder or module_dir / "build-image.sh"
    environment = os.environ.copy()
    environment["DATA_SIZE_MB"] = str(size_mib)
    subprocess.run([str(build_script), str(public_key), str(output)], check=True, env=environment)
    expected_size = size_mib * 1024 * 1024
    if not output.is_file() or output.stat().st_size != expected_size:
        raise RuntimeError(f"The image builder did not produce an exact {size_mib} MiB JP24 image.")

    digest = hashlib.sha256()
    with output.open("rb") as stream:
        for chunk in iter(lambda: stream.read(1024 * 1024), b""):
            digest.update(chunk)
    return ImageArtifact(output, output.stat().st_size, digest.hexdigest(), f"JP24-{size_mib}MiB")
