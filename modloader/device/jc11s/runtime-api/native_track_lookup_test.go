package main

import (
	"testing"
)

func TestNativeLookupURIValidation(t *testing.T) {
	expected := "file:///media/Denon/Engine%20Library/Music/a%20b.mp3"
	for _, raw := range []string{expected, `["file:///media/Denon/Engine%20Library/Music/a%20b.mp3"]`, `"file:///media/Denon/Engine%20Library/Music/a%20b.mp3"`, "/media/Denon/Engine Library/Music/a b.mp3", "file://localhost/media/Denon/Engine%20Library/Music/a%20b.mp3"} {
		if !nativeLookupURIMatches(raw, expected) {
			t.Fatalf("valid URI rejected %s", raw)
		}
	}
	for _, raw := range []string{"file:///media/MP3%202/a%20b.mp3", "https://example.com/a.mp3", "file://remote/media/Denon/Engine%20Library/Music/a%20b.mp3", expected + "?other", `["file:///a","file:///b"]`, "file:///media/Denon/../MP3%202/a.mp3"} {
		if nativeLookupURIMatches(raw, expected) {
			t.Fatalf("unsafe URI accepted %s", raw)
		}
	}
}
func TestNativeLookupAcceptsOnlyActualURIAndExactIdentity(t *testing.T) {
	s := nativeCreateState{Operation: "lookup", LookupLocalID: 16702, LookupFileURL: "file:///media/Denon/Engine%20Library/Music/new.mp3", Phase: "submitted"}
	if e := verifyNativeLookupResult(&s, "2251799813701950", `["file:///media/MP3%202/new.mp3"]`); e != nil {
		t.Fatal(e)
	}
	if s.Phase == "verified" {
		t.Fatal("wrong source file accepted")
	}
	s.Phase = "submitted"
	if e := verifyNativeLookupResult(&s, "2251799813701950", `["file:///media/Denon/Engine%20Library/Music/new.mp3"]`); e != nil || s.Phase != "verified" {
		t.Fatalf("real model reference rejected %+v %v", s, e)
	}
	s.Phase = "submitted"
	if e := verifyNativeLookupResult(&s, "2251799813701948", s.LookupFileURL); e == nil {
		t.Fatal("other local ID accepted")
	}
	if e := verifyNativeLookupResult(&s, "9007199254740993", s.LookupFileURL); e == nil {
		t.Fatal("imprecise QML ID accepted")
	}
}

func TestNativeEngineURIEnvelopeChecksOriginReference(t *testing.T) {
	uuid := "647495f9-fd0e-4772-95b9-c69de5aac767"
	expected := "file:///media/Denon/Engine%20Library/Music/new.mp3"
	raw := expected + "?databaseUuid=" + uuid + "&trackId=16702%20" + uuid + "&uri=#"
	state := nativeCreateState{LookupLocalID: 16702, LookupFileURL: expected, LookupOriginUUID: uuid, LookupOriginID: 16702, Phase: "submitted"}
	if e := verifyNativeLookupResult(&state, "2251799813701950", raw); e != nil || state.Phase != "verified" {
		t.Fatalf("real Engine URI rejected: %+v %v", state, e)
	}
	for _, bad := range []string{
		expected + "?databaseUuid=other&trackId=16702%20" + uuid + "&uri=#",
		expected + "?databaseUuid=" + uuid + "&trackId=16700%20" + uuid + "&uri=#",
		raw + "evil",
		expected + "?databaseUuid=" + uuid + "&trackId=16702%20" + uuid + "&uri=http://remote",
		expected + "?databaseUuid=" + uuid + "&trackId=16702%20" + uuid + "&extra=1",
		expected + "?databaseUuid=" + uuid + "&databaseUuid=" + uuid + "&trackId=16702%20" + uuid,
	} {
		state.Phase = "submitted"
		state.NativeTrackID = ""
		if e := verifyNativeLookupResult(&state, "2251799813701950", bad); e != nil {
			t.Fatal(e)
		}
		if state.Phase == "verified" {
			t.Fatalf("invalid envelope accepted %s", bad)
		}
	}
}
