# USB Copy hardware validation plan

## JP24 inventory result (2026-11-21)

The read-only terminal inventory reports an AArch64 Prime 4 G2 running the
inMusic `az04` base image (`5.0.19`). Engine is process 922, running as root.
The mounted `/media` volume `/media/Denon` on `/dev/sda1` (exFAT) is a 953.9 GB
SATA drive and the internal destination. The removable USB source is
`/media/MP3 2` on `/dev/sdc2` (FAT), a 29.1 GB volume. With an SD card
inserted, the slot appears as `/dev/sdb1`, a 477.5 GB exFAT USB
MassStorageClass partition mounted at `/media/SD Card`; before insertion its
parent `/dev/sdb` reported 0 bytes and was not mounted. The internal drive's
`Engine Library/Database2/m.db` is about 87 MB, with `sm.db`, `stm.db`, and
`hm.db` alongside it; the `m.db-journal` exists but is currently empty.
The exact resolved sysfs paths confirm the runtime classifier inputs:
`/sys/devices/platform/fe210000.sata/ata1/host1/target1:0:0/1:0:0:0/block/sda/sda1`
for the internal partition and
`/sys/devices/platform/fc800000.usb/usb3/3-1/3-1.3/3-1.3:1.0/host2/target2:0:0/2:0:0:0/block/sdc/sdc2`
for the music USB and
`/sys/devices/platform/fc800000.usb/usb3/3-1/3-1.4/3-1.4:1.0/host0/target0:0:0/0:0:0:0/block/sdb/sdb1`
for the SD card. The runtime classifies the first as SATA internal and both
removable devices as USB sources. This confirms the SD slot presents cards as
USB mass storage on a separate USB port path. Regression cases cover
USB, candidate external MMC, internal eMMC, SATA, NVMe, and unknown buses. MMC
source acceptance requires `mmc_host/mmcN` with N greater than 0; host 0 stays
internal due to the built-in eMMC.
The device reports both `sqlite3: not found` and `python3: not found`; do not
install utilities on the player for schema inspection.

The separately Mac-mounted `/Volumes/MP3 2` library was inspected read-only:
its `m.db` reports `user_version=4194305`, journal mode `delete`, six Track
rows, six AlbumArt rows, 2,367 Playlist rows, and 68,812 PlaylistEntity rows.
Its size is about 9.3 MB, unlike the 87 MB database on the JP24, so these
results describe only the mounted sample and must not be assumed to describe
the device's `/media/Denon` database.

