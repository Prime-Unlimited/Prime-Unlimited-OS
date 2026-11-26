"""Acceptance tests for the automatic mod release pipeline."""

import hashlib
import json
import threading
import unittest
import zipfile
from pathlib import Path

from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PrivateKey

from tools.mod_release.pipeline import (
    DEVICES,
    PUBLISH_BRANCH,
    ModReleaseError,
    activate_files,
    asset_url,
    build_catalogs,
    catalog_bytes,
    ensure_asset,
    github_asset_redirect,
    promote,
    publish_lock,
    require_review_gate,
    require_sign_environment,
    sign_catalog,
    validate_tree,
    verify_catalog,
)


def _elf(device: str) -> bytes:
    spec = DEVICES[device]
    blob = bytearray(64)
    blob[0:4] = b"\x7fELF"
    blob[4] = spec["elf_class"]
    blob[5] = 1
    blob[18] = spec["elf_machine"]
    return bytes(blob)


def _zip(path: Path, files: dict[str, bytes]) -> None:
    with zipfile.ZipFile(path, "w") as archive:
        for name, payload in files.items():
            archive.writestr(name, payload)


def _mod(root: Path, channel: str, mod_id: str, version: str, devices=("jc11s",), service=False, digest_override=None):
    directory = root / "mods" / channel / mod_id / version
    directory.mkdir(parents=True)
    targets = {}
    assets = {}
    for index, device in enumerate(devices):
        binary = _elf(device)
        files = {f"{mod_id}/start.sh": b"#!/bin/sh\nexit 0\n"}
        service_doc = None
        if service:
            name = f"{mod_id}/bin/service-{device}"
            files[name] = binary
            service_doc = {
                "binary": name,
                "sha256": hashlib.sha256(binary).hexdigest(),
                "capability": "splash-image",
            }
        _zip(directory / f"{device}.zip", files)
        blob = (directory / f"{device}.zip").read_bytes()
        digest = digest_override or hashlib.sha256(blob).hexdigest()
        asset_id = 1000 + index
        assets[device] = {"id": asset_id, "name": f"{mod_id}.zip", "sha256": digest, "size": len(blob)}
        targets[device] = {"id": f"{mod_id}-{device}"}
        if service_doc:
            targets[device]["service"] = service_doc
    document = {
        "id": mod_id,
        "name": "Example",
        "version": version,
        "revision": 1,
        "description": "Example mod",
        "firmware": ["5.3.0"],
        "entrypoint": f"{mod_id}/start.sh",
        "approval": {"maintainer": "Prime Unlimited", "devices": [DEVICES[device]["model"] for device in devices]},
        "hardwareVerification": "not-performed",
        "targets": targets,
    }
    (directory / "mod.json").write_text(json.dumps(document), encoding="utf-8")
    (directory / "release.json").write_text(json.dumps({"tag": f"mod-{mod_id}-v{version}", "assets": assets}), encoding="utf-8")
    if channel == "ready":
        (directory / "promotion.json").write_text(json.dumps({
            "confirmedBy": "github-environment:mod-release",
            "workflowRun": "run-1",
            "targets": {device: {"sha256": assets[device]["sha256"], "assetId": assets[device]["id"]} for device in devices},
        }), encoding="utf-8")
    return directory


def _catalog(root: Path, device: str, mods: list[dict]) -> None:
    path = root / "modloader" / "catalog" / f"{device}.json"
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps({
        "schemaVersion": 2,
        "catalogId": DEVICES[device]["catalog_id"],
        "mods": mods,
        "libraries": [],
    }), encoding="utf-8")


