"""Offline mod-release rules for JC11S and JP24.

The directory is the publication state. mods/development is testable.
mods/ready is administrator-confirmed. Catalog JSON, signatures and GitHub
asset ids are derived from those directories and are not hand-edited.
"""

from __future__ import annotations

import base64
import fcntl
import hashlib
import json
import os
import re
import shutil
import stat
import urllib.request
import zipfile
from contextlib import contextmanager
from pathlib import Path

REPO = "Prime-Unlimited/Prime-Unlimited-OS"
PUBLISH_BRANCH = "feat/prime-unlimited-mod-loader"
LEGACY_READY = {("prime-clock", "0.1.2"), ("splash-manager", "1.1.2")}
ID_RE = re.compile(r"^[a-z0-9][a-z0-9._-]{0,63}$")
VERSION_RE = re.compile(r"^[0-9A-Za-z][0-9A-Za-z._+-]{0,63}$")
SHA_RE = re.compile(r"^[a-f0-9]{64}$")
ASSET_URL_RE = re.compile(
    r"^https://api\.github\.com/repos/Prime-Unlimited/Prime-Unlimited-OS/releases/assets/[0-9]+$"
)
LEGACY_URL_RE = re.compile(
    r"^https://github\.com/Prime-Unlimited/Prime-Unlimited-OS/releases/download/[A-Za-z0-9._+-]+/[A-Za-z0-9._+-]+$"
)

DEVICES = {
    "jc11s": {
        "model": "JC11S",
        "architecture": "armv7l",
        "abi": "gnueabihf",
        "catalog_id": "prime-unlimited-jc11s",
        "elf_class": 1,
        "elf_machine": 40,
    },
    "jp24": {
        "model": "JP24",
        "architecture": "aarch64",
        "abi": "gnu",
        "catalog_id": "prime-unlimited-jp24",
        "elf_class": 2,
        "elf_machine": 183,
    },
}


class ModReleaseError(Exception):
    """A validation or publication rule failed. The catalog is left unchanged."""


def asset_url(asset_id: int) -> str:
    if not isinstance(asset_id, int) or isinstance(asset_id, bool) or asset_id < 1:
        raise ModReleaseError("GitHub asset id is missing")
    return f"https://api.github.com/repos/{REPO}/releases/assets/{asset_id}"


def github_asset_redirect(origin: str, location: str, hops: int) -> str:
    """Allow one hop from an official asset URL to GitHub's release CDN."""

    from urllib.parse import urljoin, urlparse

    if hops != 1 or not location:
        raise ModReleaseError("HTTP redirect blocked")
    target = urljoin(origin, location)
    origin_parts = urlparse(origin)
    parts = urlparse(target)
    official = (
        origin_parts.scheme == "https"
        and origin_parts.hostname == "api.github.com"
        and origin_parts.username is None
        and ASSET_URL_RE.fullmatch(origin) is not None
    )
    cdn = (
        parts.scheme == "https"
        and parts.hostname == "release-assets.githubusercontent.com"
        and parts.username is None
        and parts.password is None
        and not parts.fragment
    )
    if not official or not cdn:
        raise ModReleaseError("HTTP redirect blocked")
    return target


def _branch_names(policies: dict) -> set[str]:
    items = policies.get("branch_policies") if isinstance(policies, dict) else None
    if not isinstance(items, list):
        raise ModReleaseError("environment deployment branches are not restricted")
    names = set()
    for item in items:
        if not isinstance(item, dict) or item.get("type") != "branch" or not item.get("name"):
            raise ModReleaseError("environment deployment branches are not restricted")
        names.add(item["name"])
    return names


def require_sign_environment(environment: dict, policies: dict) -> None:
    """Development signing is branch-locked and has no reviewer substitute."""

    policy = environment.get("deployment_branch_policy") if isinstance(environment, dict) else None
    if not isinstance(policy, dict) or policy.get("protected_branches") or not policy.get("custom_branch_policies"):
        raise ModReleaseError(
            f"Environment mod-sign must allow only {PUBLISH_BRANCH}. "
            "Do not leave deployment branches open and do not store the signing key as a repository secret."
        )
    if _branch_names(policies) != {PUBLISH_BRANCH}:
        raise ModReleaseError(f"Environment mod-sign must allow only {PUBLISH_BRANCH}")
    rules = environment.get("protection_rules") or []
    if any(isinstance(rule, dict) and rule.get("type") == "required_reviewers" for rule in rules):
        raise ModReleaseError("Environment mod-sign must not require reviewers; promotion uses mod-release")


