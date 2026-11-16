# Local development

The current prototype is **read-only**. It does not install, activate, update or delete mods and does not write device files.

From the repository root:

```sh
python3 -m unittest discover -s modloader/backend -p 'test_*.py'
python3 modloader/backend/server.py --catalog modloader/catalog/demo.json --model JC11S --firmware DEMO-ONLY --port 8098
curl http://127.0.0.1:8098/api/v1/health
curl http://127.0.0.1:8098/api/v1/mods
```

The demo entry is intentionally hosted at an invalid domain and cannot be installed.

`ui/ModLoader.qml` is an unmounted QML component; it requires an appropriate QtQuick host for preview. It is **not** wired into the device's menu or startup process.

Before deployment, require a target-verified runtime, a validated /data-only startup path, an authenticated mutation API, strict package extraction safeguards, and an atomic rollback mechanism. No other partition may be written.
