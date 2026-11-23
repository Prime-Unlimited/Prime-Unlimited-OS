package usbcopy

import (
	"context"
	"errors"
	"golang.org/x/sys/unix"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func commandFixture(t *testing.T) (*FileLiveCommands, LiveCommand) {
	t.Helper()
	parent, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	job := strings.Repeat("a", 64)
	q, e := OpenFileLiveCommands(filepath.Join(parent, "commands"), job)
	if e != nil {
		t.Fatal(e)
	}
	c := LiveCommand{JobID: job, Intent: job + ":select_library", Operation: "select_library", Binding: LiveBinding{"lib", "sata", "boot:pid:start"}, ExpiresAt: time.Now().Add(time.Minute)}
	c.ID = LiveCommandID(c.JobID, c.Intent)
	return q, c
}

func TestNativeClaimWaitsForBriefReaderLockWithoutReplaying(t *testing.T) {
	q, c := commandFixture(t)
	ctx := context.Background()
	if e := q.Enqueue(ctx, c, time.Now()); e != nil {
		t.Fatal(e)
	}
	f, e := os.OpenFile(filepath.Join(q.root, "commands.lock"), os.O_RDWR, 0)
	if e != nil {
		t.Fatal(e)
	}
	defer f.Close()
	if e = unix.Flock(int(f.Fd()), unix.LOCK_EX); e != nil {
		t.Fatal(e)
	}
	done := make(chan error, 1)
	go func() { _, e := q.Claim(ctx, c.ID, c.Binding, time.Now()); done <- e }()
	select {
	case e := <-done:
		t.Fatalf("did not wait for reader: %v", e)
	case <-time.After(20 * time.Millisecond):
	}
	if e = unix.Flock(int(f.Fd()), unix.LOCK_UN); e != nil {
		t.Fatal(e)
	}
	if e = <-done; e != nil {
		t.Fatal(e)
	}
	if _, e = q.Claim(ctx, c.ID, c.Binding, time.Now()); e == nil {
		t.Fatal("brief lock wait permitted duplicate invocation")
	}
}
func TestNativeClaimCancelledWhileWaitingLeavesCommandQueued(t *testing.T) {
	q, c := commandFixture(t)
	ctx := context.Background()
	if e := q.Enqueue(ctx, c, time.Now()); e != nil {
		t.Fatal(e)
	}
	f, e := os.OpenFile(filepath.Join(q.root, "commands.lock"), os.O_RDWR, 0)
	if e != nil {
		t.Fatal(e)
	}
	defer f.Close()
	if e = unix.Flock(int(f.Fd()), unix.LOCK_EX); e != nil {
		t.Fatal(e)
	}
	wait, cancel := context.WithTimeout(ctx, 10*time.Millisecond)
	defer cancel()
	if _, e = q.Claim(wait, c.ID, c.Binding, time.Now()); !errors.Is(e, context.DeadlineExceeded) {
		t.Fatalf("lock wait cancellation %v", e)
	}
	if e = unix.Flock(int(f.Fd()), unix.LOCK_UN); e != nil {
		t.Fatal(e)
	}
	r, e := q.Read(ctx)
	if e != nil || r.Phase != "queued" {
		t.Fatal("cancelled lock wait consumed command")
	}
}
func TestNativeAddRequiresURIProofsAndUniqueLocalSources(t *testing.T) {
	_, c := commandFixture(t)
	c.Operation = "add_tracks"
	c.Playlist = &LivePlaylistReference{NativeID: "202", LocalID: 201, Binding: c.Binding, Title: "New import"}
	c.Tracks = []LiveTrackReference{{SourceRelative: "new.mp3", NativeID: "2251799813701950", LocalID: 16702, Binding: c.Binding}}
	if validLiveCommand(c) {
		t.Fatal("add accepted without native URI proof")
	}
	c.TrackURIs = []string{"file:///media/Denon/Engine%20Library/Music/new.mp3"}
	if !validLiveCommand(c) {
		t.Fatal("verified add rejected")
	}
	other := c.Tracks[0]
	other.NativeID = "2251799813701951"
	other.SourceRelative = "other.mp3"
	c.Tracks = append(c.Tracks, other)
	c.TrackURIs = append(c.TrackURIs, c.TrackURIs[0])
	if validLiveCommand(c) {
		t.Fatal("same SQL track accepted under two native IDs")
	}
}
func TestNativeCommandClaimSurvivesRestartWithoutReplay(t *testing.T) {
	q, c := commandFixture(t)
	ctx := context.Background()
	if e := q.Enqueue(ctx, c, time.Now()); e != nil {
		t.Fatal(e)
	}
	if _, e := q.Claim(ctx, c.ID, c.Binding, time.Now()); e != nil {
		t.Fatal(e)
	}
	other, e := OpenFileLiveCommands(q.root, c.JobID)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = other.Claim(ctx, c.ID, c.Binding, time.Now()); e == nil {
		t.Fatal("claimed native call replayed after actor restart")
	}
	if e = other.Enqueue(ctx, c, time.Now()); e == nil {
		t.Fatal("ambiguous command overwritten")
	}
	next := c
	next.Intent = c.JobID + ":copy"
	next.ID = LiveCommandID(next.JobID, next.Intent)
	if e = other.Enqueue(ctx, next, time.Now()); e == nil {
		t.Fatal("advanced past ambiguous native command")
	}
}
func TestNativeCommandConcurrentActorsOnlyOneCanClaim(t *testing.T) {
	q, c := commandFixture(t)
	ctx := context.Background()
	if e := q.Enqueue(ctx, c, time.Now()); e != nil {
		t.Fatal(e)
	}
	var wins atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			other, e := OpenFileLiveCommands(q.root, c.JobID)
			if e != nil {
				return
			}
			if _, e = other.Claim(ctx, c.ID, c.Binding, time.Now()); e == nil {
				wins.Add(1)
			}
		}()
	}
	wg.Wait()
	if wins.Load() != 1 {
		t.Fatalf("native invocation claims: %d", wins.Load())
	}
}
func TestNativeAcknowledgementRequiresIndependentVerification(t *testing.T) {
	q, c := commandFixture(t)
	ctx := context.Background()
	if e := q.Enqueue(ctx, c, time.Now()); e != nil {
		t.Fatal(e)
	}
	wrong := c.Binding
	wrong.EngineIdentity = "restarted"
	if _, e := q.Claim(ctx, c.ID, wrong, time.Now()); e == nil {
		t.Fatal("new Engine reused old command")
	}
	if _, e := q.Claim(ctx, c.ID, c.Binding, time.Now()); e != nil {
		t.Fatal(e)
	}
	if e := q.Observe(ctx, c.ID, LiveCommandObservation{Outcome: "applied", Binding: c.Binding}, time.Now()); e != nil {
		t.Fatal(e)
	}
	r, e := q.Read(ctx)
	if e != nil || r.Phase != "observed" {
		t.Fatalf("ack treated as verification: %+v %v", r, e)
	}
	if e = q.Verify(ctx, c.ID, c.Binding, nil); e == nil {
		t.Fatal("verified without native/library evidence")
	}
	if e = q.Verify(ctx, c.ID, c.Binding, func(LiveCommand, LiveCommandObservation) error { return errors.New("wrong library selected") }); e == nil {
		t.Fatal("ignored actual verification failure")
	}
	r, e = q.Read(ctx)
	if e != nil || r.Phase != "review_required" {
		t.Fatalf("lost failure: %+v %v", r, e)
	}
}
func TestNativeCommandCompletionAllowsNextButNeverSameIntent(t *testing.T) {
	q, c := commandFixture(t)
	ctx := context.Background()
	if e := q.Enqueue(ctx, c, time.Now()); e != nil {
		t.Fatal(e)
	}
	if _, e := q.Claim(ctx, c.ID, c.Binding, time.Now()); e != nil {
		t.Fatal(e)
	}
	if e := q.Observe(ctx, c.ID, LiveCommandObservation{Outcome: "applied", Binding: c.Binding}, time.Now()); e != nil {
		t.Fatal(e)
	}
	if e := q.Verify(ctx, c.ID, c.Binding, func(LiveCommand, LiveCommandObservation) error { return nil }); e != nil {
		t.Fatal(e)
	}
	if e := q.Enqueue(ctx, c, time.Now()); e == nil {
		t.Fatal("replayed completed operation")
	}
	next := c
	next.Operation = "all_tracks"
	next.Intent = c.JobID + ":all_tracks"
	next.ID = LiveCommandID(next.JobID, next.Intent)
	if e := q.Enqueue(ctx, next, time.Now()); e != nil {
		t.Fatal(e)
	}
	data, e := os.ReadFile(filepath.Join(q.root, c.ID+".completed.json"))
	if e != nil || !strings.Contains(string(data), `"phase":"verified"`) {
		t.Fatalf("lost preceding verified evidence: %s %v", data, e)
	}
}

