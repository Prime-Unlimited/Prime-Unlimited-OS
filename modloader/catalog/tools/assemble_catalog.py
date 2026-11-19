#!/usr/bin/env python3
"""Deterministically merge one approved target into its device catalog v2."""

import argparse
import hashlib
import json
import os
import posixpath
import re
import tempfile
import urllib.parse
from pathlib import Path

ID = re.compile(r"^[a-z0-9][a-z0-9._-]{0,63}$")
SHA256 = re.compile(r"^[a-f0-9]{64}$")
VERSION = re.compile(r"^[0-9A-Za-z][0-9A-Za-z._+-]{0,63}$")
PRERELEASE = re.compile(r"^[0-9A-Za-z.-]+$")
REQUIRED_STATUS = (
    "built",
    "structurallyValidated",
    "technicallyCompatible",
    "hardwareVerified",
    "officialCatalogApproved",
)


class ReleaseError(ValueError):
    pass


def reject_duplicate_pairs(pairs):
    result = {}
    for key, value in pairs:
        if key in result:
            raise ReleaseError(f"duplicate JSON key {key!r}")
        result[key] = value
    return result


def strict_json_data(data, label):
    try:
        return json.loads(data, object_pairs_hook=reject_duplicate_pairs)
    except (UnicodeError, json.JSONDecodeError) as error:
        raise ReleaseError(f"cannot read JSON {label}: {error}") from error


def strict_json(path):
    try:
        return strict_json_data(Path(path).read_text(encoding="utf-8"), path)
    except OSError as error:
        raise ReleaseError(f"cannot read JSON {path}: {error}") from error


def digest(data):
    return hashlib.sha256(data).hexdigest()


def safe_https(url):
    if not isinstance(url, str):
        return False
    try:
        parsed = urllib.parse.urlsplit(url)
        return parsed.scheme == "https" and bool(parsed.netloc) and not parsed.username and not parsed.password and not parsed.fragment
    except ValueError:
        return False


def require(condition, message):
    if not condition:
        raise ReleaseError(message)


def validate_unique(items, label):
    seen = set()
    for item in items:
        require(isinstance(item, dict) and ID.fullmatch(item.get("id", "")), f"invalid {label} id")
        require(item["id"] not in seen, f"duplicate {label} id {item['id']!r}")
        seen.add(item["id"])


def validate_string_list(value, label):
    require(isinstance(value, list) and 1 <= len(value) <= 64, f"invalid {label}")
    require(all(isinstance(item, str) and item and len(item.encode("utf-8")) <= 100 and item.strip() == item for item in value), f"invalid {label}")
    require(len(value) == len(set(value)), f"invalid {label}")


def safe_archive_path(value):
    if not isinstance(value, str) or not value or "\\" in value or value.startswith("/") or "\x00" in value:
        return False
    clean = posixpath.normpath(value)
    if clean in (".", "..") or clean.startswith("../") or clean != value.rstrip("/") or len(clean.encode("utf-8")) > 240:
        return False
    parts = clean.split("/")
    return len(parts) <= 32 and ":" not in parts[0] and clean != ".prime-unlimited-stage.json"


def validate_dependencies(value, label):
    require(isinstance(value, list) and len(value) <= 256, f"invalid {label}")
    seen = set()
    for dependency in value:
        require(isinstance(dependency, dict) and set(dependency) <= {"id", "constraint", "type"} and {"id", "constraint"} <= set(dependency), f"invalid {label} entry")
        dep_id = dependency.get("id", "")
        constraint = dependency.get("constraint")
        dep_type = dependency.get("type", "")
        require(isinstance(dep_id, str) and ID.fullmatch(dep_id) and isinstance(constraint, str) and constraint and len(constraint.encode("utf-8")) <= 100 and constraint.strip() == constraint and isinstance(dep_type, str) and dep_type in ("", "mod", "library"), f"invalid {label} entry")
        key = (dep_type, dep_id)
        require(key not in seen, f"duplicate {label} entry")
        seen.add(key)


