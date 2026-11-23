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
