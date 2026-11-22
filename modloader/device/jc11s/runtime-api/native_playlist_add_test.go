package main

import (
	"context"
	"testing"
)

func TestNativeAddRequiresExactMembershipAndSignal(t *testing.T) {
	path, db := nativeCreateFixture(t)
	for _, q := range []string{`ALTER TABLE PlaylistEntity ADD COLUMN databaseUuid TEXT`, `ALTER TABLE PlaylistEntity ADD COLUMN trackId INTEGER`, `INSERT INTO Playlist VALUES(2396,'USB Copy Live Test 20261122-124235 - 88487aa6',0,0)`} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	state := nativeCreateState{Operation: "add", Title: nativeTestPlaylistTitle, PlaylistID: 2396, TrackOriginUUID: "origin", TrackOriginID: 8, Phase: "submitted", Before: nativeLibraryCounts{Tracks: 1, Playlists: 1}}
	if err := verifyNativeAdd(context.Background(), &state, path); err != nil {
		t.Fatal(err)
	}
	if state.Phase == "verified" {
		t.Fatal("empty playlist verified")
	}
	if _, err := db.Exec(`INSERT INTO PlaylistEntity VALUES(2396,'wrong',8)`); err != nil {
		t.Fatal(err)
	}
	if err := verifyNativeAdd(context.Background(), &state, path); err == nil {
		t.Fatal("wrong origin verified")
	}
	if _, err := db.Exec(`UPDATE PlaylistEntity SET databaseUuid='origin'`); err != nil {
		t.Fatal(err)
	}
	if err := verifyNativeAdd(context.Background(), &state, path); err != nil {
		t.Fatal(err)
	}
	if state.Phase == "verified" {
		t.Fatal("missing native signal accepted")
	}
	state.RowsInserted = true
	if err := verifyNativeAdd(context.Background(), &state, path); err != nil {
		t.Fatal(err)
	}
	if state.Phase != "verified" {
		t.Fatalf("not verified: %+v", state)
	}
}
func TestNativeAddTargetMatch(t *testing.T) {
	state := nativeCreateState{SourceDevice: "/media/Denon", RootRow: 1, CurrentRow: 35, TargetListID: "2397"}
	view := nativePlaylistInstance{SourceDevice: state.SourceDevice, RootRow: 1, CurrentRow: 35, SelectedListID: "2397", SelectedTestPlaylist: true, CanAdd: true, Visible: true, Enabled: true}
	if !nativeAddMatches(view, state) {
		t.Fatal("valid target rejected")
	}
	view.SourceDevice = "/media/SD Card"
	if nativeAddMatches(view, state) {
		t.Fatal("SD accepted")
	}
	view.SourceDevice = state.SourceDevice
	view.SelectedListID = "2396"
	if nativeAddMatches(view, state) {
		t.Fatal("wrong native list ID accepted")
	}
}

func TestNativeAddPreservesExistingPlaylistMember(t *testing.T) {
	path, db := nativeCreateFixture(t)
	for _, q := range []string{`ALTER TABLE PlaylistEntity ADD COLUMN databaseUuid TEXT`, `ALTER TABLE PlaylistEntity ADD COLUMN trackId INTEGER`, `INSERT INTO Playlist VALUES(2396,'USB Copy Live Test 20261122-124235 - 88487aa6',0,0)`, `INSERT INTO PlaylistEntity VALUES(2396,'origin',16700)`} {
		if _, e := db.Exec(q); e != nil {
			t.Fatal(e)
		}
	}
	state := nativeCreateState{Operation: "add", Title: nativeTestPlaylistTitle, PlaylistID: 2396, TrackOriginUUID: "origin", TrackOriginID: 16702, Phase: "submitted", Before: nativeLibraryCounts{Tracks: 1, Playlists: 1, Entities: 1}, BeforeListMembers: []nativePlaylistMember{{UUID: "origin", Track: 16700}}, RowsInserted: true}
	if e := verifyNativeAdd(context.Background(), &state, path); e != nil {
		t.Fatal(e)
	}
	if state.Phase == "verified" {
		t.Fatal("missing new member accepted")
	}
	db.Exec(`INSERT INTO PlaylistEntity VALUES(2396,'origin',16702)`)
	if e := verifyNativeAdd(context.Background(), &state, path); e != nil {
		t.Fatal(e)
	}
	if state.Phase != "verified" {
		t.Fatalf("not verified: %+v", state)
	}
	state.Phase = "submitted"
	db.Exec(`DELETE FROM PlaylistEntity WHERE trackId=16700`)
	if e := verifyNativeAdd(context.Background(), &state, path); e == nil {
		t.Fatal("removed old member accepted")
	}
}
