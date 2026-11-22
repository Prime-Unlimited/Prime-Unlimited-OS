import base64
import hashlib
import json

import pytest
from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PrivateKey

from tools.publish_mod import canonical_json, private_key, publish_entry


def catalog(status="dev"):
    return {
        "schemaVersion": 2,
        "mods": [{
            "id": "clock",
            "version": "1.0.0",
            "status": status,
            "models": ["JC11S"],
            "architecture": "armv7l",
            "abi": "gnueabihf",
        }],
    }


def test_publish_entry_promotes_dev_and_records_immutable_asset(tmp_path):
    package = tmp_path / "clock.zip"
    package.write_bytes(b"device package")
    value = catalog()

    entry = publish_entry(value, "clock", "jc11s", "https://example.test/clock.zip", package)

    assert entry["status"] == "published"
    assert entry["size"] == len(b"device package")
    assert entry["sha256"] == hashlib.sha256(b"device package").hexdigest()
    assert json.loads(canonical_json(value))["mods"][0]["downloadUrl"].endswith("clock.zip")


def test_publish_rejects_already_published_mod(tmp_path):
    package = tmp_path / "clock.zip"
    package.write_bytes(b"package")
    with pytest.raises(ValueError, match="only a dev mod"):
        publish_entry(catalog("published"), "clock", "jc11s", "https://example.test/clock.zip", package)


def test_signing_key_requires_ed25519_seed():
    seed = bytes(range(32))
    key = private_key(base64.b64encode(seed).decode())
    signature = key.sign(b"catalog")
    Ed25519PrivateKey.from_private_bytes(seed).public_key().verify(signature, b"catalog")

    with pytest.raises(ValueError, match="32-byte"):
        private_key(base64.b64encode(b"short").decode())

