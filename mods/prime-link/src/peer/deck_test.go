package peer

import "testing"

const valid = `{"version":1,"type":"deck_status","sessionEpoch":"Abcdefgh12345678","sequence":1,"sentMonotonicMs":42,"decks":[{"deck":1,"title":"Track","artist":"Artist","bpm":128,"position":12.5,"length":300,"playState":"playing"}]}`

func TestDecodeAndSequence(t *testing.T) {
	s, err := DecodeDeckStatus([]byte(valid))
	if err != nil { t.Fatal(err) }
	var gate SequenceGate
	gate.Reset(s.SessionEpoch)
	if !gate.Accept(s) || gate.Accept(s) { t.Fatal("sequence gate accepted repeat or rejected first message") }
	gate.Reset("OtherSession123456")
	if gate.Accept(s) { t.Fatal("sequence gate accepted stale epoch") }
}

func TestRejectMalformed(t *testing.T) {
	cases := []string{
		valid + ` {}`,
		`{"version":1,"type":"deck_status","sessionEpoch":"Abcdefgh12345678","sequence":1,"decks":[],"remoteControl":true}`,
		`{"version":1,"type":"deck_status","sessionEpoch":"Abcdefgh12345678","sequence":1,"decks":[{"deck":1},{"deck":1}]}`,
		`{"version":1,"type":"deck_status","sessionEpoch":"Abcdefgh12345678","sequence":1,"decks":[{"deck":5}]}`,
	}
	for _, input := range cases {
		if _, err := DecodeDeckStatus([]byte(input)); err == nil { t.Fatalf("accepted malformed message: %s", input) }
	}
}
