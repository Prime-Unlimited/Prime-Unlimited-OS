# USB Copy — experimental 0.1.4-beta.1

USB-to-internal music import for **Prime 4 G2 (JP24), Engine OS 5.3.0** with Prime Unlimited installed. Other devices and firmware are not approved by this release.

On USB insertion, choose whether to copy. Declining preserves normal USB use. Choose the internal destination to import MP3, supported PCM/IEEE-float WAV or native FLAC through Engine while Engine remains running. Embedded metadata and artwork are verified; missing title tags fall back to the filename. Engine owns audio analysis. Exact content duplicates are skipped. Generated and resolved source playlists contain only newly copied tracks, preserving their order. Source USB contents are unchanged. SD cards were excluded from validation.

## Installation

The mod ZIP alone is insufficient. This release includes a separate signed experimental JP24 catalog and the required optional beta runtime, based on modloader r16 with USB Copy integration (runtime version 0.2.0-r17). Follow [release/INSTALL.md](release/INSTALL.md). The stable catalog and stable updater channel are unchanged.

## Verified behavior

- Normal automatic MP3 import with tags, cover and new-only playlist, plus a duplicate-only run without playlist creation.
- Fresh normal automatic WAV and FLAC import: two new files, 690 existing files skipped, SHA-256/metadata/exact playlist membership verified independently; owner confirmed both load and play from internal storage. No Engine restart or reviewed completion was required.
- A 539-new-track mixed batch (498 MP3, 41 WAV) completed and verified after an explicitly reviewed WAV-metadata correction. This larger run is not counted as a fully automatic batch pass.
- Declining the dialog, device restart, filename fallback and content deduplication were owner-tested on JP24.

## Experimental limits

Large USB collections are reread for duplicate detection and live revalidation; this can take several minutes. Live preparation currently displays queued without detailed progress. Interrupted or uncertain native operations stop for review; do not blindly retry. Unresolved source-playlist references are reported and cannot be reconstructed from filenames. RF64, compressed WAV codecs, other audio containers, every tag/codec variation, power-loss recovery and other models are not validated. The release does not include the owner's private exact-file MP3 exceptions.

Details: [live import status](LIVE_IMPORT_STATUS.md), [format scope](LIVE_AUDIO_FORMATS.md), [hardware validation history](HARDWARE_VALIDATION.md), [architecture](IMPORT_ARCHITECTURE.md).

## Build

Use Go 1.24+ and Python 3. Run worker tests in `src/worker`, then `python3 scripts/package.py --target jp24 --output artifacts`. Packaging creates unsigned preview artifacts; trust comes from the separately signed release catalog. Laboratory import/repair executables and authorization tokens are excluded. JC11S can compile for development but is not a supported release target.
