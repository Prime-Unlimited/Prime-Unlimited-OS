# USB Copy integration architecture

## Current Modloader boundary

The overlay host has a runtime token, while package QML is not given that token.
Managed native services run as a DynamicUser with `ProtectSystem=strict` and
`PrivateDevices`; they cannot monitor USB block devices or write to media
mounts. The service catalog now recognizes a separate `media-import`
capability. The API exposes opaque insertion events, internal destination
summaries, preview preparation and one-time decline; it does not grant general
file-copy or database-write access.

PR #22 adds the managed-service framework and remains open. This feature branch
is based on that PR's branch so its runtime prerequisite is explicit. The
OverlayHost now polls the `media-import` endpoint only when the `usb-copy`
overlay is enabled; the planned overlay bridge receives no runtime token. The
root API checks that an enabled, catalog-approved mod owns the capability
before it reports removable-media insertion events.

## Proposed trusted flow

1. A trusted runtime component observes mounted USB and SD card volumes and
   internal destinations. It reports opaque, short-lived volume IDs and
   display labels; packages never submit filesystem paths.
2. The trusted OverlayHost polls for a pending insertion only while the
   approved USB Copy overlay is active. The overlay displays the user's
   copy/no-copy question and eligible destinations. The host, not package QML,
   authenticates the response to the loopback runtime.
3. The runtime resolves the event and destination IDs against its own current
   mount table, rejects stale/unmounted IDs, checks free space, and performs a
   no-overwrite copy under the selected internal media root. It publishes
   bounded progress and cancellation state to the host.
4. A narrowly scoped Engine library importer records supported tags, artwork,
   source playlists where possible, and the generated `USB Copy - <volume> -
   <date>` playlist. It must serialize writes with Engine's own library use,
   preserve database invariants, and recover or roll back interrupted work.
5. Engine OS is allowed to perform BPM, waveform and other analysis. This
   behavior is a hardware hypothesis and must be observed before the UI claims
   analysis has completed.

The capability must bind every operation to the one enabled, signed, currently
approved USB Copy package. It must not allow caller-supplied paths, arbitrary
file writes, database SQL, or package-selected commands. Safe Mode, disable,
quarantine, catalog revocation and service failure must cancel an active job
cleanly and leave the source drive available. USB and external SD/MMC insertion
events and internal SATA/NVMe destination choices are now exposed through the
trusted host bridge.
JP24 hardware output confirms `/dev/sda1` resolves through
`platform/fe210000.sata/ata1`, while the source `/dev/sdc2` resolves through
`platform/fc800000.usb/usb3`. The classifier accepts this platform SATA shape
as well as `ataN` and `nvmeN`, and fails closed for MMC or unknown buses.
The JP24 SD slot reports `/dev/sdb1` through
`platform/fc800000.usb/usb3/3-1/3-1.4`; both music USB and SD therefore use the
same USB source classification. Other platforms' SD/MMC readers are accepted
as sources only through a separate `mmc_host/mmcN` path with N greater than 0.
Internal eMMC host 0 stays ineligible as a source and as a destination.
Selecting a target scans files and returns a preview. The authenticated host
can revalidate the mount identity and source plan and return only counts, byte
size, and bounded warnings. Preview validation does not copy audio, publish
files, or write the Engine database; tag and artwork blobs remain inside the
runtime.

## Source scanner

`src/worker` is a shared, read-only source scanner and planner. It accepts a
mounted source root, ignores symlinks, extracts relative M3U/M3U8 members only
when they remain inside the source, and reads tags/artwork through the pinned
BSD-2-Clause `github.com/dhowden/tag` library. It also opens
`Engine Library/Database2/m.db` with SQLite `mode=ro` and `query_only=ON` to
read playlist membership and order. It never modifies the source library.
That library documents ID3v1/v2, MP4, FLAC, and OGG metadata/artwork support.
It does not provide tag parsing for every Engine-supported codec;
unreadable/missing tags remain warnings and must not silently be fabricated
from filenames.

The planner allocates a distinct import folder, maps M3U and Engine playlist
members to their new destination paths, and builds a generated playlist with
the planned tracks. `CopyImportPlan` writes into a hidden staging directory,
reports byte/file progress, honors cancellation, syncs the copied data, and
publishes the whole import folder atomically with no-overwrite semantics.
Engine playlist reading is based on the observed `Database2` schema and still
needs validation against supported device versions. The scanner and preview
planner are wired to the runtime API; the atomic copy worker still is not. No
Engine database is modified. The runtime detects USB mounts, finds writable
FAT/exFAT SATA/NVMe volumes under `/media`, and provides opaque identifiers to
the prompt and destination-picker QML. Runtime job control, native Engine
library update, package workflow, and device validation remain to be built.

See [HARDWARE_VALIDATION.md](HARDWARE_VALIDATION.md) for the read-only JP24
inventory and cloned-library experiments required before implementing a
database writer.

