import unittest
from pathlib import Path


class QmlContractTests(unittest.TestCase):
    def test_catalog_and_lifecycle_api_wiring(self):
        text = Path(__file__).with_name("ModLoader.qml").read_text(encoding="utf-8")
        for endpoint in ("/api/v1/mods", "/api/v1/installed", "/api/v1/transactions"):
            self.assertIn(endpoint, text)
        for action in ("install", "enable", "disable"):
            self.assertIn('"%s"' % action, text)
        self.assertIn("Authorization", text)
        self.assertIn("Offline", text)


if __name__ == "__main__":
    unittest.main()
