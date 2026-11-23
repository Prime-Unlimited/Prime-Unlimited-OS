# USB Copy

## Development status — 22 November 2026

This is a new experimental mod, version **0.1.4-dev.1**. It requires the USB Copy runtime integration included in this branch; copying the QML package onto an unmodified core is insufficient. No production catalog or system update is published by this change.

- Prime 4 G2 (JP24): USB prompt, destination selection, progress, content deduplication, offline metadata/artwork import and generated playlists were exercised on hardware. Missing title tags fall back to the original filename.
- Native live operations on JP24: playlist creation, existing-track membership, and registering one new internal MP3 with ID3 title/artist and embedded cover were individually verified without restarting Engine. The new track was also added separately to the test playlist.
- Normal USB Copy admission is connected to the durable live sequence and trusted native broker behind explicit experimental runtime flags. On 2026-11-23, one new tagged MP3 with cover was automatically copied and added to a new playlist while four existing files were skipped. The owner confirmed visibility and deck loading; Engine remained running. See LIVE_IMPORT_STATUS.md for release limits.
- Prime 4 / Prime 4+ and other models: do not infer hardware validation from JP24 tests. JC11S is a preview build target only.
- **SD cards are excluded from this validation.** Do not use them for these tests. The source USB must remain unchanged.
- Interrupted native operations require review; automatic SQL repair or blind native retries are not implemented.

The experimental native path also implements WAV/FLAC container preflight; native multi-file import/playback validation is still in progress. See [LIVE_AUDIO_FORMATS.md](LIVE_AUDIO_FORMATS.md) for scope and exact-file owner approvals.

See [LIVE_IMPORT_STATUS.md](LIVE_IMPORT_STATUS.md) for verified behavior and remaining integration work. Packages built with `scripts/package.py` are unsigned preview artifacts, not an approved catalog release.

## Goal

When a USB mass-storage drive is connected, ask whether to copy its
music to an internal storage drive on the Prime 4+ (JC11S) or Prime 4 G2
(JP24). Choosing
**No** leaves the USB drive mounted and available to Engine OS as usual.
Choosing **Copy** opens a destination picker when more than one eligible
internal drive is present, then copies the selected music while preserving its
folder structure. The user can cancel before copying starts.

The copy must make imported tracks visible in Engine OS's own library, including
metadata parsed from ID3 tags and the track's cover art. The source USB must
remain unchanged. Copying must never format, erase, or unmount the source drive.
USB sticks without an Engine Library are valid sources too: scan supported audio
files, read their embedded tags and artwork, and import the copied files into
the selected internal drive's Engine library. After a successful import, create
an Engine playlist on the destination containing the tracks copied in that
operation, preserving their source folder order where available. Use a distinct
name based on the USB volume and copy date; if that name already exists, choose
a collision-safe alternative rather than replacing a user's playlist.

## Engine library integration

Engine DJ creates a per-drive SQLite library. Its documented contents include
track metadata and album-art blobs, along with performance data such as cue
points, beat grids, BPM and waveform data. The official Engine DJ 5.3.0 guide
describes adding tracks as an operation that updates both the library and the
drive. Therefore, creating an SQLite row from ID3 tags alone is not sufficient
to claim full native-library compatibility.

The user expects Engine OS to analyze copied tracks and produce its own BPM,
waveform, and other analysis data. Treat that as a hypothesis to verify on
hardware: the mod should import track metadata and cover art, then allow Engine
OS to analyze the files if its normal scanner does this automatically. It must
not fabricate or copy analysis values from an unrelated source track.

The supported implementation must use an Engine-owned import/indexing API or a
verified, version-specific database integration that safely updates the
destination library and artwork. It must protect the database from concurrent
Engine access, preserve existing library data, and recover cleanly from power
loss or a removed USB drive. Until that contract is verified on both devices,
the mod must not modify Engine database files directly.

## User flow

1. Detect a newly mounted USB mass-storage volume without
   interrupting its normal Engine OS availability.
2. Show a one-time prompt for that insertion: **Copy music to internal
   storage?** Choosing **No** closes the prompt and leaves the drive usable.
3. On **Yes**, show eligible internal destinations by display name and free
   space. Do not guess a destination when discovery is ambiguous.
4. Preview the selected destination and estimated bytes/files. Require enough
   space before starting; skip unsupported audio files with a clear summary.
5. Copy files with progress and cancellation. Keep existing destination files
   intact; report name conflicts and avoid overwriting silently.
6. Import copied audio's tags and embedded artwork into the selected Engine
   library. Verify whether Engine OS then analyzes new tracks automatically;
   report copy/import results separately from analysis completion.
7. Create a destination Engine playlist containing the successfully imported
   tracks from this copy operation. Do not include failed or unsupported files.
8. Keep the source USB available after completion. Do not eject it.

## Safety and compatibility gates

- Share one implementation between JC11S (`armv7l`, `gnueabihf`) and JP24
  (`aarch64`, `gnu`); use only target-specific metadata where needed.