def require_review_gate(environment: dict, policies: dict) -> None:
    """Promotion runs only after a required reviewer on the publish branch."""

    if not isinstance(environment, dict):
        raise ModReleaseError("Environment mod-release has no required reviewers")
    policy = environment.get("deployment_branch_policy") or {}
    if policy.get("protected_branches") or not policy.get("custom_branch_policies"):
        raise ModReleaseError(f"Environment mod-release must allow only {PUBLISH_BRANCH}")
    if _branch_names(policies) != {PUBLISH_BRANCH}:
        raise ModReleaseError(f"Environment mod-release must allow only {PUBLISH_BRANCH}")
    rules = environment.get("protection_rules") or []
    confirmed = False
    for rule in rules:
        reviewers = rule.get("reviewers") if isinstance(rule, dict) else None
        if isinstance(rule, dict) and rule.get("type") == "required_reviewers" and isinstance(reviewers, list) and reviewers:
            confirmed = True
    if not confirmed:
        raise ModReleaseError(
            "Environment mod-release has no required reviewers. "
            "Add required reviewers before the first promotion."
        )


def _read_json(path: Path) -> dict:
    try:
        value = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, UnicodeError, json.JSONDecodeError) as exc:
        raise ModReleaseError(f"{path} is not valid JSON") from exc
    if not isinstance(value, dict):
        raise ModReleaseError(f"{path} must be a JSON object")
    return value


def _sha256(data: bytes) -> str:
    return hashlib.sha256(data).hexdigest()


def _safe_zip_name(name: str) -> str:
    if name.startswith("/") or "\\" in name or name.startswith("../") or "/../" in name or name == "..":
        raise ModReleaseError(f"ZIP path is unsafe: {name}")
    parts = [part for part in name.split("/") if part not in ("", ".")]
    if any(part == ".." for part in parts):
        raise ModReleaseError(f"ZIP path is unsafe: {name}")
    return "/".join(parts)


def _check_elf(blob: bytes, device: str, label: str) -> None:
    spec = DEVICES[device]
    if len(blob) < 20 or blob[:4] != b"\x7fELF":
        raise ModReleaseError(f"{label} is not an ELF binary for {device}")
    if blob[4] != spec["elf_class"] or blob[5] != 1 or blob[18] != spec["elf_machine"] or blob[19] != 0:
        raise ModReleaseError(f"architecture is not compatible: {label}")


def validate_zip(path: Path, entrypoint: str, service: dict | None, device: str) -> tuple[str, int]:
    if device not in DEVICES:
        raise ModReleaseError(f"unknown target device {device}")
    try:
        info = path.lstat()
    except OSError as exc:
        raise ModReleaseError(f"missing package {path.name}") from exc
    if not stat.S_ISREG(info.st_mode) or stat.S_ISLNK(info.st_mode):
        raise ModReleaseError(f"{path.name} must be a regular file")
    blob = path.read_bytes()
    try:
        archive = zipfile.ZipFile(path)
        bad = archive.testzip()
    except zipfile.BadZipFile as exc:
        raise ModReleaseError("ZIP is invalid") from exc
    if bad:
        raise ModReleaseError("ZIP is invalid")
    names = {}
    for entry in archive.infolist():
        if entry.flag_bits & 0x1:
            raise ModReleaseError("ZIP entry is encrypted")
        name = _safe_zip_name(entry.filename)
        if name in names:
            raise ModReleaseError(f"duplicate ZIP path {name}")
        names[name] = entry
    if entrypoint not in names or names[entrypoint].is_dir():
        raise ModReleaseError("entrypoint is missing")
    if service:
        binary = service.get("binary")
        digest = service.get("sha256")
        capability = service.get("capability")
        if not isinstance(binary, str) or binary not in names or names[binary].is_dir():
            raise ModReleaseError("native service file is invalid")
        if capability != "splash-image" or not SHA_RE.fullmatch(str(digest or "")):
            raise ModReleaseError("native service file is invalid")
        payload = archive.read(binary)
        if _sha256(payload) != digest:
            raise ModReleaseError("service binary digest mismatch")
        _check_elf(payload, device, binary)
    archive.close()
    return _sha256(blob), len(blob)


