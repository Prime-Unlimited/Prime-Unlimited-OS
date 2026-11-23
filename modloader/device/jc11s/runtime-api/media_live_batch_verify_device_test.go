package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"golang.org/x/sys/unix"
	"os"
	"path/filepath"
	usbcopy "prime-unlimited.usb-copy/worker"
	"reflect"
	"strings"
	"testing"
	"time"
)

// Read-only post-import verification; never starts, resumes or repairs a job.
func TestVerifyCompletedLiveBatchDevice(t *testing.T) {
	id := os.Getenv("PRIME_USB_VERIFY_BATCH")
	if id == "" {
		t.Skip("explicit completed batch ID required")
	}
	if !validMediaEventID(id) || os.Geteuid() != 0 {
		t.Fatal("trusted completed job ID required")
	}
	dir := filepath.Join("/data/prime-unlimited/media-jobs/live", id)
	if info, e := os.Lstat(dir); e != nil || !info.IsDir() {
		t.Fatal("completed live directory required", e)
	}
	read := func(name string, out any) {
		t.Helper()
		path := filepath.Join(dir, name)
		if name == "batch-approved-preview.json" {
			path = filepath.Join("/data/prime-unlimited/media-jobs", "diagnostics", "batch-approved-"+id+".json")
		}
		b, e := os.ReadFile(path)
		if e != nil {
			t.Fatal(e)
		}
		if e = json.Unmarshal(b, out); e != nil {
			t.Fatal(e)
		}
	}
	var approved mediaPreviewState
	read("batch-approved-preview.json", &approved)
	var state usbcopy.LiveSequenceState
	read("journal/"+id+".json", &state)
	store, e := usbcopy.OpenJobStore("/data/prime-unlimited/media-jobs")
	if e != nil {
		t.Fatal(e)
	}
	final, e := store.Read(id)
	if e != nil {
		t.Fatal(e)
	}
	if final.Phase != usbcopy.JobCompleted || state.Phase != "completed" || final.Mode != "live" || len(state.Tracks) != approved.DuplicateSummary.NewFiles || final.CopiedFiles != len(state.Tracks) || final.SkippedFiles != approved.DuplicateSummary.ExistingFiles+approved.DuplicateSummary.SourceDuplicates || final.CreatedPlaylists != len(state.Playlists) {
		t.Fatal("batch incomplete or counts differ", final)
	}
	engine, e := nativeEngineIdentity()
	if e != nil || engine != state.Binding.EngineIdentity {
		t.Fatal("Engine instance differs", e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	database, e := internalEngineDatabase(approved.DestinationRoot)
	if e != nil {
		t.Fatal(e)
	}
	db, e := openNativeReadDB(database)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	byID := map[int64]string{}
	newSources := map[string]bool{}
	sourceTracks := map[string]usbcopy.PlannedTrack{}
	destToSource := map[string]string{}
	for _, tr := range approved.Plan.Tracks {
		sourceTracks[tr.SourceRelative] = tr
		destToSource[tr.DestinationRelative] = tr.SourceRelative
	}
	for _, ref := range state.Tracks {
		byID[ref.LocalID] = ref.SourceRelative
		newSources[ref.SourceRelative] = true
	}
	ordered := func(listID int64) []string {
		t.Helper()
		rows, e := db.QueryContext(ctx, `SELECT p.id,p.nextEntityId,t.id,t.path FROM PlaylistEntity p JOIN Track t ON t.originDatabaseUuid=p.databaseUuid AND t.originTrackId=p.trackId WHERE p.listId=?`, listID)
		if e != nil {
			t.Fatal(e)
		}
		type row struct {
			id, next, track int64
			path            string
		}
		all := map[int64]row{}
		pointed := map[int64]bool{}
		for rows.Next() {
			var r row
			var next sql.NullInt64
			if e = rows.Scan(&r.id, &next, &r.track, &r.path); e != nil {
				rows.Close()
				t.Fatal(e)
			}
			if next.Valid {
				r.next = next.Int64
			}
			all[r.id] = r
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			t.Fatal(e)
		}
		for _, r := range all {
			if _, ok := all[r.next]; ok {
				pointed[r.next] = true
			}
		}
		heads := 0
		var head int64
		for id := range all {
			if !pointed[id] {
				head = id
				heads++
			}
		}
		if heads != 1 {
			t.Fatal("ambiguous playlist order")
		}
		seen := map[int64]bool{}
		out := []string{}
		for {
			r, ok := all[head]
			if !ok {
				break
			}
			if seen[head] {
				t.Fatal("playlist cycle")
			}
			seen[head] = true
			source, ok := byID[r.track]
			if !ok {
				t.Fatal("playlist includes skipped or unexpected track")
			}
			out = append(out, source)
			head = r.next
		}
		if len(out) != len(all) {
			t.Fatal("disconnected playlist chain")
		}
		return out
	}
	expected := func(p usbcopy.PlannedPlaylist) []string {
		out := []string{}
		seen := map[string]bool{}
		for _, dest := range p.Tracks {
			s := destToSource[dest]
			if newSources[s] && !seen[s] {
				out = append(out, s)
				seen[s] = true
			}
		}
		return out
	}
	planned := append([]usbcopy.PlannedPlaylist{approved.Plan.CopyPlaylist}, approved.Plan.SourcePlaylists...)
	cursor := 0
	for _, p := range planned {
		want := expected(p)
		if len(want) == 0 {
			continue
		}
		if cursor >= len(state.Playlists) {
			t.Fatal("missing expected playlist")
		}
		ref := state.Playlists[cursor]
		cursor++
		if !reflect.DeepEqual(ordered(ref.LocalID), want) {
			t.Fatalf("playlist order differs: %s", ref.Title)
		}
		t.Logf("verified playlist order: %s (%d newly copied members)", ref.Title, len(want))
	}
	if cursor != len(state.Playlists) {
		t.Fatal("unexpected extra playlist")
	}
	formats := map[string]int{}
	fallback := 0
	for i, ref := range state.Tracks {
		tr, ok := sourceTracks[ref.SourceRelative]
		if !ok {
			t.Fatal("unknown copied source")
		}
		var dest string
		if e = db.QueryRowContext(ctx, "SELECT path FROM Track WHERE id=?", ref.LocalID).Scan(&dest); e != nil {
			t.Fatal(e)
		}
		if !strings.HasPrefix(dest, "Music/") {
			t.Fatal("destination outside Music")
		}
		tr.DestinationRelative = strings.TrimPrefix(dest, "Music/")
		if _, e = liveRegistered(ctx, database, approved.DestinationRoot, state.Binding.LibraryUUID, tr); e != nil {
			t.Fatalf("metadata/artwork verification: %s: %v", tr.SourceRelative, e)
		}
		a, e := usbcopy.FingerprintAudioFile(ctx, approved.SourceRoot, tr.SourceRelative)
		if e != nil {
			t.Fatal(e)
		}
		b, e := usbcopy.FingerprintAudioFile(ctx, filepath.Join(approved.DestinationRoot, "Engine Library"), dest)
		if e != nil || a.SHA256 != b.SHA256 {
			t.Fatal("copied content differs", e)
		}
		formats[strings.ToLower(filepath.Ext(tr.SourceRelative))]++
		if tr.Tags.Title == "" {
			fallback++
		}
		if (i+1)%25 == 0 {
			t.Logf("verified source/copy hashes and metadata: %d/%d", i+1, len(state.Tracks))
		}
	}
	if e = writeAtomicJSON(filepath.Join(dir, "batch-verification.json"), map[string]any{"verified": true, "job": final, "formats": formats, "filenameFallback": fallback, "engineIdentity": engine, "playlistOrderVerified": true}); e != nil {
		t.Fatal(e)
	}
	t.Logf("verified batch: copied=%d skipped=%d formats=%v filename fallback=%d; Engine unchanged", final.CopiedFiles, final.SkippedFiles, formats, fallback)
}

// Capture a read-only source-plan snapshot after admission. The API removes its
// transient preview on start; the worker independently binds the content digest.
func TestCaptureLiveBatchSourcePlanDevice(t *testing.T) {
	id := os.Getenv("PRIME_USB_CAPTURE_BATCH")
	if id == "" {
		t.Skip("explicit batch ID required")
	}
	if !validMediaEventID(id) {
		t.Fatal("invalid batch ID")
	}
	const root = "/data/prime-unlimited"
	request, e := readMediaJobRequest(root, id)
	if e != nil || request.Mode != "live" {
		t.Fatal("live request required", e)
	}
	snapshot, e := usbcopy.Scan(request.Mounts.Source.Root)
	if e != nil {
		t.Fatal(e)
	}
	music, e := internalMusicRoot(request.Mounts.Destination.Root)
	if e != nil {
		t.Fatal(e)
	}
	plan, e := usbcopy.BuildImportPlan(request.Mounts.Source.Root, music, filepath.Base(request.Mounts.Source.Root), request.CreatedAt, snapshot, nil)
	if e != nil {
		t.Fatal(e)
	}
	var preview mediaImportPreview
	data, e := os.ReadFile("/tmp/usb-copy-256gb-preview.json")
	if e != nil {
		t.Fatal(e)
	}
	if e = json.Unmarshal(data, &preview); e != nil {
		t.Fatal(e)
	}
	if !preview.Prepared || !preview.CommitAvailable || preview.Mode != "live" || preview.TrackCount != len(plan.Tracks) {
		t.Fatal("missing approved preview summary")
	}
	approved := mediaPreviewState{Plan: plan, SourceRoot: request.Mounts.Source.Root, DestinationRoot: request.Mounts.Destination.Root, EventID: request.EventID, DuplicateSummary: usbcopy.DuplicateSummary{SourceDigest: request.SourceDigest, NewFiles: preview.NewFiles, ExistingFiles: preview.ExistingFiles, SourceDuplicates: preview.SourceDuplicates, BytesToCopy: preview.BytesToCopy}}
	path := filepath.Join(root, "media-jobs", "diagnostics", "batch-approved-"+id+".json")
	if e = ensurePrivateDirectory(filepath.Dir(path)); e != nil {
		t.Fatal(e)
	}
	if _, e = os.Lstat(path); !os.IsNotExist(e) {
		t.Fatal("snapshot already exists or unavailable")
	}
	if e = writeAtomicJSON(path, approved); e != nil {
		t.Fatal(e)
	}
	t.Logf("captured source plan: tracks=%d source playlists=%d new=%d existing=%d aliases=%d", len(plan.Tracks), len(plan.SourcePlaylists), preview.NewFiles, preview.ExistingFiles, preview.SourceDuplicates)
}

// Read-only investigation of pre-copy failures; no queue/native/job mutations.
func TestInspectLiveBatchAudioPreflightDevice(t *testing.T) {
	id := os.Getenv("PRIME_USB_INSPECT_AUDIO_BATCH")
	if id == "" {
		t.Skip("explicit batch ID required")
	}
	if !validMediaEventID(id) {
		t.Fatal("invalid batch ID")
	}
	var approved mediaPreviewState
	b, e := os.ReadFile(filepath.Join("/data/prime-unlimited/media-jobs", "diagnostics", "batch-approved-"+id+".json"))
	if e != nil {
		t.Fatal(e)
	}
	if e = json.Unmarshal(b, &approved); e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	database, e := internalEngineDatabase(approved.DestinationRoot)
	if e != nil {
		t.Fatal(e)
	}
	db, e := openNativeReadDB(database)
	if e != nil {
		t.Fatal(e)
	}
	rows, e := db.Query("SELECT id,path FROM Track ORDER BY id")
	if e != nil {
		t.Fatal(e)
	}
	existing := []usbcopy.ExistingTrackFile{}
	for rows.Next() {
		var n int64
		var p string
		if e = rows.Scan(&n, &p); e != nil {
			t.Fatal(e)
		}
		existing = append(existing, usbcopy.ExistingTrackFile{TrackID: n, RelativePath: filepath.ToSlash(filepath.Join("Engine Library", p))})
	}
	e = rows.Err()
	rows.Close()
	db.Close()
	if e != nil {
		t.Fatal(e)
	}
	type failure struct {
		Source, Error   string
		ExistingTrackID int64
	}
	failures := []failure{}
	for i, tr := range approved.Plan.Tracks {
		if err := usbcopy.ValidateLiveAudioFile(ctx, approved.SourceRoot, tr.SourceRelative); err != nil {
			f := failure{Source: tr.SourceRelative, Error: err.Error()}
			decisions, err := usbcopy.FindDuplicateAudio(ctx, approved.SourceRoot, approved.DestinationRoot, []string{tr.SourceRelative}, existing)
			if err != nil {
				t.Fatal(err)
			}
			f.ExistingTrackID = decisions[0].ExistingTrackID
			failures = append(failures, f)
			t.Logf("audio preflight failure: %q: %s; existing internal track=%d", f.Source, f.Error, f.ExistingTrackID)
		}
		if (i+1)%50 == 0 {
			t.Logf("audio preflight inspected %d/%d", i+1, len(approved.Plan.Tracks))
		}
	}
	path := filepath.Join("/data/prime-unlimited/media-jobs", "diagnostics", "batch-audio-preflight-"+id+".json")
	if e = writeAtomicJSON(path, failures); e != nil {
		t.Fatal(e)
	}
	t.Logf("audio preflight complete: %d failures", len(failures))
}

// This bounded review applies only to the observed failed pre-copy trial. It
// never resumes that job and requires both leases, the original code/process,
// no session directory and no published job-specific music directory.
func TestReviewUnstartedMixedBatchDevice(t *testing.T) {
	const id = "948dc7b79a75485a435ce474ec0de5f4ace0123956495c3d1bb75b2aa7b91342"
	if os.Getenv("PRIME_USB_REVIEW_UNSTARTED_BATCH") != id {
		t.Skip("explicit bounded no-effects review required")
	}
	const root = "/data/prime-unlimited"
	for _, name := range []string{"admission.lock", "supervisor.lock"} {
		f, e := os.OpenFile(filepath.Join(root, "media-jobs", name), os.O_RDWR|unix.O_NOFOLLOW, 0)
		if e != nil {
			t.Fatal(e)
		}
		defer f.Close()
		if e = unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); e != nil {
			t.Fatal(e)
		}
		defer unix.Flock(int(f.Fd()), unix.LOCK_UN)
	}
	store, e := usbcopy.OpenJobStore(filepath.Join(root, "media-jobs"))
	if e != nil {
		t.Fatal(e)
	}
	j, e := store.Read(id)
	if e != nil {
		t.Fatal(e)
	}
	if j.Mode != "live" || j.Phase != usbcopy.JobInterrupted || j.Revision != 2 || j.FilesDone != 0 || j.BytesDone != 0 || j.FilesTotal != 690 || j.BytesTotal != 7583360670 || j.ResultAvailable || !j.EngineRestored {
		t.Fatal("job differs from observed pre-copy failure")
	}
	output, e := mediaEngineCommand(context.Background(), "/bin/systemctl", "show", "-p", "ActiveState", "--value", "pu-usb-job-"+id+".service")
	if e != nil {
		t.Fatal(e)
	}
	if state := strings.TrimSpace(string(output)); state != "failed" && state != "inactive" {
		t.Fatal("worker may still be active")
	}
	current, e := nativeEngineIdentity()
	if e != nil || current != "8d4b9a9d-31bc-4395-9815-5e8821ca993f:964:1540" {
		t.Fatal("Engine identity differs", e)
	}
	binary, e := os.ReadFile(filepath.Join(root, "bin/prime-unlimited-runtime-api"))
	if e != nil {
		t.Fatal(e)
	}
	sum := sha256.Sum256(binary)
	runtimeSHA := hex.EncodeToString(sum[:])
	if runtimeSHA != "1adf74886f449f6dbaa72a0f8c26e1f3bdc819df22c62e10792ac493ee9ebb65" {
		t.Fatal("runtime differs from observed no-session control flow")
	}
	liveDir := filepath.Join(root, "media-jobs", "live", id)
	if _, e = os.Lstat(liveDir); !os.IsNotExist(e) {
		t.Fatal("session/effect state exists; cannot close as unstarted")
	}
	request, e := readMediaJobRequest(root, id)
	if e != nil || request.Mounts.Source.Root != "/media/256 GB" || request.Mounts.Destination.Root != "/media/Denon" {
		t.Fatal("request differs", e)
	}
	control, e := newMediaEngineController("JP24", request.Mounts)
	if e != nil {
		t.Fatal(e)
	}
	control.validateDestination = func(ctx context.Context, v mountedBlockVolume) error {
		actual, e := readMediaDestinationIdentity(ctx, v)
		if e != nil {
			return e
		}
		if actual != request.DestinationIdentity {
			return os.ErrInvalid
		}
		return nil
	}
	if e = control.ValidateVolumes(context.Background()); e != nil {
		t.Fatal(e)
	}
	var approved mediaPreviewState
	data, e := readOwnedFile(filepath.Join(root, "media-jobs", "diagnostics", "batch-approved-"+id+".json"), 64<<20, "batch source plan")
	if e != nil {
		t.Fatal(e)
	}
	if e = json.Unmarshal(data, &approved); e != nil {
		t.Fatal(e)
	}
	music, e := internalMusicRoot(request.Mounts.Destination.Root)
	if e != nil {
		t.Fatal(e)
	}
	target := filepath.Join(music, approved.Plan.ImportDirectory+" - "+id[:16])
	if _, e = os.Lstat(target); !os.IsNotExist(e) {
		t.Fatal("job-specific music directory exists")
	}
	proof := map[string]any{"jobBefore": j, "engineIdentity": current, "runtimeSHA256": runtimeSHA, "absentLiveDirectory": liveDir, "absentMusicDirectory": target, "conclusion": "stopped before session publication; native broker rejects queued jobs; no copy/native command could begin"}
	proofDir := filepath.Join(root, "media-jobs", "reviews")
	if e = ensurePrivateDirectory(proofDir); e != nil {
		t.Fatal(e)
	}
	proofPath := filepath.Join(proofDir, "preflight-"+id+".json")
	if _, e = os.Lstat(proofPath); !os.IsNotExist(e) {
		t.Fatal("review evidence already exists")
	}
	if e = writeAtomicJSON(proofPath, proof); e != nil {
		t.Fatal(e)
	}
	data, e = os.ReadFile(proofPath)
	if e != nil {
		t.Fatal(e)
	}
	sum = sha256.Sum256(data)
	closed, e := store.CloseVerifiedUnstartedLiveFailure(id, j.Revision, hex.EncodeToString(sum[:]))
	if e != nil {
		t.Fatal(e)
	}
	t.Logf("reviewed no-effects failure closed without resuming: phase=%s revision=%d; Engine unchanged", closed.Phase, closed.Revision)
}

func TestRecheckNewAudioPreflightFailuresDevice(t *testing.T) {
	id := os.Getenv("PRIME_USB_RECHECK_AUDIO_BATCH")
	if id == "" {
		t.Skip("explicit batch ID required")
	}
	if !validMediaEventID(id) {
		t.Fatal("invalid batch ID")
	}
	var failures []struct {
		Source, Error   string
		ExistingTrackID int64
	}
	data, e := os.ReadFile(filepath.Join("/data/prime-unlimited/media-jobs", "diagnostics", "batch-audio-preflight-"+id+".json"))
	if e != nil {
		t.Fatal(e)
	}
	if e = json.Unmarshal(data, &failures); e != nil {
		t.Fatal(e)
	}
	checked := 0
	for _, f := range failures {
		if f.ExistingTrackID != 0 {
			continue
		}
		checked++
		if e = usbcopy.ValidateLiveAudioFile(context.Background(), "/media/256 GB", f.Source); e != nil {
			t.Errorf("new source still fails: %q: %v", f.Source, e)
		} else {
			t.Logf("new source now validates: %q", f.Source)
		}
	}
	if checked == 0 {
		t.Fatal("no observed new-file failures to recheck")
	}
	t.Logf("rechecked %d previously rejected new/alias source files", checked)
}

func TestRecheckOwnerApprovedBatchFilesDevice(t *testing.T) {
	id := os.Getenv("PRIME_USB_RECHECK_APPROVED_BATCH")
	if id == "" {
		t.Skip("explicit owner-approved batch required")
	}
	if !validMediaEventID(id) {
		t.Fatal("invalid batch ID")
	}
	const root = "/data/prime-unlimited"
	request, e := readMediaJobRequest(root, id)
	if e != nil {
		t.Fatal(e)
	}
	var failures []struct {
		Source, Error   string
		ExistingTrackID int64
	}
	data, e := os.ReadFile(filepath.Join(root, "media-jobs", "diagnostics", "batch-audio-preflight-"+id+".json"))
	if e != nil {
		t.Fatal(e)
	}
	if e = json.Unmarshal(data, &failures); e != nil {
		t.Fatal(e)
	}
	checked := 0
	for _, f := range failures {
		if f.ExistingTrackID != 0 {
			continue
		}
		checked++
		fp, e := usbcopy.FingerprintAudioFile(context.Background(), request.Mounts.Source.Root, f.Source)
		if e != nil {
			t.Fatal(e)
		}
		tr := usbcopy.PlannedTrack{SourceRelative: f.Source, Bytes: fp.Bytes}
		if e = validateMediaLiveAudio(context.Background(), root, request.Mounts.Source.Root, request.SourceDigest, tr, fp); e != nil {
			t.Errorf("new source rejected: %q: %v", f.Source, e)
		} else {
			t.Logf("verified unchanged approved source: %q", f.Source)
		}
	}
	if checked != 5 {
		t.Fatal("expected the five previously identified new/alias files", checked)
	}
}
