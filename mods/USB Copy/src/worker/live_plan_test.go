package usbcopy

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLivePlanConvertsActualCopyPlanPlaylistDestinations(t *testing.T) {
	source, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	dest, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(source, "new.mp3"), []byte("song"), 0600); e != nil {
		t.Fatal(e)
	}
	snapshot := Snapshot{Tracks: []Track{{RelativePath: "new.mp3", Bytes: 4}}, Playlists: []Playlist{{Name: "USB list", Tracks: []string{"new.mp3"}}}}
	p, e := BuildImportPlan(source, dest, "USB", time.Now(), snapshot, nil)
	if e != nil {
		t.Fatal(e)
	}
	if p.CopyPlaylist.Tracks[0] == "new.mp3" {
		t.Fatal("fixture no longer exercises destination mapping")
	}
	live, e := LivePlanWithSourcePlaylists(p)
	if e != nil {
		t.Fatal(e)
	}
	if live.CopyPlaylist.Tracks[0] != "new.mp3" || live.SourcePlaylists[0].Tracks[0] != "new.mp3" {
		t.Fatal("playlist identity not converted")
	}
	if p.CopyPlaylist.Tracks[0] == "new.mp3" || p.SourcePlaylists[0].Tracks[0] == "new.mp3" {
		t.Fatal("offline plan was mutated")
	}
	p.CopyPlaylist.Tracks = []string{"unknown.mp3"}
	if _, e = LivePlanWithSourcePlaylists(p); e == nil {
		t.Fatal("unknown destination accepted")
	}
}