def _require_text(value, label: str, limit: int = 2000) -> str:
    if not isinstance(value, str) or not value.strip() or value != value.strip() or len(value) > limit:
        raise ModReleaseError(f"invalid {label}")
    return value


def validate_mod_document(document: dict, directory: Path, channel: str) -> dict:
    if channel not in ("development", "ready"):
        raise ModReleaseError("publication status must be development or ready")
    mod_id = document.get("id")
    version = document.get("version")
    if not isinstance(mod_id, str) or not ID_RE.fullmatch(mod_id) or directory.parent.name != mod_id:
        raise ModReleaseError("mod id does not match its directory")
    if not isinstance(version, str) or not VERSION_RE.fullmatch(version) or len(version) > 40 or directory.name != version:
        raise ModReleaseError("version does not match its directory")
    name = _require_text(document.get("name"), "name", 100)
    revision = document.get("revision", 0)
    if not isinstance(revision, int) or isinstance(revision, bool) or revision < 0:
        raise ModReleaseError("revision must be a non-negative integer")
    firmware = document.get("firmware")
    if not isinstance(firmware, list) or not firmware or any(not isinstance(item, str) or not item for item in firmware):
        raise ModReleaseError("firmware is not supported")
    entrypoint = _require_text(document.get("entrypoint"), "entrypoint", 200)
    if entrypoint.startswith("/") or ".." in entrypoint.split("/"):
        raise ModReleaseError("entrypoint is missing")
    approval = document.get("approval") or {}
    maintainer = _require_text(approval.get("maintainer"), "approval maintainer", 100)
    hardware = document.get("hardwareVerification", "not-performed")
    if hardware not in ("not-performed", "recorded-separately"):
        raise ModReleaseError("invalid hardware verification record")
    targets = document.get("targets")
    if not isinstance(targets, dict) or not targets:
        raise ModReleaseError("at least one target device is required")
    release = _read_json(directory / "release.json") if (directory / "release.json").exists() else {"assets": {}}
    assets = release.get("assets") or {}
    if not isinstance(assets, dict):
        raise ModReleaseError("release.json assets are invalid")
    normalized_targets = {}
    for device, target in targets.items():
        if device not in DEVICES or not isinstance(target, dict):
            raise ModReleaseError(f"unknown target device {device}")
        catalog_id = target.get("id", f"{mod_id}-{device}")
        if catalog_id != f"{mod_id}-{device}":
            raise ModReleaseError(f"catalog id must stay {mod_id}-{device}")
        zip_path = directory / f"{device}.zip"
        service = target.get("service", document.get("service"))
        if service is not None and not isinstance(service, dict):
            raise ModReleaseError("native service file is invalid")
        digest, size = validate_zip(zip_path, target.get("entrypoint", entrypoint), service, device)
        recorded = assets.get(device) or {}
        if recorded:
            if recorded.get("sha256") != digest or recorded.get("size") != size:
                raise ModReleaseError(f"published artifact for {mod_id} {version} {device} changed")
            if not isinstance(recorded.get("id"), int):
                raise ModReleaseError("GitHub asset id is missing")
        normalized_targets[device] = {
            "id": catalog_id,
            "description": target.get("description", document.get("description", "")),
            "entrypoint": target.get("entrypoint", entrypoint),
            "overlay": target.get("overlay", document.get("overlay", "")),
            "firmware": target.get("firmware", firmware),
            "service": service,
            "sha256": digest,
            "size": size,
            "asset": recorded,
        }
        if not isinstance(normalized_targets[device]["firmware"], list):
            raise ModReleaseError("firmware is not supported")
    approved_devices = approval.get("devices")
    if not isinstance(approved_devices, list) or not approved_devices:
        raise ModReleaseError("approval devices are missing")
    for device in normalized_targets:
        if DEVICES[device]["model"] not in approved_devices:
            raise ModReleaseError(f"approval does not include {DEVICES[device]['model']}")
    dependencies = document.get("dependencies") or []
    conflicts = document.get("conflicts") or []
    if not isinstance(dependencies, list) or not isinstance(conflicts, list):
        raise ModReleaseError("dependencies or conflicts are invalid")
    return {
        "id": mod_id,
        "name": name,
        "version": version,
        "revision": revision,
        "channel": channel,
        "hardwareVerification": hardware,
        "approval": {"maintainer": maintainer, "devices": approved_devices},
        "dependencies": dependencies,
        "conflicts": conflicts,
        "targets": normalized_targets,
        "release": release,
        "directory": directory,
    }


