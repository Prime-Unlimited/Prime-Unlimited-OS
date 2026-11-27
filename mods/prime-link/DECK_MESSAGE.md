# Deck status message v1

The future local Prime Link service receives snapshots from the trusted QML
adapter. The current overlay only displays them; it does not transmit. The
`src/peer` Go package validates this message before a peer consumes it. The
transport must authenticate peers before decoding and explicitly reset the
sequence gate when a new authenticated session begins.

```json
{
  "version": 1,
  "type": "deck_status",
  "sessionEpoch": "random-session-id",
  "sequence": 1,
  "sentMonotonicMs": 123456,
  "decks": [{
    "deck": 1,
    "title": "Example",
    "artist": "Artist",
    "bpm": 128.0,
    "position": 42.5,
    "length": 300.0,
    "playState": "playing"
  }]
}
```

The decoder caps messages at 4096 bytes, decks at four and labels at 256 UTF-8
bytes; it rejects duplicate deck numbers, extra fields, malformed values and
trailing JSON. Null numeric/state values mean unavailable. Position/length
units and Engine play-state values remain unverified. This schema carries no
remote-control command. Media transport and authorization require separate
protocols.
