package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const nativeTestPlaylistTitle = "USB Copy Live Test 20261122-124235 - 88487aa6"

func nativeEngineIdentity() (string, error) {
	boot, err := mediaBootID()
	if err != nil {
		return "", err
	}
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return "", err
	}
	found := ""
	for _, entry := range entries {
		if _, err := strconv.Atoi(entry.Name()); err != nil {
			continue
		}
		target, _ := os.Readlink(filepath.Join("/proc", entry.Name(), "exe"))
		if target != "/usr/Engine/Engine" {
			continue
		}
		data, err := os.ReadFile(filepath.Join("/proc", entry.Name(), "stat"))
		if err != nil {
			return "", err
		}
		end := strings.LastIndex(string(data), ")")
		if end < 0 {
			return "", errors.New("invalid process stat")
		}
		fields := strings.Fields(string(data)[end+1:])
		if len(fields) < 20 {
			return "", errors.New("short process stat")
		}
		if found != "" {
			return "", errors.New("ambiguous engine instance")
		}
		found = boot + ":" + entry.Name() + ":" + fields[19]
	}
	if found == "" {
		return "", errors.New("engine unavailable")
	}
	return found, nil
}
func nativeAddMatches(view nativePlaylistInstance, state nativeCreateState) bool {
	return view.Visible && view.Enabled && view.CanAdd && view.SelectedTestPlaylist && view.SourceDevice == state.SourceDevice && view.RootRow == state.RootRow && view.CurrentRow == state.CurrentRow && view.SelectedListID == state.TargetListID
}
func prepareNativeAdd(ctx context.Context, state *nativeCreateState, database, backup string) error {
	wantedID := int64(16700)
	wantedTitle := "DJ Intro.mp3"
	if state.RegisteredTrackID == 16702 && state.UserConfirmedRegistration {
		wantedID = 16702
		wantedTitle = "Andromeda (Original Mix)"
	}
	id, err := strconv.ParseUint(state.NativeTrackID, 10, 64)
	if err != nil || id > (1<<53)-1 || id&((1<<48)-1) != uint64(wantedID) {
		return errors.New("unexpected native track identity")
	}
	current, err := nativeEngineIdentity()
	if err != nil || current != state.EngineIdentity {
		return errors.New("engine instance changed")
	}
	db, err := openNativeReadDB(database)
	if err != nil {
		return err
	}
	defer db.Close()
	var title string
	if err = db.QueryRowContext(ctx, `SELECT title,originDatabaseUuid,originTrackId FROM Track WHERE id=?`, wantedID).Scan(&title, &state.TrackOriginUUID, &state.TrackOriginID); err != nil {
		return err
	}
	if title != wantedTitle {
		return errors.New("unexpected source title")
	}
	var playlist int64
	if err = db.QueryRowContext(ctx, `SELECT id FROM Playlist WHERE title=?`, nativeTestPlaylistTitle).Scan(&playlist); err != nil {
		return err
	}
	if playlist != state.PlaylistID {
		return errors.New("test playlist changed")
	}

	state.BeforeListMembers = nil
	rows, err := db.QueryContext(ctx, "SELECT databaseUuid,trackId FROM PlaylistEntity WHERE listId=? ORDER BY id", playlist)
	if err != nil {
		return err
	}
	for rows.Next() {
		var m nativePlaylistMember
		if err = rows.Scan(&m.UUID, &m.Track); err != nil {
			rows.Close()
			return err
		}
		state.BeforeListMembers = append(state.BeforeListMembers, m)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if wantedID == 16700 && len(state.BeforeListMembers) != 0 {
		return errors.New("initial test playlist is not empty")
	}
	if wantedID == 16702 && len(state.BeforeListMembers) != 1 {
		return errors.New("registered-track test needs one existing member")
	}
	for _, m := range state.BeforeListMembers {
		if m.UUID == state.TrackOriginUUID && m.Track == state.TrackOriginID {
			return errors.New("track is already in playlist")
		}
	}
	state.Before, err = snapshotNativeCreate(ctx, database, backup, state.Identity.LibraryUUID)
	return err
}
func verifyNativeAdd(ctx context.Context, state *nativeCreateState, database string) error {
	db, err := openNativeReadDB(database)
	if err != nil {
		return err
	}
	defer db.Close()
	var title string
	if err = db.QueryRowContext(ctx, `SELECT title FROM Playlist WHERE id=?`, state.PlaylistID).Scan(&title); err != nil {
		return err
	}
	if title != state.Title {
		return errors.New("target title changed")
	}
	var total, matched int
	if err = db.QueryRowContext(ctx, `SELECT count(*),coalesce(sum(CASE WHEN databaseUuid=? AND trackId=? THEN 1 ELSE 0 END),0) FROM PlaylistEntity WHERE listId=?`, state.TrackOriginUUID, state.TrackOriginID, state.PlaylistID).Scan(&total, &matched); err != nil {
		return err
	}
	if total > len(state.BeforeListMembers)+1 || matched > 1 || (total == len(state.BeforeListMembers)+1 && matched != 1) {
		return errors.New("unexpected playlist membership")
	}
	for _, m := range state.BeforeListMembers {
		var n int
		if e := db.QueryRowContext(ctx, "SELECT count(*) FROM PlaylistEntity WHERE listId=? AND databaseUuid=? AND trackId=?", state.PlaylistID, m.UUID, m.Track).Scan(&n); e != nil || n != 1 {
			return errors.New("existing playlist member changed")
		}
	}
	state.After, err = nativeCounts(ctx, db)
	if err != nil {
		return err
	}
	if total == len(state.BeforeListMembers)+1 && matched == 1 && state.RowsInserted && state.After.Tracks == state.Before.Tracks && state.After.Playlists == state.Before.Playlists && state.After.Entities == state.Before.Entities+1 {
		state.Phase = "verified"
	}
	return nil
}
func (s *server) queueNativeAdd(w http.ResponseWriter, r *http.Request, previous nativeCreateState, loadErr error) {
	registrationReviewed := previous.Operation == "register" && previous.Phase == "registration_verified" && previous.UserConfirmedRegistration && previous.RegisteredTrackID == 16702
	initialTest := previous.Phase == "verified" && previous.Operation != "add" && previous.Operation != "register"
	if loadErr != nil || (!initialTest && !registrationReviewed) || previous.Title != nativeTestPlaylistTitle || previous.PlaylistID != 2396 {
		writeJSON(w, 409, map[string]string{"error": "verified empty test playlist required"})
		return
	}
	var input struct {
		Confirm        bool   `json:"confirm"`
		NativeTrackID  string `json:"nativeTrackId"`
		EngineIdentity string `json:"engineIdentity"`
	}
	data, err := io.ReadAll(io.LimitReader(r.Body, 1025))
	if err != nil || len(data) > 1024 || decodeStrictJSON(data, &input) != nil || !input.Confirm {
		writeJSON(w, 400, map[string]string{"error": "explicit single-track confirmation required"})
		return
	}
	release, err := nativeCreateLease(s.root)
	if err != nil {
		writeJSON(w, 409, map[string]string{"error": "import is active or requires review"})
		return
	}
	defer release()
	s.nativeProbeMu.Lock()
	var view nativePlaylistInstance
	count := 0
	if s.nativeProbeObservation != nil && time.Now().Before(s.nativeProbeUntil) && time.Since(s.nativeProbeReceivedAt) < 15*time.Second {
		for _, v := range s.nativeProbeObservation.Instances {
			if v.Visible && v.Enabled && v.CanAdd && v.SelectedTestPlaylist && v.SourceDevice == previous.SourceDevice {
				view = v
				count++
			}
		}
	}
	s.nativeProbeMu.Unlock()
	if count != 1 || view.SelectedListID == "" {
		writeJSON(w, 409, map[string]string{"error": "one visible test playlist required"})
		return
	}
	if _, err = nativeCreateTarget(r.Context(), previous); err != nil {
		writeJSON(w, 409, map[string]string{"error": "internal target changed"})
		return
	}
	id, err := newServiceToken()
	if err != nil {
		writeJSON(w, 503, map[string]string{"error": "identity unavailable"})
		return
	}
	now := time.Now().UTC()
	state := previous
	state.ID = id
	state.Operation = "add"
	state.NativeTrackID = input.NativeTrackID
	state.EngineIdentity = input.EngineIdentity
	state.TargetListID = view.SelectedListID
	state.CurrentRow = view.CurrentRow
	state.RootRow = view.RootRow
	state.Phase = "queued"
	state.CreatedAt = now
	state.ExpiresAt = now.Add(time.Minute)
	state.RowsInserted = false
	state.After = nativeLibraryCounts{}
	directory, err := nativeCreateDirectory(s.root)
	if err != nil {
		writeJSON(w, 503, map[string]string{"error": "state unavailable"})
		return
	}
	database, err := internalEngineDatabase(state.SourceDevice)
	if err == nil {
		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		defer cancel()
		err = prepareNativeAdd(ctx, &state, database, filepath.Join(directory, id+".sqlite-backup"))
	}
	if err != nil {
		writeJSON(w, 409, map[string]string{"error": "source, target or snapshot verification failed; no command queued"})
		return
	}
	if err = writeAtomicJSON(filepath.Join(directory, previous.ID+".completed.json"), previous); err != nil {
		writeJSON(w, 503, map[string]string{"error": "previous result could not be preserved"})
		return
	}
	if err = saveNativeCreate(s.root, state); err != nil {
		writeJSON(w, 503, map[string]string{"error": "state could not be saved"})
		return
	}
	writeJSON(w, 202, state)
}
