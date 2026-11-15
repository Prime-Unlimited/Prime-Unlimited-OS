# Desktop app

The PySide6 desktop wizard starts with explicit device selection and presents device-specific connection instructions. Its read-only diagnostics run outside the UI thread and stop on missing, wrong, multiple, or ambiguous devices. On Windows, JC11S diagnostics use native WinUSB, display transport milestones and the Fastboot protocol version, and expose reboot only after protocol verification. Reboot always requires a confirmation dialog and never runs during detection. JP24 remains on its separate imbupd diagnostic path.

Relevant USB candidates show actual VID/PID and discovery source alongside explicitly inferred transport/family information. Model identity, revision compatibility, and installation remain separate. Multiple candidates require an explicit selection and diagnostic rerun; unknown revisions remain blocked.

SSH setup has no skip path. The wizard can generate a passphrase-protected Ed25519 pair or validate an imported pair, keeps the private key on the computer, and clears passphrase fields after validation. For JC11S it then drives the automatic WSL image build, sparse validation, destructive-data review, separately gated WinUSB flash, reboot and host-key-pinned SSH verification. The evidence-based release gates remain false until mapping, first boot and recovery are demonstrated; a user checkbox cannot override them.

Run it from the repository root with `python -m app.main`. UI strings are English and structured so Qt translation catalogs can be introduced without changing core services.