def iter_mod_dirs(root: Path):
    for channel in ("development", "ready"):
        base = root / "mods" / channel
        if not base.exists():
            continue
        for mod_dir in sorted(path for path in base.iterdir() if path.is_dir()):
            for version_dir in sorted(path for path in mod_dir.iterdir() if path.is_dir()):
                if (version_dir / "mod.json").is_file():
                    yield channel, version_dir


def validate_tree(root: Path) -> list[dict]:
    found = []
    seen = set()
    for channel, directory in iter_mod_dirs(root):
        document = _read_json(directory / "mod.json")
        mod = validate_mod_document(document, directory, channel)
        identity = (mod["id"], mod["version"])
        if identity in seen:
            raise ModReleaseError(f"duplicate mod {mod['id']} {mod['version']}")
        seen.add(identity)
        if channel == "ready":
            _validate_promotion(mod)
        found.append(mod)
    return found


def _validate_promotion(mod: dict) -> None:
    path = mod["directory"] / "promotion.json"
    if not path.is_file():
        raise ModReleaseError(f"{mod['id']} {mod['version']} has no administrator promotion record")
    record = _read_json(path)
    confirmed = record.get("confirmedBy")
    identity = (mod["id"], mod["version"])
    if confirmed == "migrated-existing-signed-catalog":
        if identity not in LEGACY_READY:
            raise ModReleaseError("only the already signed Prime Clock and Splash Manager releases can use migration")
    elif confirmed != "github-environment:mod-release":
        raise ModReleaseError("ready publication requires administrator confirmation")
    targets = record.get("targets") or {}
    for device, target in mod["targets"].items():
        promoted = targets.get(device) or {}
        if promoted.get("sha256") != target["sha256"] or promoted.get("assetId") != target["asset"].get("id"):
            raise ModReleaseError("promotion record does not match the artifact")


def _entry(mod: dict, device: str) -> dict:
    target = mod["targets"][device]
    spec = DEVICES[device]
    asset = target["asset"]
    if not asset or "id" not in asset:
        raise ModReleaseError(f"{mod['id']} {device} has no immutable GitHub asset id")
    url = asset_url(int(asset["id"]))
    if not ASSET_URL_RE.fullmatch(url):
        raise ModReleaseError("download URL is not supported")
    entry = {
        "id": target["id"],
        "name": mod["name"],
        "version": mod["version"],
        "revision": mod["revision"],
        "description": target["description"],
        "models": [spec["model"]],
        "firmware": list(target["firmware"]),
        "downloadUrl": url,
        "sha256": target["sha256"],
        "size": target["size"],
        "architecture": spec["architecture"],
        "abi": spec["abi"],
        "entrypoint": target["entrypoint"],
        "approval": {"maintainer": mod["approval"]["maintainer"], "devices": [spec["model"]]},
        "status": mod["channel"],
        "release": {
            "hardwareVerification": mod["hardwareVerification"],
            "artifactSha256": target["sha256"],
        },
    }
    if target["overlay"]:
        entry["overlay"] = target["overlay"]
    if target["service"]:
        entry["service"] = {
            "binary": target["service"]["binary"],
            "sha256": target["service"]["sha256"],
            "capability": target["service"]["capability"],
        }
    if mod["dependencies"]:
        entry["dependencies"] = mod["dependencies"]
    if mod["conflicts"]:
        entry["conflicts"] = mod["conflicts"]
    return entry


def _previous_status(entry: dict) -> str:
    return entry.get("status") or "ready"


def _url_change_allowed(old: str, new: str) -> bool:
    if old == new:
        return True
    return bool(LEGACY_URL_RE.fullmatch(old) and ASSET_URL_RE.fullmatch(new))


