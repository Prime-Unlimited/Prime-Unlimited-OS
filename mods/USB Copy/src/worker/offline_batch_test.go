package usbcopy

import (
	"context"
	"database/sql"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func batchFixture(t *testing.T) (string, string, string) {
	t.Helper()
	base, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	source, target, proc := filepath.Join(base, "source"), filepath.Join(base, "target"), filepath.Join(base, "proc")
	for _, p := range []string{source, proc, filepath.Join(target, "Engine Library/Database2"), filepath.Join(target, "Engine Library/Music"), filepath.Join(target, "Engine Library/Artwork")} {
		if e = os.MkdirAll(p, 0700); e != nil {
			t.Fatal(e)
		}
	}
	for i, name := range []string{"one.mp3", "two.mp3"} {
		text := append([]byte{0}, []byte(name)...)
		tag := make([]byte, 10+len(text))
		copy(tag, "TIT2")
		binary.BigEndian.PutUint32(tag[4:8], uint32(len(text)))
		copy(tag[10:], text)
		header := []byte{'I', 'D', '3', 3, 0, 0, 0, 0, 0, 0}
		size := len(tag)
		for j := 9; j >= 6; j-- {
			header[j] = byte(size & 127)
			size >>= 7
		}
		data := append(header, tag...)
		for j := 0; j < 100+i; j++ {
			data = append(data, mpegFrame(9, 128)...)
		}
		if e = os.WriteFile(filepath.Join(source, name), data, 0600); e != nil {
			t.Fatal(e)
		}
	}
	if e = os.WriteFile(filepath.Join(source, "Test.m3u"), []byte("two.mp3\none.mp3\n"), 0600); e != nil {
		t.Fatal(e)
	}
	db, e := sql.Open("sqlite", filepath.Join(target, "Engine Library/Database2/m.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	_, e = db.Exec(`PRAGMA user_version=4194305;
 CREATE TABLE Information(id INTEGER PRIMARY KEY,uuid TEXT);INSERT INTO Information VALUES(1,'target');
 CREATE TABLE AlbumArt(id INTEGER PRIMARY KEY AUTOINCREMENT,hash BLOB,albumArt BLOB);
 CREATE TABLE Track(id INTEGER PRIMARY KEY AUTOINCREMENT,path TEXT UNIQUE,filename TEXT,fileBytes INTEGER,title TEXT,artist TEXT,album TEXT,genre TEXT,comment TEXT,composer TEXT,year INTEGER,albumArtId INTEGER,length INTEGER,bitrate INTEGER,fileType TEXT,isAnalyzed INTEGER,isAvailable INTEGER,isMetadataImported INTEGER,isPlayed INTEGER,isBeatGridLocked INTEGER,dateCreated INTEGER,dateAdded INTEGER,originDatabaseUuid TEXT,originTrackId INTEGER,UNIQUE(originDatabaseUuid,originTrackId));
 CREATE TABLE PerformanceData(trackId INTEGER PRIMARY KEY,trackData BLOB,beatData BLOB,overviewWaveFormData BLOB);
 CREATE TRIGGER new_performance AFTER INSERT ON Track BEGIN INSERT INTO PerformanceData(trackId) VALUES(NEW.id);UPDATE Track SET originTrackId=NEW.id,originDatabaseUuid='target' WHERE id=NEW.id;END;
 CREATE TABLE Playlist(id INTEGER PRIMARY KEY AUTOINCREMENT,title TEXT,parentListId INTEGER,isPersisted INTEGER,nextListId INTEGER,lastEditTime INTEGER,isExplicitlyExported INTEGER,UNIQUE(title,parentListId),UNIQUE(parentListId,nextListId));
 CREATE TABLE PlaylistEntity(id INTEGER PRIMARY KEY AUTOINCREMENT,listId INTEGER,trackId INTEGER,databaseUuid TEXT,nextEntityId INTEGER,membershipReference INTEGER,UNIQUE(listId,databaseUuid,trackId));
 CREATE TRIGGER before_list BEFORE INSERT ON Playlist BEGIN UPDATE Playlist SET nextListId=-(1+nextListId) WHERE nextListId=NEW.nextListId AND parentListId=NEW.parentListId;END;
 CREATE TRIGGER after_list AFTER INSERT ON Playlist BEGIN UPDATE Playlist SET nextListId=NEW.id WHERE nextListId=-(1+NEW.nextListId) AND parentListId=NEW.parentListId;END;`)
	if e != nil {
		t.Fatal(e)
	}
	return source, target, proc
}
func TestOfflineBatchCopiesThenReusesAndReportsProgress(t *testing.T) {
	source, target, proc := batchFixture(t)
	progress := []CopyProgress{}
	phases := []JobPhase{}
	result, e := RunOfflineBatch(context.Background(), source, target, proc, OfflineBatchOptions{JobID: strings.Repeat("1", 64), Progress: func(phase JobPhase, p CopyProgress) error {
		progress = append(progress, p)
		phases = append(phases, phase)
		return nil
	}})
	if e != nil {
		t.Fatal(e)
	}
	if result.Copied != 2 || result.Reused != 0 || result.Playlists != 2 {
		t.Fatalf("unexpected result: %+v", result)
	}
	if len(progress) != 4 || phases[0] != JobCopying || phases[len(phases)-1] != JobCommitting {
		t.Fatal("incorrect progress phases")
	}
	last := progress[len(progress)-1]
	if last.FilesDone != 2 || last.BytesDone != last.BytesTotal || last.BytesTotal <= 0 {
		t.Fatal("incomplete final progress")
	}
	// Different durable job identity avoids timestamp/name collision on retry.
	again, e := RunOfflineBatch(context.Background(), source, target, proc, OfflineBatchOptions{JobID: strings.Repeat("2", 64)})
	if e != nil {
		t.Fatal(e)
	}
	if again.Copied != 0 || again.Reused != 2 || again.Playlists != 0 || again.PlaylistName != "" {
		t.Fatalf("repeat duplicated files: %+v", again)
	}
	db, e := sql.Open("sqlite", filepath.Join(target, "Engine Library/Database2/m.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	var count int
	if e = db.QueryRow("SELECT count(*) FROM Track").Scan(&count); e != nil || count != 2 {
		t.Fatal("wrong destination count", e)
	}
	if e = db.QueryRow("SELECT count(*) FROM Playlist").Scan(&count); e != nil || count != 2 {
		t.Fatal("all-skipped repeat created playlists", e)
	}
	if _, e = os.Stat(filepath.Join(source, "Engine Library")); !os.IsNotExist(e) {
		t.Fatal("source library created")
	}
}
func TestOfflineBatchCancellationRollsBackFilesAndLibrary(t *testing.T) {
	for _, when := range []JobPhase{JobCopying, JobCommitting} {
		t.Run(string(when), func(t *testing.T) {
			source, target, proc := batchFixture(t)
			sentinel := errors.New("cancel before commit")
			_, e := RunOfflineBatch(context.Background(), source, target, proc, OfflineBatchOptions{Progress: func(phase JobPhase, p CopyProgress) error {
				if phase == when && p.FilesDone > 0 {
					return sentinel
				}
				return nil
			}})
			if !errors.Is(e, sentinel) {
				t.Fatalf("unexpected cancellation: %v", e)
			}
			db, e := sql.Open("sqlite", filepath.Join(target, "Engine Library/Database2/m.db"))
			if e != nil {
				t.Fatal(e)
			}
			defer db.Close()
			for _, table := range []string{"Track", "Playlist", "PlaylistEntity", "PerformanceData"} {
				var n int
				if e = db.QueryRow("SELECT count(*) FROM " + table).Scan(&n); e != nil || n != 0 {
					t.Fatalf("%s not rolled back", table)
				}
			}
			files, e := os.ReadDir(filepath.Join(target, "Engine Library/Music"))
			if e != nil || len(files) != 0 {
				t.Fatal("partial audio remains", e)
			}
		})
	}
}

func TestOfflineBatchRejectsChangedApprovedAudioBytes(t *testing.T) {
	source, target, proc := batchFixture(t)
	scan, e := Scan(source)
	if e != nil {
		t.Fatal(e)
	}
	paths := []string{}
	for _, tr := range scan.Tracks {
		paths = append(paths, tr.RelativePath)
	}
	decisions, e := FindDuplicateAudio(context.Background(), source, target, paths, nil)
	if e != nil {
		t.Fatal(e)
	}
	expected := DuplicateSourceDigest(decisions)
	path := filepath.Join(source, paths[0])
	data, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	data[len(data)-1] ^= 1
	if e = os.WriteFile(path, data, 0600); e != nil {
		t.Fatal(e)
	}
	if _, e = RunOfflineBatch(context.Background(), source, target, proc, OfflineBatchOptions{ExpectedSourceDigest: expected}); e == nil {
		t.Fatal("changed approved content imported")
	}
	db, e := sql.Open("sqlite", filepath.Join(target, "Engine Library/Database2/m.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	var n int
	if e = db.QueryRow("SELECT count(*) FROM Track").Scan(&n); e != nil || n != 0 {
		t.Fatal("destination changed on rejected content")
	}
}

func TestOfflineBatchPlaylistsContainOnlyNewlyCopiedSourcePaths(t *testing.T) {
	source, target, proc := batchFixture(t)
	if _, err := RunOfflineBatch(context.Background(), source, target, proc, OfflineBatchOptions{JobID: strings.Repeat("3", 64)}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(source, "one.mp3"))
	if err != nil {
		t.Fatal(err)
	}
	data[len(data)-1] ^= 1
	for _, name := range []string{"three.mp3", "zzduplicate.mp3"} {
		if err = os.WriteFile(filepath.Join(source, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err = os.WriteFile(filepath.Join(source, "Mixed.m3u"), []byte("one.mp3\nthree.mp3\nzzduplicate.mp3\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(source, "Skipped.m3u"), []byte("one.mp3\nzzduplicate.mp3\n"), 0600); err != nil {
		t.Fatal(err)
	}
	result, err := RunOfflineBatch(context.Background(), source, target, proc, OfflineBatchOptions{JobID: strings.Repeat("4", 64)})
	if err != nil {
		t.Fatal(err)
	}
	if result.Copied != 1 || result.Reused != 2 || result.SourceDuplicates != 1 || result.Playlists != 2 {
		t.Fatalf("unexpected result: %+v", result)
	}
	db, err := sql.Open("sqlite", filepath.Join(target, "Engine Library/Database2/m.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var lists, members, oldMembers int
	if err = db.QueryRow("SELECT count(*) FROM Playlist WHERE title LIKE ?", result.PlaylistName+"%").Scan(&lists); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow("SELECT count(*) FROM PlaylistEntity e JOIN Playlist p ON p.id=e.listId WHERE p.title LIKE ?", result.PlaylistName+"%").Scan(&members); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow("SELECT count(*) FROM PlaylistEntity e JOIN Playlist p ON p.id=e.listId WHERE p.title LIKE ? AND e.trackId IN (1,2)", result.PlaylistName+"%").Scan(&oldMembers); err != nil {
		t.Fatal(err)
	}
	if lists != 2 || members != 2 || oldMembers != 0 {
		t.Fatalf("lists=%d members=%d oldMembers=%d", lists, members, oldMembers)
	}
}
