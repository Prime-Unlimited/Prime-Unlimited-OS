import io
import json
import tempfile
import unittest
from pathlib import Path
from types import SimpleNamespace

from activate import activate


class ActivateTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(); self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name); (self.root / "registry").mkdir()
        self.calls = []

    def manifest(self, enabled):
        (self.root / "registry/startup.json").write_text(json.dumps({"schemaVersion": 1, "enabled": enabled}))

    def entry(self, ident):
        directory = self.root / "mods/installed" / ident / "1"
        directory.mkdir(parents=True); (directory / "start.sh").write_text("#!/bin/sh\n")
        return {"id": ident, "version": "1", "root": "mods/installed/%s/1" % ident,
                "entrypoint": "start.sh", "libraries": []}

    def runner(self, args, **kwargs):
        self.calls.append((args, kwargs)); return SimpleNamespace(returncode=1 if "bad" in args[0] else 0)

    def test_safe_mode_and_order_and_failure_isolation(self):
        entries = [self.entry("bad"), self.entry("good")]; self.manifest(entries)
        self.assertEqual(activate(self.root, True, runner=self.runner), [])
        self.assertEqual(activate(self.root, runner=self.runner), [("bad", 1), ("good", 0)])
        self.assertEqual(len(self.calls), 2)

    def test_library_paths_are_isolated(self):
        entry = self.entry("a"); (self.root / "libs/a").mkdir(parents=True)
        entry["libraries"] = ["libs/a"]; self.manifest([entry])
        activate(self.root, runner=self.runner)
        self.assertEqual(self.calls[0][1]["env"]["LD_LIBRARY_PATH"], str(self.root / "libs/a"))

    def test_invalid_entrypoint_rejected_without_running(self):
        self.manifest([{"id": "x", "root": "mods/installed/x/1", "entrypoint": "../../bad", "libraries": []}])
        activate(self.root, runner=self.runner, log=io.StringIO())
        self.assertEqual(self.calls, [])


if __name__ == "__main__":
    unittest.main()
