package peer

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"math"
	"unicode/utf8"
)

const MaxMessageBytes = 4096

type Deck struct {
	Deck      int      `json:"deck"`
	Title     string   `json:"title"`
	Artist    string   `json:"artist"`
	BPM       *float64 `json:"bpm"`
	Position  *float64 `json:"position"`
	Length    *float64 `json:"length"`
	PlayState *string  `json:"playState"`
}

type DeckStatus struct {
	Version         int    `json:"version"`
	Type            string `json:"type"`
	SessionEpoch    string `json:"sessionEpoch"`
	Sequence        uint64 `json:"sequence"`
	SentMonotonicMs uint64 `json:"sentMonotonicMs"`
	Decks           []Deck `json:"decks"`
}

// DecodeDeckStatus accepts one bounded JSON object. Session authentication is
// the responsibility of the transport, before this function is called.
func DecodeDeckStatus(data []byte) (DeckStatus, error) {
	var status DeckStatus
	if len(data) == 0 || len(data) > MaxMessageBytes {
		return status, errors.New("invalid message size")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&status); err != nil {
		return DeckStatus{}, err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return DeckStatus{}, errors.New("trailing JSON data")
	}
	if err := status.Validate(); err != nil {
		return DeckStatus{}, err
	}
	return status, nil
}

func (s DeckStatus) Validate() error {
	if s.Version != 1 || s.Type != "deck_status" || len(s.SessionEpoch) < 16 || len(s.SessionEpoch) > 64 || s.Sequence == 0 || len(s.Decks) > 4 || s.Decks == nil {
		return errors.New("invalid deck status header")
	}
	for _, c := range s.SessionEpoch {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_') {
			return errors.New("invalid session epoch")
		}
	}
	seen := [5]bool{}
	for _, deck := range s.Decks {
		if deck.Deck < 1 || deck.Deck > 4 || seen[deck.Deck] || !validLabel(deck.Title) || !validLabel(deck.Artist) {
			return errors.New("invalid deck identity or label")
		}
		seen[deck.Deck] = true
		for _, value := range []*float64{deck.BPM, deck.Position, deck.Length} {
			if value != nil && (math.IsNaN(*value) || math.IsInf(*value, 0) || *value < 0 || *value > 1e9) {
				return errors.New("invalid deck number")
			}
		}
		if deck.PlayState != nil && !validLabel(*deck.PlayState) {
			return errors.New("invalid play state")
		}
	}
	return nil
}

func validLabel(value string) bool {
	return len(value) <= 256 && utf8.ValidString(value)
}

// SequenceGate drops repeats, reordered messages, and stale session epochs.
// A caller must explicitly Reset after an authenticated session change.
type SequenceGate struct {
	epoch string
	last  uint64
}

func (g *SequenceGate) Reset(epoch string) { g.epoch, g.last = epoch, 0 }

func (g *SequenceGate) Accept(status DeckStatus) bool {
	if status.SessionEpoch != g.epoch || status.Sequence <= g.last {
		return false
	}
	g.last = status.Sequence
	return true
}
