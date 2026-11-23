package usbcopy

import (
	"strings"
	"testing"
)

func TestLiveJobCompletesWithoutStoppingOrRestartingEngine(t *testing.T) {
	store := jobFixture(t)
	j, e := store.Save(ImportJobStatus{ID: strings.Repeat("a", 64), Mode: "live", Phase: JobQueued, FilesTotal: 1, BytesTotal: 5})
	if e != nil {
		t.Fatal(e)
	}
	bad := j
	bad.Phase = JobStopping
	if _, e = store.Save(bad); e == nil {
		t.Fatal("live worker could stop Engine")
	}
	j.Phase = JobCopying
	j, e = store.Save(j)
	if e != nil {
		t.Fatal(e)
	}
	j.FilesDone = 1
	j.BytesDone = 5
	j.Phase = JobCommitting
	j, e = store.Save(j)
	if e != nil {
		t.Fatal(e)
	}
	bad = j
	bad.Phase = JobRestarting
	if _, e = store.Save(bad); e == nil {
		t.Fatal("live worker could enter Engine restart")
	}
	j.Phase = JobCompleted
	if _, e = store.Save(j); e == nil {
		t.Fatal("live completion accepted without checking Engine remains running")
	}
	j.EngineRestored = true
	j.ResultAvailable = true
	j.CopiedFiles = 1
	j.CreatedPlaylists = 1
	j.PlaylistName = "New songs"
	j, e = store.Save(j)
	if e != nil {
		t.Fatal(e)
	}
	read, e := store.Read(j.ID)
	if e != nil || read.Mode != "live" || read.Phase != JobCompleted {
		t.Fatal("lost live mode", e)
	}
	j.Phase = JobCopying
	if _, e = store.Save(j); e == nil {
		t.Fatal("completed live job resumed")
	}
}

func TestVerifiedReviewOnlyPermitsOneFinalMembershipStage(t *testing.T) {
	s := jobFixture(t)
	j, e := s.Save(ImportJobStatus{ID: strings.Repeat("c", 64), Mode: "live", Phase: JobQueued, FilesTotal: 1, BytesTotal: 5})
	if e != nil {
		t.Fatal(e)
	}
	j.Phase = JobCopying
	j, e = s.Save(j)
	if e != nil {
		t.Fatal(e)
	}
	j.FilesDone = 1
	j.BytesDone = 5
	j.Phase = JobCommitting
	j, e = s.Save(j)
	if e != nil {
		t.Fatal(e)
	}
	j.Phase = JobInterrupted
	j.EngineRestored = true
	j, e = s.Save(j)
	if e != nil {
		t.Fatal(e)
	}
	j, e = s.BeginVerifiedLiveFinalization(j.ID, j.Revision, strings.Repeat("e", 64))
	if e != nil || j.Phase != JobCommitting {
		t.Fatal("verified finalization unavailable", e)
	}
	bad := j
	bad.Phase = JobCopying
	if _, e = s.Save(bad); e == nil {
		t.Fatal("review permitted copying again")
	}
	j.Phase = JobInterrupted
	j, e = s.Save(j)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.BeginVerifiedLiveFinalization(j.ID, j.Revision, strings.Repeat("f", 64)); e == nil {
		t.Fatal("review reservation permitted automatic repeated finalization")
	}
}
func TestJobCannotSwitchBetweenOfflineAndLive(t *testing.T) {
	for _, mode := range []string{"", "offline", "live"} {
		store := jobFixture(t)
		j, e := store.Save(ImportJobStatus{ID: strings.Repeat("b", 64), Mode: mode, Phase: JobQueued})
		if e != nil {
			t.Fatal(e)
		}
		if mode == "live" {
			j.Mode = "offline"
			j.Phase = JobStopping
		} else {
			j.Mode = "live"
			j.Phase = JobCopying
		}
		if _, e = store.Save(j); e == nil {
			t.Fatalf("job switched from %q to %q", mode, j.Mode)
		}
	}
}

