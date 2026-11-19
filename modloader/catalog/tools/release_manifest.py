#!/usr/bin/env python3
"""Create and finalize provenance-bound cross-device release manifests."""

import argparse
import hashlib
import json
import re
from pathlib import Path

TARGETS = ("jc11s", "jp24")
HEX40 = re.compile(r"^[a-f0-9]{40}$")
HEX64 = re.compile(r"^[a-f0-9]{64}$")
REPOSITORY = re.compile(r"^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$")
ID = re.compile(r"^[a-z0-9][a-z0-9._-]{0,63}$")
VERSION = re.compile(r"^[0-9A-Za-z][0-9A-Za-z._+-]{0,63}$")


class ManifestError(ValueError):
    pass


def pairs(items):
    result = {}
    for key, value in items:
        if key in result:
            raise ManifestError(f"duplicate JSON key {key!r}")
        result[key] = value
    return result


def read_json(path):
    return json.loads(Path(path).read_text(), object_pairs_hook=pairs)


def sha256(path):
    return hashlib.sha256(Path(path).read_bytes()).hexdigest()


def atomic_json(path, value):
    path = Path(path)
    path.parent.mkdir(parents=True, exist_ok=True)
    temporary = path.with_name(f".{path.name}.tmp")
    temporary.write_text(json.dumps(value, sort_keys=True, separators=(",", ":")) + "\n")
    temporary.replace(path)


def create_candidate(repository, commit, tag, mod_id, version, packages):
    if not REPOSITORY.fullmatch(repository) or not HEX40.fullmatch(commit) or not ID.fullmatch(mod_id) or not VERSION.fullmatch(version) or tag != f"{mod_id}-v{version}":
        raise ManifestError("candidate tag/version/commit binding is invalid")
    entries = []
    packages = Path(packages)
    for target in TARGETS:
        matches = list(packages.glob(f"{mod_id}-v{version}-{target}.zip"))
        if len(matches) != 1:
            raise ManifestError(f"expected exactly one {target} package")
        package = matches[0]
        if package.name != f"{mod_id}-v{version}-{target}.zip" or package.is_symlink():
            raise ManifestError(f"unsafe {target} package")
        entries.append({"target": target, "filename": package.name, "sha256": sha256(package), "size": package.stat().st_size})
    return {"schemaVersion": 1, "repository": repository, "commitSha": commit, "tag": tag, "modId": mod_id, "version": version, "targets": entries}