## Persistent job status foundation

The shared worker now provides a private JobStore for path-free progress
records. Writes use a per-job OS lock, revision check, atomic rename and file/
directory sync. Progress cannot regress and terminal records cannot restart.
Completion after an Engine stop requires explicit EngineRestored confirmation.
Recovery marks abandoned jobs interrupted/recovery_required; it does not
resume commits, delete files or restore databases. Recovery must only be called
by an exclusive supervisor after proving no worker survives.

The authenticated, trusted-mod-gated GET /api/v1/media-import/jobs endpoint
returns up to twenty recent public records. POST is rejected. Creating jobs is handled by a separate, explicitly gated start endpoint; this
GET endpoint never starts work. The JobStore is connected to the shared worker
and supervisor in source, but the new chain is not yet deployed.
Worker tests, including concurrent stale writers, passed with the race detector.
Runtime authorization tests compile for ARMv7 and ARM64 but are not yet
executed on the target. This foundation has not been deployed to the device.

## Shared offline importer and supervisor foundation

The batch import implementation was moved from the laboratory CLI into the
shared worker as RunOfflineBatch. The CLI calls that same implementation.
It emits structured copied/reused/playlist results and progress callbacks.
Durable job IDs determine collision-resistant folder and backup identities.
Local fixtures test mixed copying/reuse and M3U creation without a source
Engine library, repeat imports, and cancellation both after a partial copy and
immediately before commit. SQLite changes and new audio are rolled back in
those pre-commit failure tests. An error returned by COMMIT is now treated as
uncertain: artifacts are retained for reconciliation instead of deleting files
that might already be referenced by a committed database.

SuperviseOfflineImport serializes supervisors with an OS lock, checks volume
identities through a trusted controller before and after stopping Engine, arms
independent recovery, persists worker progress, and confirms Engine recovery
before completing a job. Its controller interface now has a concrete systemd adapter in the runtime;
that new path is not yet deployed. Local fake-controller tests cover success, removed source,
rejected reruns and failed Engine recovery; failed recovery leaves the fallback
armed and records recovery_required. Worker tests pass with the race detector.
The gated experimental POST/start endpoint and QML button are now connected
in source; end-to-end device validation and post-power-loss artifact
reconciliation remain pending. No new
device writes or deployment were performed during this refactor.

## Concrete JP24 Engine controller

The runtime now implements the supervisor interface through fixed systemd
commands. It requires root JP24, verifies the independent SSH cgroup, arms an
Engine-start timer, confirms the native Engine process has stopped, and checks
native-process plus service recovery before disarming. Source and destination
mount ID, device number, canonical root, sysfs path and boot ID are compared;
eligibility is reevaluated before and after stopping Engine. Commands and unit
names cannot be supplied by package QML. The 480-second fallback delay is
intended for a worker unit limited to 420 seconds plus 45 seconds stopping;
the worker launch path now applies those limits and disables automatic service
restart to prevent replay of uncertain commits.

ARMv7 and ARM64 runtime test binaries compiled. Six selected authorization and
controller tests passed on the JP24 using simulated systemd/process functions;
they did not stop Engine or write media. This validates the tested adapter
logic on Linux, not a new real lifecycle run. The adapter is now used by the gated experimental start endpoint in source;
the new runtime and overlay have not been installed or validated end-to-end.

## Gated dialog-to-job integration

POST /api/v1/media-import/start accepts only an opaque preview event ID and an
explicit confirmEngineRestart=true. It requires trusted UI authentication, an
enabled approved media-import mod, JP24 and the opt-in runtime flag
--media-import-writes (default false). No QML paths, SQL, commands or units are
accepted. The root broker persists a private request and queued status before
launching its own executable as an independent systemd service with bounded
runtime and Restart=no. Event retries return the existing job; concurrent or
recovery-required jobs block admission. The child runs only the import job,
without HTTP/UI provisioning, and reloads signed approval at volume checks.
A digest of source paths/sizes/SHA-256 binds the worker to the preview; changes
are rejected before media writes. Safe free-space reservation includes backup,
journal, bounded cover allowance and a five-percent margin.

QML now requests explicit restart confirmation, starts the opaque job and
reloads saved progress/results after Engine returns. Terminal-result
acknowledgement is persistent and cannot alter the outcome or dismiss a running
job. Interrupted jobs remain blocked even after acknowledging their message.
The source UI flow was checked with mocked JavaScript plus eight existing QML
contract tests; it has not been rendered on the device in this new form.
Seven selected Linux runtime tests passed on JP24 for authorization and mocked
controller behavior; they did not execute a real import. Worker tests passed
with the race detector and runtime test binaries compile for both architectures.
The successful laboratory imports do not replace an end-to-end test of this new
queue/worker/UI path. The runtime flag stays off and packages remain preview-only.
