import json
import tempfile
import unittest
from pathlib import Path

from server import compatible, load_catalog, make_handler


class CatalogTests(unittest.TestCase):
    def setUp(self):
        self.entry = {
            "id": "demo", "name": "Demo", "version": "1.0",
            "models": ["JC11S"], "firmware": ["5.0.0"],
            "downloadUrl": "https://example.invalid/demo.zip",
            "sha256": "a" * 64,
        }

    def catalog(self, entries):
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / "catalog.json"
            path.write_text(json.dumps({"schemaVersion": 1, "mods": entries}), encoding="utf-8")
            return load_catalog(path)

    def test_valid_catalog(self):
        self.assertEqual(self.catalog([self.entry])["mods"][0]["id"], "demo")

    def test_exact_compatibility(self):
        self.assertTrue(compatible(self.entry, "JC11S", "5.0.0"))
        self.assertFalse(compatible(self.entry, "JC11S", "5.0.1"))
        self.assertFalse(compatible(self.entry, "UNKNOWN", "5.0.0"))

    def test_duplicate_ids_rejected(self):
        with self.assertRaises(ValueError):
            self.catalog([self.entry, self.entry])

    def test_path_like_id_rejected(self):
        with self.assertRaises(ValueError):
            self.catalog([{**self.entry, "id": "../escape"}])

    def test_http_handler_factory(self):
        self.assertTrue(make_handler(self.catalog([self.entry]), "JC11S", "5.0.0"))


if __name__ == "__main__":
    unittest.main()
