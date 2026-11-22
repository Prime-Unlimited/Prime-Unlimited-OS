package usbcopy

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLiveJournalCrashReservationCannotReplay(t *testing.T) {
	parent, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	root := filepath.Join(parent, "live")
	j, e := OpenFileLiveJournal(root)
	if e != nil {
		t.Fatal(e)
	}
	s := LiveSequenceState{JobID: strings.Repeat("a", 64), Phase: "prepared", Binding: LiveBinding{LibraryUUID: "lib", MountIdentity: "sata", EngineIdentity: "boot:pid:start"}}
	if e = j.Create(context.Background(), s); e != nil {
		t.Fatal(e)
	}
	s.Phase = "pending"
	s.Intent = s.JobID + ":register:0"
	if e = j.Save(context.Background(), s); e != nil {
		t.Fatal(e)
	}
	reopened, e := OpenFileLiveJournal(root)
	if e != nil {
		t.Fatal(e)
	}
	actual, e := reopened.Read(s.JobID)
	if e != nil || actual.Intent != s.Intent {
		t.Fatalf("lost durable intent: %+v %v", actual, e)
	}
	s.Phase = "prepared"
	if e = reopened.Create(context.Background(), s); e == nil {
		t.Fatal("replayed claimed native operation")
	}
	id := strings.Repeat("b", 64)
	if e = os.WriteFile(filepath.Join(root, id+".reserved"), []byte(id), 0600); e != nil {
		t.Fatal(e)
	}
	s.JobID = id
	if e = reopened.Create(context.Background(), s); e == nil {
		t.Fatal("reused crash reservation without state")
	}
}
func TestLiveJournalRejectsSymlinkAndPublicRoot(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target")
	os.Mkdir(target, 0700)
	link := filepath.Join(root, "link")
	os.Symlink(target, link)
	if _, e := OpenFileLiveJournal(link); e == nil {
		t.Fatal("indirect root accepted")
	}
	os.Chmod(target, 0755)
	if _, e := OpenFileLiveJournal(target); e == nil {
		t.Fatal("public root accepted")
	}
}