def merge_device_catalog(previous: dict | None, entries: list[dict], catalog_id: str) -> dict:
    previous_mods = []
    if previous:
        if previous.get("schemaVersion") != 2 or previous.get("catalogId") != catalog_id:
            raise ModReleaseError(f"refusing to rewrite an unrelated {catalog_id} catalog")
        previous_mods = previous.get("mods") or []
    by_id = {}
    for entry in entries:
        if entry["id"] in by_id:
            raise ModReleaseError(f"duplicate mod id {entry['id']}")
        by_id[entry["id"]] = entry
    merged = []
    seen = set()
    for old in previous_mods:
        new = by_id.get(old.get("id"))
        if new is None:
            merged.append(old)
            seen.add(old.get("id"))
            continue
        if old.get("version") == new["version"] and old.get("sha256") != new["sha256"]:
            raise ModReleaseError(f"refusing to overwrite published artifact {old.get('id')} {old.get('version')}")
        if old.get("version") == new["version"] and old.get("size") != new["size"]:
            raise ModReleaseError(f"refusing to overwrite published artifact size {old.get('id')}")
        if not _url_change_allowed(old.get("downloadUrl", ""), new["downloadUrl"]):
            if old.get("version") == new["version"]:
                raise ModReleaseError(f"download URL for {old.get('id')} is immutable")
        if _previous_status(old) == "ready" and new["status"] != "ready":
            raise ModReleaseError(f"refusing to return {old.get('id')} to development")
        if old.get("version") == new["version"] and int(old.get("revision") or 0) != int(new["revision"]):
            raise ModReleaseError(f"revision for {old.get('id')} {old.get('version')} is immutable")
        merged.append(new)
        seen.add(new["id"])
    for entry in entries:
        if entry["id"] not in seen:
            merged.append(entry)
            seen.add(entry["id"])
    if len(seen) != len(merged):
        raise ModReleaseError("duplicate catalog entries")
    merged.sort(key=lambda item: item["id"])
    libraries = previous.get("libraries") if previous else []
    return {"schemaVersion": 2, "catalogId": catalog_id, "mods": merged, "libraries": libraries or []}


def load_tree(root: Path) -> list[dict]:
    return validate_tree(root)


def build_catalogs(root: Path) -> dict[str, dict]:
    mods = validate_tree(root)
    grouped = {device: [] for device in DEVICES}
    for mod in mods:
        for device in mod["targets"]:
            grouped[device].append(_entry(mod, device))
    catalogs = {}
    for device, spec in DEVICES.items():
        path = root / "modloader" / "catalog" / f"{device}.json"
        previous = _read_json(path) if path.is_file() else None
        catalogs[device] = merge_device_catalog(previous, grouped[device], spec["catalog_id"])
    return catalogs


def catalog_bytes(document: dict) -> bytes:
    return (json.dumps(document, indent=2, sort_keys=True) + "\n").encode("utf-8")


@contextmanager
def publish_lock(root: Path):
    path = root / "modloader" / "catalog" / ".publish.lock"
    path.parent.mkdir(parents=True, exist_ok=True)
    handle = path.open("a+")
    try:
        fcntl.flock(handle.fileno(), fcntl.LOCK_EX)
        yield
    finally:
        fcntl.flock(handle.fileno(), fcntl.LOCK_UN)
        handle.close()


def sign_catalog(payload: bytes, private_key, key_id: str) -> bytes:
    signature = base64.b64encode(private_key.sign(payload)).decode("ascii")
    body = {"algorithm": "ed25519", "keyId": key_id, "signature": signature}
    return (json.dumps(body, indent=2) + "\n").encode("utf-8")


def verify_catalog(payload: bytes, signature_document: bytes, public_key_b64: str) -> None:
    from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PublicKey

    try:
        document = json.loads(signature_document)
        signature = base64.b64decode(document["signature"], validate=True)
        public = Ed25519PublicKey.from_public_bytes(base64.b64decode(public_key_b64, validate=True))
        if document.get("algorithm") != "ed25519" or not document.get("keyId"):
            raise ValueError("metadata")
        public.verify(signature, payload)
    except Exception as exc:
        raise ModReleaseError("catalog signature verification failed") from exc


