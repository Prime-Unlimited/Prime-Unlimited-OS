package usbcopy

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

func TestDuplicateSummaryReportsActualCopyBytes(t *testing.T) {
	source, target := t.TempDir(), t.TempDir()
	os.MkdirAll(filepath.Join(target, "Engine Library/Music"), 0700)
	os.MkdirAll(filepath.Join(target, "Engine Library/Database2"), 0700)
	putAudio(t, source, "a.mp3", "same")
	putAudio(t, source, "b.mp3", "new content")
	putAudio(t, source, "c.mp3", "new content")
	putAudio(t, source, "d.mp3", "evil")
	if e := os.WriteFile(filepath.Join(target, "Engine Library/Music/existing.mp3"), []byte("same"), 0600); e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(target, "Engine Library/Database2/m.db")
	db, e := sql.Open("sqlite", path)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec("CREATE TABLE Track(id INTEGER PRIMARY KEY,path TEXT);INSERT INTO Track VALUES(1,'Music/existing.mp3')"); e != nil {
		t.Fatal(e)
	}
	db.Close()
	data, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	before := sha256.Sum256(data)
	tracks := []Track{}
	for _, name := range []string{"a.mp3", "b.mp3", "c.mp3", "d.mp3"} {
		st, e := os.Stat(filepath.Join(source, name))
		if e != nil {
			t.Fatal(e)
		}
		tracks = append(tracks, Track{RelativePath: name, Bytes: st.Size(), ModifiedAt: st.ModTime().UnixNano()})
	}
	result, e := SummarizeImportDuplicates(context.Background(), source, target, path, tracks)
	if e != nil {
		t.Fatal(e)
	}
	if result.NewFiles != 2 || result.ExistingFiles != 1 || result.SourceDuplicates != 1 || result.BytesToCopy != 15 {
		t.Fatalf("unexpected summary: %+v", result)
	}
	data, e = os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	if sha256.Sum256(data) != before {
		t.Fatal("destination database changed")
	}
	tracks[0].Bytes++
	if _, e = SummarizeImportDuplicates(context.Background(), source, target, path, tracks); e == nil {
		t.Fatal("changed source accepted")
	}
}
