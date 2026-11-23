# Experimental live audio formats

The trusted live path accepts MP3, RIFF/WAVE PCM or IEEE float (including their WAVEFORMATEXTENSIBLE forms), and native FLAC. Additional containers require the verified JP24 / firmware 5.3.0 live profile. The offline SQL writer remains MP3-only. Scanner support for other extensions does not make them importable.

Before copying, MP3 uses the existing MPEG frame measurement. WAV validates container/chunk bounds, format, sample rate, channel count, sample size, block alignment and data size. FLAC validates its signature, first STREAMINFO metadata block, bounded metadata chain and initial frame header. These are container checks, not a full compressed-audio decode or a playback guarantee. Engine performs native registration and analysis; no duration, BPM, waveform or other analysis values are fabricated.

Legacy PCM files may omit the alignment byte after an odd-sized final data block. This case is accepted only when the declared audio payload ends exactly at RIFF EOF. Truncation and ambiguous unpadded intermediate chunks remain errors. RF64, compressed WAV codecs and other formats are not included in this extension.

File containment, symlink rejection, cancellation and change detection apply before container acceptance. Registration commands allow only canonical Music paths with MP3/WAV/FLAC extensions. The existing native-ID, library, mount, process, metadata/artwork and copied-content verification remains mandatory.

References: [Microsoft WAVEFORMATEX](https://learn.microsoft.com/en-us/windows/win32/api/mmreg/ns-mmreg-waveformatex), [WAVEFORMATEXTENSIBLE](https://learn.microsoft.com/en-us/windows/win32/api/mmreg/ns-mmreg-waveformatextensible), [FLAC RFC 9639](https://www.rfc-editor.org/rfc/rfc9639.html).

## Validation status

On 2026-11-23, container preflight passed for all 48 WAV and 7 FLAC files on the owner-authorized 690-track USB source. Worker race tests and the Linux ARM64 runtime suite passed. Native WAV/FLAC import and deck playback validation remain pending; this is not a stable release claim.

The same source contains two readable Engine playlists. One has 372 references without a matching track identity in its source database. These references cannot be reconstructed from unrelated audio filenames. Import planning emits an unavailable-reference warning and preserves only resolved, newly copied members in their source order.

## Mixed-source trial findings

The normal 690-track preview found 539 new files (7,583,360,670 bytes), 141 existing files and 10 source duplicates. The first live job stopped in pre-copy validation: no audio copied, no native session or playlist operation started, Engine unchanged. A bounded root review verified those conditions under both leases and closed the failed job without resuming it.

One valid MP3 carried an APEv2 trailer before its ID3v1 tag. MPEG measurement now validates the complete APE header/footer/item layout and excludes those metadata bytes from audio, following the [APEv2 specification](https://wiki.hydrogenaudio.org/index.php?title=APEv2_specification). Tests retain rejection of malformed trailers and truncated MPEG audio; the actual owner-provided file now measures successfully and fully decodes with FFmpeg.

Two other newly copied candidates have a partial final MPEG packet; they decode, but the strict preflight rejects the incomplete packet. The owner explicitly approved copying these two files unchanged. A private, root-owned approval is bound to the complete source digest, the exact file SHA-256 and byte size, and an expiry of at most 24 hours. It only permits the observed MP3 EOF error; other validation errors and changed bytes remain rejected. The approval is not part of the mod package or a client HTTP request. No complete WAV/FLAC native import or playback success is claimed from container validation alone.
