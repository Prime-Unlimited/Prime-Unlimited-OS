# Prime Link deck telemetry spike

`src/ui/DeckTelemetry.qml` is an uninstalled read-only overlay prototype for the JP24
Engine QML context. It displays active deck title and artist, and holds a
bounded snapshot with BPM, position, length and play state for a future local
Prime Link service. It does not send data, control decks or touch audio.

Property names were observed in the JP24 Engine binary and
`/GUI/Decks/ActiveDecks` in the saved `EmbeddedMain.qml`. The translator types
and overlay context still need a live, reversible hardware check before catalog
approval or device installation. Missing fields remain empty or null.

The package scripts follow the Prime Clock overlay layout. Build with
`python3 scripts/package.py --target jp24 --output dist`, then run
`python3 scripts/validate_package.py dist/prime-link-v0.1.0-dev.1-jp24.zip --target jp24`.
No signed catalog record or device installation is included.
