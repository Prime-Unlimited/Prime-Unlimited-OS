# Splash Manager

Custom Engine QML startup splash manager. Runs a lightweight HTTP server on
port 8081 that lets you upload a custom PNG splash screen via drag-and-drop.
Supports both 16:10 (JC11S) and 16:9 (JP24) screen models.

## How it works

The mod ships pre-compiled static binaries for both architectures:

- `splash-server-armhf` — ARM 32-bit (JC11S / Prime 4+)
- `splash-server-aarch64` — ARM 64-bit (JP24 / Prime 4 G2)

`start.sh` detects the architecture and launches the correct binary.
Access the upload UI at `http://<deck-ip>:8081`.

## Packaging

```
python3 scripts/package.py --target jc11s --output dist/
python3 scripts/package.py --target jp24 --output dist/
```

## Testing

```
pytest tests/
```
