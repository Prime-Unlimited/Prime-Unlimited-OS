"""Validate, publish and sign one device-specific mod catalog entry."""

from __future__ import annotations

import argparse
import base64
import hashlib
import json
from pathlib import Path

from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PrivateKey

DEVICES = {
    "jc11s": {"model": "JC11S", "architecture": "armv7l", "abi": "gnueabihf"},
    "jp24": {"model": "JP24", "architecture": "aarch64", "abi": "gnu"},
}


def canonical_json(value: object) -> bytes:
    return json.dumps(value, ensure_ascii=False, separators=(",", ":")).encode() + b"\n"


def publish_entry(catalog: dict, mod_id: str, device: str, asset_url: str, package: Path) -> dict:
    expected = DEVICES[device]
    matches = [entry for entry in catalog.get("mods", []) if entry.get("id") == mod_id]
    if len(matches) != 1:
        raise ValueError(f"expected exactly one catalog entry for {mod_id!r}")
    entry = matches[0]
    if entry.get("status", "published") != "dev":
        raise ValueError("only a dev mod can be published")
    if expected["model"] not in entry.get("models", []):
        raise ValueError(f"mod does not declare model {expected['model']}")
    for field in ("architecture", "abi"):
        if entry.get(field) != expected[field]:
            raise ValueError(f"{field} must be {expected[field]!r} for {device}")
    payload = package.read_bytes()
    entry.update(
        status="published",
        downloadUrl=asset_url,
        sha256=hashlib.sha256(payload).hexdigest(),
        size=len(payload),
    )
    return entry


def private_key(value: str) -> Ed25519PrivateKey:
    raw = base64.b64decode(value, validate=True)
    if len(raw) != 32:
        raise ValueError("catalog signing key must be a base64-encoded 32-byte Ed25519 seed")
    return Ed25519PrivateKey.from_private_bytes(raw)


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--catalog", type=Path, required=True)
    parser.add_argument("--signature", type=Path, required=True)
    parser.add_argument("--package", type=Path, required=True)
    parser.add_argument("--mod-id", required=True)
    parser.add_argument("--device", choices=DEVICES, required=True)
    parser.add_argument("--asset-url", required=True)
    parser.add_argument("--key-id", required=True)
    parser.add_argument("--private-key", required=True)
    args = parser.parse_args()
    catalog = json.loads(args.catalog.read_text(encoding="utf-8"))
    entry = publish_entry(catalog, args.mod_id, args.device, args.asset_url, args.package)
    encoded = canonical_json(catalog)
    args.catalog.write_bytes(encoded)
    signature = private_key(args.private_key).sign(encoded)
    signature_document = {
        "algorithm": "ed25519",
        "keyId": args.key_id,
        "signature": base64.b64encode(signature).decode(),
    }
    args.signature.write_text(
        json.dumps(signature_document, indent=2) + "\n",
        encoding="utf-8",
    )
    print(json.dumps(entry, separators=(",", ":")))


if __name__ == "__main__":
    main()

