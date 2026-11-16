import json
import tempfile
import threading
import unittest
import urllib.error
import urllib.request
from http.server import ThreadingHTTPServer
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
        constrained = {**self.entry, "firmware": [], "firmwareConstraint": ">=5.0,<6.0"}
        self.assertTrue(compatible(constrained, "JC11S", "5.2.0"))
        self.assertFalse(compatible(constrained, "JC11S", "6.0.0"))

    def test_duplicate_ids_rejected(self):
        with self.assertRaises(ValueError):
            self.catalog([self.entry, self.entry])

    def test_path_like_id_rejected(self):
        with self.assertRaises(ValueError):
            self.catalog([{**self.entry, "id": "../escape"}])

    def test_http_handler_factory(self):
        self.assertTrue(make_handler(self.catalog([self.entry]), "JC11S", "5.0.0"))

    def request(self, server, path, data=None, token=None):
        body = json.dumps(data).encode() if data is not None else None
        headers = {"Content-Type": "application/json"}
        if token: headers["Authorization"] = "Bearer " + token
        request = urllib.request.Request("http://127.0.0.1:%d%s" % (server.server_port, path),
                                         data=body, headers=headers)
        try:
            with urllib.request.urlopen(request) as response:
                return response.status, json.load(response)
        except urllib.error.HTTPError as error:
            return error.code, json.load(error)

    def test_http_compatibility_auth_and_validation(self):
        class Manager:
            def __init__(self): self.calls = []
            def list_installed(self): return {"demo": {"version": "1.0", "enabled": False}}
            def list_libraries(self): return []
            def get_transaction_status(self, tx=None): return [] if tx is None else None
            def list_dependents(self, ident): return []
            def install(self, ident, version): self.calls.append(("install", ident, version)); return "tx"
        manager = Manager(); token = "t" * 32
        server = ThreadingHTTPServer(("127.0.0.1", 0), make_handler(
            self.catalog([self.entry]), "JC11S", "5.0.0", manager, token))
        thread = threading.Thread(target=server.serve_forever, daemon=True); thread.start()
        self.addCleanup(server.server_close); self.addCleanup(server.shutdown)
        status, health = self.request(server, "/api/v1/health")
        self.assertEqual(status, 200); self.assertEqual(health["service"], "prime-unlimited-mod-loader")
        self.assertEqual(self.request(server, "/api/v1/mods")[0], 200)
        self.assertEqual(self.request(server, "/api/v1/install", {"id": "demo"})[0], 401)
        self.assertEqual(self.request(server, "/api/v1/install", {"id": "../bad"}, token)[0], 400)
        self.assertEqual(self.request(server, "/api/v1/install", {"id": "demo", "path": "/tmp"}, token)[0], 400)
        self.assertEqual(self.request(server, "/api/v1/install", {"id": "demo"}, token),
                         (202, {"transactionId": "tx"}))
        self.assertEqual(manager.calls, [("install", "demo", "*")])

    def test_mutations_disabled_without_runtime_auth(self):
        server = ThreadingHTTPServer(("127.0.0.1", 0), make_handler(
            self.catalog([self.entry]), "JC11S", "5.0.0"))
        thread = threading.Thread(target=server.serve_forever, daemon=True); thread.start()
        self.addCleanup(server.server_close); self.addCleanup(server.shutdown)
        self.assertEqual(self.request(server, "/api/v1/install", {"id": "demo"}, "x" * 32)[0], 401)


if __name__ == "__main__":
    unittest.main()
