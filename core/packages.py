"""Integrity checks for pinned bootstrap packages."""

from __future__ import annotations

import hashlib
from dataclasses import dataclass
from pathlib import Path


class PackageValidationError(ValueError):
    pass


@dataclass(frozen=True)
class PackageSpec:
    profile: str
    firmware: str
    size: int
    sha256: str
    redistributable: bool


def validate_package(path: Path, spec: PackageSpec, expected_profile: str) -> None:
    if spec.profile != expected_profile:
        raise PackageValidationError("The package targets a different hardware profile.")
    if not spec.redistributable:
        raise PackageValidationError("Package redistribution rights have not been confirmed.")
    if not path.is_file() or path.stat().st_size != spec.size:
        raise PackageValidationError("Package size does not match the signed manifest.")
    hasher = hashlib.sha256()
    with path.open("rb") as package:
        for chunk in iter(lambda: package.read(1024 * 1024), b""):
            hasher.update(chunk)
    digest = hasher.hexdigest()
    if digest.lower() != spec.sha256.lower():
        raise PackageValidationError("Package SHA-256 does not match the signed manifest.")
