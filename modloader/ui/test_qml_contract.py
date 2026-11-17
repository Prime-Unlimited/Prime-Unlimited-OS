import unittest
from pathlib import Path


class QmlContractTests(unittest.TestCase):
    def test_catalog_and_lifecycle_api_wiring(self):
        text = Path(__file__).with_name("ModLoader.qml").read_text(encoding="utf-8")
        for endpoint in ("/api/v1/mods", "/api/v1/installed", "/api/v1/transactions", "/api/v1/safety"):
            self.assertIn(endpoint, text)
        for action in ("cache", "stage", "inspect", "install", "enable", "disable", "uninstall", "recover", "launch-set", "safe-mode/enable", "safe-mode/disable"):
            self.assertIn('"%s"' % action, text)
        self.assertIn("Authorization", text)
        self.assertIn("__PRIME_UNLIMITED_AUTH_TOKEN__", text)
        self.assertIn("not in catalog", text)
        self.assertIn("Offline", text)

    def test_install_pipeline_is_verification_ordered(self):
        text = Path(__file__).with_name("ModLoader.qml").read_text(encoding="utf-8")
        positions = [text.index('"%s"' % step, text.index("function installVerified")) for step in ("cache", "stage", "inspect", "install")]
        self.assertEqual(positions, sorted(positions))


if __name__ == "__main__":
    unittest.main()
