package main

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"image"
	"image/jpeg"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNativeRegistrationNeedsExactTagsArtworkMembershipAndSignals(t *testing.T) {
	path, db := nativeCreateFixture(t)
	for _, q := range []string{`ALTER TABLE Track ADD COLUMN path TEXT`, `ALTER TABLE Track ADD COLUMN title TEXT`, `ALTER TABLE Track ADD COLUMN artist TEXT`, `ALTER TABLE Track ADD COLUMN albumArtId INTEGER`, `ALTER TABLE Track ADD COLUMN originDatabaseUuid TEXT`, `ALTER TABLE Track ADD COLUMN originTrackId INTEGER`, `CREATE TABLE AlbumArt(id INTEGER,hash BLOB)`, `ALTER TABLE PlaylistEntity ADD COLUMN databaseUuid TEXT`, `ALTER TABLE PlaylistEntity ADD COLUMN trackId INTEGER`, `INSERT INTO Playlist VALUES(2396,'USB Copy Live Test 20261122-124235 - 88487aa6',0,0)`} {
		if _, e := db.Exec(q); e != nil {
			t.Fatal(e)
		}
	}
	root := t.TempDir()
	state := nativeCreateState{Operation: "register", SourceDevice: root, Title: nativeTestPlaylistTitle, PlaylistID: 2396, CopiedRelative: "Music/sample.mp3", Phase: "submitted", Before: nativeLibraryCounts{Tracks: 1, Playlists: 1}}
	art, _ := hex.DecodeString(nativeSampleArt)
	if _, e := db.Exec(`INSERT INTO AlbumArt VALUES(1,?)`, art); e != nil {
		t.Fatal(e)
	}
	if _, e := db.Exec(`INSERT INTO Track(id,path,title,artist,albumArtId,originDatabaseUuid,originTrackId) VALUES(2,?,'Andromeda (Original Mix)','Al Park, AKIN K',1,'origin',2)`, state.CopiedRelative); e != nil {
		t.Fatal(e)
	}
	if _, e := db.Exec(`INSERT INTO PlaylistEntity VALUES(2396,'origin',2)`); e != nil {
		t.Fatal(e)
	}
	directory := filepath.Join(root, "Engine Library", "Artwork")
	if e := os.MkdirAll(directory, 0700); e != nil {
		t.Fatal(e)
	}
	file := filepath.Join(directory, base64.RawURLEncoding.EncodeToString(art)+".jpg")
	if e := os.WriteFile(file, []byte("not an image"), 0600); e != nil {
		t.Fatal(e)
	}
	state.RowsInserted = true
	state.NativeCollectionComplete = true
	if e := verifyNativeRegistration(context.Background(), &state, path); e != nil {
		t.Fatal(e)
	}
	if state.Phase == "verified" || state.RegisteredArtworkFile {
		t.Fatal("invalid cover accepted")
	}
	f, e := os.Create(file)
	if e != nil {
		t.Fatal(e)
	}
	e = jpeg.Encode(f, image.NewRGBA(image.Rect(0, 0, 2, 2)), nil)
	f.Close()
	if e != nil {
		t.Fatal(e)
	}
	state.NativeCollectionComplete = false
	if e := verifyNativeRegistration(context.Background(), &state, path); e != nil {
		t.Fatal(e)
	}
	if state.Phase == "verified" {
		t.Fatal("missing completion accepted")
	}
	state.NativeCollectionComplete = true
	if e := verifyNativeRegistration(context.Background(), &state, path); e != nil {
		t.Fatal(e)
	}
	if state.Phase != "verified" {
		t.Fatalf("unexpected state: %+v", state)
	}
	state.Phase = "submitted"
	db.Exec(`UPDATE Track SET artist='wrong' WHERE id=2`)
	if e := verifyNativeRegistration(context.Background(), &state, path); e != nil {
		t.Fatal(e)
	}
	if state.Phase == "verified" {
		t.Fatal("wrong artist accepted")
	}
}
func TestNativeCopiedRejectsUnexpectedLocation(t *testing.T) {
	state := nativeCreateState{ID: strings.Repeat("a", 64), SourceDevice: t.TempDir(), CopiedRelative: "../SD Card/test.mp3", FileURL: "file:///media/SD%20Card/test.mp3"}
	if e := validateNativeCopied(context.Background(), state); e == nil {
		t.Fatal("unexpected location accepted")
	}
}
func TestNativeSampleCopyRejectsChangedSourceBeforeWriting(t *testing.T) {
	source := t.TempDir()
	target := t.TempDir()
	os.WriteFile(filepath.Join(source, nativeSampleFile), []byte("changed"), 0600)
	if _, e := copyNativeSample(context.Background(), source, target, strings.Repeat("a", 64)); e == nil {
		t.Fatal("wrong sample accepted")
	}
	entries, e := os.ReadDir(target)
	if e != nil || len(entries) != 0 {
		t.Fatal("wrote target before validating source")
	}
}

func TestNativeRegistrationReviewDoesNotClaimPlaylistSuccess(t *testing.T) {
	state := nativeCreateState{Operation: "register", Phase: "registration_verified"}
	if !nativeCreateActive(state) {
		t.Fatal("unconfirmed registration released writer")
	}
	state.UserConfirmedRegistration = true
	if nativeCreateActive(state) {
		t.Fatal("reviewed registration still blocks offline import")
	}
	state.Phase = "submitted"
	if !nativeCreateActive(state) {
		t.Fatal("unresolved native request released writer")
	}
	state.Operation = "add"
	state.Phase = "registration_verified"
	if !nativeCreateActive(state) {
		t.Fatal("wrong operation released writer")
	}
}