func TestVerifiedNoEffectsReviewClosesWithoutResumingActions(t *testing.T) {
	s := jobFixture(t)
	j, e := s.Save(ImportJobStatus{ID: strings.Repeat("d", 64), Mode: "live", Phase: JobQueued, FilesTotal: 3, BytesTotal: 50})
	if e != nil {
		t.Fatal(e)
	}
	j.Phase = JobInterrupted
	j.EngineRestored = true
	j, e = s.Save(j)
	if e != nil {
		t.Fatal(e)
	}
	bad := j
	bad.Phase = JobFailed
	if _, e = s.Save(bad); e == nil {
		t.Fatal("unreviewed terminal rewrite accepted")
	}
	if _, e = s.CloseVerifiedUnstartedLiveFailure(j.ID, j.Revision, "invalid"); e == nil {
		t.Fatal("missing evidence accepted")
	}
	closed, e := s.CloseVerifiedUnstartedLiveFailure(j.ID, j.Revision, strings.Repeat("e", 64))
	if e != nil || closed.Phase != JobFailed || !closed.Acknowledged || closed.FilesDone != 0 || closed.BytesDone != 0 {
		t.Fatal("bounded no-effects closure failed", closed, e)
	}
	if _, e = s.CloseVerifiedUnstartedLiveFailure(closed.ID, closed.Revision, strings.Repeat("f", 64)); e == nil {
		t.Fatal("repeated closure accepted")
	}
	closed.Phase = JobCopying
	if _, e = s.Save(closed); e == nil {
		t.Fatal("closed job resumed copying")
	}
}
func TestNoEffectsReviewRejectsAnyPublishedProgress(t *testing.T) {
	for _, progress := range []struct {
		files int
		bytes int64
	}{{1, 0}, {0, 1}} {
		s := jobFixture(t)
		j, e := s.Save(ImportJobStatus{ID: strings.Repeat("a", 64), Mode: "live", Phase: JobQueued, FilesTotal: 3, BytesTotal: 50})
		if e != nil {
			t.Fatal(e)
		}
		j.Phase = JobCopying
		j, e = s.Save(j)
		if e != nil {
			t.Fatal(e)
		}
		j.Phase = JobInterrupted
		j.EngineRestored = true
		j.FilesDone = progress.files
		j.BytesDone = progress.bytes
		j, e = s.Save(j)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = s.CloseVerifiedUnstartedLiveFailure(j.ID, j.Revision, strings.Repeat("e", 64)); e == nil {
			t.Fatal("progressful job accepted as no-effects")
		}
	}
}

func TestReviewedNativeCompletionCannotCopyOrProceedWithPartialBytes(t *testing.T) {
	s := jobFixture(t)
	j, e := s.Save(ImportJobStatus{ID: strings.Repeat("b", 64), Mode: "live", Phase: JobQueued, FilesTotal: 3, BytesTotal: 50})
	if e != nil {
		t.Fatal(e)
	}
	j.Phase = JobCopying
	j, e = s.Save(j)
	if e != nil {
		t.Fatal(e)
	}
	j.Phase = JobInterrupted
	j.FilesDone = 3
	j.BytesDone = 49
	j.EngineRestored = true
	j, e = s.Save(j)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.BeginVerifiedLiveNativeCompletion(j.ID, j.Revision, strings.Repeat("e", 64)); e == nil {
		t.Fatal("incomplete copied bytes accepted")
	}
	// A separate complete job can enter only committing, never copying.
	s = jobFixture(t)
	j, e = s.Save(ImportJobStatus{ID: strings.Repeat("c", 64), Mode: "live", Phase: JobQueued, FilesTotal: 3, BytesTotal: 50})
	if e != nil {
		t.Fatal(e)
	}
	j.Phase = JobCopying
	j, e = s.Save(j)
	if e != nil {
		t.Fatal(e)
	}
	j.Phase = JobInterrupted
	j.FilesDone = 3
	j.BytesDone = 50
	j.EngineRestored = true
	j, e = s.Save(j)
	if e != nil {
		t.Fatal(e)
	}
	j, e = s.BeginVerifiedLiveNativeCompletion(j.ID, j.Revision, strings.Repeat("e", 64))
	if e != nil || j.Phase != JobCommitting {
		t.Fatal(j, e)
	}
	j.Phase = JobCopying
	if _, e = s.Save(j); e == nil {
		t.Fatal("reviewed completion allowed another copy")
	}
}