def validate_record(item, library=False):
    common = {
        "id", "name", "version", "models", "firmware", "architecture", "abi",
        "downloadUrl", "sha256", "size", "approval", "dependencies", "conflicts", "revoked",
    }
    allowed = common | ({"revision", "description", "entrypoint", "overlay", "libraries"} if not library else set())
    require(set(item) <= allowed, f"unsupported fields in {'library' if library else 'mod'} {item.get('id', '')!r}")
    required = {"id", "name", "version", "models", "firmware", "architecture", "abi", "downloadUrl", "sha256", "size", "approval"}
    if not library:
        required.add("entrypoint")
    require(required <= set(item), f"incomplete {'library' if library else 'mod'} {item.get('id', '')!r}")
    for field in ("id", "name", "version", "architecture", "abi"):
        require(isinstance(item.get(field), str) and item[field], f"invalid {field}")
    require(ID.fullmatch(item["id"]), "invalid record id")
    require(len(item["name"].encode("utf-8")) <= 100 and item["name"].strip() == item["name"], "invalid name")
    require(len(item["version"].encode("utf-8")) <= 40 and item["version"].strip() == item["version"] and VERSION.fullmatch(item["version"]), "invalid version")
    if not library and "revision" in item:
        require(isinstance(item["revision"], int) and not isinstance(item["revision"], bool) and item["revision"] >= 0, "invalid revision")
    require(len(item["architecture"].encode("utf-8")) <= 40 and len(item["abi"].encode("utf-8")) <= 40, "invalid architecture or ABI")
    if not library and "description" in item:
        require(isinstance(item["description"], str) and len(item["description"].encode("utf-8")) <= 2000, "invalid description")
    validate_string_list(item["models"], "models")
    validate_string_list(item["firmware"], "firmware")
    require(safe_https(item["downloadUrl"]), "unsafe record URL")
    require(isinstance(item["sha256"], str) and SHA256.fullmatch(item["sha256"]), "invalid record sha256")
    require(isinstance(item["size"], int) and not isinstance(item["size"], bool) and 0 < item["size"] <= 128 * 1024 * 1024, "invalid record size")
    approval = item["approval"]
    require(isinstance(approval, dict) and set(approval) == {"maintainer", "devices"}, "invalid record approval")
    require(isinstance(approval["maintainer"], str) and approval["maintainer"] and len(approval["maintainer"].encode("utf-8")) <= 100 and approval["maintainer"].strip() == approval["maintainer"], "invalid approval maintainer")
    validate_string_list(approval["devices"], "approval devices")
    if not library:
        require(safe_archive_path(item["entrypoint"]) and not item["entrypoint"].endswith("/"), "invalid entrypoint")
        if "overlay" in item:
            require(safe_archive_path(item["overlay"]) and item["overlay"].endswith(".qml"), "invalid overlay")
    for field in ("dependencies", "libraries"):
        if field in item:
            validate_dependencies(item[field], field)
    if "conflicts" in item:
        require(isinstance(item["conflicts"], list) and len(item["conflicts"]) <= 256, "invalid conflicts")
        require(all(isinstance(value, str) and ID.fullmatch(value) for value in item["conflicts"]), "invalid conflicts")
        require(len(item["conflicts"]) == len(set(item["conflicts"])), "invalid conflicts")
    if "revoked" in item:
        require(isinstance(item["revoked"], bool), "invalid revoked flag")


def parse_semantic_version(value):
    parts = value.split("-", 1)
    numbers = parts[0].split(".")
    if not 1 <= len(numbers) <= 3:
        return None
    parsed = []
    for number in numbers:
        if not number or (len(number) > 1 and number[0] == "0") or not number.isascii() or not number.isdigit():
            return None
        parsed.append(int(number))
    parsed.extend([0] * (3 - len(parsed)))
    prerelease = ""
    if len(parts) == 2:
        prerelease = parts[1]
        if not prerelease or not PRERELEASE.fullmatch(prerelease):
            return None
    return (*parsed, prerelease)


