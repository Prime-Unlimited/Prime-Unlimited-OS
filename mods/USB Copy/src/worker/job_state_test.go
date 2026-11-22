package usbcopy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func jobFixture(t *testing.T) *JobStore {
	t.Helper()
	root, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	s, e := OpenJobStore(filepath.Join(root, "jobs"))
	if e != nil {
		t.Fatal(e)
	}
	return s
}
func TestJobPersistsProgressAndRejectsStaleWriter(t *testing.T) {
	store := jobFixture(t)
	job, e := store.Save(ImportJobStatus{ID: strings.Repeat("a", 64), Phase: JobQueued, FilesTotal: 2, BytesTotal: 20})
	if e != nil {
		t.Fatal(e)
	}
	stale := job
	job.Phase = JobStopping
	job, e = store.Save(job)
	if e != nil {
		t.Fatal(e)
	}
	stale.Phase = JobStopping
	if _, e = store.Save(stale); e == nil {
		t.Fatal("stale writer accepted")
	}
	job.Phase = JobCopying
	job, e = store.Save(job)
	if e != nil {
		t.Fatal(e)
	}
	job.FilesDone = 1
	job.BytesDone = 10
	job, e = store.Save(job)
	if e != nil {
		t.Fatal(e)
	}
	reopened, e := OpenJobStore(store.root)
	if e != nil {
		t.Fatal(e)
	}
	saved, e := reopened.Read(job.ID)
	if e != nil || saved.Revision != job.Revision || saved.FilesDone != 1 {
		t.Fatal("progress did not survive reopen", e)
	}
	job.FilesDone = 2
	job.BytesDone = 20
	job.Phase = JobCommitting
	job, e = store.Save(job)
	if e != nil {
		t.Fatal(e)
	}
	job.Phase = JobCompleted
	if _, e = store.Save(job); e == nil {
		t.Fatal("restart phase bypassed")
	}
	job.Phase = JobRestarting
	job, e = store.Save(job)
	if e != nil {
		t.Fatal(e)
	}
	job.Phase = JobCompleted
	if _, e = store.Save(job); e == nil {
		t.Fatal("unconfirmed engine recovery accepted")
	}
	job.EngineRestored = true
	job, e = store.Save(job)
	if e != nil {
		t.Fatal(e)
	}
	job.Phase = JobCopying
	if _, e = store.Save(job); e == nil {
		t.Fatal("completed job restarted")
	}
}
func TestJobRecoveryDoesNotResumeOrLoseTerminalRecords(t *testing.T) {
	store := jobFixture(t)
	pending, e := store.Save(ImportJobStatus{ID: strings.Repeat("b", 64), Phase: JobQueued})
	if e != nil {
		t.Fatal(e)
	}
	done, e := store.Save(ImportJobStatus{ID: strings.Repeat("c", 64), Phase: JobQueued})
	if e != nil {
		t.Fatal(e)
	}
	done.Phase = JobCancelled
	done, e = store.Save(done)
	if e != nil {
		t.Fatal(e)
	}
	if e = store.RecoverInterrupted(); e != nil {
		t.Fatal(e)
	}
	interrupted, e := store.Read(pending.ID)
	if e != nil || interrupted.Phase != JobInterrupted || interrupted.ErrorCode != "recovery_required" {
		t.Fatal("abandoned job not marked", e)
	}
	if e = store.RecoverInterrupted(); e != nil {
		t.Fatal(e)
	}
	again, e := store.Read(pending.ID)
	if e != nil || again.Revision != interrupted.Revision {
		t.Fatal("recovery is not idempotent")
	}
	unchanged, e := store.Read(done.ID)
	if e != nil || unchanged.Revision != done.Revision {
		t.Fatal("terminal record altered")
	}
}
func TestJobRejectsUnsafePathsAndRegressingProgress(t *testing.T) {
	store := jobFixture(t)
	id := strings.Repeat("d", 64)
	if _, e := store.Save(ImportJobStatus{ID: "../escape", Phase: JobQueued}); e == nil {
		t.Fatal("unsafe id")
	}
	job, e := store.Save(ImportJobStatus{ID: id, Phase: JobQueued, FilesTotal: 1, BytesTotal: 10})
	if e != nil {
		t.Fatal(e)
	}
	job.Phase = JobStopping
	job, e = store.Save(job)
	if e != nil {
		t.Fatal(e)
	}
	job.Phase = JobCopying
	job.FilesDone = 1
	job.BytesDone = 10
	job, e = store.Save(job)
	if e != nil {
		t.Fatal(e)
	}
	job.BytesDone = 0
	if _, e = store.Save(job); e == nil {
		t.Fatal("regressing progress accepted")
	}
	link := filepath.Join(store.root, "linked")
	if e = os.Symlink(store.root, link); e != nil {
		t.Fatal(e)
	}
	if _, e = OpenJobStore(link); e == nil {
		t.Fatal("symlink store accepted")
	}
}

func TestConcurrentJobWritersOnlyOneRevisionWins(t *testing.T) {
	store := jobFixture(t)
	job, e := store.Save(ImportJobStatus{ID: strings.Repeat("e", 64), Phase: JobQueued})
	if e != nil {
		t.Fatal(e)
	}
	other, e := OpenJobStore(store.root)
	if e != nil {
		t.Fatal(e)
	}
	ready := make(chan struct{})
	results := make(chan error, 2)
	for _, writer := range []*JobStore{store, other} {
		go func(s *JobStore) { <-ready; next := job; next.Phase = JobStopping; _, e := s.Save(next); results <- e }(writer)
	}
	close(ready)
	success := 0
	for i := 0; i < 2; i++ {
		if <-results == nil {
			success++
		}
	}
	if success != 1 {
		t.Fatalf("successful concurrent writes=%d", success)
	}
	saved, e := store.Read(job.ID)
	if e != nil || saved.Revision != 2 {
		t.Fatal("unexpected persisted revision", e)
	}
}

func TestTerminalJobAcknowledgementIsDurableAndCannotChangeOutcome(t *testing.T) {
	store := jobFixture(t)
	job, e := store.Save(ImportJobStatus{ID: strings.Repeat("f", 64), Phase: JobQueued})
	if e != nil {
		t.Fatal(e)
	}
	if e = store.Acknowledge(job.ID); e == nil {
		t.Fatal("running job acknowledged")
	}
	job.Phase = JobCancelled
	job, e = store.Save(job)
	if e != nil {
		t.Fatal(e)
	}
	changed := job
	changed.Acknowledged = true
	changed.ErrorCode = "changed"
	if _, e = store.Save(changed); e == nil {
		t.Fatal("acknowledgement changed outcome")
	}
	if e = store.Acknowledge(job.ID); e != nil {
		t.Fatal(e)
	}
	saved, e := store.Read(job.ID)
	if e != nil || !saved.Acknowledged || saved.Phase != JobCancelled {
		t.Fatal("acknowledgement not persisted", e)
	}
	if e = store.Acknowledge(job.ID); e != nil {
		t.Fatal("repeated acknowledgement failed", e)
	}
}
