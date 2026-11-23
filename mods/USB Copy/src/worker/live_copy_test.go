package usbcopy

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func liveCopyFixture(t *testing.T) (string, string, ImportPlan, []DuplicateDecision) {
	t.Helper()
	source, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	dest, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(source, "new.mp3")
	if e := os.WriteFile(path, []byte("approved song data"), 0600); e != nil {
		t.Fatal(e)
	}
	f, e := FingerprintAudioFile(context.Background(), source, "new.mp3")
	if e != nil {
		t.Fatal(e)
	}
	p := ImportPlan{ImportDirectory: "copy", BytesToCopy: f.Bytes, Tracks: []PlannedTrack{{SourceRelative: "new.mp3", DestinationRelative: "copy/new.mp3", Bytes: f.Bytes, SourceModifiedAt: f.ModifiedAt}}, CopyPlaylist: PlannedPlaylist{Name: "Imported", Tracks: []string{"new.mp3"}}}
	return source, dest, p, []DuplicateDecision{{SourceRelative: "new.mp3", Fingerprint: f}}
}
func TestLiveCopyVerifiesPublishedBytes(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("atomic no-overwrite publication uses Linux renameat2; run on Prime")
	}
	source, dest, p, d := liveCopyFixture(t)
	r, e := CopyLivePlan(context.Background(), source, dest, strings.Repeat("a", 64), p, d, nil)
	if e != nil || !r.Committed || r.FilesCopied != 1 {
		t.Fatalf("copy %+v %v", r, e)
	}
	actual, e := FingerprintAudioFile(context.Background(), dest, "copy/new.mp3")
	if e != nil || actual.SHA256 != d[0].Fingerprint.SHA256 {
		t.Fatalf("wrong content %+v %v", actual, e)
	}
}
func TestLiveCopyRejectsSameSizeSourceReplacementBeforePublishing(t *testing.T) {
	source, dest, p, d := liveCopyFixture(t)
	path := filepath.Join(source, "new.mp3")
	st, e := os.Stat(path)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(path, []byte(strings.Repeat("x", int(st.Size()))), 0600); e != nil {
		t.Fatal(e)
	}
	if e = os.Chtimes(path, st.ModTime(), st.ModTime()); e != nil {
		t.Fatal(e)
	}
	if _, e = CopyLivePlan(context.Background(), source, dest, strings.Repeat("b", 64), p, d, nil); e == nil {
		t.Fatal("accepted changed bytes with same size and timestamp")
	}
	if _, e = os.Stat(filepath.Join(dest, "copy")); !os.IsNotExist(e) {
		t.Fatal("published changed file")
	}
}
func TestLiveCopyRejectsMutationDuringCopyDespiteRestoredTimestamp(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("atomic no-overwrite publication uses Linux renameat2; run on Prime")
	}
	source, dest, p, d := liveCopyFixture(t)
	path := filepath.Join(source, "new.mp3")
	st, e := os.Stat(path)
	if e != nil {
		t.Fatal(e)
	}
	changed := false
	_, e = CopyLivePlan(context.Background(), source, dest, strings.Repeat("c", 64), p, d, func(progress CopyProgress) {
		if changed {
			return
		}
		changed = true
		if e := os.WriteFile(path, []byte(strings.Repeat("x", int(st.Size()))), 0600); e != nil {
			t.Error(e)
		}
		if e := os.Chtimes(path, st.ModTime(), st.ModTime()); e != nil {
			t.Error(e)
		}
	})
	if !changed || e == nil || !strings.Contains(e.Error(), "published live copy failed content verification") {
		t.Fatalf("expected actual published-content check to block registration, got %v", e)
	}
}