def compare_catalog_versions(left, right):
    left_semver = parse_semantic_version(left[1:] if left.startswith("v") else left)
    right_semver = parse_semantic_version(right[1:] if right.startswith("v") else right)
    if left_semver is not None and right_semver is not None:
        for left_number, right_number in zip(left_semver[:3], right_semver[:3]):
            if left_number != right_number:
                return -1 if left_number < right_number else 1
        left_prerelease, right_prerelease = left_semver[3], right_semver[3]
        if left_prerelease == right_prerelease:
            return 0
        if not left_prerelease:
            return 1
        if not right_prerelease:
            return -1
        return -1 if left_prerelease < right_prerelease else 1
    return (left > right) - (left < right)


def validate_base(catalog):
    require(isinstance(catalog, dict), "catalog must be an object")
    require(set(catalog) == {"schemaVersion", "catalogId", "mods", "libraries"}, "catalog must use the detached schema-v2 root shape")
    require(catalog["schemaVersion"] == 2, "catalog schemaVersion must be 2")
    require(ID.fullmatch(catalog.get("catalogId", "")), "invalid catalogId")
    require(isinstance(catalog["mods"], list) and len(catalog["mods"]) <= 2048, "mods must be an array with at most 2048 entries")
    require(isinstance(catalog["libraries"], list) and len(catalog["libraries"]) <= 2048, "libraries must be an array with at most 2048 entries")
    validate_unique(catalog["mods"], "mod")
    validate_unique(catalog["libraries"], "library")
    for item in catalog["mods"]:
        validate_record(item)
    for item in catalog["libraries"]:
        validate_record(item, library=True)


def validate_package(entry, packages):
    filename = entry.get("filename", "")
    require(filename and Path(filename).name == filename and filename.endswith(".zip"), "invalid package filename")
    package = packages / filename
    require(package.is_file() and not package.is_symlink(), f"missing package {filename}")
    data = package.read_bytes()
    require(entry.get("size") == len(data) and 0 < len(data) <= 128 * 1024 * 1024, f"wrong package size for {filename}")
    require(SHA256.fullmatch(entry.get("sha256", "")) and entry["sha256"] == digest(data), f"wrong package sha256 for {filename}")
    require(safe_https(entry.get("downloadUrl", "")), f"unsafe package URL for {filename}")


def approved(entry, target):
    status = entry.get("status")
    require(isinstance(status, dict) and set(status) == set(REQUIRED_STATUS), f"invalid status for target {target}")
    return all(status[field] is True for field in REQUIRED_STATUS)


def catalog_record(entry, contract):
    record = entry.get("catalog")
    require(isinstance(record, dict), "catalog record must be an object")
    allowed = {
        "id", "name", "version", "revision", "description", "entrypoint", "overlay",
        "dependencies", "libraries", "conflicts", "revoked", "approval",
    }
    require(set(record) <= allowed, "catalog record contains unsupported fields")
    require(ID.fullmatch(record.get("id", "")), "invalid release mod id")
    for field in ("name", "version", "entrypoint"):
        require(isinstance(record.get(field), str) and record[field], f"missing catalog field {field}")
    require(isinstance(record.get("revision"), int) and not isinstance(record["revision"], bool) and record["revision"] >= 1, "catalog revision must start at 1")
    approval = record.get("approval")
    require(isinstance(approval, dict) and set(approval) == {"maintainer", "devices"}, "invalid approval object")
    require(isinstance(approval.get("maintainer"), str) and approval["maintainer"], "missing approval maintainer")
    require(approval.get("devices") == [contract["model"]], "approval devices do not match target")
    result = dict(record)
    result.update({
        "models": [contract["model"]],
        "firmware": list(contract["firmware"]),
        "downloadUrl": entry["downloadUrl"],
        "sha256": entry["sha256"],
        "size": entry["size"],
        "architecture": contract["architecture"],
        "abi": contract["abi"],
    })
    return result


