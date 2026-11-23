package main

import (
	"context"
	"database/sql"
	"errors"
	"golang.org/x/sys/unix"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	usbcopy "prime-unlimited.usb-copy/worker"
	"strings"
	"time"
)

type nativeLibraryCounts struct {
	Tracks    int `json:"tracks"`
	Playlists int `json:"playlists"`
	Entities  int `json:"entities"`
}
type nativePlaylistMember struct {
	UUID  string `json:"uuid"`
	Track int64  `json:"track"`
}
type nativeCreateState struct {
	LookupOriginUUID          string                   `json:"lookupOriginUuid,omitempty"`
	LookupOriginID            int64                    `json:"lookupOriginId,omitempty"`
	LookupLocalID             int64                    `json:"lookupLocalId,omitempty"`
	LookupFileURL             string                   `json:"lookupFileUrl,omitempty"`
	LookupURI                 string                   `json:"lookupUri,omitempty"`
	SelectionReviewed         bool                     `json:"selectionReviewed,omitempty"`
	BeforeListMembers         []nativePlaylistMember   `json:"beforeListMembers,omitempty"`
	UserConfirmedRegistration bool                     `json:"userConfirmedRegistration,omitempty"`
	NativeCollectionComplete  bool                     `json:"nativeCollectionComplete,omitempty"`
	CopiedRelative            string                   `json:"copiedRelative,omitempty"`
	FileURL                   string                   `json:"fileUrl,omitempty"`
	RegisteredTrackID         int64                    `json:"registeredTrackId,omitempty"`
	RegisteredTitle           string                   `json:"registeredTitle,omitempty"`
	RegisteredArtist          string                   `json:"registeredArtist,omitempty"`
	RegisteredArtworkHash     string                   `json:"registeredArtworkHash,omitempty"`
	RegisteredArtworkFile     bool                     `json:"registeredArtworkFile,omitempty"`
	RegisteredMembership      int                      `json:"registeredMembership,omitempty"`
	Operation                 string                   `json:"operation,omitempty"`
	NativeTrackID             string                   `json:"nativeTrackId,omitempty"`
	EngineIdentity            string                   `json:"engineIdentity,omitempty"`
	TargetListID              string                   `json:"targetListId,omitempty"`
	CurrentRow                int                      `json:"currentRow,omitempty"`
	TrackOriginUUID           string                   `json:"trackOriginUuid,omitempty"`
	TrackOriginID             int64                    `json:"trackOriginId,omitempty"`
	ID                        string                   `json:"id"`
	Title                     string                   `json:"title"`
	SourceDevice              string                   `json:"sourceDevice"`
	RootRow                   int                      `json:"rootRow"`
	Identity                  mediaDestinationIdentity `json:"identity"`
	Mount                     mediaMountIdentity       `json:"mount"`
	Phase                     string                   `json:"phase"`
	CreatedAt                 time.Time                `json:"createdAt"`
	ExpiresAt                 time.Time                `json:"expiresAt"`
	Before                    nativeLibraryCounts      `json:"before"`
	After                     nativeLibraryCounts      `json:"after"`
	PlaylistID                int64                    `json:"playlistId"`
	RowsInserted              bool                     `json:"rowsInserted"`
}

