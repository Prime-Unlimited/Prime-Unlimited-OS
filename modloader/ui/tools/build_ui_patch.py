#!/usr/bin/env python3
"""Offline, fail-closed QML patcher. Does not install or activate anything."""
import argparse
from pathlib import Path

MARKER = "// PRIME_UNLIMITED_MOD_LOADER_V1"

TILE = '''
        // PRIME_UNLIMITED_MOD_LOADER_V1
        Rectangle {
            id: puModTile
            objectName: "ControlCenter_PrimeUnlimitedModLoader"
            width: 256
            height: 104
            radius: 16
            color: Style.surface03Color
            Text {
                anchors.centerIn: parent
                text: "Prime Unlimited\\nMod Loader"
                horizontalAlignment: Text.AlignHCenter
                color: Style.font01Color
                font: Style.fontH4
            }
            MouseArea {
                anchors.fill: parent
                onClicked: root.puModLoaderOpen = true
            }
        }
'''

OVERLAY = '''
    // PRIME_UNLIMITED_MOD_LOADER_V1
    property bool puModLoaderOpen: false
    Loader {
        id: puModLoaderOverlay
        anchors.fill: parent
        z: 999999
        active: root.puModLoaderOpen
        visible: active
        source: "file:///data/prime-unlimited/ui/ModLoader.qml"
        onLoaded: {
            if (item) item.closeRequested.connect(function() { root.puModLoaderOpen = false })
        }
    }
'''


def inject(source: str, anchor: str, insertion: str, name: str) -> str:
    if MARKER in source:
        raise ValueError(f"{name}: patch already present")
    if source.count(anchor) != 1:
        raise ValueError(f"{name}: expected one anchor, found {source.count(anchor)}")
    return source.replace(anchor, anchor + insertion, 1)


def build(control: str, main: str) -> tuple[str, str]:
    if "import EngineOS.UI.Style" not in control:
        raise ValueError("Control Center: Style import missing")
    if "id: root" not in main or "AIRMainWindow" not in main:
        raise ValueError("EmbeddedMain: unsupported root")
    return (inject(control, "DayModeEnable { }", TILE, "Control Center"),
            inject(main, 'objectName: "EmbeddedMainWindow"', OVERLAY, "EmbeddedMain"))


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--control-center", type=Path, required=True)
    parser.add_argument("--embedded-main", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    control, main_qml = build(args.control_center.read_text(), args.embedded_main.read_text())
    if args.output.resolve() in (args.control_center.resolve(), args.embedded_main.resolve()):
        raise ValueError("Output must be separate from inputs")
    targets = [args.output / "ControlCenter" / "Tabs" / "ControlCenterTab.qml",
               args.output / "EmbeddedMain.qml"]
    if any(t.exists() for t in targets):
        raise FileExistsError("Refusing to overwrite an existing patch output")
    for target, content in zip(targets, (control, main_qml)):
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_text(content)
    print("Generated two QML overrides; no device changes made")


if __name__ == "__main__":
    main()
