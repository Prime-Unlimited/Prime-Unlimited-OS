# EngineX USB Import 6.0.0 reference review

This note records a read-only review of the user-provided
`usb-import-6.0.0.tar.gz`. The archive itself was not extracted into the
repository, and neither native executable was run.

## Archive contents

The archive contains `README.md`, `mod.json`, `UsbImportDialog.qml`,
`start.sh`, a license, and prebuilt aarch64/arm32 executables. It does **not**
contain the `usb-import-server.c` source named in the README's build section.
This means the core daemon can currently be assessed only through its stated
behavior and static binary strings, not source review.

## Useful design ideas

- The touchscreen asks before importing; Ignore dismisses the detected drive.
- The UI polls a loopback daemon for detected drives and bounded progress.
- The daemon checks free space, skips existing files, ignores symlinks, backs
  up the destination database, and intends to use a transaction with rollback.
- The new playlist is built using Engine's playlist linked-list fields. Static
  SQL strings show lookups for the list tail and updates to `nextEntityId` and
  `nextListId`.
- The static binary contains queries for `originDatabaseUuid` and
  `originTrackId`, matching nontrivial identity semantics in the observed
  Engine `Database2` schema.

These are design leads, not verified Prime 4/Prime 4 G2 behavior. The original
mod targets EngineX OS, replaces the stock `DriveMigrationDialog.qml`, uses a
custom daemon on port 8082, and may rely on EngineX-only paths and APIs. That
integration must not be copied into the Prime Unlimited overlay host without
separate validation.

## Static database findings

`strings` on the included aarch64 executable found these SQL templates:

- Track insertion sets `path`, `filename`, `title`, `fileBytes`, `fileType`,
  analysis/availability/import flags, dates, `originDatabaseUuid`, and
  `originTrackId`.
- Playlist insertion creates a persisted root-level playlist named `USB
  Import`; membership rows use `trackId`, `databaseUuid`, and linked-list
  pointers.
- The scanned executable strings contained no Track insert/update for artist,
  album, genre or other embedded tags, and no visible `AlbumArt` operation.

This suggests the shown binary path may register files and create a playlist
without importing the user's requested tags and cover art. String absence alone
cannot prove there is no alternative code path, so the daemon source is needed
to settle this. Neither SQL execution nor Engine behavior was observed.

## Gaps for USB Copy

- Obtain and review `usb-import-server.c` before borrowing the database writer.
- Verify the actual destination-selection logic; the README describes selecting
  the non-removable mount with the largest Engine DB, while this project requires
  a user-visible destination choice when multiple internal targets exist.
- Read ID3/other supported metadata and embedded artwork, then determine the
  Engine-compatible `Track` and `AlbumArt` writes.
- Preserve imported source playlists if required, while also creating the
  requested per-copy playlist without replacing a user's existing list.
- Confirm Engine process coordination, journal mode, backup/rollback behavior,
  refresh/restart requirements, and both target architectures on hardware.
- Replace direct mod-provided root service access with the signed, narrowly
  scoped runtime broker and trusted prompt flow described in
  `IMPORT_ARCHITECTURE.md`.

The archive's MIT license applies to that EngineX implementation. Its current
binary payloads are not being copied into the Prime Unlimited mod.

## Newer public repository review

The user also pointed to
[`Frequenzbetrieb/enginex-usb-import`](https://github.com/Frequenzbetrieb/enginex-usb-import).
Its current `main` has the previously missing `usb-import-server.c`; the file
identifies itself as daemon v8.0. This source was read through GitHub's file API
and was not built or run. The reviewed source blob was
`f8dfcbd88515dd55a6a2b17e42500ecc0b84654`.

### What the newer source adds

- The C code does parse several ID3v2 frames and ID3v1 title/artist/album/year,
  reads one APIC image, inserts artist/album/genre/year, and tries to add an
  `AlbumArt` row. It links playlist entities using `originTrackId` and
  `databaseUuid` and builds the `nextEntityId` chain.
- The QML includes a user-selected destination dialog, an optional remembered
  target, progress, a cancel button, and a restart choice.
- The code uses `BEGIN IMMEDIATE`, SQLite backup, a bounded busy timeout, and
  transaction rollback for the database phase.

These are useful design references. They do not establish safety or compatibility
with the Prime Unlimited runtime or both devices.

### Concrete defects visible in this source

- **Cancel is not implemented by the daemon.** The QML posts to
  `/api/cancel`, but the C request handler has no cancel route or cancellation
  flag. The UI therefore cannot stop a running copy.
- **Same-size conflicts are treated as identical files.** `unique_dest` accepts
  an existing file with the same byte length without comparing content. The
  copy loop then skips the write but reads tags from the USB source for the
  existing destination path. If there is no existing database row, it can
  register those tags against different audio; if there is already a row,
  `INSERT OR IGNORE` leaves that row's metadata unchanged.
- **A saturated conflict loop can overwrite.** If names through the suffix
  limit are already occupied, `unique_dest` leaves the last existing path in
  `dst`; the later `rename()` replaces that path.
- **Tag parsing is MP3/ID3-specific despite the broader audio extension list.**
  FLAC, WAV, M4A, OGG and AAC pass the extension filter, but the parser only
  searches for ID3v2/ID3v1 structures. Their native Vorbis/MP4/WAV tags are not
  read by this code path.
- **The ID3 text decoder is lossy.** UTF-16 handling copies only ASCII
  characters; ID3v2.2's 6-byte frame headers are not parsed; tag unsynchronizing
  and frame flags are ignored. Non-ASCII titles/artists and some otherwise
  valid tags can be lost or misread.
- **APIC description parsing is not encoding-aware and has no size bound.** It
  searches for a single zero byte even for UTF-16 descriptions and allocates the
  remaining frame bytes as the picture without an explicit cap.
- **The artwork hash is a custom 20-byte DJB2-derived value bound as a BLOB.**
  The inspected Engine schema declares `AlbumArt.hash` as TEXT. Whether Engine
  accepts this value is unverified; hash collisions could also reuse the wrong
  image. Artwork insert errors are not consistently escalated to transaction
  failure, so a track can be registered without its intended image.
- **Drive detection is broad.** The poller treats any directory under `/media`
  as a new drive; it does not confirm a mount or check for music before asking.
  Mount parsing does not decode `/proc/mounts` escaped spaces, and USB
  classification assumes a simple trailing-digit device name.
- **Target validation is shallow.** `/api/select-target` accepts any sanitized
  name and constructs `/media/<name>` without revalidating that it is a mounted
  non-removable Engine destination. If no non-USB Engine drive is found, the UI
  falls back to offering all Engine drives, including removable ones.
- **The preflight space check is only a fixed 100 MiB threshold.** It does not
  sum source sizes, so a larger import can fill the destination partway through.
- **The playlist is reused globally.** Every run appends into the same
  root-level `USB Import` playlist instead of making a distinct playlist for
  that copy. Some linked-list update calls are not checked before commit.
- **The integration is EngineX-specific.** It binds an unauthenticated daemon
  to loopback port 8082, polls `/media`, writes `/data/enginex/...`, kills the
  `Engine` process on restart, and the mod manifest replaces the stock
  `DriveMigrationDialog.qml`.

These findings come from static source inspection only. No binary, build, or
hardware behavior was executed, and database schema semantics that are not
explicit in the observed files remain unverified.
