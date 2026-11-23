package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/unix"
	usbcopy "prime-unlimited.usb-copy/worker"
)

const mediaLiveEngineSHA = "63dc843feb6738d463eb68a62c9f1bd2cd86dcb562fcaa77a4fef607fefdf0ed"

func (s *server) mediaLiveAvailable() bool {
	return s.mediaImportLive && s.mediaCommitAvailable() && s.firmware == "5.3.0" && s.architecture == "aarch64"
}
func verifyMediaLiveEngine(ctx context.Context) error {
	f, e := os.Open("/usr/Engine/Engine")
	if e != nil {
		return e
	}
	defer f.Close()
	h := sha256.New()
	buf := make([]byte, 128<<10)
	for {
		if e = ctx.Err(); e != nil {
			return e
		}
		n, readErr := f.Read(buf)
		if n > 0 {
			h.Write(buf[:n])
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return readErr
		}
	}
	if hex.EncodeToString(h.Sum(nil)) != mediaLiveEngineSHA {
		return errors.New("native Engine build is not verified for live imports")
	}
	return nil
}
func prepareMediaLivePlan(ctx context.Context, request mediaJobRequest) (usbcopy.ImportPlan, []usbcopy.DuplicateDecision, error) {
	var empty usbcopy.ImportPlan
	source, dest := request.Mounts.Source.Root, request.Mounts.Destination.Root
	snapshot, e := usbcopy.Scan(source)
	if e != nil || len(snapshot.Tracks) == 0 || len(snapshot.Warnings) > 0 {
		return empty, nil, errors.New("source scan is incomplete")
	}
	database, e := internalEngineDatabase(dest)
	if e != nil {
		return empty, nil, e
	}
	db, e := openNativeReadDB(database)
	if e != nil {
		return empty, nil, e
	}
	var version int
	e = db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version)
	if e != nil || version != 4194305 {
		db.Close()
		return empty, nil, errors.New("unsupported live library schema")
	}
	rows, e := db.QueryContext(ctx, "SELECT id,path FROM Track ORDER BY id")
	if e != nil {
		db.Close()
		return empty, nil, e
	}
	existing := []usbcopy.ExistingTrackFile{}
	for rows.Next() {
		var id int64
		var path sql.NullString
		if e = rows.Scan(&id, &path); e != nil {
			rows.Close()
			db.Close()
			return empty, nil, e
		}
		if !path.Valid || path.String == "" {
			continue
		}
		clean := filepath.Clean(filepath.FromSlash(path.String))
		if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, "../") {
			rows.Close()
			db.Close()
			return empty, nil, errors.New("unsafe existing live library path")
		}
		existing = append(existing, usbcopy.ExistingTrackFile{TrackID: id, RelativePath: filepath.ToSlash(filepath.Join("Engine Library", clean))})
	}
	e = rows.Err()
	rows.Close()
	db.Close()
	if e != nil {
		return empty, nil, e
	}
	sources := []string{}
	for _, t := range snapshot.Tracks {
		if !strings.EqualFold(filepath.Ext(t.RelativePath), ".mp3") {
			return empty, nil, errors.New("live import currently supports MP3 only")
		}
		sources = append(sources, t.RelativePath)
	}
	decisions, e := usbcopy.FindDuplicateAudio(ctx, source, dest, sources, existing)
	if e != nil {
		return empty, nil, e
	}
	if usbcopy.DuplicateSourceDigest(decisions) != request.SourceDigest {
		return empty, nil, errors.New("source changed after approved preview")
	}
	names, e := usbcopy.ReadEnginePlaylistNames(database)
	if e != nil {
		return empty, nil, e
	}
	music, e := internalMusicRoot(dest)
	if e != nil {
		return empty, nil, e
	}
	p, e := usbcopy.BuildImportPlan(source, music, filepath.Base(source), request.CreatedAt, snapshot, names)
	if e != nil {
		return empty, nil, e
	}
	old := p.ImportDirectory
	p.ImportDirectory = old + " - " + request.ID[:16]
	byDestination := map[string]string{}
	for i := range p.Tracks {
		t := &p.Tracks[i]
		prefix := old + "/"
		if !strings.HasPrefix(t.DestinationRelative, prefix) {
			return empty, nil, errors.New("unexpected live destination plan")
		}
		next := p.ImportDirectory + "/" + strings.TrimPrefix(t.DestinationRelative, prefix)
		byDestination[t.DestinationRelative] = next
		t.DestinationRelative = next
	}
	for i, path := range p.CopyPlaylist.Tracks {
		p.CopyPlaylist.Tracks[i] = byDestination[path]
	}
	for i := range p.SourcePlaylists {
		for j, path := range p.SourcePlaylists[i].Tracks {
			p.SourcePlaylists[i].Tracks[j] = byDestination[path]
		}
	}
	p.CopyPlaylist.Name += " - " + request.ID[:16]
	p, e = usbcopy.LivePlanWithSourcePlaylists(p)
	if e != nil {
		return empty, nil, e
	}
	filtered, _, e := usbcopy.LiveNewOnlyPlan(p, decisions)
	if e != nil {
		return empty, nil, e
	}
	for _, t := range filtered.Tracks {
		if _, e = usbcopy.ReadMP3Properties(ctx, source, t.SourceRelative); e != nil {
			return empty, nil, e
		}
	}
	return p, decisions, nil
}
func (s *server) runMediaLiveJob(ctx context.Context, request mediaJobRequest, store *usbcopy.JobStore) (err error) {
	if !s.mediaLiveAvailable() || request.Mode != "live" || !request.DestinationIdentity.valid() {
		return errors.New("live import approval unavailable")
	}
	job, e := store.Read(request.ID)
	if e != nil {
		return e
	}
	if job.Mode != "live" || job.Phase != usbcopy.JobQueued {
		return errors.New("live job is not fresh; automatic resume is forbidden")
	}
	lease, e := os.OpenFile(filepath.Join(s.root, "media-jobs", "supervisor.lock"), os.O_CREATE|os.O_RDWR|unix.O_NOFOLLOW, 0600)
	if e != nil {
		return e
	}
	defer lease.Close()
	if e = unix.Flock(int(lease.Fd()), unix.LOCK_EX|unix.LOCK_NB); e != nil {
		return e
	}
	defer unix.Flock(int(lease.Fd()), unix.LOCK_UN)
	engine, e := nativeEngineIdentity()
	if e != nil {
		return e
	}
	defer func() {
		if err != nil {
			latest, readErr := store.Read(request.ID)
			if readErr == nil && latest.Phase != usbcopy.JobCompleted {
				latest.Phase = usbcopy.JobInterrupted
				latest.ErrorCode = "live_review_required"
				current, _ := nativeEngineIdentity()
				latest.EngineRestored = current == engine
				_, _ = store.Save(latest)
			}
		}
	}()
	if e = verifyMediaLiveEngine(ctx); e != nil {
		return e
	}
	control, e := newMediaEngineController(s.model, request.Mounts)
	if e != nil {
		return e
	}
	control.approved = func() bool {
		latest, _ := loadVerifiedCatalogWithFallback(s.catalogPath, s.catalogSignaturePath, s.catalogTrustKeyPath)
		s.catalogMu.Lock()
		s.catalog = latest
		s.catalogMu.Unlock()
		return s.mediaImportEnabled()
	}
	control.validateDestination = func(ctx context.Context, v mountedBlockVolume) error {
		actual, e := readMediaDestinationIdentity(ctx, v)
		if e != nil {
			return e
		}
		if actual != request.DestinationIdentity {
			return errors.New("live destination changed")
		}
		return nil
	}
	if e = control.ValidateVolumes(ctx); e != nil {
		return e
	}
	plan, decisions, e := prepareMediaLivePlan(ctx, request)
	if e != nil {
		return e
	}
	filtered, skipped, e := usbcopy.LiveNewOnlyPlan(plan, decisions)
	if e != nil {
		return e
	}
	if len(plan.Tracks) != job.FilesTotal || filtered.BytesToCopy != job.BytesTotal {
		return errors.New("duplicate state changed after preview; new preview required")
	}
	if e = control.ValidateVolumes(ctx); e != nil {
		return e
	}
	session, e := createMediaLiveSession(s.root, request, engine)
	if e != nil {
		return e
	}
	dir, e := mediaLiveDirectory(s.root, request.ID)
	if e != nil {
		return e
	}
	queue, e := usbcopy.OpenFileLiveCommands(filepath.Join(dir, "commands"), request.ID)
	if e != nil {
		return e
	}
	journal, e := usbcopy.OpenFileLiveJournal(filepath.Join(dir, "journal"))
	if e != nil {
		return e
	}
	database, e := internalEngineDatabase(request.Mounts.Destination.Root)
	if e != nil {
		return e
	}
	adapter := &mediaLiveAdapter{id: request.ID, root: s.root, database: database, source: request.Mounts.Source.Root, destination: request.Mounts.Destination.Root, binding: session.Binding, queue: queue, plan: filtered, decisions: decisions, skipped: skipped}
	adapter.validate = func(ctx context.Context) (usbcopy.LiveBinding, error) {
		if e := control.ValidateVolumes(ctx); e != nil {
			return usbcopy.LiveBinding{}, e
		}
		current, e := nativeEngineIdentity()
		if e != nil || current != engine {
			return usbcopy.LiveBinding{}, errors.New("live native process changed")
		}
		return session.Binding, nil
	}
	job.Phase = usbcopy.JobCopying
	job, e = store.Save(job)
	if e != nil {
		return e
	}
	last := time.Time{}
	adapter.progress = func(phase usbcopy.JobPhase, p usbcopy.CopyProgress) error {
		if phase == job.Phase && p.FilesDone+skipped == job.FilesDone && p.BytesDone < job.BytesTotal && time.Since(last) < 200*time.Millisecond {
			return nil
		}
		next := job
		next.Phase = phase
		next.FilesDone = p.FilesDone + skipped
		next.BytesDone = p.BytesDone
		saved, e := store.Save(next)
		if e != nil {
			return e
		}
		job = saved
		last = time.Now()
		return nil
	}
	state, e := usbcopy.RunLiveSequence(ctx, request.ID, plan, decisions, journal, adapter)
	if e != nil {
		return e
	}
	if state.Phase != "completed" {
		return errors.New("live sequence did not complete")
	}
	if _, e = adapter.Validate(ctx); e != nil {
		return e
	}
	// Even an all-skipped job must pass the commit phase, without native calls.
	if job.Phase == usbcopy.JobCopying {
		job.Phase = usbcopy.JobCommitting
		job.FilesDone = job.FilesTotal
		job.BytesDone = job.BytesTotal
		job, e = store.Save(job)
		if e != nil {
			return e
		}
	}
	job.Phase = usbcopy.JobCompleted
	job.EngineRestored = true
	job.FilesDone = job.FilesTotal
	job.BytesDone = job.BytesTotal
	job.ResultAvailable = true
	job.CopiedFiles = len(filtered.Tracks)
	job.SkippedFiles = skipped
	job.CreatedPlaylists = len(state.Playlists)
	if job.CopiedFiles > 0 {
		job.PlaylistName = filtered.CopyPlaylist.Name
	}
	_, e = store.Save(job)
	return e
}