func nativeCounts(ctx context.Context, db *sql.DB) (out nativeLibraryCounts, err error) {
	for table, dest := range map[string]*int{"Track": &out.Tracks, "Playlist": &out.Playlists, "PlaylistEntity": &out.Entities} {
		if err = db.QueryRowContext(ctx, "SELECT count(*) FROM "+table).Scan(dest); err != nil {
			return
		}
	}
	return
}
func openNativeReadDB(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", (&url.URL{Scheme: "file", Path: path, RawQuery: "mode=ro"}).String())
	if err == nil {
		db.SetMaxOpenConns(1)
	}
	return db, err
}
func snapshotNativeCreate(ctx context.Context, database, backup, uuid string) (nativeLibraryCounts, error) {
	var out nativeLibraryCounts
	db, err := openNativeReadDB(database)
	if err != nil {
		return out, err
	}
	defer db.Close()
	if _, err = db.ExecContext(ctx, "PRAGMA busy_timeout=1500"); err != nil {
		return out, err
	}
	var actual string
	if err = db.QueryRowContext(ctx, "SELECT uuid FROM Information ORDER BY id LIMIT 1").Scan(&actual); err != nil || strings.ToLower(actual) != uuid {
		return out, errors.New("library changed before snapshot")
	}
	if _, err = db.ExecContext(ctx, "VACUUM INTO ?", backup); err != nil {
		return out, err
	}
	copy, err := openNativeReadDB(backup)
	if err != nil {
		return out, err
	}
	defer copy.Close()
	var integrity string
	if err = copy.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&integrity); err != nil || integrity != "ok" {
		return out, errors.New("snapshot integrity failed")
	}
	return nativeCounts(ctx, copy)
}
func nativeCreateDirectory(root string) (string, error) {
	path := filepath.Join(root, "native-playlist-test")
	return path, ensurePrivateDirectory(path)
}
func readNativeCreate(root string) (nativeCreateState, error) {
	var state nativeCreateState
	directory, err := nativeCreateDirectory(root)
	if err != nil {
		return state, err
	}
	data, err := readOwnedFile(filepath.Join(directory, "state.json"), 16384, "native create state")
	if err != nil {
		return state, err
	}
	if decodeStrictJSON(data, &state) != nil || !validMediaEventID(state.ID) || !state.Identity.valid() {
		return state, errors.New("invalid native create state")
	}
	return state, nil
}
func saveNativeCreate(root string, state nativeCreateState) error {
	directory, err := nativeCreateDirectory(root)
	if err != nil {
		return err
	}
	return writeAtomicJSON(filepath.Join(directory, "state.json"), state)
}
func nativeCreateTarget(ctx context.Context, state nativeCreateState) (mountedBlockVolume, error) {
	volumes, err := readMountedBlockVolumes()
	if err != nil {
		return mountedBlockVolume{}, err
	}
	volume, ok := mediaJobMountMatch(volumes, state.Mount)
	if !ok || !isInternalMediaDestination(volume) {
		return volume, errors.New("native target mount changed")
	}
	identity, err := readMediaDestinationIdentity(ctx, volume)
	if err != nil || identity != state.Identity {
		return volume, errors.New("native target library changed")
	}
	return volume, nil
}
func (s *server) nativeCreateViewMatches(state nativeCreateState) bool {
	s.nativeProbeMu.Lock()
	defer s.nativeProbeMu.Unlock()
	if s.nativeProbeObservation == nil || !time.Now().Before(s.nativeProbeUntil) || time.Since(s.nativeProbeReceivedAt) > 15*time.Second {
		return false
	}
	if state.Operation == "register" && (!s.nativeProbeObservation.CollectionIsLibrary || !s.nativeProbeObservation.CanRegisterFiles) {
		return false
	}
	if state.Operation == "lookup" {
		return nativeSelectionMatches(s.nativeProbeObservation, state, true) && s.nativeProbeObservation.CollectionPath == state.SourceDevice && (s.nativeProbeObservation.CollectionListID == "0" || s.nativeProbeObservation.CollectionListID == "-1")
	}
	if state.Operation == "all_tracks" {
		return nativeSelectionMatches(s.nativeProbeObservation, state, true)
	}
	if state.Operation == "select" {
		return nativeSelectionMatches(s.nativeProbeObservation, state, false)
	}
	n := 0
	for _, view := range s.nativeProbeObservation.Instances {
		if state.Operation == "add" || state.Operation == "register" {
			if nativeAddMatches(view, state) {
				n++
			}
			continue
		}
		if view.Visible && view.Enabled && view.CanCreate && view.RootRow == state.RootRow && view.SourceDevice == state.SourceDevice {
			n++
		}
	}
	return n == 1
}
func verifyNativeCreate(ctx context.Context, state *nativeCreateState, database string) error {
	db, err := openNativeReadDB(database)
	if err != nil {
		return err
	}
	defer db.Close()
	if _, err = db.ExecContext(ctx, "PRAGMA busy_timeout=1500"); err != nil {
		return err
	}
	rows, err := db.QueryContext(ctx, "SELECT id,parentListId,nextListId FROM Playlist WHERE title=?", state.Title)
	if err != nil {
		return err
	}
	count := 0
	var id int64
	var parent, next sql.NullInt64
	for rows.Next() {
		count++
		if err = rows.Scan(&id, &parent, &next); err != nil {
			rows.Close()
			return err
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if count == 0 {
		return nil
	}
	if count != 1 {
		return errors.New("ambiguous created playlist")
	}
	var members int
	if err = db.QueryRowContext(ctx, "SELECT count(*) FROM PlaylistEntity WHERE listId=?", id).Scan(&members); err != nil {
		return err
	}
	if members != 0 {
		return errors.New("unexpected playlist members")
	}
	if next.Valid && next.Int64 > 0 {
		var links int
		if err = db.QueryRowContext(ctx, "SELECT count(*) FROM Playlist WHERE id=? AND parentListId IS ?", next.Int64, parent).Scan(&links); err != nil || links != 1 {
			return errors.New("created playlist has broken next link")
		}
	}
	state.After, err = nativeCounts(ctx, db)
	if err != nil {
		return err
	}
	state.PlaylistID = id
	if state.RowsInserted && state.After.Tracks == state.Before.Tracks && state.After.Entities == state.Before.Entities && state.After.Playlists == state.Before.Playlists+1 {
		state.Phase = "verified"
	}
	return nil
}
func nativeCreateActive(state nativeCreateState) bool {
	if state.Operation == "lookup" && state.Phase == "lookup_failed" {
		return false
	}
	if state.Phase == "selection_noop" && state.Operation == "select" && state.SelectionReviewed {
		return false
	}
	return state.Phase != "verified" && state.Phase != "not_invoked" && !(state.Phase == "registration_verified" && state.Operation == "register" && state.UserConfirmedRegistration)
}
func nativeCreateLease(root string) (func(), error) {
	store, err := usbcopy.OpenJobStore(filepath.Join(root, "media-jobs"))
	if err != nil {
		return nil, err
	}
	files := []*os.File{}
	release := func() {
		for i := len(files) - 1; i >= 0; i-- {
			unix.Flock(int(files[i].Fd()), unix.LOCK_UN)
			files[i].Close()
		}
	}
	for _, name := range []string{"admission.lock", "supervisor.lock"} {
		f, e := os.OpenFile(filepath.Join(root, "media-jobs", name), os.O_CREATE|os.O_RDWR|unix.O_NOFOLLOW, 0600)
		if e != nil {
			release()
			return nil, e
		}
		if e = unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); e != nil {
			f.Close()
			release()
			return nil, e
		}
		files = append(files, f)
	}
	jobs, err := store.List()
	if err != nil {
		release()
		return nil, err
	}
	for _, job := range jobs {
		switch job.Phase {
		case usbcopy.JobCompleted, usbcopy.JobFailed, usbcopy.JobCancelled:
		default:
			release()
			return nil, errors.New("another import is active or requires review")
		}
	}
	return release, nil
}
func (s *server) handleNativePlaylistCreate(w http.ResponseWriter, r *http.Request) {
	if !s.authorized(r) {
		writeJSON(w, 401, map[string]string{"error": "trusted UI authorization required"})
		return
	}
	if r.URL.RawQuery != "" || r.URL.Fragment != "" {
		writeJSON(w, 400, map[string]string{"error": "query parameters are not supported"})
		return
	}
	if s.model != "JP24" || !s.mediaImportWrites || !s.mediaImportEnabled() {
		writeJSON(w, 403, map[string]string{"error": "experimental JP24 import approval required"})
		return
	}
	s.nativeCreateMu.Lock()
	defer s.nativeCreateMu.Unlock()
	suffix := strings.TrimPrefix(r.URL.Path, "/api/v1/native-playlist/create-test")
	state, loadErr := readNativeCreate(s.root)
	if suffix == "/lookup-track" && r.Method == http.MethodPost {
		s.queueNativeTrackLookup(w, r, state, loadErr)
		return
	}
	if suffix == "/all-tracks" && r.Method == http.MethodPost {
		s.queueNativeLibrarySelection(w, r, state, loadErr, "all_tracks")
		return
	}
	if suffix == "/select-library" && r.Method == http.MethodPost {
		s.queueNativeLibrarySelection(w, r, state, loadErr, "select")
		return
	}
	if suffix == "/register-file" && r.Method == http.MethodPost {
		s.queueNativeRegistration(w, r, state, loadErr)
		return
	}
	if suffix == "/add-existing" && r.Method == http.MethodPost {
		s.queueNativeAdd(w, r, state, loadErr)
		return
	}
	if suffix == "" && r.Method == http.MethodPost {
		var input struct {
			Confirm bool `json:"confirm"`
		}
		data, err := io.ReadAll(io.LimitReader(r.Body, 257))
		if err != nil || len(data) > 256 || decodeStrictJSON(data, &input) != nil || !input.Confirm {
			writeJSON(w, 400, map[string]string{"error": "explicit confirmation required"})
			return
		}
		if loadErr != nil && !os.IsNotExist(loadErr) {
			writeJSON(w, 409, map[string]string{"error": "previous test state requires review"})
			return
		}
		if loadErr == nil && state.Phase != "verified" && state.Phase != "not_invoked" {
			writeJSON(w, 409, map[string]string{"error": "previous native command must be reviewed; no automatic retry"})
			return
		}
		release, leaseErr := nativeCreateLease(s.root)
		if leaseErr != nil {
			writeJSON(w, 409, map[string]string{"error": "another import is active or requires review"})
			return
		}
		defer release()
		s.nativeProbeMu.Lock()
		var view nativePlaylistInstance
		eligible := 0
		if s.nativeProbeObservation != nil && s.nativeProbeObservation.MainScreen && time.Now().Before(s.nativeProbeUntil) && time.Since(s.nativeProbeReceivedAt) < 15*time.Second {
			for _, v := range s.nativeProbeObservation.Instances {
				if v.Visible && v.Enabled && v.CanCreate && v.RootRow >= 0 && v.SourceDevice != "" {
					view = v
					eligible++
				}
			}
		}
		s.nativeProbeMu.Unlock()
		if eligible != 1 {
			writeJSON(w, 409, map[string]string{"error": "one visible native playlist root required"})
			return
		}
		volumes, err := readMountedBlockVolumes()
		if err != nil {
			writeJSON(w, 503, map[string]string{"error": "mount state unavailable"})
			return
		}
		var target mountedBlockVolume
		count := 0
		for _, v := range volumes {
			if v.mountPath == view.SourceDevice && isInternalMediaDestination(v) {
				target = v
				count++
			}
		}
		if count != 1 {
			writeJSON(w, 409, map[string]string{"error": "native target is not the internal music drive"})
			return
		}
		identity, err := readMediaDestinationIdentity(r.Context(), target)
		if err != nil {
			writeJSON(w, 409, map[string]string{"error": "native library identity unavailable"})
			return
		}
		id, err := newServiceToken()
		if err != nil {
			writeJSON(w, 503, map[string]string{"error": "command identity unavailable"})
			return
		}
		now := time.Now().UTC()
		state = nativeCreateState{ID: id, Title: "USB Copy Live Test " + now.Format("20060102-150405") + " - " + id[:8], SourceDevice: target.mountPath, RootRow: view.RootRow, Identity: identity, Mount: mediaIdentity(target), Phase: "queued", CreatedAt: now, ExpiresAt: now.Add(time.Minute)}
		directory, err := nativeCreateDirectory(s.root)
		if err != nil {
			writeJSON(w, 503, map[string]string{"error": "test state unavailable"})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		defer cancel()
		database, err := internalEngineDatabase(target.mountPath)
		if err == nil {
			state.Before, err = snapshotNativeCreate(ctx, database, filepath.Join(directory, id+".sqlite-backup"), identity.LibraryUUID)
		}
		if err != nil {
			writeJSON(w, 409, map[string]string{"error": "consistent snapshot could not be verified; no command queued"})
			return
		}
		if err = saveNativeCreate(s.root, state); err != nil {
			writeJSON(w, 503, map[string]string{"error": "command state could not be saved"})
			return
		}
		writeJSON(w, 202, state)
		return
	}
	if loadErr != nil {
		if suffix == "/pending" && os.IsNotExist(loadErr) {
			w.WriteHeader(204)
			return
		}
		writeJSON(w, 404, map[string]string{"error": "native test unavailable"})
		return
	}
	if suffix == "/pending" && r.Method == http.MethodGet {
		if state.Phase != "queued" || time.Now().After(state.ExpiresAt) {
			w.WriteHeader(204)
			return
		}
		writeJSON(w, 200, state)
		return
	}
	if suffix == "" && r.Method == http.MethodGet {
		if state.Phase == "claimed" || state.Phase == "submitted" {
			target, err := nativeCreateTarget(r.Context(), state)
			if err == nil {
				database, e := internalEngineDatabase(target.mountPath)
				if e != nil {
					err = e
				} else {
					if state.Operation == "all_tracks" {
						if state.Phase == "submitted" && s.nativeAllTracksConfirmed(state) {
							state.Phase = "verified"
						}
					} else if state.Operation == "select" {
						if state.Phase == "submitted" && s.nativeSelectionConfirmed(state) {
							state.Phase = "verified"
						}
					} else if state.Operation == "lookup" {
						// Read-only lookup is completed only by its checked URI response.
					} else if state.Operation == "register" {
						err = verifyNativeRegistration(r.Context(), &state, database)
					} else if state.Operation == "add" {
						err = verifyNativeAdd(r.Context(), &state, database)
					} else {
						err = verifyNativeCreate(r.Context(), &state, database)
					}
				}
			}
			if err != nil {
				writeJSON(w, 409, map[string]string{"error": "native result requires manual review"})
				return
			}
			if err = saveNativeCreate(s.root, state); err != nil {
				writeJSON(w, 503, map[string]string{"error": "result could not be saved"})
				return
			}
		}
		writeJSON(w, 200, state)
		return
	}
	if (suffix == "/claim" || suffix == "/ack") && r.Method == http.MethodPost {
		var input struct {
			ID            string `json:"id"`
			Stage         string `json:"stage"`
			NativeTrackID string `json:"nativeTrackId,omitempty"`
			NativeURI     string `json:"nativeUri,omitempty"`
		}
		data, err := io.ReadAll(io.LimitReader(r.Body, 8193))
		if err != nil || len(data) > 8192 || decodeStrictJSON(data, &input) != nil || input.ID != state.ID {
			writeJSON(w, 400, map[string]string{"error": "invalid command acknowledgement"})
			return
		}
		if suffix == "/claim" {
			release, leaseErr := nativeCreateLease(s.root)
			if leaseErr != nil {
				writeJSON(w, 409, map[string]string{"error": "another import is active"})
				return
			}
			defer release()
			if state.Phase != "queued" || time.Now().After(state.ExpiresAt) || !s.nativeCreateViewMatches(state) {
				writeJSON(w, 409, map[string]string{"error": "command already claimed or view changed"})
				return
			}
			if _, err = nativeCreateTarget(r.Context(), state); err != nil {
				writeJSON(w, 409, map[string]string{"error": "native target changed"})
				return
			}
			if state.Operation == "add" || state.Operation == "register" || state.Operation == "select" || state.Operation == "all_tracks" || state.Operation == "lookup" {
				identity, e := nativeEngineIdentity()
				if e != nil || identity != state.EngineIdentity {
					writeJSON(w, 409, map[string]string{"error": "engine instance changed"})
					return
				}
			}
			if state.Operation == "register" {
				if e := validateNativeCopied(r.Context(), state); e != nil {
					writeJSON(w, 409, map[string]string{"error": "copied file changed"})
					return
				}
			}
			state.Phase = "claimed"
		} else {
			if state.Phase != "claimed" && state.Phase != "submitted" {
				writeJSON(w, 409, map[string]string{"error": "command not awaiting acknowledgement"})
				return
			}
			switch input.Stage {
			case "confirm_registration_visible":
				if state.Operation != "register" {
					writeJSON(w, 400, map[string]string{"error": "registration command required"})
					return
				}
				target, e := nativeCreateTarget(r.Context(), state)
				if e == nil {
					var database string
					database, e = internalEngineDatabase(target.mountPath)
					if e == nil {
						e = confirmNativeRegistration(r.Context(), &state, database)
					}
				}
				if e != nil {
					writeJSON(w, 409, map[string]string{"error": "registration verification failed"})
					return
				}
			case "review_selection_noop":
				if state.Operation != "select" {
					writeJSON(w, 400, map[string]string{"error": "selection review required"})
					return
				}
				identity, e := nativeEngineIdentity()
				s.nativeProbeMu.Lock()
				otherCurrent := 0
				if s.nativeProbeObservation != nil {
					for _, d := range s.nativeProbeObservation.Devices {
						if d.Current && d.Path != state.SourceDevice {
							otherCurrent++
						}
					}
				}
				fresh := time.Now().Before(s.nativeProbeUntil) && time.Since(s.nativeProbeReceivedAt) < 15*time.Second && nativeSelectionMatches(s.nativeProbeObservation, state, false)
				s.nativeProbeMu.Unlock()
				if e != nil || identity != state.EngineIdentity || !fresh || otherCurrent != 1 {
					writeJSON(w, 409, map[string]string{"error": "selection outcome not established"})
					return
				}
				state.SelectionReviewed = true
				state.Phase = "selection_noop"
			case "lookup_failed":
				if state.Operation != "lookup" {
					writeJSON(w, 400, map[string]string{"error": "lookup command required"})
					return
				}
				state.Phase = "lookup_failed"
			case "lookup_result":
				if state.Operation != "lookup" || !s.nativeAllTracksConfirmed(state) {
					writeJSON(w, 409, map[string]string{"error": "native lookup context changed"})
					return
				}
				identity, e := nativeEngineIdentity()
				if e != nil || identity != state.EngineIdentity {
					writeJSON(w, 409, map[string]string{"error": "engine instance changed"})
					return
				}
				target, e := nativeCreateTarget(r.Context(), state)
				if e == nil {
					var database string
					database, e = internalEngineDatabase(target.mountPath)
					if e == nil {
						var expected string
						expected, e = nativeLookupFileURL(r.Context(), database, target.mountPath, state.LookupLocalID)
						if expected != state.LookupFileURL {
							e = errors.New("lookup track moved")
						}
						if e == nil {
							fresh := state
							e = populateNativeLookupOrigin(r.Context(), &fresh, database)
							if fresh.LookupOriginUUID != state.LookupOriginUUID || fresh.LookupOriginID != state.LookupOriginID {
								e = errors.New("lookup origin changed")
							}
						}
					}
				}
				if e == nil {
					e = verifyNativeLookupResult(&state, input.NativeTrackID, input.NativeURI)
				}
				if e != nil {
					writeJSON(w, 409, map[string]string{"error": "native lookup result did not verify"})
					return
				}
			case "submitted":
				state.Phase = "submitted"
			case "collection_complete":
				if state.Operation != "register" {
					writeJSON(w, 400, map[string]string{"error": "registration command required"})
					return
				}
				state.NativeCollectionComplete = true
			case "model_rows_inserted":
				state.RowsInserted = true
			case "not_invoked":
				state.Phase = "not_invoked"
			case "uncertain":
				state.Phase = "uncertain"
			default:
				writeJSON(w, 400, map[string]string{"error": "unsupported acknowledgement"})
				return
			}
		}
		if err = saveNativeCreate(s.root, state); err != nil {
			writeJSON(w, 503, map[string]string{"error": "command state could not be saved"})
			return
		}
		writeJSON(w, 200, state)
		return
	}
	writeJSON(w, 405, map[string]string{"error": "method not allowed"})
}