def finalize(candidate, approval, contracts, packages, assets, bases):
    if candidate.get("schemaVersion") != 1 or approval.get("schemaVersion") != 1:
        raise ManifestError("unsupported manifest schema")
    if set(candidate) != {"schemaVersion", "repository", "commitSha", "tag", "modId", "version", "targets"}:
        raise ManifestError("candidate fields are invalid")
    if set(approval) != {"schemaVersion", "modId", "version", "targets"}:
        raise ManifestError("approval fields are invalid")
    if not isinstance(assets, dict) or set(assets) != set(TARGETS):
        raise ManifestError("asset map must exactly cover the target matrix")
    for field in ("modId", "version"):
        if approval.get(field) != candidate.get(field):
            raise ManifestError(f"approval {field} does not match candidate")
    repository, commit, tag, mod_id, version = (candidate.get(name) for name in ("repository", "commitSha", "tag", "modId", "version"))
    if not REPOSITORY.fullmatch(repository or "") or not HEX40.fullmatch(commit or "") or not ID.fullmatch(mod_id or "") or not VERSION.fullmatch(version or "") or tag != f"{mod_id}-v{version}":
        raise ManifestError("candidate provenance is invalid")
    if not isinstance(contracts, dict) or set(contracts) != set(TARGETS):
        raise ManifestError("target contracts must exactly cover the supported matrix")
    approvals = approval.get("targets")
    if not isinstance(approvals, dict) or set(approvals) != set(TARGETS):
        raise ManifestError("reviewed approval must cover the complete target matrix")
    candidate_list = candidate.get("targets")
    if not isinstance(candidate_list, list) or len(candidate_list) != len(TARGETS):
        raise ManifestError("candidate target matrix is incomplete")
    candidates = {entry.get("target"): entry for entry in candidate_list if isinstance(entry, dict)}
    if set(candidates) != set(TARGETS) or len(candidates) != len(candidate_list):
        raise ManifestError("candidate target matrix is duplicate or incomplete")
    entries = []
    for target in TARGETS:
        item, reviewed, contract = candidates[target], approvals[target], contracts[target]
        if set(item) != {"target", "filename", "sha256", "size"} or not HEX64.fullmatch(item.get("sha256", "")) or type(item.get("size")) is not int or item["size"] <= 0:
            raise ManifestError(f"invalid candidate metadata for {target}")
        if set(reviewed) != {"technicallyCompatible", "hardwareVerified", "officialCatalogApproved", "catalog"}:
            raise ManifestError(f"invalid reviewed approval for {target}")
        if any(type(reviewed[field]) is not bool for field in ("technicallyCompatible", "hardwareVerified", "officialCatalogApproved")):
            raise ManifestError(f"approval flags must be booleans for {target}")
        required_contract = {"catalogId", "model", "architecture", "abi", "firmware", "qtQuick", "overlayHostAvailable"}
        if set(contract) != required_contract or not all(isinstance(contract[field], str) and contract[field] for field in ("catalogId", "model", "architecture", "abi", "qtQuick")) or not isinstance(contract["firmware"], list) or not contract["firmware"] or type(contract["overlayHostAvailable"]) is not bool:
            raise ManifestError(f"invalid target contract for {target}")
        filename = item.get("filename")
        if not isinstance(filename, str) or Path(filename).name != filename or filename != f"{mod_id}-v{version}-{target}.zip":
            raise ManifestError(f"unsafe candidate filename for {target}")
        package = Path(packages) / item["filename"]
        if not package.is_file() or package.is_symlink() or sha256(package) != item.get("sha256") or package.stat().st_size != item.get("size"):
            raise ManifestError(f"candidate package changed for {target}")
        url = assets.get(target)
        expected_prefix = f"https://api.github.com/repos/{repository}/releases/assets/"
        if not isinstance(url, str) or not url.startswith(expected_prefix) or not url.removeprefix(expected_prefix).isdigit():
            raise ManifestError(f"missing immutable asset URL for {target}")
        entries.append({
            "target": target, "model": contract["model"], "architecture": contract["architecture"],
            "abi": contract["abi"], "firmware": contract["firmware"], "qtQuick": contract["qtQuick"],
            "filename": item["filename"], "downloadUrl": url, "sha256": item["sha256"], "size": item["size"],
            "status": {"built": True, "structurallyValidated": True,
                       "technicallyCompatible": reviewed["technicallyCompatible"],
                       "hardwareVerified": reviewed["hardwareVerified"],
                       "officialCatalogApproved": reviewed["officialCatalogApproved"]},
            "catalog": reviewed["catalog"],
        })
    expected = {target: sha256(Path(bases) / f"{target}.json") for target in TARGETS}
    return {"schemaVersion": 1, "repository": repository, "commitSha": commit, "tag": tag,
            "modId": mod_id, "version": version, "expectedBaseCatalogSha256": expected, "targets": entries}


def main():
    parser = argparse.ArgumentParser()
    commands = parser.add_subparsers(dest="command", required=True)
    candidate = commands.add_parser("candidate")
    for name in ("repository", "commit", "tag", "mod-id", "version", "packages", "output"):
        candidate.add_argument(f"--{name}", required=True)
    final = commands.add_parser("finalize")
    for name in ("candidate", "approval", "contracts", "packages", "assets", "bases", "output"):
        final.add_argument(f"--{name}", required=True)
    args = parser.parse_args()
    if args.command == "candidate":
        result = create_candidate(args.repository, args.commit, args.tag, args.mod_id, args.version, args.packages)
    else:
        result = finalize(read_json(args.candidate), read_json(args.approval), read_json(args.contracts), args.packages, read_json(args.assets), args.bases)
    atomic_json(args.output, result)


if __name__ == "__main__":
    try:
        main()
    except (OSError, KeyError, TypeError, json.JSONDecodeError, ManifestError) as error:
        raise SystemExit(f"release manifest failed: {error}")
