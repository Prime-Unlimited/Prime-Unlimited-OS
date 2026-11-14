import hashlib
from pathlib import Path

import pytest

from core.packages import PackageSpec, PackageValidationError, validate_package


def spec_for(data: bytes, **changes) -> PackageSpec:
    values = {
        "profile": "jc11s",
        "firmware": "5.3.0",
        "size": len(data),
        "sha256": hashlib.sha256(data).hexdigest(),
        "redistributable": True,
    }
    values.update(changes)
    return PackageSpec(**values)


def test_package_integrity_and_profile(tmp_path: Path):
    package = tmp_path / "package.img"
    package.write_bytes(b"fixture")
    validate_package(package, spec_for(b"fixture"), "jc11s")


@pytest.mark.parametrize(
    "changes,message",
    [
        ({"profile": "jp24"}, "different hardware"),
        ({"redistributable": False}, "rights"),
        ({"size": 99}, "size"),
        ({"sha256": "0" * 64}, "SHA-256"),
    ],
)
def test_package_validation_rejects_unsafe_input(tmp_path: Path, changes, message):
    package = tmp_path / "package.img"
    package.write_bytes(b"fixture")
    with pytest.raises(PackageValidationError, match=message):
        validate_package(package, spec_for(b"fixture", **changes), "jc11s")
