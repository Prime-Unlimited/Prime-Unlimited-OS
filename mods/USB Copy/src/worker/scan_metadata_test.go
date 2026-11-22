package usbcopy

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestScanAndImportIgnoreMacMetadataSidecars(t *testing.T) {
	source, target, proc := batchFixture(t)
	for _, name := range []string{"._one.mp3", "._Test.m3u", ".DS_Store"} {
		if err := os.WriteFile(filepath.Join(source, name), []byte{0, 5, 22, 7, 0, 2, 0, 0}, 0600); err != nil {
			t.Fatal(err)
		}
	}
	snapshot, err := Scan(source)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Tracks) != 2 || len(snapshot.Playlists) != 1 || len(snapshot.Warnings) != 0 {
		t.Fatalf("tracks=%d lists=%d warnings=%v", len(snapshot.Tracks), len(snapshot.Playlists), snapshot.Warnings)
	}
	result, err := RunOfflineBatch(context.Background(), source, target, proc, OfflineBatchOptions{JobID: strings.Repeat("5", 64)})
	if err != nil || result.Copied != 2 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if _, err = os.Stat(filepath.Join(source, "._one.mp3")); err != nil {
		t.Fatal("source sidecar changed", err)
	}
}