def assemble(base_bytes, manifest, contracts, packages, target):
    base_digest = digest(base_bytes)
    expected = manifest.get("expectedBaseCatalogSha256")
    require(isinstance(expected, dict) and expected.get(target) == base_digest, "stale base catalog digest")
    catalog = strict_json_data(base_bytes, "base catalog")
    validate_base(catalog)
    require(manifest.get("schemaVersion") == 1, "release manifest schemaVersion must be 1")
    targets = manifest.get("targets")
    require(isinstance(targets, list) and targets, "release targets must be a nonempty array")
    names = [entry.get("target") for entry in targets if isinstance(entry, dict)]
    require(len(names) == len(targets) and len(names) == len(set(names)), "duplicate or invalid release target")
    require(set(names) == set(contracts), "release target matrix is incomplete")
    selected_contract = contracts.get(target)
    require(isinstance(selected_contract, dict), f"unknown target {target}")
    require(catalog.get("catalogId") == selected_contract.get("catalogId"), f"wrong catalogId for target {target}")

    mods = {item["id"]: item for item in catalog["mods"]}
    for entry in targets:
        entry_target = entry["target"]
        contract = contracts.get(entry_target)
        require(isinstance(contract, dict), f"unknown target {entry_target}")
        validate_package(entry, packages)
        require(entry.get("model") == contract.get("model"), f"wrong model for target {entry_target}")
        require(entry.get("architecture") == contract.get("architecture"), f"wrong architecture for target {entry_target}")
        require(entry.get("abi") == contract.get("abi"), f"wrong ABI for target {entry_target}")
        require(entry.get("firmware") == contract.get("firmware") and bool(contract.get("firmware")), f"wrong or empty firmware for target {entry_target}")
        require(entry.get("qtQuick") == contract.get("qtQuick") and bool(contract.get("qtQuick")), f"wrong or empty QtQuick for target {entry_target}")
        is_approved = approved(entry, entry_target)
        if not is_approved:
            continue
        record = catalog_record(entry, contract)
        validate_record(record)
        if "overlay" in record:
            require(contract.get("overlayHostAvailable") is True, f"overlay host is not approved for target {entry_target}")
        if entry_target != target:
            continue
        previous = mods.get(record["id"])
        if previous and previous.get("revoked") is True and record.get("revoked") is not True:
            raise ReleaseError(f"refusing to un-revoke mod {record['id']!r}")
        version_order = compare_catalog_versions(previous["version"], record["version"]) if previous else -1
        if previous:
            require(version_order <= 0, f"version regression for {record['id']!r}")
        if previous and version_order == 0:
            require(previous == record or record["revision"] > previous.get("revision", 0), f"revision must increase for changed {record['id']!r}")
        mods[record["id"]] = record

    catalog["mods"] = [mods[key] for key in sorted(mods)]
    catalog["libraries"] = sorted(catalog["libraries"], key=lambda item: item["id"])
    return (json.dumps(catalog, sort_keys=True, separators=(",", ":")) + "\n").encode()


def atomic_write(path, data):
    path = Path(path)
    path.parent.mkdir(parents=True, exist_ok=True)
    descriptor, temporary = tempfile.mkstemp(prefix=".catalog-", dir=path.parent)
    try:
        with os.fdopen(descriptor, "wb") as output:
            output.write(data)
            output.flush()
            os.fsync(output.fileno())
        os.chmod(temporary, 0o644)
        os.replace(temporary, path)
        directory = os.open(path.parent, os.O_RDONLY)
        try:
            os.fsync(directory)
        finally:
            os.close(directory)
    finally:
        try:
            os.unlink(temporary)
        except FileNotFoundError:
            pass


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--base", required=True, type=Path)
    parser.add_argument("--manifest", required=True, type=Path)
    parser.add_argument("--targets", required=True, type=Path)
    parser.add_argument("--target", required=True)
    parser.add_argument("--packages", required=True, type=Path)
    parser.add_argument("--output", required=True, type=Path)
    args = parser.parse_args()
    base_bytes = args.base.read_bytes()
    result = assemble(base_bytes, strict_json(args.manifest), strict_json(args.targets), args.packages, args.target)
    atomic_write(args.output, result)
    print(f"catalog_sha256={digest(result)}")


if __name__ == "__main__":
    try:
        main()
    except (OSError, ReleaseError, json.JSONDecodeError) as error:
        raise SystemExit(f"catalog assembly failed: {error}")
