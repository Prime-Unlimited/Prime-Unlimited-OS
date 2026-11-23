# Live import status — 2026-11-23

## Verified on Prime 4 G2 / JP24, firmware 5.3.0

Normal authenticated preview/start admission now runs the live sequence through the trusted native broker. Engine remains running; the live path does not write SQLite directly or fall back automatically to the offline writer.

The complete Sunbeam “Outside World” test passed in 72.79 seconds: one new MP3 (13,784,495 bytes) copied to internal Denon storage, four existing files skipped, title/artist and embedded cover verified against the destination library, copied SHA-256 verified, and exactly the new track added to a generated playlist. Engine boot/PID/start identity was unchanged. The owner confirmed the track and cover were visible and the track loaded into a deck from that playlist.

Playlist: `USB Copy - MP3 2 - 2026-11-23 - fb59dc00372495c9`.
Source/copy SHA-256: `0b029c098824d985f9da294af1b34e892b6ecde02dd1c378401a59fb45606796`.

The preceding Afterlife trial exposed provisional playlist model signals: native creation succeeded but acknowledgement was uncertain. It stopped for review. A separately verified, single missing membership operation completed that job without repeating copy, registration or creation. The actor now waits for a stable exact-title native playlist identity; the fresh Sunbeam trial required no manual completion.

A subsequent normal duplicate-only import passed in 8.22 seconds: five tracks skipped, zero copied bytes, no playlist and no native commands. Track, Playlist and PlaylistEntity counts and Engine identity remained unchanged.

## Integration and safeguards

The runtime validates the selected internal library, Engine binary/profile and process identity, source/destination mount generations and available space. Kernel disk sequence numbers distinguish genuine reinsertion even when device numbers and mount IDs are reused.

The sequence holds the shared writer lease, backs up destination databases consistently, revalidates source content, copies atomically, registers through Engine, resolves actual native IDs, creates playlists, adds members and independently verifies exact membership, tags, artwork and content. Both generated and source playlists include only newly copied tracks. All-skipped plans create no playlist. Durable command reservations prevent blind replay after ambiguous outcomes.

The authenticated actor is embedded in the trusted runtime and loaded through the approved overlay mechanism. No authorization token is included in source or preview packages. Native IDs are read from the actual model, never synthesized from SQL IDs.

The mode-aware UI 0.1.4-dev.1 was subsequently installed through signed local catalog admission (stage/inspect/update/enable), using the existing device trust key. The runtime advertises the new overlay and the native actor; Engine identity remained unchanged. The source is published in draft PR #24, and preview CI passed for the live integration commit. This local test installation is not a production catalog release.

## Remaining release validation

- Exercise multi-file/source-playlist batches, filtered views and UI/context changes, source removal and interrupted API/Engine/native acknowledgements with the integrated live adapter.
- Validate other hardware separately. JC11S is a compile target only; JP24 evidence does not establish Prime 4 compatibility.

Live mode remains explicitly experimental and is not enabled by default. No production catalog release has been made. SD cards and USB source files were not modified in these trials.

## Larger mixed-source trial history

The owner confirmed normal use after declining and successful device restart. A subsequent 690-file source contains 635 MP3, 48 WAV and 7 FLAC files; all WAV/FLAC container checks passed. Preview identified 539 new files, 141 existing files and 10 source aliases. Completion and its limits are recorded below.

The first attempt stopped before session publication, with zero copied files/bytes and no native or playlist commands. An explicit bounded root review under both leases closed that failed job without replaying it. The MP3 reader was corrected to exclude validated APE trailers. The owner separately approved two independently decoded files with partial final MPEG packets, scoped to their exact bytes and source digest. The subsequent trial and its reviewed completion are recorded below.

The source database has 15 physical track records and a playlist with 372 unresolved references. The planner now reports unavailable references rather than fabricating members. Library-relative paths that resolve within the source volume are supported; escaping the volume remains forbidden.

## Completed mixed-source validation — 2026-11-23

The 690-file USB batch completed with 539 newly copied tracks (498 MP3 and 41 WAV), 151 skips and 7,583,360,670 copied bytes. All seven FLAC files already existed on the destination, so this run does not validate a new FLAC registration. Independent verification passed for source/copy SHA-256, native metadata and artwork, 12 filename-fallback titles, and exact new-only playlist membership and source order. The generated playlist contains 539 tracks; the two resolved source playlists contain 13 new tracks each. Engine process identity remained unchanged, and source USB and SD contents were not modified.

The owner confirmed that an imported tagged WAV was visible, loadable and playable from the generated playlist. The mixed batch required a reviewed continuation after the WAVE metadata reader correction: the first 32 registrations were verified and preserved; only the missing 507 registrations and playlist work were performed. No audio was recopied. This is a verified completed batch, not a fully automatic end-to-end batch pass. A fresh normal WAV import after the correction and a genuinely new FLAC import remain release-validation tasks.

## Fresh normal WAV/FLAC import — 2026-11-23

After the WAVE metadata reader correction, a fresh normal authenticated preview/start imported one new FLAC and one new WAV to internal Denon storage. The unchanged 690 other files were skipped; 9,905,526 bytes were copied and exactly one generated playlist was created with only the two new tracks. The job completed automatically without reviewed continuation, re-registration, direct SQL writes or Engine restart. Independent read-only verification passed (0.71 seconds) for both source/copy SHA-256 values, native metadata/artwork checks, exact playlist order and unchanged Engine identity.

The tagless FLAC appears as `FLAC-Testfile.flac`; the WAV contains the embedded title `Pink Noise` and artist `16 Bit 44.1 KHz`. The owner confirmed both fresh files are visible, loadable and playable from the generated playlist. Large-source preview and live revalidation reread existing content and can take several minutes; the queued live preparation currently lacks detailed durable progress. This evidence establishes normal new WAV/FLAC registration on the tested JP24 profile, not compatibility with every codec variant or other models.
