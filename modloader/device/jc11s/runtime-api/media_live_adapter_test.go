package main

import (
	"bytes"
	"context"
	"crypto/sha1"
	"database/sql"
	"encoding/base64"
	"errors"
	"image"
	"image/jpeg"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	usbcopy "prime-unlimited.usb-copy/worker"
)

type liveAdapterFixture struct {
	a             *mediaLiveAdapter
	plan          usbcopy.ImportPlan
	decisions     []usbcopy.DuplicateDecision
	calls         []string
	db            *sql.DB
	journal       usbcopy.LiveSequenceJournal
	failOperation string
	wrongURI      bool
}

func newLiveAdapterFixture(t *testing.T) *liveAdapterFixture {
	t.Helper()
	parent, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	root, source, dest := filepath.Join(parent, "runtime"), filepath.Join(parent, "usb"), filepath.Join(parent, "internal")
	for _, dir := range []string{root, source, filepath.Join(dest, "Engine Library", "Music"), filepath.Join(dest, "Engine Library", "Database2"), filepath.Join(dest, "Engine Library", "Artwork")} {
		if e = os.MkdirAll(dir, 0700); e != nil {
			t.Fatal(e)
		}
	}
	database := filepath.Join(dest, "Engine Library", "Database2", "m.db")
	db, e := sql.Open("sqlite", database)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { db.Close() })
	uuid := "647495f9-fd0e-4772-95b9-c69de5aac767"
	_, e = db.Exec(`PRAGMA user_version=4194305;CREATE TABLE Information(id INTEGER,uuid TEXT);CREATE TABLE AlbumArt(id INTEGER PRIMARY KEY,hash BLOB);CREATE TABLE Track(id INTEGER PRIMARY KEY,path TEXT,fileBytes INTEGER,title TEXT,artist TEXT,album TEXT,genre TEXT,comment TEXT,composer TEXT,year INTEGER,albumArtId INTEGER,originDatabaseUuid TEXT,originTrackId INTEGER);CREATE TABLE Playlist(id INTEGER PRIMARY KEY,title TEXT,parentListId INTEGER);CREATE TABLE PlaylistEntity(id INTEGER PRIMARY KEY,listId INTEGER,databaseUuid TEXT,trackId INTEGER);INSERT INTO Information VALUES(1,?);INSERT INTO Track VALUES(1,'Music/old.mp3',19,'Old','','','','','',0,NULL,?,1)`, uuid, uuid)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(source, "new.mp3"), []byte("new approved audio"), 0600); e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(source, "old.mp3"), []byte("already copied song"), 0600); e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(dest, "Engine Library", "Music", "old.mp3"), []byte("already copied song"), 0600); e != nil {
		t.Fatal(e)
	}
	newFP, e := usbcopy.FingerprintAudioFile(context.Background(), source, "new.mp3")
	if e != nil {
		t.Fatal(e)
	}
	oldFP, e := usbcopy.FingerprintAudioFile(context.Background(), source, "old.mp3")
	if e != nil {
		t.Fatal(e)
	}
	var imageData bytes.Buffer
	if e = jpeg.Encode(&imageData, image.NewRGBA(image.Rect(0, 0, 2, 2)), nil); e != nil {
		t.Fatal(e)
	}
	plan := usbcopy.ImportPlan{ImportDirectory: "Imported", BytesToCopy: newFP.Bytes + oldFP.Bytes, Tracks: []usbcopy.PlannedTrack{{SourceRelative: "new.mp3", DestinationRelative: "Imported/new.mp3", Bytes: newFP.Bytes, SourceModifiedAt: newFP.ModifiedAt, Tags: usbcopy.Tags{Title: "New song", Artist: "Artist", Album: "Album"}, Artwork: imageData.Bytes()}, {SourceRelative: "old.mp3", DestinationRelative: "Imported/old.mp3", Bytes: oldFP.Bytes, SourceModifiedAt: oldFP.ModifiedAt}}, CopyPlaylist: usbcopy.PlannedPlaylist{Name: "Copied songs", Tracks: []string{"new.mp3", "old.mp3"}}, SourcePlaylists: []usbcopy.PlannedPlaylist{{Name: "USB source list", Tracks: []string{"old.mp3", "new.mp3"}}}}
	decisions := []usbcopy.DuplicateDecision{{SourceRelative: "new.mp3", Fingerprint: newFP}, {SourceRelative: "old.mp3", Fingerprint: oldFP, ExistingTrackID: 1}}
	filtered, skipped, e := usbcopy.LiveNewOnlyPlan(plan, decisions)
	if e != nil {
		t.Fatal(e)
	}
	id := strings.Repeat("a", 64)
	binding := usbcopy.LiveBinding{LibraryUUID: uuid, MountIdentity: strings.Repeat("b", 64), EngineIdentity: "boot:pid:start"}
	a := &mediaLiveAdapter{id: id, root: root, database: database, source: source, destination: dest, binding: binding, plan: filtered, decisions: decisions, skipped: skipped, validate: func(context.Context) (usbcopy.LiveBinding, error) { return binding, nil }}
	journal, e := usbcopy.OpenFileLiveJournal(filepath.Join(root, "journal"))
	if e != nil {
		t.Fatal(e)
	}
	f := &liveAdapterFixture{a: a, plan: plan, decisions: decisions, db: db, journal: journal}
	a.exchange = func(ctx context.Context, c usbcopy.LiveCommand, proof func(usbcopy.LiveCommandObservation) error) (usbcopy.LiveCommandObservation, error) {
		f.calls = append(f.calls, c.Operation)
		o := usbcopy.LiveCommandObservation{Outcome: "applied", Binding: binding, SourceDevice: dest, CollectionDevice: dest, LibraryUUID: uuid, AllTracks: true}
		switch c.Operation {
		case "register_files":
			for _, p := range c.Paths {
				t := a.plan.Tracks[0]
				sum := sha1.Sum(t.Artwork)
				if _, e := db.Exec("INSERT INTO AlbumArt(id,hash) VALUES(1,?)", sum[:]); e != nil {
					return o, e
				}
				if e := os.WriteFile(filepath.Join(dest, "Engine Library", "Artwork", base64.RawURLEncoding.EncodeToString(sum[:])+".jpg"), t.Artwork, 0600); e != nil {
					return o, e
				}
				if _, e := db.Exec("INSERT INTO Track VALUES(2,?,?,?,?,?,?,?,?,0,1,?,2)", p, t.Bytes, t.Tags.Title, t.Tags.Artist, t.Tags.Album, "", "", "", uuid); e != nil {
					return o, e
				}
			}
		case "lookup_tracks":
			for _, id := range c.LocalIDs {
				var path string
				if e := db.QueryRow("SELECT path FROM Track WHERE id=?", id).Scan(&path); e != nil {
					return o, e
				}
				native := strconv.FormatInt(2251799813685248+id, 10) // simulated native model only
				uri := (&url.URL{Scheme: "file", Path: filepath.Join(dest, "Engine Library", path)}).String()
				if f.wrongURI {
					uri = "file:///wrong.mp3"
				}
				o.Tracks = append(o.Tracks, usbcopy.LiveTrackReference{NativeID: native, LocalID: id, Binding: binding})
				o.URIs = append(o.URIs, uri)
			}
		case "create_playlist":
			res, e := db.Exec("INSERT INTO Playlist(title,parentListId) VALUES(?,0)", c.Title)
			if e != nil {
				return o, e
			}
			id, _ := res.LastInsertId()
			o.Playlist = &usbcopy.LivePlaylistReference{NativeID: strconv.FormatInt(700+id, 10), Binding: binding}
		case "add_tracks":
			if c.Playlist.NativeID != strconv.FormatInt(700+c.Playlist.LocalID, 10) {
				return o, errors.New("adapter conflated native and SQL list IDs")
			}
			for _, ref := range c.Tracks {
				if _, e := db.Exec("INSERT INTO PlaylistEntity(listId,databaseUuid,trackId) VALUES(?,?,?)", c.Playlist.LocalID, uuid, ref.LocalID); e != nil {
					return o, e
				}
			}
		}
		if f.failOperation == c.Operation {
			return o, errors.New("native result ambiguous after invocation")
		}
		return o, proof(o)
	}
	return f
}
func TestConcreteLiveAdapterCopiesRegistersAndListsOnlyNewFiles(t *testing.T) {
	f := newLiveAdapterFixture(t)
	state, e := usbcopy.RunLiveSequence(context.Background(), f.a.id, f.plan, f.decisions, f.journal, f.a)
	if e != nil {
		t.Fatal(e)
	}
	if state.Phase != "completed" || state.Skipped != 1 || len(state.Tracks) != 1 || len(state.Playlists) != 2 {
		t.Fatalf("result %+v", state)
	}
	want := []string{"select_library", "all_tracks", "register_files", "lookup_tracks", "create_playlist", "add_tracks", "create_playlist", "add_tracks"}
	if !reflect.DeepEqual(f.calls, want) {
		t.Fatal(f.calls)
	}
	var bad int
	if e = f.db.QueryRow("SELECT count(*) FROM PlaylistEntity WHERE trackId=1").Scan(&bad); e != nil || bad != 0 {
		t.Fatal("skipped track was inserted", bad, e)
	}
	var members int
	if e = f.db.QueryRow("SELECT count(*) FROM PlaylistEntity WHERE trackId=2").Scan(&members); e != nil || members != 2 {
		t.Fatal("new memberships missing", members, e)
	}
	if _, e = os.Stat(filepath.Join(f.a.destination, "Engine Library", "Database2", "usb-copy-before-live-"+f.a.id+".sqlite-backup")); e != nil {
		t.Fatal("consistent backup missing", e)
	}
	if _, e = os.Stat(filepath.Join(f.a.destination, "Engine Library", "Music", "Imported", "old.mp3")); !os.IsNotExist(e) {
		t.Fatal("skipped file copied")
	}
}
func TestConcreteLiveAdapterAmbiguousRegistrationStopsWithoutPlaylistOrRetry(t *testing.T) {
	f := newLiveAdapterFixture(t)
	f.failOperation = "register_files"
	state, e := usbcopy.RunLiveSequence(context.Background(), f.a.id, f.plan, f.decisions, f.journal, f.a)
	if e == nil || state.Phase != "review_required" {
		t.Fatal("ambiguous result accepted", e)
	}
	if strings.Count(strings.Join(f.calls, ","), "register_files") != 1 || strings.Contains(strings.Join(f.calls, ","), "create_playlist") {
		t.Fatal("replayed registration or created playlists", f.calls)
	}
	if _, e = os.Stat(filepath.Join(f.a.destination, "Engine Library", "Music", "Imported", "new.mp3")); e != nil {
		t.Fatal("deleted potentially observed native copy", e)
	}
}
func TestConcreteLiveAdapterRejectsWrongNativeURI(t *testing.T) {
	f := newLiveAdapterFixture(t)
	f.wrongURI = true
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	state, e := usbcopy.RunLiveSequence(ctx, f.a.id, f.plan, f.decisions, f.journal, f.a)
	if e == nil || state.Phase != "review_required" {
		t.Fatal("wrong URI accepted", e)
	}
	if !strings.Contains(strings.Join(f.calls, ","), "lookup_tracks") {
		t.Fatal("test did not reach native URI validation", f.calls, e)
	}
	for _, call := range f.calls {
		if call == "create_playlist" || call == "add_tracks" {
			t.Fatal("unverified native track used", f.calls)
		}
	}
}
func TestLiveRegistrationVerifierChecksTagsCoverAndFilenameFallback(t *testing.T) {
	f := newLiveAdapterFixture(t)
	tdata := f.a.plan.Tracks[0]
	ctx := context.Background()
	_, e := f.a.invoke(ctx, f.a.id+":fixture", "register_files", usbcopy.LiveCommand{Paths: []string{liveTrackPath(tdata)}}, func(usbcopy.LiveCommandObservation) error { return nil })
	if e != nil {
		t.Fatal(e)
	}
	if _, e = liveRegistered(ctx, f.a.database, f.a.destination, f.a.binding.LibraryUUID, tdata); e != nil {
		t.Fatal(e)
	}
	wrong := tdata
	wrong.Tags.Artist = "different"
	if _, e = liveRegistered(ctx, f.a.database, f.a.destination, f.a.binding.LibraryUUID, wrong); e == nil {
		t.Fatal("missing ID3 metadata accepted")
	}
	wrong = tdata
	wrong.Artwork = []byte("different cover")
	if _, e = liveRegistered(ctx, f.a.database, f.a.destination, f.a.binding.LibraryUUID, wrong); e == nil {
		t.Fatal("wrong cover association accepted")
	}
	if _, e = f.db.Exec("UPDATE Track SET title='new.mp3' WHERE id=2"); e != nil {
		t.Fatal(e)
	}
	tdata.Tags.Title = ""
	if _, e = liveRegistered(ctx, f.a.database, f.a.destination, f.a.binding.LibraryUUID, tdata); e != nil {
		t.Fatal("filename fallback rejected", e)
	}
	if _, e = f.db.Exec("UPDATE Track SET title='' WHERE id=2"); e != nil {
		t.Fatal(e)
	}
	if _, e = liveRegistered(ctx, f.a.database, f.a.destination, f.a.binding.LibraryUUID, tdata); e == nil {
		t.Fatal("blank filename fallback accepted")
	}
}
