# Live import status

## Verified on Prime 4 G2 / JP24

The device-native controller created an empty playlist while Engine remained running. An existing internal track was added with a native acknowledgement and read-only database verification. One previously absent MP3 was then copied to internal SATA storage, hashed, registered by Engine with title, artist and embedded cover, found under All Tracks and loaded into a deck. Its source and internal SHA-256 matched. Registration alone did not add it to the selected playlist. A separate native membership command added it exactly once and preserved the original member. The final playlist/deck visual confirmation for that last membership step is still pending; database membership and the native signal were verified.

## Implemented reusable worker components

`live_sequence.go` filters both generated and source playlists to newly copied files, serializes backup/copy/register/create/add/verify, records intent before every external operation, binds native IDs to the selected internal library and Engine instance, and stops for review on ambiguous outcomes. All-skipped batches create no playlist. The adapter must provide verified IDs from the actual native model; IDs must never be synthesized from a hardcoded namespace.

`live_journal.go` persists private state with fsync and atomic rename. Exclusive reservations survive crashes and prevent a second execution of the same job ID. A trusted runtime must hold the shared writer/admission lease and supply the native adapter before using this sequence.

## Remaining before enabling live imports by default

1. Replace fixed diagnostic sample/playlist assumptions with a general trusted native command broker.
2. Bind native context automatically to the selected internal destination even when the user is browsing USB, another view or a deck screen.
3. Resolve every newly registered track through current native model IDs and verify tags/cover, including filename fallback for absent title tags.
4. Connect the live sequence, durable journal and shared writer lease to normal admission/progress/review.
5. Verify multiple files, source playlists, all-skipped batches, source removal, UI changes, API/Engine interruption and uncertain native acknowledgements on hardware.

The tested offline path remains available. No automatic native retry, direct live SQLite write, SD mutation or production release is introduced.
