package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"golang.org/x/sys/unix"
	"os"
	"path/filepath"
	usbcopy "prime-unlimited.usb-copy/worker"
	"strconv"
	"strings"
	"testing"
	"time"
)

const mixedReviewID = "bd1b37702b0d07e2802571276901015a3aaee799c77473685dc61c1a9c1e9495"

type mixedReviewInputs struct {
	a       *mediaLiveAdapter
	store   *usbcopy.JobStore
	job     usbcopy.ImportJobStatus
	state   usbcopy.LiveSequenceState
	command usbcopy.LiveCommandRecord
	proof   map[string]any
}

func mixedReviewLeases(t *testing.T) {
	t.Helper()
	for _, name := range []string{"admission.lock", "supervisor.lock"} {
		f, e := os.OpenFile(filepath.Join("/data/prime-unlimited/media-jobs", name), os.O_RDWR|unix.O_NOFOLLOW, 0)
		if e != nil {
			t.Fatal(e)
		}
		t.Cleanup(func() { f.Close() })
		if e = unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); e != nil {
			t.Fatal(e)
		}
		t.Cleanup(func() { unix.Flock(int(f.Fd()), unix.LOCK_UN) })
	}
}
func loadMixedReview(ctx context.Context, hashCopies bool) (out mixedReviewInputs, err error) {
	const root = "/data/prime-unlimited"
	id := mixedReviewID
	out.store, err = usbcopy.OpenJobStore(filepath.Join(root, "media-jobs"))
	if err != nil {
		return
	}
	jobs, e := out.store.List()
	if e != nil {
		return out, e
	}
	for _, j := range jobs {
		if j.ID != id && j.Phase != usbcopy.JobCompleted && j.Phase != usbcopy.JobFailed && j.Phase != usbcopy.JobCancelled {
			return out, errors.New("another job active or awaiting review")
		}
	}
	out.job, err = out.store.Read(id)
	if err != nil {
		return
	}
	j := out.job
	if j.Mode != "live" || j.Phase != usbcopy.JobInterrupted || j.Revision != 1388 || !j.EngineRestored || j.FilesDone != 690 || j.FilesTotal != 690 || j.BytesDone != 7583360670 || j.BytesTotal != 7583360670 || j.ResultAvailable {
		return out, errors.New("job differs from fully copied mixed trial")
	}
	dir := filepath.Join(root, "media-jobs", "live", id)
	read := func(path string, value any) error {
		b, e := readOwnedFile(path, 64<<20, "mixed review state")
		if e != nil {
			return e
		}
		return json.Unmarshal(b, value)
	}
	request, e := readMediaJobRequest(root, id)
	if e != nil {
		return out, e
	}
	session, e := readMediaLiveSession(root, id)
	if e != nil {
		return out, e
	}
	if session.Binding.EngineIdentity != "7cb243ac-bfbb-4200-b459-32151dee02b1:975:1481" {
		return out, errors.New("unexpected original Engine identity")
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
			return errors.New("review destination changed")
		}
		return nil
	}
	validate := func(ctx context.Context) (usbcopy.LiveBinding, error) {
		if e := control.ValidateVolumes(ctx); e != nil {
			return session.Binding, e
		}
		current, e := nativeEngineIdentity()
		if e != nil || current != session.Binding.EngineIdentity {
			return session.Binding, errors.New("review Engine changed")
		}
		return session.Binding, nil
	}
	if _, e = validate(ctx); e != nil {
		return out, e
	}
	runtimeBytes, e := os.ReadFile(filepath.Join(root, "bin/prime-unlimited-runtime-api"))
	if e != nil {
		return out, e
	}
	runtimeSum := sha256.Sum256(runtimeBytes)
	if hex.EncodeToString(runtimeSum[:]) != "9b72f5a1ec8e7cfcc4d5f757a0e43b4635e688cddae407ef286ae5e52ebd48ae" {
		return out, errors.New("review broker runtime differs from verified protocol")
	}
	if e = verifyMediaLiveEngine(ctx); e != nil {
		return out, e
	}
	if err = read(filepath.Join(dir, "journal", id+".json"), &out.state); err != nil {
		return
	}
	if out.state.Phase != "review_required" || out.state.Intent != id+":register:0" || len(out.state.Tracks) != 0 || len(out.state.Playlists) != 0 || out.state.Skipped != 151 || out.state.Binding != session.Binding {
		return out, errors.New("original journal differs")
	}
	if err = read(filepath.Join(dir, "commands", "command.json"), &out.command); err != nil {
		return
	}
	c := out.command
	if c.Phase != "review_required" || c.Command.Operation != "register_files" || c.Command.Intent != id+":register:0:files:0" || len(c.Command.Paths) != 32 || c.Command.Binding != session.Binding || c.Observation == nil || c.Observation.Outcome != "applied" || !c.Observation.AllTracks || c.Observation.Binding != session.Binding || c.Observation.SourceDevice != request.Mounts.Destination.Root || c.Observation.CollectionDevice != request.Mounts.Destination.Root || c.Observation.LibraryUUID != session.Binding.LibraryUUID {
		return out, errors.New("first applied registration acknowledgement differs")
	}
	files, e := os.ReadDir(filepath.Join(dir, "commands"))
	if e != nil {
		return out, e
	}
	for _, file := range files {
		if !strings.HasSuffix(file.Name(), ".completed.json") {
			continue
		}
		var old usbcopy.LiveCommandRecord
		if e = read(filepath.Join(dir, "commands", file.Name()), &old); e != nil {
			return out, e
		}
		if old.Command.Operation != "select_library" && old.Command.Operation != "all_tracks" {
			return out, errors.New("unexpected prior native mutation")
		}
	}
	var approved mediaPreviewState
	if e = read(filepath.Join(root, "media-jobs", "diagnostics", "batch-approved-"+id+".json"), &approved); e != nil {
		return out, e
	}
	if approved.DuplicateSummary.NewFiles != 539 || approved.DuplicateSummary.ExistingFiles != 141 || approved.DuplicateSummary.SourceDuplicates != 10 || approved.DuplicateSummary.SourceDigest != request.SourceDigest {
		return out, errors.New("approved counts/content digest differ")
	}
	var manifest struct {
		Binding         usbcopy.LiveBinding
		ImportDirectory string
		Tracks          []struct {
			Source, Destination, SHA256 string
			Bytes                       int64
		}
	}
	if e = read(filepath.Join(dir, "manifest.json"), &manifest); e != nil {
		return out, e
	}
	if manifest.Binding != session.Binding || len(manifest.Tracks) != 539 || !strings.HasSuffix(manifest.ImportDirectory, " - "+id[:16]) {
		return out, errors.New("copy manifest differs")
	}
	database, e := internalEngineDatabase(request.Mounts.Destination.Root)
	if e != nil {
		return out, e
	}
	db, e := openNativeReadDB(database)
	if e != nil {
		return out, e
	}
	defer db.Close()
	var backup struct {
		Path   string
		Counts nativeLibraryCounts
	}
	if e = read(filepath.Join(dir, "backup.json"), &backup); e != nil {
		return out, e
	}
	baselineDB, e := openNativeReadDB(backup.Path)
	if e != nil {
		return out, e
	}
	baseline, e := nativeCounts(ctx, baselineDB)
	baselineDB.Close()
	if e != nil || baseline != backup.Counts {
		return out, errors.New("original consistent backup differs")
	}
	counts, e := nativeCounts(ctx, db)
	if e != nil {
		return out, e
	}
	if counts.Tracks != baseline.Tracks+32 || counts.Playlists != baseline.Playlists || counts.Entities != baseline.Entities {
		return out, errors.New("library effects differ from exactly 32 registered tracks")
	}
	copiedRoot := filepath.Join(request.Mounts.Destination.Root, "Engine Library", "Music", manifest.ImportDirectory)
	snapshot, e := usbcopy.Scan(copiedRoot)
	if e != nil || len(snapshot.Tracks) != 539 {
		return out, errors.New("copied audio inventory differs")
	}
	metadata := map[string]usbcopy.Track{}
	for _, tr := range snapshot.Tracks {
		metadata[tr.RelativePath] = tr
	}
	plan, e := usbcopy.LivePlanWithSourcePlaylists(approved.Plan)
	if e != nil {
		return out, e
	}
	oldTracks := map[string]usbcopy.PlannedTrack{}
	for _, tr := range plan.Tracks {
		oldTracks[tr.SourceRelative] = tr
	}
	plan.Tracks = nil
	plan.BytesToCopy = 0
	plan.ImportDirectory = manifest.ImportDirectory
	copied := map[string]bool{}
	seenDest := map[string]bool{}
	decisions := []usbcopy.DuplicateDecision{}
	registered := map[string]liveRegisteredTrack{}
	digest := sha256.New()
	for i, m := range manifest.Tracks {
		tr, ok := oldTracks[m.Source]
		if !ok || copied[m.Source] || seenDest[m.Destination] || !validMediaEventID(m.SHA256) || m.Bytes != tr.Bytes || !strings.HasPrefix(m.Destination, "Music/"+manifest.ImportDirectory+"/") {
			return out, errors.New("invalid/duplicate copied manifest entry")
		}
		copied[m.Source] = true
		seenDest[m.Destination] = true
		meta, ok := metadata[m.Source]
		if !ok || meta.Bytes != m.Bytes {
			return out, errors.New("copied metadata source missing")
		}
		tr.DestinationRelative = strings.TrimPrefix(m.Destination, "Music/")
		tr.Tags = meta.Tags
		tr.Artwork = meta.Artwork
		if hashCopies {
			fp, e := usbcopy.FingerprintAudioFile(ctx, filepath.Join(request.Mounts.Destination.Root, "Engine Library"), m.Destination)
			if e != nil || fp.SHA256 != m.SHA256 || fp.Bytes != m.Bytes {
				return out, errors.New("copied bytes differ from original approved source fingerprint")
			}
		}
		digest.Write([]byte(m.Source + "\x00" + m.Destination + "\x00" + m.SHA256 + "\n"))
		var n int
		if e = db.QueryRowContext(ctx, "SELECT count(*) FROM Track WHERE path=?", m.Destination).Scan(&n); e != nil {
			return out, e
		}
		if i < 32 {
			if c.Command.Paths[i] != m.Destination || n != 1 {
				return out, errors.New("registered prefix path differs")
			}
			r, e := liveRegistered(ctx, database, request.Mounts.Destination.Root, session.Binding.LibraryUUID, tr)
			if e != nil {
				return out, e
			}
			registered[m.Source] = r
		} else if n != 0 {
			return out, errors.New("unrecorded registration outside applied prefix")
		}
		plan.Tracks = append(plan.Tracks, tr)
		plan.BytesToCopy += m.Bytes
		decisions = append(decisions, usbcopy.DuplicateDecision{SourceRelative: m.Source, Fingerprint: usbcopy.FileFingerprint{SHA256: m.SHA256, Bytes: m.Bytes, ModifiedAt: tr.SourceModifiedAt}})
	}
	if plan.BytesToCopy != j.BytesTotal {
		return out, errors.New("copied byte total differs")
	}
	filter := func(paths []string) []string {
		result := []string{}
		seen := map[string]bool{}
		for _, p := range paths {
			if copied[p] && !seen[p] {
				result = append(result, p)
				seen[p] = true
			}
		}
		return result
	}
	plan.CopyPlaylist.Tracks = filter(plan.CopyPlaylist.Tracks)
	plan.CopyPlaylist.Name += " - " + id[:16]
	lists := []usbcopy.PlannedPlaylist{}
	for _, p := range plan.SourcePlaylists {
		p.Tracks = filter(p.Tracks)
		if len(p.Tracks) > 0 {
			lists = append(lists, p)
		}
	}
	plan.SourcePlaylists = lists
	if len(plan.CopyPlaylist.Tracks) != 539 {
		return out, errors.New("generated playlist does not cover all new copies")
	}
	queue, e := usbcopy.OpenFileLiveCommands(filepath.Join(dir, "commands"), id)
	if e != nil {
		return out, e
	}
	out.a = &mediaLiveAdapter{id: id, root: root, database: database, source: request.Mounts.Source.Root, destination: request.Mounts.Destination.Root, binding: session.Binding, queue: queue, plan: plan, decisions: decisions, skipped: 151, validate: validate, reviewedRegistration: registered}
	out.proof = map[string]any{"job": j, "binding": session.Binding, "copiedFiles": 539, "copiedBytes": plan.BytesToCopy, "copiedSHA256Verified": hashCopies, "manifestFingerprintDigest": hex.EncodeToString(digest.Sum(nil)), "registeredPrefix": registered, "before": baseline, "current": counts, "appliedCommand": c.Command, "interpretation": "embedded WAV tags now read; first 32 registrations verified, remaining 507 absent; no copy or applied registration may be replayed"}
	return
}
func TestInspectReviewedMixedBatchDevice(t *testing.T) {
	if os.Getenv("PRIME_USB_INSPECT_MIXED_REVIEW") != mixedReviewID {
		t.Skip("explicit read-only mixed review required")
	}
	mixedReviewLeases(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	out, e := loadMixedReview(ctx, true)
	if e != nil {
		t.Fatal(e)
	}
	t.Logf("verified copied bytes=%d; native prefix=%d; remaining=%d; source playlists=%d", out.a.plan.BytesToCopy, len(out.a.reviewedRegistration), len(out.a.plan.Tracks)-len(out.a.reviewedRegistration), len(out.a.plan.SourcePlaylists))
}
func TestCompleteReviewedMixedNativeWork(t *testing.T) {
	if os.Getenv("PRIME_USB_COMPLETE_MIXED_REVIEW") != mixedReviewID {
		t.Skip("explicit reviewed native completion required")
	}
	mixedReviewLeases(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	out, e := loadMixedReview(ctx, true)
	if e != nil {
		t.Fatal(e)
	}
	if b, e := mediaEngineCommand(ctx, "/bin/systemctl", "show", "-p", "ActiveState", "--value", "pu-usb-job-"+mixedReviewID+".service"); e != nil || strings.TrimSpace(string(b)) != "active" {
		t.Fatal("original supervised unit must own reviewed continuation", e)
	}
	dir := filepath.Join("/data/prime-unlimited/media-jobs/live", mixedReviewID)
	proofPath := filepath.Join(dir, "metadata-review-completion.json")
	if _, e = os.Lstat(proofPath); !os.IsNotExist(e) {
		t.Fatal("review already reserved")
	}
	if e = writeAtomicJSON(proofPath, out.proof); e != nil {
		t.Fatal(e)
	}
	b, e := os.ReadFile(proofPath)
	if e != nil {
		t.Fatal(e)
	}
	sum := sha256.Sum256(b)
	job, e := out.store.BeginVerifiedLiveNativeCompletion(mixedReviewID, out.job.Revision, hex.EncodeToString(sum[:]))
	if e != nil {
		t.Fatal(e)
	}
	completed := false
	defer func() {
		if !completed {
			latest, err := out.store.Read(mixedReviewID)
			if err == nil {
				latest.Phase = usbcopy.JobInterrupted
				latest.ErrorCode = "live_review_required"
				current, _ := nativeEngineIdentity()
				latest.EngineRestored = current == out.a.binding.EngineIdentity
				_, _ = out.store.Save(latest)
			}
		}
	}()
	// Adopt only the first already-applied command after independent metadata,
	// content and global-effect proof. Preserve the original record as evidence.
	if e = writeAtomicJSON(filepath.Join(dir, "original-first-registration.json"), out.command); e != nil {
		t.Fatal(e)
	}
	adopted := out.command
	adopted.Phase = "verified"
	if e = writeAtomicJSON(filepath.Join(dir, "commands", "command.json"), adopted); e != nil {
		t.Fatal(e)
	}
	state := out.state
	state.Phase = "reviewed_native_completion"
	state.Intent = ""
	state.Tracks = nil
	state.Playlists = nil
	persist := func(intent string) {
		t.Helper()
		state.Intent = intent
		if e = writeAtomicJSON(filepath.Join(dir, "reviewed-native-state.json"), state); e != nil {
			t.Fatal(e)
		}
	}
	persist(mixedReviewID + ":review:select")
	if e = out.a.SelectLibrary(ctx, state.Intent); e != nil {
		t.Fatal(e)
	}
	// registerAll skips and rechecks exactly the proven prefix; it claims only
	// the still-unissued files:32/... commands and fresh actual-ID lookups.
	persist(mixedReviewID + ":register:0")
	for _, tr := range out.a.plan.Tracks {
		ref, err := out.a.Register(ctx, state.Intent, tr)
		if err != nil {
			t.Fatal(err)
		}
		state.Tracks = append(state.Tracks, ref)
	}
	bySource := map[string]usbcopy.LiveTrackReference{}
	for _, ref := range state.Tracks {
		bySource[ref.SourceRelative] = ref
	}
	plans := append([]usbcopy.PlannedPlaylist{out.a.plan.CopyPlaylist}, out.a.plan.SourcePlaylists...)
	for i, p := range plans {
		persist(mixedReviewID + ":review:create:" + strconv.Itoa(i))
		list, err := out.a.CreatePlaylist(ctx, state.Intent, p.Name)
		if err != nil {
			t.Fatal(err)
		}
		state.Playlists = append(state.Playlists, list)
		refs := []usbcopy.LiveTrackReference{}
		for _, source := range p.Tracks {
			ref, ok := bySource[source]
			if !ok {
				t.Fatal("playlist references skipped source")
			}
			refs = append(refs, ref)
		}
		persist(mixedReviewID + ":review:add:" + strconv.Itoa(i))
		if e = out.a.Add(ctx, state.Intent, list, refs); e != nil {
			t.Fatal(e)
		}
	}
	persist(mixedReviewID + ":review:verify")
	if e = out.a.Verify(ctx, out.a.plan, state); e != nil {
		t.Fatal(e)
	}
	if _, e = out.a.Validate(ctx); e != nil {
		t.Fatal(e)
	}
	state.Phase = "completed"
	state.Intent = ""
	if e = writeAtomicJSON(filepath.Join(dir, "reviewed-native-state.json"), state); e != nil {
		t.Fatal(e)
	}
	job.Phase = usbcopy.JobCompleted
	job.EngineRestored = true
	job.ResultAvailable = true
	job.CopiedFiles = 539
	job.SkippedFiles = 151
	job.CreatedPlaylists = len(state.Playlists)
	job.PlaylistName = out.a.plan.CopyPlaylist.Name
	job.ErrorCode = ""
	job, e = out.store.Save(job)
	if e != nil {
		t.Fatal(e)
	}
	if e = writeAtomicJSON(filepath.Join(dir, "reviewed-completion-result.json"), map[string]any{"job": job, "state": state, "engineIdentity": out.a.binding.EngineIdentity}); e != nil {
		t.Fatal(e)
	}
	completed = true
	t.Logf("reviewed native completion: copied=%d skipped=%d playlists=%d name=%s", job.CopiedFiles, job.SkippedFiles, job.CreatedPlaylists, job.PlaylistName)
}
