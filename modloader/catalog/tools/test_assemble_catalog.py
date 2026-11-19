import copy
import hashlib
import json
import tempfile
import unittest
from pathlib import Path
from unittest import mock

from assemble_catalog import ReleaseError, assemble, atomic_write, strict_json_data


class AssembleCatalogTests(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory()
        self.root = Path(self.directory.name)
        self.packages = self.root / "packages"
        self.packages.mkdir()
        self.base = {
            "schemaVersion": 2,
            "catalogId": "prime-unlimited-jc11s",
            "mods": [{"id": "other", "name": "Other", "version": "1", "models": ["JC11S"], "firmware": ["5.3.0"], "architecture": "armv7l", "abi": "gnueabihf", "downloadUrl": "https://example.invalid/other.zip", "sha256": "a" * 64, "size": 10, "entrypoint": "other/start.sh", "approval": {"maintainer": "Prime Unlimited", "devices": ["JC11S"]}, "dependencies": [{"id": "lib", "constraint": "1"}], "conflicts": ["old"], "revoked": True}],
            "libraries": [{"id": "lib", "name": "Library", "version": "1", "models": ["JC11S"], "firmware": ["5.3.0"], "architecture": "armv7l", "abi": "gnueabihf", "downloadUrl": "https://example.invalid/lib.zip", "sha256": "b" * 64, "size": 11, "approval": {"maintainer": "Prime Unlimited", "devices": ["JC11S"]}, "conflicts": ["old-lib"], "revoked": True}],
        }
        self.base_bytes = (json.dumps(self.base, separators=(",", ":")) + "\n").encode()
        self.contracts = {
            "jc11s": {"catalogId": "prime-unlimited-jc11s", "model": "JC11S", "architecture": "armv7l", "abi": "gnueabihf", "firmware": ["5.3.0"], "qtQuick": "2.12", "overlayHostAvailable": True},
            "jp24": {"catalogId": "prime-unlimited-jp24", "model": "JP24", "architecture": "aarch64", "abi": "gnu", "firmware": ["5.3.0"], "qtQuick": "2.12", "overlayHostAvailable": False},
        }
        self.manifest = {"schemaVersion": 1, "expectedBaseCatalogSha256": {"jc11s": hashlib.sha256(self.base_bytes).hexdigest()}, "targets": []}
        self.manifest["targets"] = [self.entry("jc11s", approved=True), self.entry("jp24", approved=False)]

    def tearDown(self):
        self.directory.cleanup()

    def entry(self, target, approved):
        contract = self.contracts[target]
        data = (target + " package").encode()
        filename = f"mod-{target}.zip"
        (self.packages / filename).write_bytes(data)
        return {
            "target": target, "model": contract["model"], "architecture": contract["architecture"], "abi": contract["abi"],
            "firmware": contract["firmware"], "qtQuick": contract["qtQuick"], "filename": filename,
            "downloadUrl": f"https://example.invalid/{filename}", "sha256": hashlib.sha256(data).hexdigest(), "size": len(data),
            "status": {name: approved for name in ("built", "structurallyValidated", "technicallyCompatible", "hardwareVerified", "officialCatalogApproved")},
            "catalog": {"id": f"mod-{target}", "name": "Mod", "version": "1.0.0", "revision": 1, "entrypoint": "mod/start.sh", "overlay": "mod/ui/Mod.qml", "approval": {"maintainer": "Prime Unlimited", "devices": [contract["model"]]}},
        }

    def assemble_result(self, manifest=None, contracts=None, base=None):
        return assemble(base or self.base_bytes, manifest or self.manifest, contracts or self.contracts, self.packages, "jc11s")

    def test_deterministic_preserving_merge_and_unapproved_exclusion(self):
        first = self.assemble_result()
        second = self.assemble_result()
        self.assertEqual(first, second)
        result = json.loads(first)
        self.assertEqual([item["id"] for item in result["mods"]], ["mod-jc11s", "other"])
        self.assertNotIn("mod-jp24", [item["id"] for item in result["mods"]])
        self.assertEqual(result["mods"][1], self.base["mods"][0])
        self.assertEqual(result["libraries"], self.base["libraries"])

    def test_replaces_only_same_id(self):
        original = self.entry("jc11s", approved=True)["catalog"]
        original.update({"models": ["JC11S"], "firmware": ["5.3.0"], "downloadUrl": "https://old.invalid/a", "sha256": "a" * 64, "size": 1, "architecture": "armv7l", "abi": "gnueabihf"})
        base = copy.deepcopy(self.base)
        base["mods"].append(original)
        encoded = (json.dumps(base, separators=(",", ":")) + "\n").encode()
        manifest = copy.deepcopy(self.manifest)
        manifest["targets"][0]["catalog"]["revision"] = 2
        manifest["expectedBaseCatalogSha256"]["jc11s"] = hashlib.sha256(encoded).hexdigest()
        result = json.loads(self.assemble_result(manifest=manifest, base=encoded))
        self.assertEqual(sum(item["id"] == "mod-jc11s" for item in result["mods"]), 1)

    def test_stale_base_rejected(self):
        manifest = copy.deepcopy(self.manifest); manifest["expectedBaseCatalogSha256"]["jc11s"] = "0" * 64
        with self.assertRaisesRegex(ReleaseError, "stale base"):
            self.assemble_result(manifest=manifest)

    def test_incomplete_and_duplicate_matrix_rejected(self):
        for targets in ([self.manifest["targets"][0]], [self.manifest["targets"][0], self.manifest["targets"][0]]):
            manifest = copy.deepcopy(self.manifest); manifest["targets"] = targets
            with self.assertRaises(ReleaseError): self.assemble_result(manifest=manifest)

    def test_unknown_contract_and_wrong_compatibility_rejected(self):
        manifest = copy.deepcopy(self.manifest); manifest["targets"][0]["abi"] = "wrong"
        with self.assertRaisesRegex(ReleaseError, "wrong ABI"): self.assemble_result(manifest=manifest)
        contracts = copy.deepcopy(self.contracts); contracts["new"] = contracts.pop("jp24")
        with self.assertRaises(ReleaseError): self.assemble_result(contracts=contracts)

    def test_empty_firmware_or_qt_rejected(self):
        for field in ("firmware", "qtQuick"):
            manifest = copy.deepcopy(self.manifest); manifest["targets"][0][field] = [] if field == "firmware" else None
            with self.assertRaises(ReleaseError): self.assemble_result(manifest=manifest)

    def test_wrong_package_hash_and_size_rejected(self):
        for field, value in (("sha256", "0" * 64), ("size", 999)):
            manifest = copy.deepcopy(self.manifest); manifest["targets"][0][field] = value
            with self.assertRaises(ReleaseError): self.assemble_result(manifest=manifest)

    def test_unapproved_target_cannot_claim_partial_status(self):
        manifest = copy.deepcopy(self.manifest); manifest["targets"][1]["status"]["built"] = True
        output = json.loads(self.assemble_result(manifest=manifest))
        self.assertNotIn("mod-jp24", [item["id"] for item in output["mods"]])

    def test_approved_target_requires_overlay_host(self):
        manifest = copy.deepcopy(self.manifest)
        for value in manifest["targets"][1]["status"]: manifest["targets"][1]["status"][value] = True
        with self.assertRaisesRegex(ReleaseError, "overlay host"):
            self.assemble_result(manifest=manifest)

    def test_revoked_record_cannot_be_silently_unrevoked(self):
        revoked = copy.deepcopy(self.manifest["targets"][0]["catalog"])
        revoked.update({"models": ["JC11S"], "firmware": ["5.3.0"], "downloadUrl": "https://example.invalid/revoked.zip", "sha256": "c" * 64, "size": 12, "architecture": "armv7l", "abi": "gnueabihf", "revoked": True})
        base = copy.deepcopy(self.base); base["mods"].append(revoked)
        encoded = (json.dumps(base, separators=(",", ":")) + "\n").encode()
        manifest = copy.deepcopy(self.manifest); manifest["expectedBaseCatalogSha256"]["jc11s"] = hashlib.sha256(encoded).hexdigest()
        with self.assertRaisesRegex(ReleaseError, "un-revoke"): self.assemble_result(manifest=manifest, base=encoded)

    def test_duplicate_base_ids_rejected(self):
        base = copy.deepcopy(self.base); base["mods"].append(copy.deepcopy(base["mods"][0]))
        encoded = (json.dumps(base, separators=(",", ":")) + "\n").encode()
        manifest = copy.deepcopy(self.manifest); manifest["expectedBaseCatalogSha256"]["jc11s"] = hashlib.sha256(encoded).hexdigest()
        with self.assertRaisesRegex(ReleaseError, "duplicate mod"):
            self.assemble_result(manifest=manifest, base=encoded)

    def test_unsafe_archive_paths_rejected(self):
        for field, value in (("entrypoint", "../escape/start.sh"), ("overlay", "mod/../escape.qml"), ("overlay", "mod/ui/not-qml")):
            manifest = copy.deepcopy(self.manifest)
            manifest["targets"][0]["catalog"][field] = value
            with self.subTest(field=field, value=value), self.assertRaises(ReleaseError):
                self.assemble_result(manifest=manifest)

    def test_runtime_string_and_version_limits_rejected(self):
        cases = (("name", " bad"), ("name", "x" * 101), ("name", "é" * 51), ("version", "bad version"), ("version", "x" * 41), ("description", "x" * 2001))
        for field, value in cases:
            manifest = copy.deepcopy(self.manifest)
            manifest["targets"][0]["catalog"][field] = value
            with self.subTest(field=field), self.assertRaises(ReleaseError):
                self.assemble_result(manifest=manifest)

    def test_dependency_and_conflict_contract_rejected(self):
        invalid = (
            {"dependencies": [{"id": "dep", "constraint": "1", "type": "unknown"}]},
            {"dependencies": [{"id": "dep", "constraint": "1"}, {"id": "dep", "constraint": "2"}]},
            {"dependencies": [{"id": [], "constraint": "1"}]},
            {"conflicts": ["old", "old"]},
            {"conflicts": [["old"]]},
        )
        for fields in invalid:
            manifest = copy.deepcopy(self.manifest)
            manifest["targets"][0]["catalog"].update(fields)
            with self.subTest(fields=fields), self.assertRaises(ReleaseError):
                self.assemble_result(manifest=manifest)

    def test_update_metadata_fails_closed_until_runtime_supports_it(self):
        manifest = copy.deepcopy(self.manifest)
        manifest["targets"][0]["catalog"]["update"] = {"channel": "stable"}
        with self.assertRaisesRegex(ReleaseError, "unsupported"):
            self.assemble_result(manifest=manifest)

    def test_changed_same_version_requires_revision_increase(self):
        current = self.entry("jc11s", approved=True)["catalog"]
        current.update({"models": ["JC11S"], "firmware": ["5.3.0"], "downloadUrl": "https://old.invalid/a", "sha256": "a" * 64, "size": 1, "architecture": "armv7l", "abi": "gnueabihf"})
        base = copy.deepcopy(self.base); base["mods"].append(current)
        encoded = (json.dumps(base, separators=(",", ":")) + "\n").encode()
        manifest = copy.deepcopy(self.manifest)
        manifest["expectedBaseCatalogSha256"]["jc11s"] = hashlib.sha256(encoded).hexdigest()
        with self.assertRaisesRegex(ReleaseError, "revision must increase"):
            self.assemble_result(manifest=manifest, base=encoded)

    def test_runtime_semantic_version_regressions_rejected(self):
        for current_version, replacement_version in (
            ("1.1.0", "1.0.99"),
            ("1.0.0", "1.0.0-rc1"),
            ("v2", "1.9.9"),
        ):
            with self.subTest(current=current_version, replacement=replacement_version):
                current = self.entry("jc11s", approved=True)["catalog"]
                current.update({"version": current_version, "models": ["JC11S"], "firmware": ["5.3.0"], "downloadUrl": "https://old.invalid/a", "sha256": "a" * 64, "size": 1, "architecture": "armv7l", "abi": "gnueabihf"})
                base = copy.deepcopy(self.base); base["mods"].append(current)
                encoded = (json.dumps(base, separators=(",", ":")) + "\n").encode()
                manifest = copy.deepcopy(self.manifest)
                manifest["targets"][0]["catalog"]["version"] = replacement_version
                manifest["targets"][0]["catalog"]["revision"] = 99
                manifest["expectedBaseCatalogSha256"]["jc11s"] = hashlib.sha256(encoded).hexdigest()
                with self.assertRaisesRegex(ReleaseError, "version regression"):
                    self.assemble_result(manifest=manifest, base=encoded)

    def test_runtime_equivalent_version_requires_revision_increase(self):
        current = self.entry("jc11s", approved=True)["catalog"]
        current.update({"version": "1", "models": ["JC11S"], "firmware": ["5.3.0"], "downloadUrl": "https://old.invalid/a", "sha256": "a" * 64, "size": 1, "architecture": "armv7l", "abi": "gnueabihf"})
        base = copy.deepcopy(self.base); base["mods"].append(current)
        encoded = (json.dumps(base, separators=(",", ":")) + "\n").encode()
        manifest = copy.deepcopy(self.manifest)
        manifest["targets"][0]["catalog"]["version"] = "1.0.0"
        manifest["expectedBaseCatalogSha256"]["jc11s"] = hashlib.sha256(encoded).hexdigest()
        with self.assertRaisesRegex(ReleaseError, "revision must increase"):
            self.assemble_result(manifest=manifest, base=encoded)

    def test_duplicate_json_keys_rejected(self):
        with self.assertRaisesRegex(ReleaseError, "duplicate JSON key"):
            strict_json_data('{"schemaVersion":1,"schemaVersion":2}', "fixture")

    def test_atomic_write_syncs_file_and_parent_directory(self):
        output = self.root / "nested" / "catalog.json"
        with mock.patch("assemble_catalog.os.fsync", wraps=__import__("os").fsync) as fsync:
            atomic_write(output, b"catalog\n")
        self.assertEqual(output.read_bytes(), b"catalog\n")
        self.assertEqual(fsync.call_count, 2)


if __name__ == "__main__":
    unittest.main()
