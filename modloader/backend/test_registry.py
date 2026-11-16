"""Synthetic, filesystem-only tests for persistent mod state."""
import hashlib
import io
import json
import tempfile
import unittest
import zipfile
from pathlib import Path

from registry import ModError, Registry, safe_extract


def archive(path, files):
    with zipfile.ZipFile(path, "w") as z:
        for name, data in files.items():
            z.writestr(name, data)
    return hashlib.sha256(path.read_bytes()).hexdigest()


class RegistryTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        self.db = Registry(self.root)
        self.pkg = self.root / "mod.zip"
        self.sha = archive(self.pkg, {"start.sh": "#!/bin/sh\nexit 0\n"})
        self.libpkg = self.root / "lib.zip"
        self.libsha = archive(self.libpkg, {"libexample.so": "synthetic"})
        self.lib = {"id": "example", "version": "1.2.0", "architecture": "arm64",
                    "abi": "gnu", "sha256": self.libsha}
        self.spec = {"id": "first", "version": "1.0.0", "approval": "approved",
                     "models": ["JC11S"], "firmware": ["DEMO-ONLY"],
                     "architecture": "arm64", "abi": "gnu",
                     "sha256": self.sha, "entrypoint": "start.sh",
                     "libraries": [self.lib]}

    def install(self, spec):
        self.db.install(spec, self.pkg, "JC11S", "DEMO-ONLY", "arm64", "gnu")

    def test_shared_library_lifecycle(self):
        key = self.db.provision_library(self.lib, self.libpkg)
        self.assertEqual(key, self.db.provision_library(self.lib, self.libpkg))
        self.install(self.spec)
        self.install({**self.spec, "id": "second"})
        self.assertEqual(self.db.libraries()[0]["consumers"], ["first", "second"])
        self.db.uninstall("first")
        self.assertEqual(self.db.garbage_collect(set()), [])
        self.db.uninstall("second")
        self.assertEqual(self.db.garbage_collect(set()), [key])

    def test_enable_persists_and_recovery(self):
        self.db.provision_library(self.lib, self.libpkg)
        self.install(self.spec)
        self.db.enable("first")
        restarted = Registry(self.root)
        self.assertTrue(restarted.installed()["first"]["enabled"])
        self.assertEqual(json.loads(restarted.startup.read_text())["enabled"][0]["id"], "first")
        restarted.enable("first", False)
        self.assertEqual(json.loads(restarted.startup.read_text())["enabled"], [])

    def test_reject_unapproved_incompatible_and_abi(self):
        with self.assertRaises(ModError):
            self.install({**self.spec, "approval": "pending"})
        with self.assertRaises(ModError):
            self.install({**self.spec, "models": ["JP24"]})
        with self.assertRaises(ModError):
            self.install({**self.spec, "abi": "musl"})

    def test_missing_dependencies_and_conflicts(self):
        with self.assertRaises(ModError):
            self.install({**self.spec, "libraries": [], "dependencies": [{"id": "missing", "version": "1.0"}]})
        with self.assertRaises(ModError):
            self.install({**self.spec, "libraries": [], "conflicts": ["first"],
                          "dependencies": [{"id": "first", "version": "1.0"}]})

    def test_unsafe_archive(self):
        bad = self.root / "bad.zip"
        archive(bad, {"../outside": "bad"})
        with self.assertRaises(ModError):
            safe_extract(bad, self.root / "out")

    def test_atomic_recovery(self):
        next_state = self.db.read()
        next_state["mods"]["recovered"] = {"version": "1", "enabled": False}
        self.db.journal.write_text(json.dumps({"next": next_state}))
        self.assertIn("recovered", Registry(self.root).installed())

    def test_no_unverified_garbage_collection(self):
        with self.assertRaises(ModError):
            self.db.garbage_collect()


if __name__ == "__main__":
    unittest.main()
