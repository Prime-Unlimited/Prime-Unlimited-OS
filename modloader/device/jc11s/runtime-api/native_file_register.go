package main

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"golang.org/x/sys/unix"
	"image"
	_ "image/jpeg"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	usbcopy "prime-unlimited.usb-copy/worker"
	"time"
)

const nativeSampleFile = "Al Park, AKIN K - Andromeda (Original Mix).mp3"
const nativeSampleSHA = "23e09621c32de9a3c85296a672e37d31e2af859ffcb4e7d1acc3b944d2263abd"
const nativeSampleArt = "ae8a0e958265bd1671ebaa058729719262015dc4"

func copyNativeSample(ctx context.Context, sourceRoot, targetRoot, id string) (string, error) {
	// Existing worker validation checks every parent, regular-file status and source stability.
	fp, err := usbcopy.FingerprintAudioFile(ctx, sourceRoot, nativeSampleFile)
	if err != nil || fp.SHA256 != nativeSampleSHA || fp.Bytes != 23817189 {
		return "", errors.New("source sample changed")
	}
	parent := filepath.Join(targetRoot, "Engine Library", "Music")
	for _, path := range []string{targetRoot, filepath.Join(targetRoot, "Engine Library"), parent} {
		st, e := os.Lstat(path)
		if e != nil || !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
			return "", errors.New("unsafe target directory")
		}
	}
	directory := filepath.Join(parent, "USB Copy Native Test - "+id[:16])
	if err = os.Mkdir(directory, 0700); err != nil {
		return "", err
	}
	fd, err := unix.Open(filepath.Join(sourceRoot, nativeSampleFile), unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return "", err
	}
	input := os.NewFile(uintptr(fd), "native source")
	defer input.Close()
	output, err := os.OpenFile(filepath.Join(directory, "audio.partial"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(output, h), io.LimitReader(input, 23817190))
	if err == nil {
		err = output.Sync()
	}
	ce := output.Close()
	if err == nil {
		err = ce
	}
	if err != nil {
		return "", err
	}
	if n != 23817189 || hex.EncodeToString(h.Sum(nil)) != nativeSampleSHA {
		return "", errors.New("copied sample differs")
	}
	final := filepath.Join(directory, nativeSampleFile)
	if err = os.Rename(filepath.Join(directory, "audio.partial"), final); err != nil {
		return "", err
	}
	d, err := os.Open(directory)
	if err != nil {
		return "", err
	}
	err = d.Sync()
	d.Close()
	if err != nil {
		return "", err
	}
	if _, err = usbcopy.FingerprintAudioFile(ctx, sourceRoot, nativeSampleFile); err != nil {
		return "", err
	}
	return filepath.ToSlash(filepath.Join("Music", filepath.Base(directory), nativeSampleFile)), nil
}
func (s *server) queueNativeRegistration(w http.ResponseWriter, r *http.Request, previous nativeCreateState, loadErr error) {
	if loadErr != nil || previous.Operation != "add" || previous.Phase != "verified" || previous.PlaylistID != 2396 || previous.Title != nativeTestPlaylistTitle {
		writeJSON(w, 409, map[string]string{"error": "verified preceding playlist test required"})
		return
	}
	data, err := io.ReadAll(io.LimitReader(r.Body, 33))
	if err != nil || string(data) != "{\"confirm\":true}" {
		writeJSON(w, 400, map[string]string{"error": "single-file confirmation required"})
		return
	}
	release, err := nativeCreateLease(s.root)
	if err != nil {
		writeJSON(w, 409, map[string]string{"error": "import requires review"})
		return
	}
	defer release()
	s.nativeProbeMu.Lock()
	var view nativePlaylistInstance
	count := 0
	if s.nativeProbeObservation != nil && s.nativeProbeObservation.CollectionIsLibrary && s.nativeProbeObservation.CanRegisterFiles && time.Now().Before(s.nativeProbeUntil) && time.Since(s.nativeProbeReceivedAt) < 15*time.Second {
		for _, v := range s.nativeProbeObservation.Instances {
			if nativeAddMatches(v, previous) {
				view = v
				count++
			}
		}
	}
	s.nativeProbeMu.Unlock()
	if count != 1 {
		writeJSON(w, 409, map[string]string{"error": "current internal test collection required"})
		return
	}
	if _, err = nativeCreateTarget(r.Context(), previous); err != nil {
		writeJSON(w, 409, map[string]string{"error": "target changed"})
		return
	}
	volumes, err := readMountedBlockVolumes()
	if err != nil {
		writeJSON(w, 503, map[string]string{"error": "mounts unavailable"})
		return
	}
	sourceFound := false
	for _, v := range volumes {
		if v.mountPath == "/media/MP3 2" && v.sysfsPath != "" && !isInternalMediaDestination(v) {
			sourceFound = true
		}
	}
	if !sourceFound {
		writeJSON(w, 409, map[string]string{"error": "source USB unavailable"})
		return
	}
	id, err := newServiceToken()
	if err != nil {
		writeJSON(w, 503, map[string]string{"error": "identity unavailable"})
		return
	}
	state := previous
	state.ID = id
	state.Operation = "register"
	state.Phase = "preparing"
	state.NativeTrackID = ""
	state.TrackOriginUUID = ""
	state.TrackOriginID = 0
	state.RowsInserted = false
	state.After = nativeLibraryCounts{}
	state.CurrentRow = view.CurrentRow
	state.CreatedAt = time.Now().UTC()
	state.ExpiresAt = state.CreatedAt.Add(3 * time.Minute)
	state.EngineIdentity, err = nativeEngineIdentity()
	if err != nil {
		writeJSON(w, 409, map[string]string{"error": "engine unavailable"})
		return
	}
	directory, err := nativeCreateDirectory(s.root)
	if err != nil {
		writeJSON(w, 503, map[string]string{"error": "state unavailable"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	database, err := internalEngineDatabase(state.SourceDevice)
	if err == nil {
		state.Before, err = snapshotNativeCreate(ctx, database, filepath.Join(directory, id+".sqlite-backup"), state.Identity.LibraryUUID)
	}
	if err != nil {
		writeJSON(w, 409, map[string]string{"error": "snapshot failed; nothing copied"})
		return
	}
	db, err := openNativeReadDB(database)
	if err != nil {
		writeJSON(w, 409, map[string]string{"error": "database unavailable"})
		return
	}
	rows, err := db.QueryContext(ctx, "SELECT id,path FROM Track WHERE path IS NOT NULL AND path<>''")
	existing := []usbcopy.ExistingTrackFile{}
	if err == nil {
		for rows.Next() {
			var f usbcopy.ExistingTrackFile
			err = rows.Scan(&f.TrackID, &f.RelativePath)
			if err != nil {
				break
			}
			existing = append(existing, f)
		}
		if err == nil {
			err = rows.Err()
		}
		rows.Close()
	}
	db.Close()
	var decisions []usbcopy.DuplicateDecision
	if err == nil {
		decisions, err = usbcopy.FindDuplicateAudio(ctx, "/media/MP3 2", filepath.Join(state.SourceDevice, "Engine Library"), []string{nativeSampleFile}, existing)
	}
	if err != nil || len(decisions) != 1 || decisions[0].Fingerprint.SHA256 != nativeSampleSHA || decisions[0].ExistingTrackID != 0 {
		writeJSON(w, 409, map[string]string{"error": "sample changed or already imported; nothing copied"})
		return
	}
	if err = writeAtomicJSON(filepath.Join(directory, previous.ID+".completed.json"), previous); err == nil {
		err = saveNativeCreate(s.root, state)
	}
	if err != nil {
		writeJSON(w, 503, map[string]string{"error": "state unavailable; nothing copied"})
		return
	}
	if _, err = nativeCreateTarget(ctx, state); err != nil {
		state.Phase = "not_invoked"
		saveNativeCreate(s.root, state)
		writeJSON(w, 409, map[string]string{"error": "target changed; nothing copied"})
		return
	}
	state.CopiedRelative, err = copyNativeSample(ctx, "/media/MP3 2", state.SourceDevice, id)
	if err != nil {
		state.Phase = "uncertain"
		saveNativeCreate(s.root, state)
		writeJSON(w, 409, map[string]string{"error": "copy requires review; no native command queued"})
		return
	}
	state.FileURL = (&url.URL{Scheme: "file", Path: filepath.Join(state.SourceDevice, "Engine Library", state.CopiedRelative)}).String()
	state.Phase = "queued"
	if err = saveNativeCreate(s.root, state); err != nil {
		writeJSON(w, 503, map[string]string{"error": "copied file requires review; command state unavailable"})
		return
	}
	writeJSON(w, 202, state)
}
func validateNativeCopied(ctx context.Context, state nativeCreateState) error {
	expected := filepath.ToSlash(filepath.Join("Music", "USB Copy Native Test - "+state.ID[:16], nativeSampleFile))
	expectedURL := (&url.URL{Scheme: "file", Path: filepath.Join(state.SourceDevice, "Engine Library", expected)}).String()
	if state.CopiedRelative != expected || state.FileURL != expectedURL {
		return errors.New("unexpected copied location")
	}
	fp, err := usbcopy.FingerprintAudioFile(ctx, filepath.Join(state.SourceDevice, "Engine Library"), expected)
	if err != nil || fp.SHA256 != nativeSampleSHA || fp.Bytes != 23817189 {
		return errors.New("copied file changed")
	}
	return nil
}
func verifyNativeRegistration(ctx context.Context, state *nativeCreateState, database string) error {
	db, err := openNativeReadDB(database)
	if err != nil {
		return err
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx, `SELECT t.id,coalesce(t.title,''),coalesce(t.artist,''),coalesce(a.hash,x''),t.originDatabaseUuid,t.originTrackId FROM Track t LEFT JOIN AlbumArt a ON a.id=t.albumArtId WHERE t.path=?`, state.CopiedRelative)
	if err != nil {
		return err
	}
	n := 0
	var art []byte
	for rows.Next() {
		n++
		err = rows.Scan(&state.RegisteredTrackID, &state.RegisteredTitle, &state.RegisteredArtist, &art, &state.TrackOriginUUID, &state.TrackOriginID)
		if err != nil {
			rows.Close()
			return err
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if n > 1 {
		return errors.New("ambiguous registered track")
	}
	state.After, err = nativeCounts(ctx, db)
	if err != nil {
		return err
	}
	if n == 0 {
		return nil
	}
	state.RegisteredArtworkHash = hex.EncodeToString(art)
	path := filepath.Join(state.SourceDevice, "Engine Library", "Artwork", base64.RawURLEncoding.EncodeToString(art)+".jpg")
	if st, e := os.Lstat(path); e == nil && st.Mode().IsRegular() && st.Size() > 0 {
		f, e := os.Open(path)
		if e == nil {
			cfg, format, e := image.DecodeConfig(f)
			f.Close()
			state.RegisteredArtworkFile = e == nil && format == "jpeg" && cfg.Width > 0 && cfg.Height > 0 && cfg.Width <= 4096 && cfg.Height <= 4096
		}
	}
	if err = db.QueryRowContext(ctx, `SELECT count(*) FROM PlaylistEntity WHERE listId=? AND databaseUuid=? AND trackId=?`, state.PlaylistID, state.TrackOriginUUID, state.TrackOriginID).Scan(&state.RegisteredMembership); err != nil {
		return err
	}
	if state.NativeCollectionComplete && state.RowsInserted && state.After.Tracks == state.Before.Tracks+1 && state.After.Playlists == state.Before.Playlists && state.After.Entities == state.Before.Entities+1 && state.RegisteredMembership == 1 && state.RegisteredTitle == "Andromeda (Original Mix)" && state.RegisteredArtist == "Al Park, AKIN K" && state.RegisteredArtworkHash == nativeSampleArt && state.RegisteredArtworkFile {
		state.Phase = "verified"
	}
	return nil
}

// This explicit review records user-confirmed visibility/deck load without claiming playlist success.
func confirmNativeRegistration(ctx context.Context, state *nativeCreateState, database string) error {
	if err := verifyNativeRegistration(ctx, state, database); err != nil {
		return err
	}
	if state.RegisteredTrackID <= 0 || state.RegisteredTitle != "Andromeda (Original Mix)" || state.RegisteredArtist != "Al Park, AKIN K" || state.RegisteredArtworkHash != nativeSampleArt || !state.RegisteredArtworkFile || state.RegisteredMembership != 0 || state.After.Tracks != state.Before.Tracks+1 || state.After.Playlists != state.Before.Playlists || state.After.Entities != state.Before.Entities {
		return errors.New("registration-only result changed")
	}
	if err := validateNativeCopied(ctx, *state); err != nil {
		return err
	}
	state.UserConfirmedRegistration = true
	state.Phase = "registration_verified"
	return nil
}
