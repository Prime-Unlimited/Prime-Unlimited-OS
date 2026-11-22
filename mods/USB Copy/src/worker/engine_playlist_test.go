package usbcopy

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func TestDesktopExportResolvesOriginAndPreservesOrder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "m.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	statements := []string{
		`CREATE TABLE Information(id INTEGER,uuid TEXT)`,
		`INSERT INTO Information VALUES(1,'local')`,
		`CREATE TABLE Track(id INTEGER,path TEXT,originDatabaseUuid TEXT,originTrackId INTEGER)`,
		`INSERT INTO Track VALUES(1,'Music/one.mp3','desktop',101),(2,'Music/two.mp3','desktop',102)`,
		`CREATE TABLE Playlist(id INTEGER,parentListId INTEGER,title TEXT)`,
		`INSERT INTO Playlist VALUES(1,0,'Export')`,
		`CREATE TABLE PlaylistEntity(id INTEGER,listId INTEGER,trackId INTEGER,databaseUuid TEXT,nextEntityId INTEGER)`,
		`INSERT INTO PlaylistEntity VALUES(10,1,102,'desktop',11),(11,1,101,'desktop',12),(12,1,1,'unrelated',0)`,
	}
	for _, q := range statements {
		if _, err := db.Exec(q); err != nil {
			db.Close()
			t.Fatal(err)
		}
	}
	db.Close()
	available := map[string]int{filepath.FromSlash("Engine Library/Music/one.mp3"): 0, filepath.FromSlash("Engine Library/Music/two.mp3"): 1}
	lists, err := readEnginePlaylists(path, available)
	if err != nil {
		t.Fatal(err)
	}
	if len(lists) != 1 {
		t.Fatalf("playlists=%d", len(lists))
	}
	got := lists[0]
	if len(got.Tracks) != 2 || got.Missing != 1 {
		t.Fatalf("members=%d missing=%d", len(got.Tracks), got.Missing)
	}
	if got.Tracks[0] != "Engine Library/Music/two.mp3" || got.Tracks[1] != "Engine Library/Music/one.mp3" {
		t.Fatalf("unexpected order: %v", got.Tracks)
	}
}

func TestEngineTrackIdentityRejectsConflictingAliases(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE Track(id INTEGER,path TEXT,originDatabaseUuid TEXT,originTrackId INTEGER);
 INSERT INTO Track VALUES(1,'Music/one.mp3','local',2),(2,'Music/two.mp3','desktop',102)`); err != nil {
		t.Fatal(err)
	}
	if _, err := readEngineTrackIdentities(db, "local"); err == nil {
		t.Fatal("conflicting local/origin identity accepted")
	}
}
