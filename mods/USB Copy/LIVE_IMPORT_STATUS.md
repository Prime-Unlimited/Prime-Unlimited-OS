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

## Remaining release validation

- Exercise multi-file/source-playlist batches, filtered views and UI/context changes, source removal and interrupted API/Engine/native acknowledgements with the integrated live adapter.
- Deliver the updated mode-aware UI through a correctly signed test package; installed test UI remains version 0.1.3. Publish the reconciled source and run preview CI.
- Validate other hardware separately. JC11S is a compile target only; JP24 evidence does not establish Prime 4 compatibility.

Live mode remains explicitly experimental and is not enabled by default. No production catalog release has been made. SD cards and USB source files were not modified in these trials.
