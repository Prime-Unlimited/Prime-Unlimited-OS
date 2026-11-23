# Deck status message v0

The future local Prime Link service receives snapshots from the trusted QML
adapter. The current overlay only displays them; it does not transmit.

```json
{
  "version": 0,
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
    "playState": true
  }]
}
```

The service must cap the number of decks at four, cap strings, reject stale
epochs/sequences, and treat null values as unavailable. This schema is not a
remote-control command. Media transport and authorization require separate
protocols.
