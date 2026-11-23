# JP24 hardware trial gate

Observed read-only on the owner's Prime 4 G2 (2026-11-23): `/data/O` exists,
the UI hook logged the `EmbeddedMain.qml` overlay injection, and the Modloader
runtime API is active. This verifies the generic host path, not this overlay.
The device has a global IPv6 address; IPv6 is enabled. Public reachability
from another network has not been tested. `ffmpeg`, GStreamer, and an Opus
shared library were not found in the checked executable/library paths.

The JP24 catalog identity is `JP24`, `aarch64`, `gnu`, firmware `5.3.0`.
The package profile now matches that runtime ABI. The separate JC11S package
remains unverified on this hardware.

Before a live overlay trial, prepare an immutable package URL and a signed
test catalog record with the exact package SHA-256 and size. The signed record
must declare `prime-link/start.sh` and `prime-link/ui/DeckTelemetry.qml` and
must use a separate `prime-link-jp24` ID. Preserve all currently installed
catalog entries, including USB Copy; do not replace the active beta runtime or
trust root. Install disabled, inspect, enable briefly, observe the deck values
and Engine/physical controls, disable, and remove the test package. Record
screen evidence and the raw translator types/units before interpreting
position, length or play-state values.

On 2026-11-23, a local trial catalog was prepared by appending Prime Link to
the device's four active mod records. Its detached signature was independently
verified against the device's public trust key. The package was also built and
validated (JP24: 1763 bytes, SHA-256
`a125178120f237bb5eda0b005e84576470eb3d76300b2349602bc24ebf869536`).
These are temporary local artifacts, not published catalog or release assets.
The active Modloader startup script configures online catalog sync, which can
replace a locally staged test catalog during runtime restart. A live trial
therefore needs a controlled catalog-source change or an immutable published
test catalog, with the current catalog/start script restored afterward. The
running device was not changed during this preparation.

Until those steps pass, the overlay remains build-verified only. It must not
be described as a working device-to-device link or an audio path.