- Device and Engine OS support must be verified separately for each target.
- USB detection, supported filesystem behavior, internal-volume discovery,
  Engine library import/indexing, embedded artwork, interruption recovery and
  no-copy normal use all require hardware validation.
- The existing Modloader overlay contract runs unprivileged and has no storage
  or database write capability. A new narrowly scoped trusted runtime contract
  is required before the mod can perform this task. Do not infer permission
  from the Splash Manager's `splash-image` service capability.
- Never write to a mounted SQLite library without an Engine-owned API or a
  verified coordination/locking mechanism.

## Implementation history

The following milestones describe development stages. The current live integration and hardware evidence are recorded in [LIVE_IMPORT_STATUS.md](LIVE_IMPORT_STATUS.md).

Implementation has started on `feat/usb-copy-to-internal`, based on the open
service-mod work in PR #22. The shared scanner recognizes audio files, extracts
supported embedded metadata/artwork, and resolves relative M3U/M3U8 playlists
without following symlinks. It can also read playlists from
`Engine Library/Database2/m.db` in SQLite read-only mode and reconstruct member
order from the observed linked-list schema. A read-only plan maps tracks and
playlists to the selected destination, assigns a unique import folder and
collision-safe filenames, and builds the requested new playlist. The worker
can stage all audio under a hidden folder, report progress, honor cancellation,
and publish the complete folder atomically without replacing existing files.
This copy worker is not yet connected to the trusted runtime, and it does not
write an Engine database.

The dependencies `github.com/dhowden/tag`, `golang.org/x/sys`, and
`modernc.org/sqlite` are pinned; their licenses are included. The tag reader
documents ID3v1/v2, MP4, FLAC and OGG support. No device has been modified.

The trusted Modloader runtime now detects mounted USB and removable SD/MMC
block volumes from
`/proc/self/mountinfo` and `/sys/dev/block`, then exposes short-lived insertion
events to the authenticated OverlayHost only when an approved `media-import`
service mod is enabled. The target picker lists only writable FAT/exFAT volumes
under `/media` whose sysfs path indicates SATA or NVMe; USB and MMC/SD devices
are excluded as destinations. External MMC/SD paths can trigger the source
prompt, while internal eMMC remains excluded. The inserted JP24 SD card is
reported as USB MassStorageClass (`/dev/sdb1`, `TRAN=usb`), so it follows the
USB source path. Its confirmed sysfs path is
`platform/fc800000.usb/usb3/3-1/3-1.4`, matching the USB source's controller
while using a distinct port path. JP24 hardware output confirms the internal partition resolves
through `platform/fe210000.sata/ata1`, while the USB source resolves through
`platform/fc800000.usb/usb3`; the classifier accepts that SATA platform path
and fails closed for MMC or unknown buses when selecting internal destinations.
It exposes opaque IDs, display names,
and free space, never mount paths. Declining is recorded for that mount. Selecting a destination now scans
the USB, reads Engine or M3U playlists, inspects destination playlist names
read-only, and prepares a preview of tracks, playlists, bytes, and warnings.
The preview is displayed and can be closed. The trusted broker revalidates the
source and destination and returns only counts, byte size, and bounded warnings.
Preview validation does not copy audio or change the Engine library. The UI
labels this state as a preview and states that no files have yet been copied.
The runtime API builds for the JC11S ARMv7
and JP24 ARM64 targets; JP24 mount-table classification is confirmed from the
reported sysfs paths, while JC11S still needs separate hardware validation.

See [IMPORT_ARCHITECTURE.md](IMPORT_ARCHITECTURE.md) for the trusted runtime
contract. The JP24 storage-bus inventory is complete; remaining hardware work
is documented in [HARDWARE_VALIDATION.md](HARDWARE_VALIDATION.md), especially
Engine import behavior on a cloned test library. No database writer is enabled
before that verification.
The earlier EngineX implementation is reviewed in
[ENGINE-X-REFERENCE.md](ENGINE-X-REFERENCE.md); its archive did not include the
daemon's C source, so its database-writer behavior remains partly unknown.

### Read-only reference inspection

On 2026-11-20, the attached USB volume `MP3 2` was inspected read-only. It has
`Engine Library/Database2/m.db`, separate files under `Engine Library/Artwork/`,
and SQLite journal files alongside the database. The observed `Track` table
contains tag fields (title, artist, album, genre, year, and others), file paths,
analysis state, and an `albumArtId` foreign key. `AlbumArt` stores image bytes
in a BLOB. The sample database had six track rows and six artwork rows; no
track names or tag contents were recorded here.

This confirms the user's workflow can target the actual `Database2` library
layout and associated artwork, but it does not establish a safe writer, the
precise required values for every column, or parity with Engine's own scanner
and analyzer. The presence of journal files also means a live database cannot
be edited by a separate process without a verified coordination mechanism.

The observed `m.db` schema also includes:

- `Information`: database UUID and schema-version fields.
- `PerformanceData`: per-track BLOB fields for waveform, beat grid, quick cues
  and loops. A trigger creates a row when a track is inserted.
