# Device core

The core contains immutable profiles, read-only USB diagnostics, profile matching, Ed25519 key handling, pinned-host SSH verification, package integrity checks, strict mod manifests, and secret redaction.

Both transport writers fail closed. Enabling either one requires positive device and firmware identity, licensed pinned packages, tested backup/recovery, explicit confirmation, real progress/cancellation support, and hardware end-to-end validation.
