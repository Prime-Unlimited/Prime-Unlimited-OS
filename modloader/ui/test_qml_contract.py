import unittest
from pathlib import Path


class QmlContractTests(unittest.TestCase):
    def test_catalog_and_lifecycle_api_wiring(self):
        text = Path(__file__).with_name("ModLoader.qml").read_text(encoding="utf-8")
        for endpoint in ("/api/v1/mods", "/api/v1/installed", "/api/v1/transactions", "/api/v1/safety"):
            self.assertIn(endpoint, text)
        for action in ("cache", "stage", "inspect", "install", "update", "rollback", "catalog/sync", "enable", "disable", "uninstall", "safe-mode/enable", "safe-mode/disable"):
            self.assertIn('"%s"' % action, text)
        self.assertIn("Authorization", text)
        self.assertIn("__PRIME_UNLIMITED_AUTH_TOKEN__", text)
        self.assertIn("not in catalog", text)
        self.assertIn("Offline", text)

    def test_install_pipeline_is_verification_ordered(self):
        text = Path(__file__).with_name("ModLoader.qml").read_text(encoding="utf-8")
        positions = [text.index('"%s"' % step, text.index("function installVerified")) for step in ("cache", "stage", "inspect", "install")]
        self.assertEqual(positions, sorted(positions))

    def test_update_pipeline_and_developer_catalog_controls(self):
        text = Path(__file__).with_name("ModLoader.qml").read_text(encoding="utf-8")
        update = text[text.index("function updateVerified"):]
        positions = [update.index('"%s"' % step) for step in ("cache", "stage", "inspect", "update")]
        self.assertEqual(positions, sorted(positions))
        self.assertIn("developerMode", text)
        self.assertIn("deviceEligible", text)
        self.assertIn("isApprovedForDevice", text)
        self.assertIn("Developer preview / not approved", text)
        self.assertIn("SYNC CATALOG", text)
        self.assertIn("updateAvailable", text)
        self.assertIn('"/api/v1/developer-mode/qr.png"', text)
        self.assertIn('"#1f8a4c"', text)
        self.assertNotIn("developerToken", text)
        self.assertIn("longMutation ? 720000 : 30000", text)
        self.assertIn("Change timed out; checking device state", text)

    def test_independent_self_updater_contract(self):
        text = Path(__file__).with_name("ModLoader.qml").read_text(encoding="utf-8")
        self.assertIn("127.0.0.1:8099/api/v1/status", text)
        self.assertIn('updaterMutation(selfUpdate.updateAvailable ? "update" : "check", true)', text)
        self.assertIn('return selfUpdate.updateAvailable ? "UPDATE" : "CHECK UPDATES"', text)
        self.assertIn("updaterMutation", text)
        self.assertIn('updaterMutation("check", true)', text)
        self.assertIn("MODLOADER UPDATE AVAILABLE", text)
        self.assertIn('updaterMutation("update", false)', text)
        self.assertIn("restartModLoader()", text)
        self.assertIn("host.active = true", text)

    def test_publish_and_progress_contract(self):
        text = Path(__file__).with_name("ModLoader.qml").read_text(encoding="utf-8")
        self.assertIn('mutation("publish", id', text)
        self.assertIn('"/api/v1/publish-status"', text)
        self.assertIn('modelData.status === "development"', text)
        self.assertIn('"PUBLISHING..."', text)
        self.assertIn('selfUpdate.progress', text)
        self.assertIn('"Download 0%"', text)
        self.assertIn('"Installation 100%"', text)

    def test_web_access_is_only_offered_for_web_capable_services(self):
        text = Path(__file__).with_name("ModLoader.qml").read_text(encoding="utf-8")
        self.assertIn('modelData.service.capability === "splash-image"', text)
        self.assertIn('text: "WEB ACCESS"', text)

    def test_header_order_and_reduced_lifecycle_controls(self):
        text = Path(__file__).with_name("ModLoader.qml").read_text(encoding="utf-8")
        header = text[text.index("id: header") : text.index("id: device")]
        expected = [
            "PRIME UNLIMITED MODLOADER",
            'model: ["AVAILABLE", "INSTALLED", "ACTIVITY"]',
            'developerMode ? "DEV MODE',
            "selfUpdateButtonText()",
            'text: "CLOSE"',
        ]
        positions = [header.index(value) for value in expected]
        self.assertEqual(positions, sorted(positions))
        self.assertNotIn("headerVersion", header)
        device = text[text.index("id: device") : text.index("id: status")]
        self.assertIn('deviceText + "   MODLOADER v" + selfUpdate.installed.version', device)
        self.assertNotIn('text: "LAUNCH"', text)
        self.assertNotIn('text: "RECOVER"', text)
        self.assertNotIn('mutation("launch-set"', text)
        self.assertNotIn('mutation("recover"', text)

    def test_enabled_mod_is_disabled_before_uninstall(self):
        text = Path(__file__).with_name("ModLoader.qml").read_text(encoding="utf-8")
        confirmation = text[text.index("var target = pendingUninstall"):]
        disable_at = confirmation.index('mutation("disable"')
        uninstall_at = confirmation.index('mutation("uninstall"')
        self.assertLess(disable_at, uninstall_at)

    def test_overlay_host_runs_only_on_main_screen(self):
        text = Path(__file__).with_name("OverlayHost.qml").read_text(encoding="utf-8")
        self.assertIn("planckQuickView.isMainScreen", text)
        self.assertIn("model: host.mainScreen ? host.overlays : []", text)

    def test_control_center_reloads_modloader_after_close(self):
        hook = Path(__file__).parents[1] / "device" / "jc11s" / "ui-hook" / "ControlCenterTab.qml"
        text = hook.read_text(encoding="utf-8")
        self.assertIn('ModLoader.qml?"+Date.now()', text)
        self.assertIn('l.active=false;l.source=""', text)
        self.assertLessEqual(len(text.encode("utf-8")), 1156)


if __name__ == "__main__":
    unittest.main()

