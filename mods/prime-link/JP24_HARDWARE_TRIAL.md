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
`scripts/prepare_trial_catalog.py` makes this append reproducible from a
read-only copy of the active catalog and the exact built ZIP. It refuses to
overwrite an output file and does not sign or install anything.

Until those steps pass, the overlay remains build-verified only. It must not
be described as a working device-to-device link or an audio path.

## Reversible API admission trial — 2026-11-23

With the device free of running USB Copy jobs, the exact JP24 ZIP was copied
into a root-only trial directory and its SHA-256 verified on-device. A separate
transient runtime API on the normal loopback port used the locally signed
catalog and the existing device trust root; the original transient API was
stopped temporarily. The signed metadata preserved all four active catalog
records. The normal authenticated `cache`, `stage`, `inspect`, `install` and
`enable` calls all succeeded. Inspection reported `eligible:true`, `aarch64`,
`gnu`, and the declared QML overlay. Engine remained active. No matching
DeckTelemetry or QML error appeared in the checked Engine journal window.

The trial disabled and uninstalled Prime Link. The original catalog SHA-256
was unchanged, the trial package cache file was removed, and the normal API
and Engine were active after restoration. The first automated restore used
`systemctl start` on the stopped transient API, whose unit definition had
disappeared; this left the API briefly inactive. It was immediately recreated
with `systemd-run` using its original start script and verified healthy.
Future trial cleanup must recreate the transient unit rather than call
`systemctl start` on a vanished unit.

These results verify catalog admission and enable/disable lifecycle on JP24.
They do not confirm that the overlay visibly rendered, that any Planck
translator value was correct, or that deck audio can be accessed.

## Visibility diagnosis — 2026-11-23

A second one-minute enable trial produced no visible Prime Link panel, as
reported by the owner. A third trial checked `GET /api/v1/overlays` immediately
after a successful enable response and found `{"overlays":[]}`. The device's
`GET /api/v1/safety` then showed `"safeMode":true`; the persisted safety state
predated the Prime Link trial. `enabledOverlays` deliberately returns an empty
list in Safe Mode. Thus the no-panel result does not test QML rendering at all.

An interim hypothesis that the generic JP24 overlay Loader was behind Engine's
overlay layer led to one temporary hook build and an Engine restart. Once Safe
Mode was identified, that unneeded hook change was rolled back from a SHA-256
verified backup and Engine was restarted again. The original hook SHA-256,
Engine, independent SSH and normal API were verified; Safe Mode remains on.
The corresponding source edit was also reverted. A real visibility check
requires temporarily disabling Safe Mode with owner authorization, then
restoring it after the test.

## Overlay endpoint and visibility trial — 2026-11-24

The next safety check found Safe Mode already off. A temporary API attempt
under a different systemd unit name caused existing managed services, which
bind to `prime-unlimited-modloader-api.service`, to fail reconciliation with
HTTP 409. Prime Link was disabled and uninstalled through the normal API. The
test-induced service failure counts were cleared through the authenticated
recovery API after the original services were running again; no mod remained
quarantined.

The corrected trial reused the normal transient systemd unit name with the
locally signed test catalog. Cache, stage, inspect, install and enable all
succeeded. `GET /api/v1/overlays` returned the installed Prime Link QML URL
alongside the existing USB Copy overlays. Engine remained active and no Prime
Link QML load error appeared in the checked journal. The owner confirmed that
the Prime Link overlay and its BPM label were visible on the device. Whether
the BPM field showed a numeric value or `?`, and the other deck field values,
are still being clarified.

Cleanup disabled and uninstalled Prime Link, restarted the original API start
script, and verified the unchanged original catalog hash, active Engine/API,
installed USB Copy, absent Prime Link, and `{"safeMode":false,"mods":{}}`
safety state. The original JP24 UI hook remained installed during this trial.
