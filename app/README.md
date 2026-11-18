# Desktop app

The PySide6 desktop wizard starts with explicit device selection and presents device-specific connection instructions. Its read-only diagnostics run outside the UI thread, probe both Fastboot and the reported imbupd USB interface, and stop on missing, wrong, multiple, or ambiguous devices. A matching but unverified transport may proceed only to local SSH key preparation.

SSH setup has no skip path. The wizard can generate a passphrase-protected Ed25519 pair or validate an imported pair, keeps the private key on the computer, clears passphrase fields after validation, and then stops at the installation safety gate. Authenticated SSH verification and the mod dashboard stay locked until a validated bootstrap can safely be installed.

Run it from the repository root with `python -m app.main`. UI strings are English and structured so Qt translation catalogs can be introduced without changing core services.
