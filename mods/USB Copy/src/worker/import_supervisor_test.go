package usbcopy

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type testImportControl struct {
	failDisarm           bool
	events               []string
	validateCalls        int
	failSecondValidation bool
	failStart            bool
}

func (c *testImportControl) ValidateVolumes(context.Context) error {
	c.events = append(c.events, "validate")
	c.validateCalls++
	if c.failSecondValidation && c.validateCalls == 2 {
		return errors.New("removed source")
	}
	return nil
}
func (c *testImportControl) ArmRecovery(context.Context, string) error {
	c.events = append(c.events, "arm")
	return nil
}
func (c *testImportControl) StopEngine(context.Context) error {
	c.events = append(c.events, "stop")
	return nil
}
func (c *testImportControl) StartEngine(context.Context) error {
	c.events = append(c.events, "start")
	if c.failStart {
		return errors.New("engine recovery failed")
	}
	return nil
}
func (c *testImportControl) DisarmRecovery(context.Context, string) error {
	c.events = append(c.events, "disarm")
	if c.failDisarm {
		return errors.New("timer removal failed")
	}
	return nil
}
func queueFixtureJob(t *testing.T, store *JobStore, source string) ImportJobStatus {
	t.Helper()
	var bytes int64
	for _, name := range []string{"one.mp3", "two.mp3"} {
		s, e := os.Stat(filepath.Join(source, name))
		if e != nil {
			t.Fatal(e)
		}
		bytes += s.Size()
	}
	job, e := store.Save(ImportJobStatus{ID: strings.Repeat("a", 64), Phase: JobQueued, FilesTotal: 2, BytesTotal: bytes})
	if e != nil {
		t.Fatal(e)
	}
	return job
}
func TestImportSupervisorCompletesOnlyAfterEngineRecovery(t *testing.T) {
	source, target, proc := batchFixture(t)
	store := jobFixture(t)
	job := queueFixtureJob(t, store, source)
	control := &testImportControl{}
	result, e := SuperviseOfflineImport(context.Background(), store, job.ID, source, target, proc, control)
	if e != nil {
		t.Fatal(e)
	}
	if result.Copied != 2 {
		t.Fatal("missing imports")
	}
	saved, e := store.Read(job.ID)
	if e != nil || saved.Phase != JobCompleted || !saved.EngineRestored {
		t.Fatal("incorrect terminal status", e)
	}
	if !saved.ResultAvailable || saved.CopiedFiles != 2 || saved.SkippedFiles != 0 || saved.CreatedPlaylists != 2 || saved.PlaylistName == "" {
		t.Fatalf("missing saved outcome: %+v", saved)
	}
	if e = store.Acknowledge(saved.ID); e != nil {
		t.Fatal(e)
	}
	acked, e := store.Read(saved.ID)
	if e != nil || !acked.Acknowledged || acked.CopiedFiles != 2 || acked.PlaylistName != saved.PlaylistName {
		t.Fatal("acknowledgement lost outcome", e)
	}
	repeatID := strings.Repeat("b", 64)
	if _, e = store.Save(ImportJobStatus{ID: repeatID, Phase: JobQueued, FilesTotal: 2}); e != nil {
		t.Fatal(e)
	}
	if _, e = SuperviseOfflineImport(context.Background(), store, repeatID, source, target, proc, &testImportControl{}); e != nil {
		t.Fatal(e)
	}
	repeated, e := store.Read(repeatID)
	if e != nil || !repeated.ResultAvailable || repeated.CopiedFiles != 0 || repeated.SkippedFiles != 2 || repeated.CreatedPlaylists != 0 || repeated.PlaylistName != "" {
		t.Fatalf("wrong all-skipped outcome: %+v %v", repeated, e)
	}
	if strings.Join(control.events, ",") != "validate,arm,stop,validate,validate,start,disarm" {
		t.Fatal("unsafe lifecycle order", control.events)
	}
	if _, e = SuperviseOfflineImport(context.Background(), store, job.ID, source, target, proc, control); e == nil {
		t.Fatal("terminal job rerun")
	}
}
func TestImportSupervisorRestartsAfterRemovedSource(t *testing.T) {
	source, target, proc := batchFixture(t)
	store := jobFixture(t)
	job := queueFixtureJob(t, store, source)
	control := &testImportControl{failSecondValidation: true}
	if _, e := SuperviseOfflineImport(context.Background(), store, job.ID, source, target, proc, control); e == nil {
		t.Fatal("removed source accepted")
	}
	saved, e := store.Read(job.ID)
	if e != nil || saved.Phase != JobFailed || !saved.EngineRestored {
		t.Fatal("engine not recovered after failed validation", e)
	}
	if strings.Join(control.events, ",") != "validate,arm,stop,validate,start,disarm" {
		t.Fatal(control.events)
	}
}
func TestImportSupervisorKeepsRecoveryArmedOnRestartFailure(t *testing.T) {
	source, target, proc := batchFixture(t)
	store := jobFixture(t)
	job := queueFixtureJob(t, store, source)
	control := &testImportControl{failStart: true}
	if _, e := SuperviseOfflineImport(context.Background(), store, job.ID, source, target, proc, control); e == nil {
		t.Fatal("restart failure ignored")
	}
	saved, e := store.Read(job.ID)
	if e != nil || saved.Phase != JobInterrupted || saved.EngineRestored || saved.ErrorCode != "recovery_required" {
		t.Fatal("uncertain job not retained", e)
	}
	for _, event := range control.events {
		if event == "disarm" {
			t.Fatal("recovery timer disabled after restart failure")
		}
	}
}

func TestSupervisorBlocksRetryWhenRecoveryTimerCannotBeDisarmed(t *testing.T) {
	source, target, proc := batchFixture(t)
	store := jobFixture(t)
	job := queueFixtureJob(t, store, source)
	control := &testImportControl{failDisarm: true}
	if _, e := SuperviseOfflineImport(context.Background(), store, job.ID, source, target, proc, control); e == nil {
		t.Fatal("timer cleanup failure ignored")
	}
	saved, e := store.Read(job.ID)
	if e != nil || saved.Phase != JobInterrupted || !saved.EngineRestored || saved.ErrorCode != "recovery_required" {
		t.Fatal("retry not blocked after uncertain timer cleanup", e)
	}
}
