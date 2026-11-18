"""Strict manifests and SSH-oriented mod operation plans."""

from __future__ import annotations

import re
from dataclasses import dataclass
from typing import Any


class ManifestError(ValueError):
    pass


_ID = re.compile(r"^[a-z0-9]+(?:-[a-z0-9]+)*$")
_VERSION = re.compile(r"^\d+\.\d+\.\d+$")


@dataclass(frozen=True)
class ModManifest:
    id: str
    name: str
    version: str
    profiles: tuple[str, ...]
    firmware: tuple[str, ...]
    sha256: str
    install: tuple[str, ...]
    uninstall: tuple[str, ...]
    rollback: tuple[str, ...]

    @classmethod
    def from_dict(cls, value: dict[str, Any]) -> ModManifest:
        required = {"id", "name", "version", "profiles", "firmware", "sha256", "operations"}
        unknown = set(value) - required
        missing = required - set(value)
        if missing or unknown:
            raise ManifestError(f"Invalid manifest keys; missing={sorted(missing)}, unknown={sorted(unknown)}")
        if not isinstance(value["id"], str) or not _ID.fullmatch(value["id"]):
            raise ManifestError("Mod id must use lowercase kebab-case.")
        if not isinstance(value["version"], str) or not _VERSION.fullmatch(value["version"]):
            raise ManifestError("Mod version must be semantic major.minor.patch.")
        if not isinstance(value["sha256"], str) or not re.fullmatch(r"[0-9a-fA-F]{64}", value["sha256"]):
            raise ManifestError("Mod package requires a 64-character SHA-256 digest.")
        operations = value["operations"]
        if not isinstance(operations, dict) or set(operations) != {"install", "uninstall", "rollback"}:
            raise ManifestError("Operations must define install, uninstall, and rollback arrays.")
        for operation, commands in operations.items():
            if not isinstance(commands, list) or not all(isinstance(item, str) for item in commands):
                raise ManifestError(f"Operation {operation} must be an array of commands.")
        profiles = tuple(value["profiles"])
        if not profiles or not set(profiles) <= {"jc11s", "jp24"}:
            raise ManifestError("Manifest profiles must contain jc11s and/or jp24.")
        return cls(
            id=value["id"],
            name=value["name"],
            version=value["version"],
            profiles=profiles,
            firmware=tuple(value["firmware"]),
            sha256=value["sha256"].lower(),
            install=tuple(operations["install"]),
            uninstall=tuple(operations["uninstall"]),
            rollback=tuple(operations["rollback"]),
        )

    def supports(self, profile: str, firmware: str) -> bool:
        return profile in self.profiles and firmware in self.firmware
