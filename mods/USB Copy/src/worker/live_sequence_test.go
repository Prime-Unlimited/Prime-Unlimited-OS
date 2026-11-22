package usbcopy

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

type memoryLiveJournal struct {
	created    bool
	states     []LiveSequenceState
	failIntent string
}

func (j *memoryLiveJournal) Create(_ context.Context, s LiveSequenceState) error {
	if j.created {
		return errors.New("already reserved")
	}
	j.created = true
	return j.Save(context.Background(), s)
}
func (j *memoryLiveJournal) Save(_ context.Context, s LiveSequenceState) error {
	if s.Intent == j.failIntent && s.Intent != "" {
		return errors.New("disk unavailable")
	}
	j.states = append(j.states, s)
	return nil
}

type fakeLiveAdapter struct {
	binding         LiveBinding
	calls           []string
	fail            string
	journal         *memoryLiveJournal
	registrations   int
	copied          ImportPlan
	added           [][]LiveTrackReference
	changeAfterCopy bool
	fingerprints    []DuplicateDecision
}

func (a *fakeLiveAdapter) Validate(context.Context) (LiveBinding, error) { return a.binding, nil }
func (a *fakeLiveAdapter) call(action string) error {
	a.calls = append(a.calls, action)
	last := a.journal.states[len(a.journal.states)-1]
	if last.Phase != "pending" || !strings.Contains(last.Intent, action) {
		return errors.New("intent not persisted before effect")
	}
	if a.fail == action {
		return errors.New("ambiguous native result")
	}
	return nil
}
func (a *fakeLiveAdapter) Backup(context.Context, string) error { return a.call("backup") }
func (a *fakeLiveAdapter) Copy(_ context.Context, _ string, p ImportPlan, d []DuplicateDecision) error {
	a.copied = p
	a.fingerprints = d
	e := a.call("copy")
	if a.changeAfterCopy {
		a.binding.EngineIdentity = "different"
	}
	return e
}
func (a *fakeLiveAdapter) Register(_ context.Context, _ string, t PlannedTrack) (LiveTrackReference, error) {
	a.registrations++
	e := a.call("register")
	return LiveTrackReference{SourceRelative: t.SourceRelative, NativeID: fmt.Sprint(100 + a.registrations), LocalID: int64(a.registrations), Binding: a.binding}, e
}
func (a *fakeLiveAdapter) CreatePlaylist(context.Context, string, string) (LivePlaylistReference, error) {
	e := a.call("create")
	return LivePlaylistReference{NativeID: fmt.Sprint(1000 + len(a.calls)), LocalID: int64(len(a.calls)), Binding: a.binding}, e
}
func (a *fakeLiveAdapter) Add(_ context.Context, _ string, _ LivePlaylistReference, refs []LiveTrackReference) error {
	a.added = append(a.added, refs)
	return a.call("add")
}
func (a *fakeLiveAdapter) Verify(context.Context, ImportPlan, LiveSequenceState) error {
	return a.call("verify")
}
func liveFixture() (ImportPlan, []DuplicateDecision) {
	p := ImportPlan{ImportDirectory: "copy", Tracks: []PlannedTrack{{SourceRelative: "a.mp3", DestinationRelative: "copy/a.mp3", Bytes: 5}, {SourceRelative: "b.mp3", DestinationRelative: "copy/b.mp3", Bytes: 5}, {SourceRelative: "c.mp3", DestinationRelative: "copy/c.mp3", Bytes: 5}}, CopyPlaylist: PlannedPlaylist{Name: "New songs", Tracks: []string{"a.mp3", "b.mp3", "c.mp3"}}, SourcePlaylists: []PlannedPlaylist{{Name: "USB playlist", Tracks: []string{"b.mp3", "c.mp3", "a.mp3"}}}}
	d := []DuplicateDecision{{SourceRelative: "a.mp3", Fingerprint: FileFingerprint{SHA256: strings.Repeat("a", 64), Bytes: 5}}, {SourceRelative: "b.mp3", Fingerprint: FileFingerprint{SHA256: strings.Repeat("b", 64), Bytes: 5}, ExistingTrackID: 77}, {SourceRelative: "c.mp3", Fingerprint: FileFingerprint{SHA256: strings.Repeat("a", 64), Bytes: 5}, DuplicateOfSource: "a.mp3"}}
	return p, d
}
func liveAdapter(j *memoryLiveJournal) *fakeLiveAdapter {
	return &fakeLiveAdapter{binding: LiveBinding{LibraryUUID: "library", MountIdentity: "sata", EngineIdentity: "boot:pid:start"}, journal: j}
}
func TestLiveSequenceOnlyCopiesAndListsNewFiles(t *testing.T) {
	p, d := liveFixture()
	j := &memoryLiveJournal{}
	a := liveAdapter(j)
	s, e := RunLiveSequence(context.Background(), strings.Repeat("a", 64), p, d, j, a)
	if e != nil {
		t.Fatal(e)
	}
	if s.Phase != "completed" || s.Skipped != 2 || len(s.Tracks) != 1 || len(s.Playlists) != 2 {
		t.Fatalf("state %+v", s)
	}
	if len(a.copied.Tracks) != 1 || a.copied.BytesToCopy != 5 || len(a.fingerprints) != 1 {
		t.Fatal("copied skipped files or lost fingerprints")
	}
	for _, refs := range a.added {
		if len(refs) != 1 || refs[0].SourceRelative != "a.mp3" {
			t.Fatalf("skipped track in playlist: %+v", refs)
		}
	}
	expected := []string{"backup", "copy", "register", "create", "add", "create", "add", "verify"}
	if !reflect.DeepEqual(a.calls, expected) {
		t.Fatalf("calls %v", a.calls)
	}
	before := len(a.calls)
	if _, e = RunLiveSequence(context.Background(), strings.Repeat("a", 64), p, d, j, a); e == nil || len(a.calls) != before {
		t.Fatal("replayed existing sequence")
	}
}
func TestLiveSequenceStopsAfterAmbiguousNativeResult(t *testing.T) {
	p, d := liveFixture()
	j := &memoryLiveJournal{}
	a := liveAdapter(j)
	a.fail = "register"
	s, e := RunLiveSequence(context.Background(), strings.Repeat("b", 64), p, d, j, a)
	if e == nil || s.Phase != "review_required" || a.registrations != 1 || len(a.added) != 0 {
		t.Fatalf("unexpected result %+v %v", s, e)
	}
}
func TestLiveSequenceNeverCallsAfterJournalFailure(t *testing.T) {
	p, d := liveFixture()
	id := strings.Repeat("c", 64)
	j := &memoryLiveJournal{failIntent: id + ":register:0"}
	a := liveAdapter(j)
	_, e := RunLiveSequence(context.Background(), id, p, d, j, a)
	if e == nil || a.registrations != 0 {
		t.Fatal("native call without persisted intent")
	}
}
func TestLiveSequenceStopsIfEngineChanges(t *testing.T) {
	p, d := liveFixture()
	j := &memoryLiveJournal{}
	a := liveAdapter(j)
	a.changeAfterCopy = true
	s, e := RunLiveSequence(context.Background(), strings.Repeat("d", 64), p, d, j, a)
	if e == nil || s.Phase != "review_required" || a.registrations != 0 {
		t.Fatal("used changed native namespace")
	}
}
func TestLiveSequenceAllSkippedCreatesNoPlaylist(t *testing.T) {
	p, d := liveFixture()
	d[0].ExistingTrackID = 88
	j := &memoryLiveJournal{}
	a := liveAdapter(j)
	s, e := RunLiveSequence(context.Background(), strings.Repeat("e", 64), p, d, j, a)
	if e != nil || s.Phase != "completed" || len(a.calls) != 0 || s.Skipped != 3 {
		t.Fatalf("unexpected all-skipped result %+v %v", s, e)
	}
}
func TestLivePlanRejectsBadAliasAndStaleFingerprint(t *testing.T) {
	p, d := liveFixture()
	d[2].DuplicateOfSource = "c.mp3"
	if _, _, e := LiveNewOnlyPlan(p, d); e == nil {
		t.Fatal("self alias accepted")
	}
	d[2].DuplicateOfSource = "a.mp3"
	d[2].Fingerprint.SHA256 = strings.Repeat("c", 64)
	if _, _, e := LiveNewOnlyPlan(p, d); e == nil {
		t.Fatal("different-content alias accepted")
	}
	d[2].Fingerprint.SHA256 = d[0].Fingerprint.SHA256
	d[0].Fingerprint.ModifiedAt = 1
	if _, _, e := LiveNewOnlyPlan(p, d); e == nil {
		t.Fatal("changed source timestamp accepted")
	}
}
