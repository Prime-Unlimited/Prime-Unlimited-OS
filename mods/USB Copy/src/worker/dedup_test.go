package usbcopy

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func putAudio(t *testing.T, root, name, data string) {
	t.Helper()
	if e := os.WriteFile(filepath.Join(root, name), []byte(data), 0600); e != nil {
		t.Fatal(e)
	}
}
func TestDuplicateAudioUsesContentsAndReusesLowestTrackID(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()
	putAudio(t, src, "renamed.mp3", "same audio")
	putAudio(t, dst, "first.mp3", "same audio")
	putAudio(t, dst, "second.mp3", "same audio")
	matches, e := FindDuplicateAudio(context.Background(), src, dst, []string{"renamed.mp3"}, []ExistingTrackFile{{9, "second.mp3"}, {2, "first.mp3"}})
	if e != nil {
		t.Fatal(e)
	}
	if len(matches) != 1 || matches[0].ExistingTrackID != 2 {
		t.Fatal("identical renamed audio was not reused deterministically")
	}
}
func TestDuplicateAudioDoesNotMatchSameNameOrSizeAlone(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()
	putAudio(t, src, "song.mp3", "different A")
	putAudio(t, dst, "song.mp3", "different B")
	matches, e := FindDuplicateAudio(context.Background(), src, dst, []string{"song.mp3"}, []ExistingTrackFile{{1, "song.mp3"}})
	if e != nil {
		t.Fatal(e)
	}
	if matches[0].ExistingTrackID != 0 || matches[0].DuplicateOfSource != "" {
		t.Fatal("different bytes treated as duplicate")
	}
}
func TestDuplicateSourcesCopiedOnlyOnce(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()
	putAudio(t, src, "a.mp3", "same audio")
	putAudio(t, src, "b.mp3", "same audio")
	matches, e := FindDuplicateAudio(context.Background(), src, dst, []string{"a.mp3", "b.mp3"}, nil)
	if e != nil {
		t.Fatal(e)
	}
	if matches[0].DuplicateOfSource != "" || matches[1].DuplicateOfSource != "a.mp3" {
		t.Fatal("duplicate source not linked to first copy")
	}
}
func TestDuplicateCheckRejectsCancellationAndEscapingSources(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()
	putAudio(t, src, "a.mp3", "same audio")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := FindDuplicateAudio(ctx, src, dst, []string{"a.mp3"}, nil); e == nil {
		t.Fatal("cancellation ignored")
	}
	if _, e := FingerprintAudioFile(context.Background(), src, "../outside.mp3"); e == nil {
		t.Fatal("escape accepted")
	}
	if e := os.Symlink(filepath.Join(src, "a.mp3"), filepath.Join(src, "link.mp3")); e != nil {
		t.Fatal(e)
	}
	if _, e := FingerprintAudioFile(context.Background(), src, "link.mp3"); e == nil {
		t.Fatal("symlink accepted")
	}
}

func TestDuplicateProgressPreservesContentMatchesAndSkipsSymlinks(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()
	putAudio(t, src, "source.mp3", "same audio")
	putAudio(t, dst, "target.mp3", "same audio")
	if err := os.Symlink(filepath.Join(dst, "target.mp3"), filepath.Join(dst, "link.mp3")); err != nil {
		t.Fatal(err)
	}
	completed := map[string]bool{}
	result, err := FindDuplicateAudioWithProgress(context.Background(), src, dst, []string{"source.mp3"}, []ExistingTrackFile{{1, "link.mp3"}, {2, "target.mp3"}}, func(stage string, done, total int) {
		if done == total {
			completed[stage] = true
		}
	})
	if err != nil || len(result) != 1 || result[0].ExistingTrackID != 2 {
		t.Fatalf("result=%v err=%v", result, err)
	}
	for _, stage := range []string{"source", "index", "compare"} {
		if !completed[stage] {
			t.Fatalf("missing completed stage %s", stage)
		}
	}
}
