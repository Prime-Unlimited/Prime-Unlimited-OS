# Prime-Unlimited OS

Prime-Unlimited OS is a cross-platform desktop foundation for safely bootstrapping SSH and managing compatible mods on Denon DJ Prime 4+ and Prime 4 G2 controllers.

> **Development status:** the application is usable for guided setup and read-only diagnostics. Hardware writing is deliberately disabled. The repository does not yet contain the confirmed identities, licensed payloads, or tested recovery procedures required for production flashing.

## Implemented

- Polished PySide6 desktop shell with manual Prime 4+ (JC11S) and Prime 4 G2 (JP24) selection.
- Separate Fastboot and imbupd diagnostic paths with fail-closed writers.
- Explicit handling for absent, wrong, multiple, ambiguous, and unverified devices.
- Passphrase-protected Ed25519 generation, key-pair validation, and host-key-pinned SSH verification services.
- SHA-256 package validation and a strict mod manifest model with device/firmware compatibility and rollback declarations.
- Secret-redacting diagnostics and persistent non-sensitive window settings.
- Hardware-independent unit tests and PyInstaller configuration.

## Run locally

Python 3.12 or newer is required. Create and activate a virtual environment, then run:

```sh
python -m venv .venv
# Windows: .venv\Scripts\activate
# macOS: source .venv/bin/activate
python -m pip install -e .
python -m app.main
```

## Tests

```sh
python -m pip install -e .[dev]
pytest
```

Ordinary tests use fixtures and mocks and never require a controller.

## Packaging

Install development dependencies, then build on each target operating system:

```sh
pyinstaller packaging/prime-unlimited-os.spec
```

Sign and notarize release artifacts using project-controlled credentials. Credentials must never be committed.

## Hardware blockers

- **JC11S:** the exact positive Fastboot model identifier, independently reproduced bootstrap, redistributable payload, and proven backup/recovery procedure remain unavailable.
- **JP24:** VID:PID `15e4:d014` is only transport evidence. The imbupd protocol implementation, positive model identity, redistributable ARM64 payload, and proven backup/recovery procedure remain unavailable.
- A USB command reporting success is insufficient. Completion will require normal device operation and an authenticated, host-key-verified SSH session on port 22 (JC11S) or 2222 (JP24).

See [Bootstrap specification](docs/BOOTSTRAP_SPEC.md), [device selection](docs/DEVICE_SELECTION.md), and the [JC11S engineering record](docs/JC11S_PROCESS.md).

## Disclaimer

Unofficial community project. Not affiliated with or endorsed by Denon DJ or inMusic.
