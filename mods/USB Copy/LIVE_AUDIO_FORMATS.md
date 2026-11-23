# Experimental live audio formats

The trusted live path accepts MP3, RIFF/WAVE PCM or IEEE float (including their WAVEFORMATEXTENSIBLE forms), and native FLAC. Additional containers require the verified JP24 / firmware 5.3.0 live profile. The offline SQL writer remains MP3-only. Scanner support for other extensions does not make them importable.

Before copying, MP3 uses the existing MPEG frame measurement. WAV validates container/chunk bounds, format, sample rate, channel count, sample size, block alignment and data size. FLAC validates its signature, first STREAMINFO metadata block, bounded metadata chain and initial frame header. These are container checks, not a full compressed-audio decode or a playback guarantee. Engine performs native registration and analysis; no duration, BPM, waveform or other analysis values are fabricated.

Legacy PCM files may omit the alignment byte after an odd-sized final data block. This case is accepted only when the declared audio payload ends exactly at RIFF EOF. Truncation and ambiguous unpadded intermediate chunks remain errors. RF64, compressed WAV codecs and other formats are not included in this extension.

File containment, symlink rejection, cancellation and change detection apply before container acceptance. Registration commands allow only canonical Music paths with MP3/WAV/FLAC extensions. The existing native-ID, library, mount, process, metadata/artwork and copied-content verification remains mandatory.

References: [Microsoft WAVEFORMATEX](https://learn.microsoft.com/en-us/windows/win32/api/mmreg/ns-mmreg-waveformatex), [WAVEFORMATEXTENSIBLE](https://learn.microsoft.com/en-us/windows/win32/api/mmreg/ns-mmreg-waveformatextensible), [FLAC RFC 9639](https://www.rfc-editor.org/rfc/rfc9639.html).

## Validation status

On 2026-11-23, container preflight passed for all 48 WAV and 7 FLAC files on the owner-authorized 690-track USB source. Worker race tests and the Linux ARM64 runtime suite passed. Native WAV import and owner-confirmed playback subsequently passed as recorded below; new FLAC import remains untested. This is not a stable release claim.

The same source contains two readable Engine playlists. One has 372 references without a matching track identity in its source database. These references cannot be reconstructed from unrelated audio filenames. Import planning emits an unavailable-reference warning and preserves only resolved, newly copied members in their source order.

## Mixed-source trial findings

The normal 690-track preview found 539 new files (7,583,360,670 bytes), 141 existing files and 10 source duplicates. The first live job stopped in pre-copy validation: no audio copied, no native session or playlist operation started, Engine unchanged. A bounded root review verified those conditions under both leases and closed the failed job without resuming it.

One valid MP3 carried an APEv2 trailer before its ID3v1 tag. MPEG measurement now validates the complete APE header/footer/item layout and excludes those metadata bytes from audio, following the [APEv2 specification](https://wiki.hydrogenaudio.org/index.php?title=APEv2_specification). Tests retain rejection of malformed trailers and truncated MPEG audio; the actual owner-provided file now measures successfully and fully decodes with FFmpeg.

Two other newly copied candidates have a partial final MPEG packet; they decode, but the strict preflight rejects the incomplete packet. The owner explicitly approved copying these two files unchanged. A private, root-owned approval is bound to the complete source digest, the exact file SHA-256 and byte size, and an expiry of at most 24 hours. It only permits the observed MP3 EOF error; other validation errors and changed bytes remain rejected. The approval is not part of the mod package or a client HTTP request. No complete WAV/FLAC native import or playback success is claimed from container validation alone.

## WAV metadata interpretation

The read-only scanner now parses embedded WAVE ID3 chunks and LIST/INFO fields. Explicit ID3 fields take priority; INFO supplies missing basic fields. Raw tagless WAVs retain an empty embedded title so filename fallback is checked only when metadata is truly absent. Tests cover both chunk orders, bounds and unchanged source bytes.

The mixed-source trial revealed three correctly registered WAV titles that the former scanner had mistaken for tagless files. After correcting the reader, all 32 already applied registrations verified. A separately reviewed continuation rechecks every copied SHA-256, the unchanged Engine/mount binding, the exact registered prefix and database effect counts. It skips the proven prefix, performs only missing native work and never calls the copy adapter. This reviewed completion does not count as an automatic end-to-end batch pass.

## Completed mixed-source validation — 2026-11-23

The 690-file USB batch completed with 539 newly copied tracks (498 MP3 and 41 WAV), 151 skips and 7,583,360,670 copied bytes. All seven FLAC files already existed on the destination, so this run does not validate a new FLAC registration. Independent verification passed for source/copy SHA-256, native metadata and artwork, 12 filename-fallback titles, and exact new-only playlist membership and source order. The generated playlist contains 539 tracks; the two resolved source playlists contain 13 new tracks each. Engine process identity remained unchanged, and source USB and SD contents were not modified.

The owner confirmed that an imported tagged WAV was visible, loadable and playable from the generated playlist. The mixed batch required a reviewed continuation after the WAVE metadata reader correction: the first 32 registrations were verified and preserved; only the missing 507 registrations and playlist work were performed. No audio was recopied. This is a verified completed batch, not a fully automatic end-to-end batch pass. A fresh normal WAV import after the correction and a genuinely new FLAC import remain release-validation tasks.

## Fresh normal WAV/FLAC import — 2026-11-23

After the WAVE metadata reader correction, a fresh normal authenticated preview/start imported one new FLAC and one new WAV to internal Denon storage. The unchanged 690 other files were skipped; 9,905,526 bytes were copied and exactly one generated playlist was created with only the two new tracks. The job completed automatically without reviewed continuation, re-registration, direct SQL writes or Engine restart. Independent read-only verification passed (0.71 seconds) for both source/copy SHA-256 values, native metadata/artwork checks, exact playlist order and unchanged Engine identity.

The tagless FLAC appears as `FLAC-Testfile.flac`; the WAV contains the embedded title `Pink Noise` and artist `16 Bit 44.1 KHz`. The owner confirmed both fresh files are visible, loadable and playable from the generated playlist. Large-source preview and live revalidation reread existing content and can take several minutes; the queued live preparation currently lacks detailed durable progress. This evidence establishes normal new WAV/FLAC registration on the tested JP24 profile, not compatibility with every codec variant or other models.