class PipelineTests(unittest.TestCase):
    def test_media_import_service_is_valid_for_development_catalog(self):
        import tempfile
        with tempfile.TemporaryDirectory() as raw:
            root = Path(raw)
            directory = _mod(root, "development", "usb-copy", "0.1.0-beta.1", ("jp24",), service=True)
            document = json.loads((directory / "mod.json").read_text())
            document["targets"]["jp24"]["service"]["capability"] = "media-import"
            (directory / "mod.json").write_text(json.dumps(document))
            found = validate_tree(root)
            self.assertEqual(found[0]["targets"]["jp24"]["service"]["capability"], "media-import")

    def test_capture_meter_requires_shared_tap_sibling(self):
        import tempfile
        with tempfile.TemporaryDirectory() as raw:
            root = Path(raw)
            directory = _mod(root, "development", "prime-audio-analyzer", "0.2.0", ("jc11s",), service=True)
            document = json.loads((directory / "mod.json").read_text())
            service = document["targets"]["jc11s"]["service"]
            service["capability"] = "capture-meter"
            (directory / "mod.json").write_text(json.dumps(document))
            with self.assertRaises(ModReleaseError):
                validate_tree(root)
            binary_name = service["binary"]
            sibling = str(Path(binary_name).parent / "libprime-analyzer-tap.so")
            archive_path = directory / "jc11s.zip"
            with zipfile.ZipFile(archive_path, "a") as archive:
                archive.writestr(sibling, _elf("jc11s"))
            with self.assertRaises(ModReleaseError):
                validate_tree(root)
            with zipfile.ZipFile(archive_path, "w") as archive:
                archive.writestr("prime-audio-analyzer/start.sh", b"#!/bin/sh\nexit 0\n")
                archive.writestr(binary_name, _elf("jc11s"))
                tap = bytearray(_elf("jc11s"))
                tap[16] = 3
                archive.writestr(sibling, bytes(tap))
            blob = archive_path.read_bytes()
            release = json.loads((directory / "release.json").read_text())
            release["assets"]["jc11s"]["sha256"] = hashlib.sha256(blob).hexdigest()
            release["assets"]["jc11s"]["size"] = len(blob)
            (directory / "release.json").write_text(json.dumps(release))
            found = validate_tree(root)
            self.assertEqual(found[0]["targets"]["jc11s"]["service"]["capability"], "capture-meter")

    def test_new_development_mod_is_detected_for_both_devices(self):
        root = Path(self.id().replace(".", "-"))
        # Use a temp directory rather than the test id as a relative path.
        import tempfile
        with tempfile.TemporaryDirectory() as raw:
            root = Path(raw)
            _mod(root, "development", "demo", "1.0.0", ("jc11s", "jp24"))
            found = validate_tree(root)
            self.assertEqual([(item["id"], item["channel"]) for item in found], [("demo", "development")])
            catalogs = build_catalogs(root)
            self.assertEqual(set(catalogs), {"jc11s", "jp24"})
            self.assertEqual(catalogs["jc11s"]["mods"][0]["status"], "development")
            self.assertEqual(catalogs["jp24"]["mods"][0]["architecture"], "aarch64")
            self.assertTrue(catalogs["jc11s"]["mods"][0]["downloadUrl"].endswith("/assets/1000"))

    def test_ready_and_newer_development_release_share_stable_id(self):
        import tempfile
        with tempfile.TemporaryDirectory() as raw:
            root = Path(raw)
            _mod(root, "ready", "demo", "1.1.2", ("jc11s",))
            _mod(root, "development", "demo", "1.1.3", ("jc11s",))
            mods = build_catalogs(root)["jc11s"]["mods"]
            self.assertEqual([(item["id"], item["version"], item.get("status", "ready")) for item in mods], [
                ("demo-jc11s", "1.1.2", "ready"),
                ("demo-jc11s", "1.1.3", "development"),
            ])

    def test_ready_mod_is_visible_without_changing_identity(self):
        import tempfile
        with tempfile.TemporaryDirectory() as raw:
            root = Path(raw)
            directory = _mod(root, "development", "demo", "1.0.0", ("jc11s",))
            before = json.loads((directory / "release.json").read_text(encoding="utf-8"))
            sha = before["assets"]["jc11s"]["sha256"]
            promote(root, "demo", "1.0.0", {"jc11s": sha}, "run-42")
            promoted = validate_tree(root)[0]
            self.assertEqual(promoted["channel"], "ready")
            self.assertEqual(promoted["version"], "1.0.0")
            self.assertEqual(promoted["revision"], 1)
            self.assertEqual(promoted["targets"]["jc11s"]["sha256"], sha)
            self.assertEqual(promoted["targets"]["jc11s"]["asset"]["id"], 1000)
            self.assertFalse((root / "mods" / "development" / "demo").exists())
            entry = build_catalogs(root)["jc11s"]["mods"][0]
            self.assertNotIn("status", entry)
            self.assertNotIn("release", entry)
            self.assertEqual(entry["id"], "demo-jc11s")
            self.assertEqual(entry["sha256"], sha)

    def test_invalid_zip_wrong_hash_and_architecture_are_rejected(self):
        import tempfile
        with tempfile.TemporaryDirectory() as raw:
            root = Path(raw)
            directory = _mod(root, "development", "demo", "1.0.0", ("jc11s",), service=True)
            (directory / "jc11s.zip").write_bytes(b"not a zip")
            with self.assertRaisesRegex(ModReleaseError, "ZIP is invalid"):
                validate_tree(root)
            shutil = __import__("shutil")
            shutil.rmtree(root / "mods" / "development" / "demo")
            _mod(root, "development", "other", "1.0.0", ("jc11s",))
            release = json.loads((root / "mods" / "development" / "other" / "1.0.0" / "release.json").read_text())
            release["assets"]["jc11s"]["sha256"] = "0" * 64
            (root / "mods" / "development" / "other" / "1.0.0" / "release.json").write_text(json.dumps(release))
            with self.assertRaisesRegex(ModReleaseError, "changed"):
                validate_tree(root)
            third = root / "mods" / "development" / "arch" / "1.0.0"
            third.mkdir(parents=True)
            wrong = _elf("jp24")
            name = "arch/bin/service-jc11s"
            _zip(third / "jc11s.zip", {"arch/start.sh": b"#!/bin/sh\n", name: wrong})
            blob = (third / "jc11s.zip").read_bytes()
            document = {
                "id": "arch", "name": "Arch", "version": "1.0.0", "revision": 1,
                "firmware": ["5.3.0"], "entrypoint": "arch/start.sh",
                "approval": {"maintainer": "Prime Unlimited", "devices": ["JC11S"]},
                "targets": {"jc11s": {"service": {"binary": name, "sha256": hashlib.sha256(wrong).hexdigest(), "capability": "splash-image"}}},
            }
            (third / "mod.json").write_text(json.dumps(document))
            (third / "release.json").write_text(json.dumps({"assets": {"jc11s": {"id": 5, "sha256": hashlib.sha256(blob).hexdigest(), "size": len(blob)}}}))
            with self.assertRaisesRegex(ModReleaseError, "architecture is not compatible"):
                validate_tree(root)

    def test_signature_failure_does_not_replace_the_catalog(self):
        import tempfile
        with tempfile.TemporaryDirectory() as raw:
            root = Path(raw)
            destination = root / "catalog.json"
            destination.write_text("original\n", encoding="utf-8")
            signature = root / "catalog.sig.json"
            signature.write_text("original-sig\n", encoding="utf-8")
            private = Ed25519PrivateKey.generate()
            payload = b'{"schemaVersion":2}\n'
            signed = sign_catalog(payload, private, "test-key")
            public = private.public_key().public_bytes_raw()
            import base64
            verify_catalog(payload, signed, base64.b64encode(public).decode())
            signed_bad = signed.replace(b"ed25519", b"bogus12")

            def verify(items):
                by_name = {path.name: body for path, body in items}
                verify_catalog(by_name["catalog.json"], by_name["catalog.sig.json"], base64.b64encode(public).decode())

            with self.assertRaisesRegex(ModReleaseError, "catalog signature verification failed"):
                activate_files([(destination, payload), (signature, signed_bad)], verify)
            self.assertEqual(destination.read_text(encoding="utf-8"), "original\n")
            self.assertEqual(signature.read_text(encoding="utf-8"), "original-sig\n")

    def test_existing_mods_survive_and_parallel_updates_do_not_drop_them(self):
        import tempfile
        with tempfile.TemporaryDirectory() as raw:
            root = Path(raw)
            _catalog(root, "jc11s", [{
                "id": "kept-jc11s", "name": "Kept", "version": "1.0.0", "revision": 1,
                "models": ["JC11S"], "firmware": ["5.3.0"],
                "downloadUrl": asset_url(9), "sha256": "ab" * 32, "size": 10,
                "architecture": "armv7l", "abi": "gnueabihf", "entrypoint": "kept/start.sh",
                "approval": {"maintainer": "Prime Unlimited", "devices": ["JC11S"]},
            }])
            _catalog(root, "jp24", [])
            _mod(root, "development", "demo", "1.0.0", ("jc11s",))
            catalog = build_catalogs(root)["jc11s"]
            ids = [item["id"] for item in catalog["mods"]]
            self.assertEqual(ids, ["demo-jc11s", "kept-jc11s"])
            results = []

            def update(name):
                with publish_lock(root):
                    path = root / "counter.txt"
                    current = int(path.read_text()) if path.exists() else 0
                    path.write_text(str(current + 1))
                    results.append(name)

            threads = [threading.Thread(target=update, args=(str(index),)) for index in range(2)]
            for thread in threads:
                thread.start()
            for thread in threads:
                thread.join()
            self.assertEqual(int((root / "counter.txt").read_text()), 2)
            self.assertEqual(sorted(results), ["0", "1"])

    def test_generated_urls_are_immutable_asset_urls(self):
        self.assertEqual(
            asset_url(773325277),
            "https://api.github.com/repos/Prime-Unlimited/Prime-Unlimited-OS/releases/assets/773325277",
        )
        with self.assertRaises(ModReleaseError):
            asset_url(0)

    def test_release_asset_is_not_replaced(self):
        class Api:
            def __init__(self):
                self.blob = b"same-bytes"
                self.created = False

            def release_by_tag(self, tag):
                if not self.created:
                    return None
                return {"id": 7, "assets": [{"id": 11, "name": "demo.zip", "size": len(self.blob)}]}

            def create_release(self, tag):
                self.created = True
                return {"id": 7, "assets": []}

            def upload_asset(self, release_id, filename, blob):
                self.blob = blob
                return {"id": 11, "size": len(blob)}

            def download_asset(self, asset_id):
                return self.blob

        api = Api()
        first = ensure_asset(api, "mod-demo-v1.0.0", "demo.zip", b"same-bytes")
        second = ensure_asset(api, "mod-demo-v1.0.0", "demo.zip", b"same-bytes")
        self.assertEqual(first["id"], second["id"])
        with self.assertRaisesRegex(ModReleaseError, "different size"):
            ensure_asset(api, "mod-demo-v1.0.0", "demo.zip", b"different")

    def test_catalog_bytes_round_trip_with_signature(self):
        private = Ed25519PrivateKey.generate()
        payload = catalog_bytes({"schemaVersion": 2, "catalogId": "prime-unlimited-jc11s", "mods": [], "libraries": []})
        signature = sign_catalog(payload, private, "prime-unlimited-2026-1")
        import base64
        verify_catalog(payload, signature, base64.b64encode(private.public_key().public_bytes_raw()).decode())
        with self.assertRaisesRegex(ModReleaseError, "catalog signature verification failed"):
            verify_catalog(payload + b" ", signature, base64.b64encode(private.public_key().public_bytes_raw()).decode())

    def test_asset_redirect_allows_one_cdn_hop(self):
        origin = asset_url(773325277)
        target = github_asset_redirect(origin, "https://release-assets.githubusercontent.com/github-production-release-asset/1?token=1", 1)
        self.assertTrue(target.startswith("https://release-assets.githubusercontent.com/"))
        with self.assertRaisesRegex(ModReleaseError, "HTTP redirect blocked"):
            github_asset_redirect(origin, "https://evil.example/stolen", 1)
        with self.assertRaisesRegex(ModReleaseError, "HTTP redirect blocked"):
            github_asset_redirect(origin, "https://release-assets.githubusercontent.com/a", 2)
        with self.assertRaisesRegex(ModReleaseError, "HTTP redirect blocked"):
            github_asset_redirect(
                "https://github.com/Prime-Unlimited/Prime-Unlimited-OS/releases/download/tag/file.zip",
                "https://release-assets.githubusercontent.com/a",
                1,
            )

    def test_signing_and_promotion_environments_are_closed(self):
        policies = {"branch_policies": [{"name": PUBLISH_BRANCH, "type": "branch"}]}
        sign = {
            "deployment_branch_policy": {"protected_branches": False, "custom_branch_policies": True},
            "protection_rules": [],
        }
        require_sign_environment(sign, policies)
        release = {
            "deployment_branch_policy": {"protected_branches": False, "custom_branch_policies": True},
            "protection_rules": [{"type": "required_reviewers", "reviewers": [{"type": "User"}]}],
        }
        require_review_gate(release, policies)
        with self.assertRaisesRegex(ModReleaseError, "required reviewers"):
            require_review_gate({**release, "protection_rules": []}, policies)
        with self.assertRaisesRegex(ModReleaseError, "mod-sign"):
            require_sign_environment(release, policies)
        with self.assertRaisesRegex(ModReleaseError, PUBLISH_BRANCH):
            require_review_gate(release, {"branch_policies": [{"name": "main", "type": "branch"}]})


if __name__ == "__main__":
    unittest.main()