def activate_files(pairs: list[tuple[Path, bytes]], verify) -> None:
    """Verify every new byte before any destination is replaced.

    A failed verification deletes the temporary files and restores any
    destination that was already replaced.
    """

    temps = []
    originals = []
    try:
        for destination, payload in pairs:
            destination.parent.mkdir(parents=True, exist_ok=True)
            temporary = destination.with_name(destination.name + ".new")
            temporary.write_bytes(payload)
            temps.append((temporary, destination, payload))
        verify([(destination, payload) for _temporary, destination, payload in temps])
        for temporary, destination, _payload in temps:
            originals.append((destination, destination.read_bytes() if destination.exists() else None))
            os.replace(temporary, destination)
            os.chmod(destination, 0o644)
        temps.clear()
    except Exception:
        for temporary, _destination, _payload in temps:
            temporary.unlink(missing_ok=True)
        for destination, previous in reversed(originals):
            if previous is None:
                destination.unlink(missing_ok=True)
            else:
                destination.write_bytes(previous)
        raise


def promote(root: Path, mod_id: str, version: str, digests: dict[str, str], run_id: str) -> Path:
    if not ID_RE.fullmatch(mod_id) or not VERSION_RE.fullmatch(version):
        raise ModReleaseError("promotion target is invalid")
    if not run_id or not re.fullmatch(r"[A-Za-z0-9._:-]{1,80}", run_id):
        raise ModReleaseError("promotion is not bound to a workflow run")
    source = root / "mods" / "development" / mod_id / version
    if not (source / "mod.json").is_file():
        raise ModReleaseError("development mod version was not found")
    mod = validate_mod_document(_read_json(source / "mod.json"), source, "development")
    if set(digests) != set(mod["targets"]):
        raise ModReleaseError("promotion must name every device artifact")
    targets = {}
    for device, expected in digests.items():
        if expected != mod["targets"][device]["sha256"]:
            raise ModReleaseError(f"{device} artifact does not match the confirmed digest")
        asset_id = mod["targets"][device]["asset"].get("id")
        if not isinstance(asset_id, int):
            raise ModReleaseError("promotion requires the already published asset id")
        targets[device] = {"sha256": expected, "assetId": asset_id}
    destination = root / "mods" / "ready" / mod_id / version
    if destination.exists():
        raise ModReleaseError("ready version already exists and is immutable")
    record = {
        "modId": mod_id,
        "version": version,
        "confirmedBy": "github-environment:mod-release",
        "workflowRun": run_id,
        "targets": targets,
    }
    destination.parent.mkdir(parents=True, exist_ok=True)
    shutil.move(str(source), str(destination))
    parent = source.parent
    if parent.is_dir() and not any(parent.iterdir()):
        parent.rmdir()
    try:
        (destination / "promotion.json").write_text(json.dumps(record, indent=2) + "\n", encoding="utf-8")
        if source.exists():
            raise ModReleaseError("development copy was not removed")
        validate_mod_document(_read_json(destination / "mod.json"), destination, "ready")
    except Exception:
        if destination.exists() and not source.exists():
            source.parent.mkdir(parents=True, exist_ok=True)
            shutil.move(str(destination), str(source))
            (source / "promotion.json").unlink(missing_ok=True)
        raise
    return destination


def ensure_asset(api, tag: str, filename: str, blob: bytes) -> dict:
    """Upload a release asset once. A different payload for the same name fails."""

    digest = _sha256(blob)
    release = api.release_by_tag(tag)
    if release is None:
        release = api.create_release(tag)
    existing = next((asset for asset in release.get("assets", []) if asset.get("name") == filename), None)
    if existing:
        if existing.get("size") != len(blob):
            raise ModReleaseError(f"release asset {filename} already exists with a different size")
        remote = api.download_asset(int(existing["id"]))
        if _sha256(remote) != digest:
            raise ModReleaseError(f"release asset {filename} already exists with a different digest")
        return {"id": int(existing["id"]), "name": filename, "sha256": digest, "size": len(blob)}
    created = api.upload_asset(int(release["id"]), filename, blob)
    if int(created["size"]) != len(blob) or int(created["id"]) < 1:
        raise ModReleaseError("GitHub asset upload was not confirmed")
    confirmed = api.download_asset(int(created["id"]))
    if _sha256(confirmed) != digest:
        raise ModReleaseError("uploaded GitHub asset digest mismatch")
    return {"id": int(created["id"]), "name": filename, "sha256": digest, "size": len(blob)}


