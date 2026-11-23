#!/usr/bin/env python3
"""Append a JP24 Prime Link trial record to a copied catalog; never sign or install it."""

import argparse
import hashlib
import json
from pathlib import Path
from urllib.parse import urlparse


def prepare(source: Path, package: Path, output: Path, download_url: str):
    catalog = json.loads(source.read_text())
    if catalog.get("schemaVersion") != 2 or catalog.get("catalogId") != "prime-unlimited-jp24":
        raise ValueError("expected a JP24 schema-v2 catalog")
    if not isinstance(catalog.get("mods"), list) or any(m.get("id") == "prime-link-jp24" for m in catalog["mods"]):
        raise ValueError("Prime Link record already exists or catalog is invalid")
    address = urlparse(download_url)
    if address.scheme != "https" or not address.hostname or address.username or address.password or address.query or address.fragment:
        raise ValueError("expected an immutable HTTPS package URL")
    if address.hostname not in {"api.github.com", "github.com"}:
        raise ValueError("unsupported package host")
    package_bytes = package.read_bytes()
    catalog["mods"].append({
        "id": "prime-link-jp24", "name": "Prime Link Deck Telemetry",
        "version": "0.1.0-dev.1",
        "description": "Local read-only deck telemetry hardware trial; no peer connection or audio.",
        "models": ["JP24"], "firmware": ["5.3.0"],
        "architecture": "aarch64", "abi": "gnu", "downloadUrl": download_url,
        "sha256": hashlib.sha256(package_bytes).hexdigest(), "size": len(package_bytes),
        "entrypoint": "prime-link/start.sh", "overlay": "prime-link/ui/DeckTelemetry.qml",
        "approval": {"maintainer": "Prime Unlimited", "devices": ["JP24"]},
    })
    if output.exists():
        raise FileExistsError(output)
    output.write_text(json.dumps(catalog, sort_keys=True, separators=(",", ":")) + "\n")


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--source", required=True, type=Path)
    parser.add_argument("--package", required=True, type=Path)
    parser.add_argument("--output", required=True, type=Path)
    parser.add_argument("--download-url", required=True)
    args = parser.parse_args()
    prepare(args.source, args.package, args.output, args.download_url)


if __name__ == "__main__":
    main()
