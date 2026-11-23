package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
	usbcopy "prime-unlimited.usb-copy/worker"
)

const reviewedLiveID = "f86093c8943c85c109f6bcec9457c4e8412e20d50871079edb244019b076de60"

type reviewedLiveInputs struct {
	Adapter  *mediaLiveAdapter
	Store    *usbcopy.JobStore
	Job      usbcopy.ImportJobStatus
	State    usbcopy.LiveSequenceState
	Command  usbcopy.LiveCommandRecord
	Plan     usbcopy.ImportPlan
	Playlist usbcopy.LivePlaylistReference
	Probe    json.RawMessage
}

func loadReviewedLiveInputs(ctx context.Context, needNative bool) (out reviewedLiveInputs, err error) {
	const root = "/data/prime-unlimited"
	id := reviewedLiveID
	out.Store, err = usbcopy.OpenJobStore(filepath.Join(root, "media-jobs"))
	if err != nil {
		return
	}
	jobs, e := out.Store.List()
	if e != nil {
		return out, e
	}
	for _, j := range jobs {
		switch j.Phase {
		case usbcopy.JobCompleted, usbcopy.JobFailed, usbcopy.JobCancelled:
		case usbcopy.JobInterrupted:
			if j.ID != id {
				return out, errors.New("another job needs review")
			}
		default:
			return out, errors.New("another job is active")
		}
	}
	out.Job, err = out.Store.Read(id)
	if err != nil {
		return
	}
	if out.Job.Mode != "live" || out.Job.Phase != usbcopy.JobInterrupted || !out.Job.EngineRestored || out.Job.FilesDone != 4 || out.Job.FilesTotal != 4 || out.Job.BytesDone != 18371297 || out.Job.ResultAvailable {
		return out, errors.New("reviewed job differs from the bounded trial")
	}
	request, e := readMediaJobRequest(root, id)
	if e != nil {
		return out, e
	}
	session, e := readMediaLiveSession(root, id)
	if e != nil {
		return out, e
	}
	control, e := newMediaEngineController("JP24", request.Mounts)
	if e != nil {
		return out, e
	}
	control.validateDestination = func(ctx context.Context, v mountedBlockVolume) error {
		actual, e := readMediaDestinationIdentity(ctx, v)
		if e != nil {
			return e
		}
		if actual != request.DestinationIdentity {
			return errors.New("review library changed")
		}
		return nil
	}
	validate := func(ctx context.Context) (usbcopy.LiveBinding, error) {
		if e := control.ValidateVolumes(ctx); e != nil {
			return usbcopy.LiveBinding{}, e
		}
		engine, e := nativeEngineIdentity()
		if e != nil || engine != session.Binding.EngineIdentity {
			return usbcopy.LiveBinding{}, errors.New("review Engine changed")
		}
		return session.Binding, nil
	}
	if _, e = validate(ctx); e != nil {
		return out, e
	}
	dir, e := mediaLiveDirectory(root, id)
	if e != nil {
		return out, e
	}
	journal, e := usbcopy.OpenFileLiveJournal(filepath.Join(dir, "journal"))
	if e != nil {
		return out, e
	}
	out.State, e = journal.Read(id)
	if e != nil {
		return out, e
	}
	if out.State.Phase != "review_required" || out.State.Intent != id+":create:0" || len(out.State.Tracks) != 1 || len(out.State.Playlists) != 0 || out.State.Skipped != 3 || out.State.Binding != session.Binding {
		return out, errors.New("review journal stage differs")
	}
	queue, e := usbcopy.OpenFileLiveCommands(filepath.Join(dir, "commands"), id)
	if e != nil {
		return out, e
	}
	out.Command, e = queue.Read(ctx)
	if e != nil {
		return out, e
	}
	if out.Command.Command.Operation != "create_playlist" || out.Command.Phase != "review_required" || out.Command.Observation == nil || out.Command.Observation.Outcome != "uncertain" {
		return out, errors.New("native creation outcome differs")
	}
	var manifest struct {
		Binding         usbcopy.LiveBinding
		ImportDirectory string
		Tracks          []struct {
			Source, Destination, SHA256, ArtworkSHA1 string
			Bytes                                    int64
			Tags                                     usbcopy.Tags
		}
	}
	b, e := readOwnedFile(filepath.Join(dir, "manifest.json"), 1<<20, "review manifest")
	if e != nil || decodeStrictJSON(b, &manifest) != nil {
		return out, errors.New("review manifest unavailable")
	}
	if manifest.Binding != session.Binding || len(manifest.Tracks) != 1 {
		return out, errors.New("review manifest binding differs")
	}
	m := manifest.Tracks[0]
	if m.SHA256 != "25329649045fa63536308e244770369cd1d0252b90fe3222fbaacb8202be967b" || !strings.HasPrefix(m.Destination, "Music/") {
		return out, errors.New("review copy differs")
	}
	fp, e := usbcopy.FingerprintAudioFile(ctx, filepath.Join(request.Mounts.Destination.Root, "Engine Library"), m.Destination)
	if e != nil || fp.SHA256 != m.SHA256 || fp.Bytes != m.Bytes {
		return out, errors.New("reviewed copy changed")
	}
	snapshot, e := usbcopy.Scan(request.Mounts.Source.Root)
	if e != nil {
		return out, e
	}
	var source usbcopy.Track
	for _, s := range snapshot.Tracks {
		if s.RelativePath == m.Source {
			source = s
		}
	}
	sourceFP, e := usbcopy.FingerprintAudioFile(ctx, request.Mounts.Source.Root, m.Source)
	if e != nil || sourceFP.SHA256 != m.SHA256 {
		return out, errors.New("review source changed")
	}
	planned := usbcopy.PlannedTrack{SourceRelative: m.Source, DestinationRelative: strings.TrimPrefix(m.Destination, "Music/"), Bytes: m.Bytes, Tags: m.Tags, Artwork: source.Artwork}
	database, e := internalEngineDatabase(request.Mounts.Destination.Root)
	if e != nil {
		return out, e
	}
	registered, e := liveRegistered(ctx, database, request.Mounts.Destination.Root, session.Binding.LibraryUUID, planned)
	if e != nil {
		return out, e
	}
	ref := out.State.Tracks[0]
	var uri string
	files, e := os.ReadDir(filepath.Join(dir, "commands"))
	if e != nil {
		return out, e
	}
	for _, f := range files {
		if !strings.HasSuffix(f.Name(), ".completed.json") {
			continue
		}
		data, e := readOwnedFile(filepath.Join(dir, "commands", f.Name()), 1<<20, "review native evidence")
		if e != nil {
			return out, e
		}
		var r usbcopy.LiveCommandRecord
		if decodeStrictJSON(data, &r) != nil {
			return out, errors.New("invalid native evidence")
		}
		if r.Command.Operation == "lookup_tracks" && r.Phase == "verified" && r.Observation != nil && len(r.Observation.Tracks) == 1 && len(r.Observation.URIs) == 1 && r.Observation.Tracks[0].NativeID == ref.NativeID {
			uri = r.Observation.URIs[0]
		}
	}
	if uri == "" {
		return out, errors.New("verified native URI missing")
	}
	if e = liveCheckNativeTrack(ctx, database, request.Mounts.Destination.Root, registered, ref, uri, session.Binding); e != nil {
		return out, e
	}
	db, e := openNativeReadDB(database)
	if e != nil {
		return out, e
	}
	defer db.Close()
	var title = out.Command.Command.Title
	var listID int64
	var matching int
	if e = db.QueryRowContext(ctx, "SELECT count(*),coalesce(max(id),0) FROM Playlist WHERE title=? AND parentListId=0", title).Scan(&matching, &listID); e != nil {
		return out, e
	}
	if matching != 1 {
		return out, errors.New("created playlist not unique")
	}
	members, e := liveMembers(ctx, db, listID)
	if e != nil || len(members) != 0 {
		return out, errors.New("reviewed playlist is not empty")
	}
	var backup struct {
		Path   string
		Counts nativeLibraryCounts
	}
	data, e := readOwnedFile(filepath.Join(dir, "backup.json"), 8192, "review backup")
	if e != nil || decodeStrictJSON(data, &backup) != nil {
		return out, errors.New("backup evidence missing")
	}
	counts, e := nativeCounts(ctx, db)
	if e != nil {
		return out, e
	}
	if counts.Tracks != backup.Counts.Tracks+1 || counts.Playlists != backup.Counts.Playlists+1 || counts.Entities != backup.Counts.Entities {
		return out, errors.New("unexpected library changes since trial snapshot")
	}
	out.Playlist = usbcopy.LivePlaylistReference{LocalID: listID, Binding: session.Binding, Title: title}
	if needNative {
		data, e := readOwnedFile("/tmp/usb-copy-live-playlist-review.json", 8192, "fresh native playlist proof")
		if e != nil {
			return out, e
		}
		var p struct {
			Observation *nativePlaylistObservation `json:"observation"`
			ReceivedAt  time.Time                  `json:"receivedAt"`
			Enabled     bool                       `json:"enabled"`
			Revision    string                     `json:"scriptRevision"`
		}
		if decodeStrictJSON(data, &p) != nil || p.Observation == nil || time.Since(p.ReceivedAt) > 60*time.Second || len(p.Observation.Instances) != 1 || p.Observation.Errors != 0 {
			return out, errors.New("fresh native playlist proof required")
		}
		v := p.Observation.Instances[0]
		if v.SourceDevice != request.Mounts.Destination.Root || !v.CanAdd || v.SelectedListID == "" {
			return out, errors.New("native playlist target differs")
		}
		out.Playlist.NativeID = v.SelectedListID
		out.Probe = data
	}
	out.Plan = usbcopy.ImportPlan{ImportDirectory: manifest.ImportDirectory, Tracks: []usbcopy.PlannedTrack{planned}, CopyPlaylist: usbcopy.PlannedPlaylist{Name: title, Tracks: []string{m.Source}}, BytesToCopy: m.Bytes}
	out.Adapter = &mediaLiveAdapter{id: id, root: root, database: database, source: request.Mounts.Source.Root, destination: request.Mounts.Destination.Root, binding: session.Binding, queue: queue, plan: out.Plan, decisions: []usbcopy.DuplicateDecision{{SourceRelative: m.Source, Fingerprint: sourceFP}}, skipped: 3, validate: validate, registered: map[string]liveRegisteredTrack{m.Source: registered}, refs: map[string]usbcopy.LiveTrackReference{m.Source: ref}, uris: map[string]string{m.Source: uri}, lists: map[int64]liveCreatedList{listID: {Ref: out.Playlist, Name: title}}}
	return out, nil
}
func lockReviewedLiveLeases(t *testing.T) func() {
	t.Helper()
	files := []*os.File{}
	for _, name := range []string{"admission.lock", "supervisor.lock"} {
		f, e := os.OpenFile(filepath.Join("/data/prime-unlimited/media-jobs", name), os.O_RDWR|unix.O_NOFOLLOW, 0)
		if e != nil {
			t.Fatal(e)
		}
		if e = unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); e != nil {
			f.Close()
			t.Fatal(e)
		}
		files = append(files, f)
	}
	return func() {
		for i := len(files) - 1; i >= 0; i-- {
			unix.Flock(int(files[i].Fd()), unix.LOCK_UN)
			files[i].Close()
		}
	}
}
func TestInstallExplicitlyReviewedLiveRuntime(t *testing.T) {
	if os.Getenv("PRIME_USB_REVIEW_UPDATE") != "1" {
		t.Skip("explicit root maintenance opt-in required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	release := lockReviewedLiveLeases(t)
	defer release()
	if _, e := loadReviewedLiveInputs(ctx, false); e != nil {
		t.Fatal(e)
	}
	cmd := exec.CommandContext(ctx, "/bin/sh", "/tmp/install-reviewed-native.sh")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if e := cmd.Run(); e != nil {
		t.Fatal(e)
	}
}
func TestFinalizeExplicitlyReviewedLiveDevice(t *testing.T) {
	if os.Getenv("PRIME_USB_LIVE_FINALIZE") != reviewedLiveID {
		t.Skip("explicit original trial finalization required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	release := lockReviewedLiveLeases(t)
	defer release()
	in, e := loadReviewedLiveInputs(ctx, true)
	if e != nil {
		t.Fatal(e)
	}
	dir, e := mediaLiveDirectory("/data/prime-unlimited", reviewedLiveID)
	if e != nil {
		t.Fatal(e)
	}
	evidence := struct {
		Original    usbcopy.LiveCommandRecord
		Playlist    usbcopy.LivePlaylistReference
		NativeProof json.RawMessage
		At          time.Time
	}{in.Command, in.Playlist, in.Probe, time.Now().UTC()}
	data, e := json.Marshal(evidence)
	if e != nil {
		t.Fatal(e)
	}
	sum := sha256.Sum256(data)
	if e = writeAtomicJSON(filepath.Join(dir, "explicit-review.json"), evidence); e != nil {
		t.Fatal(e)
	}
	// The original create is adopted only after independent SQL, native model,
	// file, artwork, URI and unchanged-instance review. It is never called again.
	in.Command.Phase = "verified"
	in.Command.Observation = &usbcopy.LiveCommandObservation{Outcome: "applied", Reason: "verified_after_explicit_read_only_review", Binding: in.Adapter.binding, SourceDevice: in.Adapter.destination, CollectionDevice: in.Adapter.destination, LibraryUUID: in.Adapter.binding.LibraryUUID, Playlist: &in.Playlist}
	f, e := os.OpenFile(filepath.Join(dir, "commands", "commands.lock"), os.O_RDWR|unix.O_NOFOLLOW, 0)
	if e != nil {
		t.Fatal(e)
	}
	if e = unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); e != nil {
		f.Close()
		t.Fatal(e)
	}
	e = writeAtomicJSON(filepath.Join(dir, "commands", "command.json"), in.Command)
	unix.Flock(int(f.Fd()), unix.LOCK_UN)
	f.Close()
	if e != nil {
		t.Fatal(e)
	}
	job, e := in.Store.BeginVerifiedLiveFinalization(reviewedLiveID, in.Job.Revision, hex.EncodeToString(sum[:]))
	if e != nil {
		t.Fatal(e)
	}
	completed := false
	defer func() {
		if !completed {
			latest, e := in.Store.Read(reviewedLiveID)
			if e == nil {
				latest.Phase = usbcopy.JobInterrupted
				latest.ErrorCode = "live_review_required"
				_, _ = in.Store.Save(latest)
			}
		}
	}()
	if e = in.Adapter.Add(ctx, reviewedLiveID+":review:add:0", in.Playlist, in.State.Tracks); e != nil {
		t.Fatal(e)
	}
	in.State.Playlists = []usbcopy.LivePlaylistReference{in.Playlist}
	if e = in.Adapter.Verify(ctx, in.Plan, in.State); e != nil {
		t.Fatal(e)
	}
	in.State.Phase = "completed"
	in.State.Intent = ""
	if e = writeAtomicJSON(filepath.Join(dir, "review-completed.json"), in.State); e != nil {
		t.Fatal(e)
	}
	job.Phase = usbcopy.JobCompleted
	job.EngineRestored = true
	job.ResultAvailable = true
	job.CopiedFiles = 1
	job.SkippedFiles = 3
	job.CreatedPlaylists = 1
	job.PlaylistName = in.Playlist.Title
	if _, e = in.Store.Save(job); e != nil {
		t.Fatal(e)
	}
	completed = true
	t.Logf("completed original trial without copy/register/create replay: %s", job.PlaylistName)
}
