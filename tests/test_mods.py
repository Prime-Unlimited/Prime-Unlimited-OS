import pytest

from core.mods import ManifestError, ModManifest


def valid_manifest():
    return {
        "id": "usb-copy",
        "name": "USB Copy",
        "version": "1.2.3",
        "profiles": ["jp24"],
        "firmware": ["5.3.0"],
        "sha256": "a" * 64,
        "operations": {"install": ["install.sh"], "uninstall": [], "rollback": ["rollback.sh"]},
    }


def test_manifest_parses_and_checks_exact_compatibility():
    manifest = ModManifest.from_dict(valid_manifest())
    assert manifest.supports("jp24", "5.3.0")
    assert not manifest.supports("jc11s", "5.3.0")


def test_manifest_rejects_unknown_fields():
    value = valid_manifest()
    value["download"] = "https://example.invalid/mod"
    with pytest.raises(ManifestError, match="unknown"):
        ModManifest.from_dict(value)


def test_manifest_requires_rollback_definition():
    value = valid_manifest()
    del value["operations"]["rollback"]
    with pytest.raises(ManifestError, match="rollback"):
        ModManifest.from_dict(value)