The root session reports `engine.service` active and running, with process 922
with an empty `User=` field (systemd's default user applies). `/proc/922/exe`
resolves to `/usr/bin/busybox.nosuid`, so the process name `engine` is not the
Engine executable itself and may be a wrapper/applet. `systemctl show` reports
`ExecStart=/usr/Engine/Scripts/runengine`; that process (PID 885) exited with
status 0 after startup at 2026-11-20 20:52:18 UTC, while PID 922 remained. The
read-only launcher inspection shows this path is the Prime Unlimited wrapper:
it sets `LD_PRELOAD=/data/prime-unlimited/lib/libprime-ui-hook.so` and execs
`/data/prime-unlimited/bin/runengine.real`. The device copy of `runengine.real`
sets `MALLOC_ARENA_MAX=1`, runs `setup-prerequisites.sh`, and backgrounds
`/usr/Engine/Scripts/engine` while recording its PID in `/tmp/engine.pid`. The
device copy of `engine` is a shell loop that launches `/usr/Engine/Engine -d0`
with the UI-hook preload. Thus PID 922 is the persistent launcher, while the
native Engine child PID and its local socket/API state still need read-only
confirmation.
The filtered service list showed no other Engine/library/media
service; the filtered systemd socket list was empty; the process list showed
only `engine`. This does not rule out an in-process API or a socket with an
unrelated name.

The host SSH agent is not accessible to the Codex process, and desktop control
cannot operate the Terminal app in this session. Root access remains available
in the user's already authenticated terminal. Any further on-device capture
must be run there; the key passphrase should only be entered into that terminal.

## Current decision

Live process inspection identifies native Engine as `EMain`, PID 944, with
`/proc/944/exe` resolving to `/usr/Engine/Engine`; PID 922 is its shell launcher.
Its open descriptors include `m.db`, `sm.db`, `stm.db`, `hm.db` and journal
files on the internal SATA drive, music USB and SD card. Multiple descriptors
reference the same `m.db`. A separate application library under
`/data/AppDataUser/AIR Music Technology/Engine/Engine Library/Database2` is also
open, including `fsm.db`, `itm.db`, `trm.db`, and `rbm.db`. Their roles remain
unknown. Open descriptors demonstrate concurrent Engine access, not active
transactions or a particular lock state. Five matched Unix sockets were
connected and unnamed; this provides no callable import endpoint.

Do not write to `m.db` or leave imported audio files in `Engine Library/Music`
until Engine OS import behavior and database coordination are verified. The
current runtime preview is read-only and does not copy audio files.

## First pass: JP24 read-only inventory

Use the already established authenticated root SSH access on the Prime 4 G2.
Record the exact model/firmware, mount table, Engine process and database file
locations. Use read-only commands only. Do not open SQLite in write mode, stop
Engine, remount drives, or launch an import job.

Capture with personal track names and tags redacted:

- `uname -a`, `/etc/os-release`, and Engine OS version
- `/proc/self/mountinfo` and `lsblk -o NAME,TYPE,FSTYPE,RO,MOUNTPOINTS`
- Engine process command line and effective user
- top-level names under `/media` and the Engine Library directory layout
- metadata for `m.db`, `sm.db`, `stm.db`, `hm.db`, WAL/journal files: owner,
  mode, size, and modification time
- SQLite `PRAGMA query_only=ON`, `PRAGMA journal_mode`, `PRAGMA user_version`,
  table/trigger definitions, and aggregate counts only
- whether an Engine-owned import/indexing command, local socket, or library
  service exists; inspect help/metadata without invoking mutations

Do not collect media filenames, tag contents, artwork blobs, secrets, or SSH
private keys in the report.

### Copyable read-only inventory

If you are already at a `root@...` prompt on the JP24, paste this block there.
It only reads system state and database file metadata; it does not show song
names, tags, or cover art:

```sh
echo '== identity =='; uname -a; cat /etc/os-release
echo '== mounts =='; cat /proc/self/mountinfo
echo '== block devices =='; lsblk -o NAME,TYPE,FSTYPE,RO,MOUNTPOINTS
echo '== Engine process =='; ps -eo pid,user,comm | grep -i Engine
echo '== library database metadata =='
find /media -type f \( -name m.db -o -name sm.db -o -name stm.db -o -name hm.db -o -name 'm.db-*' \) -exec stat -c '%n|%U|%G|%a|%s|%Y' {} \; 2>/dev/null
```

Kopiere danach die Ausgabe hierher. Falls eine Zeile `command not found`
meldet, lass sie einfach weg. Tracknamen oder Tags musst du nicht senden.

Wenn du **noch nicht** an der `root@...`-Eingabe bist: Im Terminal zuerst
Use your own SSH helper and device address in a local terminal, unlock the device key there, then run the diagnostic block at the device root prompt.

The device does not include a local SQLite or Python CLI. Do not install tools
on the player. The schema inspection must instead run on a read-only copy of
the database on the host. Do not copy the full database off the device until
the user confirms that local transfer is acceptable; the database contains
private library metadata. Redact IP addresses and media labels before sharing
any output.

### Engine API discovery without invoking it

Limit discovery to service/process metadata and executable names. Do not call
an undocumented endpoint or run an Engine binary with guessed import flags.
The service and socket inventory above is complete. Next inspect the Engine
unit metadata and executable path without running the binary. If a
candidate import command appears, record its path and file metadata only; its
usage/help should be reviewed separately before execution.

## Controlled behavior experiment

Only with a backed-up test drive and a disposable audio file:

1. With Engine running, use Engine's own UI to import/add that file to the
   internal library. Compare filesystem and database metadata before/after.
2. Record whether ID3 tags and embedded cover appear, whether a playlist is
   created, and whether BPM/waveform analysis follows automatically.
3. Repeat on a cloned/test library with Engine closed to identify which
   database and artwork files Engine changes. Never test against the only copy.
4. Reboot and reopen Engine, then verify library entry, cover, playlist,
   waveform/BPM and playback.
5. Repeat separately on JC11S. A JP24 result does not imply JC11S behavior.

## Go/no-go for a native writer

Proceed only if Engine exposes a supported import/indexing API, or a
version-bound offline writer is validated against a clone. Validation must
cover `Track`, `AlbumArt`, `PerformanceData`, required secondary databases,
playlist linked-list fields, constraints, and trigger effects; Engine must
reopen and analyze the result safely.

The writer also needs an Engine coordination/lock mechanism, no-overwrite file
publication, rollback/recovery after power loss, and tested Engine OS versions
for both models. If Engine remains live and no API or lock exists, stop at the
preview stage.

## Confirmed JP24 playlist experiment (2026-11-21)

On the designated MP3 2 USB test drive, a new laboratory playlist was created
while the drive was on the host and Engine DJ Desktop was closed. With local
Information.uuid/Track.id references, the device showed the playlist but
reported NoTracks Found. After changing its ten membership references to the
tracks' originDatabaseUuid/originTrackId, the owner confirmed all ten tracks
were visible and loadable. membershipReference remained zero. Original and
pre-correction databases were backed up locally before writes.

This confirms membership resolution for existing exported tracks and the
observed playlist ordering structure on this JP24. It does not validate new
Track insertion, generated artwork on the device, automatic BPM analysis,
secondary database coordination, JC11S compatibility, or live database writes.
The production importer remains disabled pending those checks.

## Confirmed JP24 new-track experiment (2026-11-21)

The designated USB test drive received one additional audio copy, a new Track
row and a new playlist while attached to the host. Tags were parsed from the
song. Duration and bitrate were checked using ffprobe against the same audio's
Engine Desktop export. Track insertion created an empty PerformanceData row;
isAnalyzed was zero and no BPM, waveform or beat-grid blobs were copied.
The owner confirmed on the Prime: correct tags and cover, loading and playback,
and generation of BPM and waveform. This establishes that Engine analyzes this
new MP3 entry in the demonstrated device workflow. It does not establish that
analysis runs immediately on insertion without loading the track.

The original embedded image's hash matched an existing artwork file, which
was preserved during deployment. Thus the generated 256x256 JPEG encoding
still lacks device validation. The experiment was host-side and offline; it
does not validate database writes while Engine is running, interruption
recovery, other codecs or JC11S behavior. Internal SATA and SD were untouched.

## Generated JPEG follow-up (2026-11-21)

The shared artwork JPG on the designated USB test drive was backed up and
replaced with the worker-generated 256x256 JPEG. The host verified the new
bytes by SHA-256; database and audio files were not changed. The owner reported
that the cover was displayed on the Prime after reinsertion. Because the
artwork identity was reused, cached artwork has not been ruled out; this is
positive display evidence, not conclusive fresh-identity decoder validation.
A future test should use an embedded image not previously seen by the device
and its correctly derived new hash/filename, without deleting device caches.

## JP24 stop/start coordination rehearsal (2026-11-21)

With explicit owner authorization for interrupted playback, a temporary shell
script scheduled an independent systemd Engine-start timer before stopping
engine.service. The native /usr/Engine/Engine process disappeared, the USB
m.db remained accessible, and prime4-mod-sshd.service remained active. Starting
engine.service restored a native Engine process and active service; the
fallback timer was then stopped. Script exit status was zero. This rehearsal
performed no direct writes to media or library databases; Engine itself can
perform normal shutdown/startup writes. UI recovery still needs owner
confirmation. No importer was installed or run. Native-process absence is a
prerequisite for the contemplated offline writer, not proof of exclusive
access against every other service or of an atomic full import.

The owner subsequently confirmed the device UI and controls returned to normal
after the coordination rehearsal. This completes the observed stop/start UI
recovery check for this JP24 session.

## On-device offline USB import rehearsal (2026-11-21)

The experimental worker command `cmd/jp24-lab-import` was compiled for ARM64
and tested end-to-end against a disposable host library copy before deployment
to /tmp on JP24. With the owner's authorization, a separate timed systemd
service ran it after Engine stopped. A 90-second independent recovery timer
was armed first; the importer had a 35-second runtime limit and 5-second stop
limit. Engine and other open m.db users were checked before writing.

The device reported IMPORT_COMMITTED for playlist
`USB Copy Device Test 20261121-080118`, with a database backup created first.
The test copied one existing MP3 to a new location, parsed its tags, reused its
existing artwork and duration/bitrate for identical audio, and created a new
Track plus empty PerformanceData and a new playlist. No analysis data was
copied. The import service exited successfully; Engine's native process was
observed after restart and the recovery timer was stopped. Device UI, playlist
visibility and playback still require the owner's observation for this run.

This is a fixed-volume laboratory command, not a production importer. It does
not implement full power-loss recovery or general duration parsing, and the
worker is not exposed through the runtime API or USB Copy overlay. SD and SATA
libraries were not directly written by the importer. Engine may perform its
usual writes on other mounted volumes during its own shutdown/startup.

The owner confirmed this on-device test succeeded after restart, in response
to the checks for title/tags/artwork visibility, loading/playback and BPM/
waveform generation. This validates the demonstrated stopped-Engine USB
import workflow for one duplicated MP3 on JP24. General source codecs, fresh
artwork identity, crash recovery, production runtime/UI integration and JC11S
remain unverified.

## USB-to-internal read-only preflight (2026-11-21)

The fixed-root `cmd/jp24-preflight` inspected both m.db files with mode=ro and
query_only in read transactions. MP3 2 has 12 Track, 2,370 Playlist and 11
AlbumArt rows; Denon has 16,243 Track, 2,367 Playlist and 1,465 AlbumArt rows.
Both report user_version 4194305 and the same full sqlite_master SQL hash:
41c49a3dec7f8306f017e3055edb42319f9f49e1a349ce90c9d5593857186e59.
The SATA volume reports 822,632,320 KiB available (about 785 GiB). This confirms
schema parity for this session, not safe concurrency or complete import
compatibility. No internal library data was copied off the device or written.
A fresh backup while Engine is stopped is required before an internal test.

## Internal database backup and restore rehearsal (2026-11-21)

Engine was stopped under the independent recovery-timer protocol. All four
active Database2 .db files and three journal files were copied to
`/data/prime-unlimited/backups/usb-copy/internal-20261121T081110Z/files`.
Each copy matched its original SHA-256. Backup files were then copied into
`restore-rehearsal` in that backup directory and independently verified against
SHA256SUMS. All seven files passed both checks. The backup service exited zero
and Engine's native process returned after restart. The live databases were
not overwritten. This verifies file restoration into a separate directory,
not an Engine launch from a restored live database. The snapshot excludes
artwork/audio and historical backups; the planned import must separately track
any newly created media/artwork for rollback. The complete snapshot remains on
the Prime; no library database was transferred to the host.

## Internal test aborted on preexisting integrity finding (2026-11-21)

The first one-song USB-to-SATA rehearsal stopped Engine and created a fresh
m.db backup, then aborted at integrity_check before transaction commit. The
transaction rolled back and the new audio directory was removed; Engine was
restarted by the exit handler. Read-only follow-up found 16,243 tracks and
2,367 playlists, unchanged from preflight. Both live internal m.db and the
pre-attempt baseline at internal-20261121T081110Z/files/m.db report:
`Page 21096: never used`. This proves the finding predates the attempted
import. No internal import success is claimed. Do not bypass the integrity
check or repair the live library automatically. Investigate on a separate
on-device copy before any further internal write attempt.

## Baseline rebuild investigation (2026-11-21)

The baseline-check tool ran against the saved, pre-attempt m.db only. VACUUM
INTO produced a separate candidate at
`internal-20261121T081110Z/rebuild-check-20261121T092738Z/m.db` under the on-device
backup root. Original integrity_check reported Page 21096: never used; rebuilt
integrity_check returned ok. Streaming content fingerprints for all ten tables
(including sqlite_sequence), schema SQL and user_version matched. No live
library was replaced. This demonstrates a structure-only rebuild of this
snapshot preserves the compared logical content. A live replacement, if
separately authorized, must use a fresh offline snapshot, repeat the checks,
and retain an exact current backup; never install the older snapshot over a
library that may have changed since the snapshot.

## Authorized current-library rebuild completed (2026-11-21)

Following explicit owner approval, the systemd-supervised repair stopped
Engine and saved a fresh seven-file snapshot at
`/data/prime-unlimited/backups/usb-copy/internal-20261121T093122Z`.
The rebuilt current m.db matched all ten table fingerprints, schema SQL and
user_version; integrity_check returned ok. It was staged on the same SATA
filesystem and renamed over live m.db after confirming the original still
matched the fresh snapshot. Engine and SSH returned active. The failure handler
was prepared to restore the exact fresh original if restart failed; it was not
needed. A subsequent read-only check of the live database after restart also
returned ok. Counts remained 16,243 tracks, 2,367 playlists, 1,465 artwork rows;
the schema hash was unchanged. Live size is 85,786,624 bytes. No USB-to-internal
import has succeeded yet; UI/library visibility after repair awaits the owner.

The owner confirmed the internal library and UI were present after the rebuild.

## First committed USB-to-internal rehearsal (2026-11-21)

After that confirmation, the fixed-volume importer was rerun with USB source
`/media/MP3 2` mounted read-only inside its systemd service and SATA destination
`/media/Denon`. The destination m.db was freshly backed up before writing.
The service reported IMPORT_COMMITTED for
`USB Copy Internal Test 20261121-093735`, passed its database integrity check,
and exited successfully. Engine's native process returned after restart. No
performance data was copied. This establishes on-device completion of the
experimental USB-to-SATA transaction, not UI/playback success; the owner still
needs to select Denon, inspect the playlist and test after safely ejecting USB.

The owner reported the requested internal-SSD checks succeeded, in response
to selecting Denon, checking title/tags/artwork/playback, then safely ejecting
the USB and checking playback again. This is the first owner-confirmed
USB-to-internal-SATA import and source-independent playback on JP24. It covers
one duplicated MP3 in the experimental offline workflow. General batch imports,
source playlists, deduplication, progress/cancellation, crash recovery, codecs
without an Engine library, and JC11S remain to be implemented or validated.

## JP24 content-duplicate rehearsal (2026-11-21)

The SHA-256 duplicate-aware importer reran the same USB-to-SATA selection.
It reported EXISTING_TRACK_REUSED and audio files copied: 0, committing only
playlist `USB Copy Internal Test 20261121-100353`. Internal Track count stayed
16,244; Playlist increased from 2,368 to 2,369; AlbumArt stayed 1,465. Exactly
one prior internal-test audio file remained before and after. USB counts stayed
12 tracks, 2,370 playlists and 11 artwork rows. Engine returned active. The
new playlist still needs the owner's UI/playback check. This validates indexed
content reuse in the laboratory flow, not acoustic duplicates or unindexed
files and not the unimplemented production insertion-dialog import.

The owner confirmed the duplicate-reuse playlist on Denon displayed the title
and that it could be loaded. Indexed byte-identical reuse is therefore
confirmed through the device UI for this JP24 test.

## Local batch rehearsal (2026-11-21)

The experimental batch command was tested with separate local source and
destination libraries, including one existing destination audio file and one
extra byte-identical source copy. First pass: 9 new files, 1 existing member
reused, 1 within-batch duplicate skipped, 3 playlists committed. Second pass:
0 new files, 11 existing members reused, 3 new playlists; destination Track
count stayed 19 on both passes. Source database SHA-256 remained unchanged.
This validates the tested mixed and repeat batches locally only. The command
currently requires MP3 sources indexed in an Engine library for duration and
bitrate; no-library sources and other codecs are not implemented by this lab
batch. The production dialog remains preview-only. Device batch testing is
still pending.

## JP24 multiple-source reuse batch (2026-11-21)

The batch importer ran on the actual USB source and internal SATA destination
with Engine stopped, fresh database backup and independent restart supervision.
It reported BATCH_COMMITTED: copied=0, reused=12, batch_duplicates=0, playlists=5.
The main playlist is `USB Copy Batch Test 20261121-101841`; four additional
source-derived playlists were created. Internal Track count stayed 16,244,
Playlist increased 2,369 to 2,374, AlbumArt stayed 1,465; USB counts were
unchanged. Engine and SSH were active after restart. All source content matched
existing target files, so this device run validates the all-reuse path; mixed
new-file copying remains covered locally and by the earlier one-song device
copy, not by a multi-new-file device test. Playlist UI/playback awaits owner.

The owner confirmed the multiple-source reuse batch playlists worked on JP24.

## Plain MP3 source support: local verification (2026-11-21)

The experimental batch no longer depends on source Engine databases or their
duration fields. It measures MPEG frames using the pinned MIT tcolgate/mp3
reader; the license is included. Tests cover CBR, VBR, ID3v2/ID3v1 exclusion,
truncation and cancellation. A local two-song source without Engine Library
imported two new Track rows and generated/imported-M3U playlists successfully.
Measured durations of those real songs matched ffprobe within 0.1 seconds;
both new entries retained isAnalyzed=0. No Engine library was created on the
source. The plain-source device path remains untested. Other audio codecs
remain outside this experimental batch, even though the scanner recognizes
several formats. General ID3/footer/tag variants need broader corpus coverage.

## JP24 no-source-library rehearsal (2026-11-21)

A separate on-device fixture under /data/prime-unlimited/usb-copy-lab/plain-source
contained two MP3 copies and an M3U, with no Engine Library. Its copied ID3v1
test comments differed from the USB originals so byte-hash reuse would not
mask new-track insertion. USB originals were read only. The importer measured
MPEG duration/bitrate directly and parsed tags/artwork, then reported
BATCH_COMMITTED: copied=2, reused=0, playlists=2; main playlist
`USB Copy Batch Test 20261121-102603`. Native Engine returned after restart.
This tests a directory simulating a plain USB source, not a separate physical
plain USB device. Owner UI/playback/analysis confirmation is pending. A later
aggregate-count reread could not authenticate (publickey denied), so post-run
counts are not independently recorded. No source Engine database was used by
the batch and no Engine Library existed in the fixture before the run.

The owner confirmed both titles in USB Copy Batch Test 20261121-102603 were
visible with tags/artwork and playable on Denon. This confirms the reported
plain-source MP3 import through the JP24 UI; the separate /data fixture still
simulates, rather than physically replaces, a plain USB stick.

The local runtime preview now stores/revalidates a separate duplicate summary
and reports new files, indexed existing matches, repeated source content and
actual estimated audio-copy bytes. The complete plan remains separate and
production commit remains unavailable. Local tests verify mixed-summary byte
counts, changed-source rejection and unchanged read-only target database bytes.
These preview changes are not yet deployed or device-UI validated.

After SSH was reauthorized, the pending read-only post-import count check
confirmed 16,246 internal tracks, 2,376 playlists and 1,465 artwork records.
USB counts remained 12/2,370/11 and Engine was active.

## Local preview lifecycle corrections

OverlayHost now pauses media-event polling during preparation/close and uses a
request generation to ignore stale poll replies. Preparation has a bounded
three-minute request window. USBCopy displays the already-returned POST preview
without a redundant GET/hash pass; it captures the insertion ID before the
request. Closing waits for DELETE completion instead of immediately reopening
an unresolved event. Loss of bridge availability clears stale preview state.
Read-only mounted USB/SD sources are eligible; targets still require writable
internal mounts. A local mocked JavaScript flow check passed stale-response,
duplicate-request, close-serialization and direct-preview scenarios. Eight
existing QML contract tests passed. This is logic/static checking, not Qt
rendering or deployed device verification. No new device writes were performed.

## 2026-11-23 — normal live admission, complete automatic import

JP24 / firmware 5.3.0: Sunbeam Outside World imported through normal preview/start with one new MP3, four duplicates skipped, one generated playlist containing only the new track. Destination SHA-256, ID3 and embedded cover verified; Engine identity unchanged. Owner confirmed cover, playlist membership and deck loading. The preceding Afterlife uncertain-create recovery was separate and is not counted as an automatic end-to-end pass. See LIVE_IMPORT_STATUS.md for remaining release checks.

## Completed mixed-source validation — 2026-11-23

The 690-file USB batch completed with 539 newly copied tracks (498 MP3 and 41 WAV), 151 skips and 7,583,360,670 copied bytes. All seven FLAC files already existed on the destination, so this run does not validate a new FLAC registration. Independent verification passed for source/copy SHA-256, native metadata and artwork, 12 filename-fallback titles, and exact new-only playlist membership and source order. The generated playlist contains 539 tracks; the two resolved source playlists contain 13 new tracks each. Engine process identity remained unchanged, and source USB and SD contents were not modified.

The owner confirmed that an imported tagged WAV was visible, loadable and playable from the generated playlist. The mixed batch required a reviewed continuation after the WAVE metadata reader correction: the first 32 registrations were verified and preserved; only the missing 507 registrations and playlist work were performed. No audio was recopied. This is a verified completed batch, not a fully automatic end-to-end batch pass. A fresh normal WAV import after the correction and a genuinely new FLAC import remain release-validation tasks.

## Fresh normal WAV/FLAC import — 2026-11-23

After the WAVE metadata reader correction, a fresh normal authenticated preview/start imported one new FLAC and one new WAV to internal Denon storage. The unchanged 690 other files were skipped; 9,905,526 bytes were copied and exactly one generated playlist was created with only the two new tracks. The job completed automatically without reviewed continuation, re-registration, direct SQL writes or Engine restart. Independent read-only verification passed (0.71 seconds) for both source/copy SHA-256 values, native metadata/artwork checks, exact playlist order and unchanged Engine identity.

The tagless FLAC appears as `FLAC-Testfile.flac`; the WAV contains the embedded title `Pink Noise` and artist `16 Bit 44.1 KHz`. The owner confirmed both fresh files are visible, loadable and playable from the generated playlist. Large-source preview and live revalidation reread existing content and can take several minutes; the queued live preparation currently lacks detailed durable progress. This evidence establishes normal new WAV/FLAC registration on the tested JP24 profile, not compatibility with every codec variant or other models.
