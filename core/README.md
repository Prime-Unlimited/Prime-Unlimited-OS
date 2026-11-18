# Device core

The core contains immutable profiles, platform-aware read-only USB diagnostics, profile matching, Ed25519 key handling, pinned-host SSH verification, package integrity checks, strict mod manifests, and secret redaction. Fastboot discovery uses `fastboot devices`; the JP24 diagnostic reads the operating system's USB inventory and treats VID:PID `15e4:d014` only as unverified transport evidence.

Both transport writers fail closed. Enabling either one requires positive device and firmware identity, licensed pinned packages, tested backup/recovery, explicit confirmation, real progress/cancellation support, and hardware end-to-end validation.