- `Playlist`, `PlaylistEntity`, and `Smartlist`: hierarchy, ordering, track
  membership, and saved-list rules. Recursive views derive playlist parents,
  children, and paths.
- `Pack` and `ChangeLog`: pack/sync bookkeeping.
- `PreparelistEntity`: a prepared-track list.

`Track.path` is unique, `(originDatabaseUuid, originTrackId)` is also unique,
and a track references `AlbumArt` through `albumArtId`. Triggers maintain origin
identity, timestamps, and performance rows. These constraints and side effects
make ad-hoc `INSERT` statements an unsafe import strategy.

The same `Database2` folder contains `sm.db`, `stm.db`, and `hm.db`. In this
sample, `sm.db` and `stm.db` expose the main track, artwork, performance,
playlist, and smartlist table families; their exact roles are not inferred from
filenames. `hm.db` additionally contains `Historylist` and
`HistorylistEntity`. The observed SQLite `user_version` was `4194305` for all
three secondary databases and `m.db`; this value alone does not establish an
Engine OS compatibility contract. The observed `m.db` contained six Track rows
and six AlbumArt rows. No row values were inspected for this follow-up.

## References

- [Engine DJ 5.3.0 User Guide — Using External Storage Drives and Adding Tracks](https://cdn.inmusicbrands.com/engine/53/Engine%20DJ%20-%20User%20Guide%20-%20v5.3.0.pdf)
- [Engine DJ — Setting Up Your Drive](https://support.enginedj.com/support/solutions/articles/69000826536-engine-dj-setting-up-your-drive)
- [Community-documented Engine Library format](https://github.com/mixxxdj/mixxx/wiki/Engine-Library-Format)

## JP24 experimental device milestone (2026-11-21)

A fixed-volume laboratory command imported one additional MP3 copy directly
on the JP24 USB test drive while Engine was stopped, after creating a SQLite
backup. Engine was restarted through the independently supervised rehearsal.
The owner confirmed visibility, playback and automatic analysis. This is not
the production insertion-dialog flow: the laboratory command reuses known
source duration/bitrate and existing artwork, targets only the designated USB,
and lacks full recovery and general codec handling. Production runtime
operations remain limited to read-only preview pending those integrations.

## USB-to-internal milestone

On 2026-11-21 the experimental JP24 importer copied one MP3 from the designated
USB source to the internal SATA library while Engine was stopped. A fresh
backup preceded the transaction; tags were read from audio, artwork was mapped
in the destination, a new playlist was created, and Engine restarted. The
owner confirmed the requested internal-library and playback checks, including
playback after USB removal. This validates the intended direction for one MP3;
the insertion-dialog batch importer is still unfinished and is not enabled by
this laboratory command.

## Content duplicate handling under development

The worker compares actual file sizes and then SHA-256 for same-size candidates
referenced by the destination library. A byte-identical match selects an existing
destination track instead of copying audio. Repeated identical source files
within a batch are linked to the first planned copy. The experimental single-song
importer uses this result to create a playlist referencing the existing track;
it revalidates content before reuse. Filename equality alone is not a match.
The new logic has local unit coverage and builds for ARM64, but has not yet been
retested on the device or connected to the insertion-dialog preview. Unindexed
files and audio-identical files with changed tags still need separate handling;
this is not acoustic fingerprinting or a persistent hash cache.

## Local preview package

`scripts/package.py --target jp24 --output <directory>` builds the managed
lifecycle executable, overlay, target profile and bundled licenses into a
reproducible preview ZIP. `jc11s` is also buildable, but hardware support is
not inferred from that build. Adjacent metadata records archive/native SHA-256,
entrypoint and overlay paths. Artifacts are unsigned and unapproved; packaging
does not publish, alter a catalog, install or enable anything. The lifecycle
service opens no media or network listeners. The preview requires the updated
trusted runtime and OverlayHost; the laboratory import/repair executables are
excluded from the package. The normal admission path supports experimental live imports with the compatible trusted runtime; these unsigned packages alone do not enable it. Package tests verify both ELF architectures, hashes, archive
paths, no embedded runtime token and absence of laboratory executables.

## Building a preview

Use Go 1.24 or newer and Python 3 from a checkout of this branch:

```sh
(cd 'mods/USB Copy/src/worker' && go test -race ./...)
python3 'mods/USB Copy/scripts/package.py' --target jp24 --output artifacts
python3 'mods/USB Copy/scripts/package.py' --target jc11s --output artifacts
```

The `USB Copy preview` GitHub Actions workflow produces unsigned ZIP and metadata artifacts for both targets. These packages do not install the trusted runtime changes or enable native live imports. Do not sign them into a production catalog before the compatibility and release checks are complete.

## Integration validation

The publication branch preserves the newer core GitHub device-login/publication changes. The merged full ARM64 runtime suite passed on JP24; ARMv7 compiled successfully. Worker race tests and preview package content/architecture checks passed locally. This is a development snapshot; no production catalog entry is generated by the preview workflow.
