# Offline Control Center integration

The standalone patcher generates two QML resource overrides from verified,
firmware-matched input QML files: a Control Center tile inserted immediately
after the existing DayModeEnable component, and a lazy-loaded overlay in
EmbeddedMain. The overlay points to
`file:///data/prime-unlimited/ui/ModLoader.qml`.

Run on a development machine:

```sh
python3 modloader/ui/tools/build_ui_patch.py \
  --control-center /path/to/ControlCenterTab.qml \
  --embedded-main /path/to/EmbeddedMain.qml \
  --output /path/to/empty/output
python3 -m unittest discover -s modloader/ui/tools -p 'test_*.py'
```

This is **offline-only**. It neither activates a resource override nor changes
the Engine launch environment. Its output must be tested against the exact
device firmware, including QML scope, file URL permissions, resource registration
and Control Center geometry. Unknown anchors and duplicate patches fail closed.

No installer, SSH hook, firmware partitions, system services or boot scripts
are changed. Persistent runtime artifacts must remain under /data. A safe
Engine-process activation path has not yet been verified.
