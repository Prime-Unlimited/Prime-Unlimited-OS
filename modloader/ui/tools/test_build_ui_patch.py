import unittest
from build_ui_patch import build

CONTROL = 'import EngineOS.UI.Style 1.0\nItem {\n DayModeEnable { }\n}\n'
MAIN = 'AIRMainWindow {\n id: root\n objectName: "EmbeddedMainWindow"\n}\n'


class PatchTests(unittest.TestCase):
    def test_generates_tile_and_overlay(self):
        control, main = build(CONTROL, MAIN)
        self.assertIn('ControlCenter_PrimeUnlimitedModLoader', control)
        self.assertIn('root.puModLoaderOpen = true', control)
        self.assertIn('file:///data/prime-unlimited/ui/ModLoader.qml', main)
        self.assertIn('closeRequested.connect', main)

    def test_duplicate_rejected(self):
        control, main = build(CONTROL, MAIN)
        with self.assertRaises(ValueError):
            build(control, main)

    def test_changed_anchor_rejected(self):
        with self.assertRaises(ValueError):
            build(CONTROL.replace('DayModeEnable { }', 'DayModeEnable {}'), MAIN)


if __name__ == '__main__':
    unittest.main()
