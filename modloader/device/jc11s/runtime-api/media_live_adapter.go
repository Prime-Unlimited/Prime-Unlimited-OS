package main

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	usbcopy "prime-unlimited.usb-copy/worker"
)

type liveCreatedList struct {
	Ref  usbcopy.LivePlaylistReference
	Name string
}
type mediaLiveAdapter struct {
	id, root, database, source, destination string
	binding                                 usbcopy.LiveBinding
	queue                                   *usbcopy.FileLiveCommands
	plan                                    usbcopy.ImportPlan
	decisions                               []usbcopy.DuplicateDecision
	skipped                                 int
	validate                                func(context.Context) (usbcopy.LiveBinding, error)
	progress                                func(usbcopy.JobPhase, usbcopy.CopyProgress) error
	// Optional test transport; production uses the durable on-disk broker.
	exchange   func(context.Context, usbcopy.LiveCommand, func(usbcopy.LiveCommandObservation) error) (usbcopy.LiveCommandObservation, error)
	registered map[string]liveRegisteredTrack
	refs       map[string]usbcopy.LiveTrackReference
	uris       map[string]string
	lists      map[int64]liveCreatedList
}

var _ usbcopy.LiveSequenceAdapter = (*mediaLiveAdapter)(nil)

func (a *mediaLiveAdapter) Validate(ctx context.Context) (usbcopy.LiveBinding, error) {
	if a.validate == nil {
		return usbcopy.LiveBinding{}, errors.New("live validator unavailable")
	}
	b, e := a.validate(ctx)
	if e != nil {
		return b, e
	}
	if b != a.binding {
		return b, errors.New("live library or Engine changed")
	}
	return b, nil
}
func (a *mediaLiveAdapter) invoke(ctx context.Context, intent, operation string, c usbcopy.LiveCommand, proof func(usbcopy.LiveCommandObservation) error) (usbcopy.LiveCommandObservation, error) {
	var empty usbcopy.LiveCommandObservation
	if proof == nil {
		return empty, errors.New("native command proof unavailable")
	}
	if _, e := a.Validate(ctx); e != nil {
		return empty, e
	}
	c.JobID = a.id
	c.Intent = intent
	c.Operation = operation
	c.Binding = a.binding
	c.ExpiresAt = time.Now().UTC().Add(2 * time.Minute)
	c.ID = usbcopy.LiveCommandID(a.id, intent)
	verify := func(o usbcopy.LiveCommandObservation) error {
		if o.Outcome != "applied" || o.Binding != a.binding || o.SourceDevice != a.destination || o.CollectionDevice != a.destination || o.LibraryUUID != a.binding.LibraryUUID {
			return errors.New("native command context differs from approved internal library")
		}
		if e := liveWaitProof(ctx, func() error { return proof(o) }); e != nil {
			return e
		}
		_, e := a.Validate(ctx)
		return e
	}
	if a.exchange != nil {
		return a.exchange(ctx, c, verify)
	}
	if a.queue == nil {
		return empty, errors.New("native command broker unavailable")
	}
	if e := a.queue.Enqueue(ctx, c, time.Now()); e != nil {
		return empty, e
	}
	o, e := a.queue.AwaitObservation(ctx, c)
	if e != nil {
		return empty, e
	}
	if _, e = a.Validate(ctx); e != nil {
		return empty, e
	}
	e = a.queue.Verify(ctx, c.ID, a.binding, func(_ usbcopy.LiveCommand, observed usbcopy.LiveCommandObservation) error { return verify(observed) })
	return o, e
}
func (a *mediaLiveAdapter) Backup(ctx context.Context, id string) error {
	if id != a.id {
		return errors.New("live backup job differs")
	}
	st, e := os.Stat(a.database)
	if e != nil {
		return e
	}
	if e = usbcopy.RequireLiveImportSpace(a.destination, a.decisions, st.Size()); e != nil {
		return e
	}
	dir, e := mediaLiveDirectory(a.root, id)
	if e != nil {
		return e
	}
	type manifestTrack struct {
		Source, Destination, SHA256, ArtworkSHA1 string
		Bytes                                    int64
		Tags                                     usbcopy.Tags
	}
	manifest := struct {
		Binding         usbcopy.LiveBinding
		ImportDirectory string
		Tracks          []manifestTrack
	}{Binding: a.binding, ImportDirectory: a.plan.ImportDirectory}
	bySource := map[string]usbcopy.FileFingerprint{}
	for _, d := range a.decisions {
		bySource[d.SourceRelative] = d.Fingerprint
	}
	for _, t := range a.plan.Tracks {
		art := ""
		if len(t.Artwork) > 0 {
			sum := sha1.Sum(t.Artwork)
			art = hex.EncodeToString(sum[:])
		}
		manifest.Tracks = append(manifest.Tracks, manifestTrack{t.SourceRelative, liveTrackPath(t), bySource[t.SourceRelative].SHA256, art, t.Bytes, t.Tags})
	}
	if e = writeAtomicJSON(filepath.Join(dir, "manifest.json"), manifest); e != nil {
		return e
	}
	backup := filepath.Join(filepath.Dir(a.database), "usb-copy-before-live-"+id+".sqlite-backup")
	counts, e := snapshotNativeCreate(ctx, a.database, backup, a.binding.LibraryUUID)
	if e != nil {
		return e
	}
	return writeAtomicJSON(filepath.Join(dir, "backup.json"), struct {
		Path   string
		Counts nativeLibraryCounts
	}{backup, counts})
}
func (a *mediaLiveAdapter) SelectLibrary(ctx context.Context, intent string) error {
	if _, e := a.invoke(ctx, intent+":source", "select_library", usbcopy.LiveCommand{}, func(usbcopy.LiveCommandObservation) error { return nil }); e != nil {
		return e
	}
	_, e := a.invoke(ctx, intent+":all", "all_tracks", usbcopy.LiveCommand{}, func(o usbcopy.LiveCommandObservation) error {
		if !o.AllTracks {
			return errors.New("native All Tracks view unavailable")
		}
		return nil
	})
	return e
}
func (a *mediaLiveAdapter) Copy(ctx context.Context, id string, p usbcopy.ImportPlan, d []usbcopy.DuplicateDecision) error {
	if id != a.id || len(p.Tracks) != len(a.plan.Tracks) || p.ImportDirectory != a.plan.ImportDirectory {
		return errors.New("live copy plan differs")
	}
	limited, cancel := context.WithCancel(ctx)
	defer cancel()
	var progressErr error
	_, e := usbcopy.CopyLivePlan(limited, a.source, filepath.Join(a.destination, "Engine Library", "Music"), id, p, d, func(progress usbcopy.CopyProgress) {
		if progressErr == nil && a.progress != nil {
			progressErr = a.progress(usbcopy.JobCopying, progress)
			if progressErr != nil {
				cancel()
			}
		}
	})
	if progressErr != nil {
		return progressErr
	}
	if e != nil {
		return e
	}
	if a.progress != nil {
		return a.progress(usbcopy.JobCommitting, usbcopy.CopyProgress{FilesDone: len(p.Tracks), FilesTotal: len(p.Tracks), BytesDone: p.BytesToCopy, BytesTotal: p.BytesToCopy})
	}
	return nil
}
func (a *mediaLiveAdapter) checkCopies(ctx context.Context, tracks []usbcopy.PlannedTrack) error {
	fingerprints := map[string]usbcopy.FileFingerprint{}
	for _, d := range a.decisions {
		fingerprints[d.SourceRelative] = d.Fingerprint
	}
	for _, t := range tracks {
		fp, e := usbcopy.FingerprintAudioFile(ctx, filepath.Join(a.destination, "Engine Library"), liveTrackPath(t))
		expected := fingerprints[t.SourceRelative]
		if e != nil || expected.SHA256 == "" || fp.SHA256 != expected.SHA256 || fp.Bytes != expected.Bytes {
			return errors.New("copied file differs from approved source fingerprint")
		}
	}
	return nil
}
func (a *mediaLiveAdapter) registerAll(ctx context.Context, intent string) error {
	a.registered = map[string]liveRegisteredTrack{}
	a.refs = map[string]usbcopy.LiveTrackReference{}
	a.uris = map[string]string{}
	// Keep native import calls bounded; scan the model in larger lookup batches.
	for start := 0; start < len(a.plan.Tracks); start += 32 {
		end := start + 32
		if end > len(a.plan.Tracks) {
			end = len(a.plan.Tracks)
		}
		batch := a.plan.Tracks[start:end]
		if e := a.checkCopies(ctx, batch); e != nil {
			return e
		}
		paths := []string{}
		for _, t := range batch {
			paths = append(paths, liveTrackPath(t))
		}
		_, e := a.invoke(ctx, fmt.Sprintf("%s:files:%d", intent, start), "register_files", usbcopy.LiveCommand{Paths: paths}, func(o usbcopy.LiveCommandObservation) error {
			if !o.AllTracks {
				return errors.New("registration left internal All Tracks context")
			}
			for _, t := range batch {
				registered, e := liveRegistered(ctx, a.database, a.destination, a.binding.LibraryUUID, t)
				if e != nil {
					return e
				}
				a.registered[t.SourceRelative] = registered
			}
			return a.checkCopies(ctx, batch)
		})
		if e != nil {
			return e
		}
	}
	for start := 0; start < len(a.plan.Tracks); start += 512 {
		end := start + 512
		if end > len(a.plan.Tracks) {
			end = len(a.plan.Tracks)
		}
		batch := a.plan.Tracks[start:end]
		ids := []int64{}
		for _, t := range batch {
			ids = append(ids, a.registered[t.SourceRelative].ID)
		}
		_, e := a.invoke(ctx, fmt.Sprintf("%s:lookup:%d", intent, start), "lookup_tracks", usbcopy.LiveCommand{LocalIDs: ids}, func(o usbcopy.LiveCommandObservation) error {
			if !o.AllTracks || len(o.Tracks) != len(batch) || len(o.URIs) != len(batch) {
				return errors.New("incomplete native batch lookup")
			}
			seen := map[string]bool{}
			for i, t := range batch {
				ref := o.Tracks[i]
				if seen[ref.NativeID] {
					return errors.New("duplicate native lookup identity")
				}
				seen[ref.NativeID] = true
				if e := liveCheckNativeTrack(ctx, a.database, a.destination, a.registered[t.SourceRelative], ref, o.URIs[i], a.binding); e != nil {
					return e
				}
				ref.SourceRelative = t.SourceRelative
				a.refs[t.SourceRelative] = ref
				a.uris[t.SourceRelative] = o.URIs[i]
			}
			return nil
		})
		if e != nil {
			return e
		}
	}
	return nil
}
func (a *mediaLiveAdapter) Register(ctx context.Context, intent string, t usbcopy.PlannedTrack) (usbcopy.LiveTrackReference, error) {
	if a.refs == nil {
		if e := a.registerAll(ctx, intent); e != nil {
			return usbcopy.LiveTrackReference{}, e
		}
	}
	ref, ok := a.refs[t.SourceRelative]
	if !ok {
		return ref, errors.New("copied track lacks verified native identity")
	}
	return ref, nil
}
func (a *mediaLiveAdapter) CreatePlaylist(ctx context.Context, intent, title string) (usbcopy.LivePlaylistReference, error) {
	var ref usbcopy.LivePlaylistReference
	db, e := openNativeReadDB(a.database)
	if e != nil {
		return ref, e
	}
	var existing int
	e = db.QueryRowContext(ctx, "SELECT count(*) FROM Playlist WHERE title=?", title).Scan(&existing)
	var beforeMax int64
	if e == nil {
		e = db.QueryRowContext(ctx, "SELECT coalesce(max(id),0) FROM Playlist").Scan(&beforeMax)
	}
	before, beforeErr := nativeCounts(ctx, db)
	db.Close()
	if e != nil {
		return ref, e
	}
	if beforeErr != nil {
		return ref, beforeErr
	}
	if existing != 0 {
		return ref, errors.New("native playlist name already exists")
	}
	_, e = a.invoke(ctx, intent, "create_playlist", usbcopy.LiveCommand{Title: title}, func(o usbcopy.LiveCommandObservation) error {
		if o.Playlist == nil || o.Playlist.Binding != a.binding {
			return errors.New("native playlist identity missing")
		}
		n, e := strconv.ParseUint(o.Playlist.NativeID, 10, 64)
		if e != nil || n == 0 || n > (1<<53)-1 {
			return errors.New("unsupported native playlist identity")
		}
		db, e := openNativeReadDB(a.database)
		if e != nil {
			return e
		}
		defer db.Close()
		rows, e := db.QueryContext(ctx, "SELECT id FROM Playlist WHERE title=? AND parentListId=0", title)
		if e != nil {
			return e
		}
		count := 0
		var id int64
		for rows.Next() {
			count++
			if e = rows.Scan(&id); e != nil {
				rows.Close()
				return e
			}
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return e
		}
		if count != 1 || id <= beforeMax {
			return errors.New("new native playlist is not uniquely registered at root")
		}
		after, e := nativeCounts(ctx, db)
		if e != nil {
			return e
		}
		if after.Playlists != before.Playlists+1 || after.Tracks != before.Tracks || after.Entities != before.Entities {
			return errors.New("unexpected library changes during playlist creation")
		}
		members, e := liveMembers(ctx, db, id)
		if e != nil {
			return e
		}
		if len(members) != 0 {
			return errors.New("new native playlist is not empty")
		}
		ref = usbcopy.LivePlaylistReference{NativeID: o.Playlist.NativeID, LocalID: id, Binding: a.binding, Title: title}
		return nil
	})
	if e != nil {
		return ref, e
	}
	if a.lists == nil {
		a.lists = map[int64]liveCreatedList{}
	}
	if _, exists := a.lists[ref.LocalID]; exists {
		return ref, errors.New("native playlist ID reused")
	}
	a.lists[ref.LocalID] = liveCreatedList{ref, title}
	return ref, nil
}
func (a *mediaLiveAdapter) Add(ctx context.Context, intent string, list usbcopy.LivePlaylistReference, refs []usbcopy.LiveTrackReference) error {
	created, ok := a.lists[list.LocalID]
	if !ok || created.Ref != list {
		return errors.New("unverified native playlist target")
	}
	for start := 0; start < len(refs); start += 512 {
		end := start + 512
		if end > len(refs) {
			end = len(refs)
		}
		batch := refs[start:end]
		db, e := openNativeReadDB(a.database)
		if e != nil {
			return e
		}
		var title string
		e = db.QueryRowContext(ctx, "SELECT title FROM Playlist WHERE id=?", list.LocalID).Scan(&title)
		expected, memberErr := liveMembers(ctx, db, list.LocalID)
		db.Close()
		if e != nil {
			return e
		}
		if memberErr != nil {
			return memberErr
		}
		if title != created.Name {
			return errors.New("native playlist changed")
		}
		uris := []string{}
		for _, ref := range batch {
			if a.refs[ref.SourceRelative] != ref || a.uris[ref.SourceRelative] == "" {
				return errors.New("playlist includes an unverified or skipped track")
			}
			key := liveMemberKey(a.registered[ref.SourceRelative].Origin)
			if expected[key] != 0 {
				return errors.New("track already present in new playlist")
			}
			expected[key] = 1
			uris = append(uris, a.uris[ref.SourceRelative])
		}
		_, e = a.invoke(ctx, fmt.Sprintf("%s:batch:%d", intent, start), "add_tracks", usbcopy.LiveCommand{Playlist: &list, Tracks: batch, TrackURIs: uris}, func(usbcopy.LiveCommandObservation) error {
			db, e := openNativeReadDB(a.database)
			if e != nil {
				return e
			}
			defer db.Close()
			var current string
			if e = db.QueryRowContext(ctx, "SELECT title FROM Playlist WHERE id=?", list.LocalID).Scan(&current); e != nil {
				return e
			}
			if current != created.Name {
				return errors.New("native playlist title changed")
			}
			actual, e := liveMembers(ctx, db, list.LocalID)
			if e != nil {
				return e
			}
			return liveExactMembers(actual, expected)
		})
		if e != nil {
			return e
		}
	}
	return nil
}
func (a *mediaLiveAdapter) Verify(ctx context.Context, p usbcopy.ImportPlan, state usbcopy.LiveSequenceState) error {
	if len(state.Tracks) != len(p.Tracks) || len(state.Playlists) != len(a.lists) {
		return errors.New("incomplete live import result")
	}
	if e := a.checkCopies(ctx, p.Tracks); e != nil {
		return e
	}
	for _, t := range p.Tracks {
		registered, e := liveRegistered(ctx, a.database, a.destination, a.binding.LibraryUUID, t)
		if e != nil {
			return e
		}
		if registered != a.registered[t.SourceRelative] {
			return errors.New("registered track changed before completion")
		}
	}
	plans := append([]usbcopy.PlannedPlaylist{p.CopyPlaylist}, p.SourcePlaylists...)
	for _, plan := range plans {
		var list liveCreatedList
		found := 0
		for _, v := range a.lists {
			if v.Name == plan.Name {
				list = v
				found++
			}
		}
		if found != 1 {
			return errors.New("planned playlist result is missing")
		}
		expected := map[string]int{}
		for _, source := range plan.Tracks {
			registered, ok := a.registered[source]
			if !ok {
				return errors.New("playlist refers to a skipped track")
			}
			expected[liveMemberKey(registered.Origin)] = 1
		}
		db, e := openNativeReadDB(a.database)
		if e != nil {
			return e
		}
		actual, e := liveMembers(ctx, db, list.Ref.LocalID)
		db.Close()
		if e != nil {
			return e
		}
		if e = liveExactMembers(actual, expected); e != nil {
			return e
		}
	}
	return nil
}