class _RedirectCaptured(Exception):
    def __init__(self, code: int, location: str):
        self.code = code
        self.location = location


class _StopRedirect(urllib.request.HTTPRedirectHandler):
    def http_error_301(self, req, fp, code, msg, headers):
        raise _RedirectCaptured(code, headers.get("Location") or "")

    http_error_302 = http_error_303 = http_error_307 = http_error_308 = http_error_301


class GitHubAPI:
    """Small client for immutable release assets. One CDN hop, and the token stays on api.github.com."""

    def __init__(self, token: str, repo: str = REPO):
        if not token:
            raise ModReleaseError("GH_TOKEN is not configured")
        self.token = token
        self.repo = repo

    def _request(self, method: str, url: str, data: bytes | None, headers: dict, token: str | None) -> bytes:
        import urllib.error
        import urllib.request

        request_headers = {"User-Agent": "Prime-Unlimited-Mod-Release"}
        request_headers.update(headers)
        if token:
            request_headers["Authorization"] = f"Bearer {token}"
        request = urllib.request.Request(url, data=data, headers=request_headers, method=method)
        opener = urllib.request.build_opener(_StopRedirect)
        try:
            with opener.open(request, timeout=120) as response:
                if getattr(response, "status", 200) >= 300:
                    raise ModReleaseError("HTTP redirect blocked")
                return response.read()
        except _RedirectCaptured as captured:
            raise ModReleaseError("HTTP redirect blocked") from captured
        except urllib.error.HTTPError as exc:
            if exc.code == 404 and method == "GET":
                raise FileNotFoundError(url) from exc
            raise ModReleaseError(f"GitHub API request failed with HTTP {exc.code}") from exc

    def _open(self, method: str, url: str, data: bytes | None = None, headers: dict | None = None) -> bytes:
        request_headers = {"Accept": "application/vnd.github+json"}
        if headers:
            request_headers.update(headers)
        return self._request(method, url, data, request_headers, self.token)

    def release_by_tag(self, tag: str):
        try:
            return json.loads(self._open("GET", f"https://api.github.com/repos/{self.repo}/releases/tags/{tag}"))
        except FileNotFoundError:
            return None

    def create_release(self, tag: str):
        payload = json.dumps({"tag_name": tag, "name": tag, "prerelease": True, "draft": False}).encode()
        return json.loads(self._open("POST", f"https://api.github.com/repos/{self.repo}/releases", payload, {"Content-Type": "application/json"}))

    def upload_asset(self, release_id: int, filename: str, blob: bytes):
        if not re.fullmatch(r"[A-Za-z0-9._+-]+", filename):
            raise ModReleaseError("release asset name is unsafe")
        url = f"https://uploads.github.com/repos/{self.repo}/releases/{release_id}/assets?name={filename}"
        return json.loads(self._open("POST", url, blob, {"Content-Type": "application/octet-stream"}))

    def download_asset(self, asset_id: int) -> bytes:
        import urllib.error
        import urllib.request

        url = asset_url(asset_id)
        request = urllib.request.Request(
            url,
            headers={
                "Accept": "application/octet-stream",
                "Authorization": f"Bearer {self.token}",
                "User-Agent": "Prime-Unlimited-Mod-Release",
            },
            method="GET",
        )
        opener = urllib.request.build_opener(_StopRedirect)
        try:
            with opener.open(request, timeout=120) as response:
                return response.read()
        except urllib.error.HTTPError as exc:
            if exc.code == 404:
                raise FileNotFoundError(url) from exc
            raise ModReleaseError(f"GitHub API request failed with HTTP {exc.code}") from exc
        except _RedirectCaptured as captured:
            target = github_asset_redirect(url, captured.location, 1)
            follow = urllib.request.Request(
                target,
                headers={"Accept": "application/octet-stream", "User-Agent": "Prime-Unlimited-Mod-Release"},
                method="GET",
            )
            try:
                with opener.open(follow, timeout=120) as response:
                    return response.read()
            except _RedirectCaptured as extra:
                raise ModReleaseError("HTTP redirect blocked") from extra
            except urllib.error.HTTPError as exc:
                raise ModReleaseError("release asset unreachable") from exc