func TestNativeCommandWaitTimeoutNeverReclaimsOrResubmits(t *testing.T) {
	q, c := commandFixture(t)
	ctx := context.Background()
	if e := q.Enqueue(ctx, c, time.Now()); e != nil {
		t.Fatal(e)
	}
	if _, e := q.Claim(ctx, c.ID, c.Binding, time.Now()); e != nil {
		t.Fatal(e)
	}
	wait, cancel := context.WithTimeout(ctx, 10*time.Millisecond)
	defer cancel()
	if _, e := q.AwaitObservation(wait, c); !errors.Is(e, context.DeadlineExceeded) {
		t.Fatalf("timeout %v", e)
	}
	r, e := q.Read(ctx)
	if e != nil || r.Phase != "claimed" {
		t.Fatalf("timeout altered durable claim: %+v %v", r, e)
	}
	if _, e = q.Claim(ctx, c.ID, c.Binding, time.Now()); e == nil {
		t.Fatal("timeout allowed native replay")
	}
}

func TestNativeCommandWaitReturnsEvidenceWithoutTrustingIt(t *testing.T) {
	q, c := commandFixture(t)
	ctx := context.Background()
	if e := q.Enqueue(ctx, c, time.Now()); e != nil {
		t.Fatal(e)
	}
	if _, e := q.Claim(ctx, c.ID, c.Binding, time.Now()); e != nil {
		t.Fatal(e)
	}
	if e := q.Observe(ctx, c.ID, LiveCommandObservation{Outcome: "applied", Binding: c.Binding}, time.Now()); e != nil {
		t.Fatal(e)
	}
	o, e := q.AwaitObservation(ctx, c)
	if e != nil || o.Outcome != "applied" {
		t.Fatalf("observation %+v %v", o, e)
	}
	r, e := q.Read(ctx)
	if e != nil || r.Phase != "observed" {
		t.Fatal("wait bypassed independent verification")
	}
}
func TestNativeCommandRejectsCrashReservationStaleAndArbitraryCalls(t *testing.T) {
	q, c := commandFixture(t)
	ctx := context.Background()
	c.Operation = "execute_sql"
	if e := q.Enqueue(ctx, c, time.Now()); e == nil {
		t.Fatal("arbitrary native operation accepted")
	}
	c.Operation = "register_files"
	c.Paths = []string{"../outside.mp3"}
	if e := q.Enqueue(ctx, c, time.Now()); e == nil {
		t.Fatal("unapproved path accepted")
	}
	c.Paths = []string{"Music/new.mp3"}
	if e := q.Enqueue(ctx, c, c.ExpiresAt); e == nil {
		t.Fatal("expired command accepted")
	}
	if e := os.WriteFile(filepath.Join(q.root, c.ID+".reserved"), []byte(c.ID), 0600); e != nil {
		t.Fatal(e)
	}
	if e := q.Enqueue(ctx, c, time.Now()); e == nil {
		t.Fatal("crash reservation replayed")
	}
}
