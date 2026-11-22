package usbcopy

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestImportTrackTitlePreservesTagsAndFallsBackToOriginalFilename(t *testing.T) {
	for _, tc := range []struct{ tag, path, want string }{{"Existing Title", "folder/01-file.mp3", "Existing Title"}, {"", "folder/Rübenfeld.MP3", "Rübenfeld.MP3"}, {" \t ", "folder/0001-original.name.mp3", "0001-original.name.mp3"}} {
		if got := ImportTrackTitle(Tags{Title: tc.tag}, tc.path); got != tc.want {
			t.Fatalf("got %q want %q", got, tc.want)
		}
	}
}
func TestOfflineBatchStoresFilenameWhenMP3HasNoTags(t *testing.T) {
	source, target, proc := batchFixture(t)
	// Replace one fixture with valid MPEG frames and no ID3 header.
	data := []byte{}
	for i := 0; i < 100; i++ {
		data = append(data, mpegFrame(9, 128)...)
	}
	if err := os.WriteFile(filepath.Join(source, "one.mp3"), data, 0600); err != nil {
		t.Fatal(err)
	}
	result, err := RunOfflineBatch(context.Background(), source, target, proc, OfflineBatchOptions{JobID: strings.Repeat("8", 64)})
	if err != nil || result.Copied != 2 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	db, err := sql.Open("sqlite", filepath.Join(target, "Engine Library/Database2/m.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var title string
	if err = db.QueryRow("SELECT title FROM Track WHERE filename LIKE '%one.mp3'").Scan(&title); err != nil || title != "one.mp3" {
		t.Fatalf("title=%q err=%v", title, err)
	}
}
