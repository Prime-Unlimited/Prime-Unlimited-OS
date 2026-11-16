import hashlib
import json
import shutil
import tempfile
import unittest
import zipfile
from pathlib import Path

from manager import PackageManager
from registry import ModError, _atomic


DEVICE = {"model": "SYNTH", "firmware": "1.0.0", "architecture": "arm64", "abi": "gnu"}


def package(path, files):
    with zipfile.ZipFile(path, "w") as archive:
        for name, contents in files.items():
            archive.writestr(name, contents)
    return hashlib.sha256(path.read_bytes()).hexdigest()


class ManagerTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(); self.addCleanup(self.temp.cleanup)
        self.base = Path(self.temp.name); self.root = self.base / "root"; self.sources = {}
        lib = self.base / "lib.zip"; libsha = package(lib, {"libx.so": "x"})
        a = self.base / "a.zip"; asha = package(a, {"start.sh": "#!/bin/sh\n"})
        b1 = self.base / "b1.zip"; b1sha = package(b1, {"start.sh": "#!/bin/sh\n#1"})
        b2 = self.base / "b2.zip"; b2sha = package(b2, {"start.sh": "#!/bin/sh\n#2"})
        self.sources = {"https://synthetic/lib": lib, "https://synthetic/a": a,
                        "https://synthetic/b1": b1, "https://synthetic/b2": b2}
        common = {"models": ["SYNTH"], "firmware": ["1.0.0"],
                  "architecture": "arm64", "abi": "gnu"}
        library = {**common, "id": "x", "version": "1.2.0", "name": "X",
                   "downloadUrl": "https://synthetic/lib", "sha256": libsha}
        dep = {"id": "x", "constraint": "=1.2.0"}
        self.catalog = {"schemaVersion": 2, "catalogId": "synthetic", "libraries": [library], "mods": [
            {**common, "id": "a", "name": "A", "version": "1.0.0", "entrypoint": "start.sh",
             "downloadUrl": "https://synthetic/a", "sha256": asha, "libraries": [dep]},
            {**common, "id": "b", "name": "B", "version": "1.0.0", "entrypoint": "start.sh",
             "downloadUrl": "https://synthetic/b1", "sha256": b1sha, "libraries": [dep]},
            {**common, "id": "b", "name": "B", "version": "2.0.0", "entrypoint": "start.sh",
             "downloadUrl": "https://synthetic/b2", "sha256": b2sha, "libraries": [dep]}]}

    def download(self, url, destination):
        shutil.copyfile(self.sources[url], destination)

    def manager(self, **kwargs):
        return PackageManager(self.root, self.catalog, DEVICE, self.download, trusted=True, **kwargs)

    def test_complete_shared_lifecycle_update_rollback_restart(self):
        db = self.manager(); db.install("a", "=1.0.0"); db.install("b", "=1.0.0")
        self.assertEqual(len(db.list_libraries()), 1)
        db.enable("a"); db.enable("b")
        restarted = self.manager()
        self.assertTrue(restarted.list_installed()["a"]["enabled"])
        startup = json.loads(restarted.registry.startup.read_text())
        self.assertEqual([x["id"] for x in startup["enabled"]], ["a", "b"])
        restarted.disable("a"); self.assertTrue(restarted.list_installed()["b"]["enabled"])
        restarted.uninstall("a"); self.assertEqual(restarted.list_libraries()[0]["consumers"], ["b"])
        userdata = self.root / "userdata/b/save"; userdata.parent.mkdir(parents=True); userdata.write_text("keep")
        restarted.update("b", "=2.0.0")
        self.assertEqual(restarted.list_installed()["b"]["rollback"]["version"], "1.0.0")
        restarted.rollback("b"); self.assertEqual(restarted.list_installed()["b"]["version"], "1.0.0")
        self.assertEqual(userdata.read_text(), "keep")
        restarted.uninstall("b")
        key = restarted.list_libraries()[0]["key"]
        self.assertEqual(restarted.registry.garbage_collect(set()), [key])

    def test_untrusted_invalid_checksum_and_download_failure(self):
        with self.assertRaisesRegex(ModError, "trusted catalog"):
            PackageManager(self.root, self.catalog, DEVICE, self.download).install("a")
        broken = json.loads(json.dumps(self.catalog)); broken["mods"][0]["sha256"] = "0" * 64
        with self.assertRaisesRegex(ModError, "checksum"):
            PackageManager(self.root, broken, DEVICE, self.download, trusted=True).install("a", "=1.0.0")
        def failed(url, destination):
            raise OSError("offline")
        with self.assertRaisesRegex(OSError, "offline"):
            PackageManager(self.root, self.catalog, DEVICE, failed, trusted=True).install("a", "=1.0.0")

    def test_interrupted_transaction_recovery(self):
        db = self.manager()
        scratch = self.root / "transactions/abandoned"; scratch.mkdir(parents=True)
        db.registry.transactions.mkdir(parents=True, exist_ok=True)
        _atomic(db.registry.transactions / "dead.json", {"id": "dead", "status": "running",
                "stage": "extraction", "scratch": "transactions/abandoned"})
        recovered = self.manager().get_transaction_status("dead")
        self.assertEqual(recovered["status"], "recovered"); self.assertFalse(scratch.exists())

    def test_failure_after_library_commit_restores_snapshot(self):
        def fail(stage):
            if stage == "library-installed":
                raise RuntimeError("injected")
        db = self.manager(failure_injector=fail)
        with self.assertRaisesRegex(RuntimeError, "injected"):
            db.install("a", "=1.0.0")
        self.assertEqual(db.list_installed(), {})
        self.assertEqual(db.list_libraries(), [])
        self.assertEqual([p for p in (self.root / "libs").glob("**/*") if p.is_file()], [])

    def test_failure_injection_records_stage(self):
        def inject(stage):
            if stage == "dependencies-resolved":
                raise RuntimeError("power loss")
        with self.assertRaisesRegex(RuntimeError, "power loss"):
            self.manager(failure_injector=inject).install("a", "=1.0.0")
        records = self.manager().get_transaction_status()
        self.assertEqual(records[-1]["status"], "failed")


if __name__ == "__main__":
    unittest.main()
