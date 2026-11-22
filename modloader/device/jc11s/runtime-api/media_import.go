package main

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode"

	usbcopy "prime-unlimited.usb-copy/worker"
)

type mediaInsertEvent struct {
	ID           string                   `json:"id"`
	DisplayName  string                   `json:"displayName"`
	Destinations []mediaImportDestination `json:"destinations"`
	Resolved     bool                     `json:"-"`
	mountKey     string
	mountPath    string
}

type mediaImportDestination struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	FreeBytes  uint64 `json:"freeBytes"`
	TotalBytes uint64 `json:"totalBytes"`
	mountKey   string
	mountPath  string
}

type mediaImportDecision struct {
	EventID       string `json:"eventId"`
	Decision      string `json:"decision"`
	DestinationID string `json:"destinationId,omitempty"`
}

type mediaImportPreview struct {
	CommitAvailable  bool     `json:"commitAvailable"`
	Accepted         bool     `json:"accepted"`
	Prepared         bool     `json:"prepared"`
	TrackCount       int      `json:"trackCount"`
	NewFiles         int      `json:"newFiles"`
	ExistingFiles    int      `json:"existingFiles"`
	SourceDuplicates int      `json:"sourceDuplicates"`
	PlaylistCount    int      `json:"playlistCount"`
	BytesToCopy      int64    `json:"bytesToCopy"`
	Warnings         []string `json:"warnings,omitempty"`
}

type mediaPreviewState struct {
	DestinationIdentity mediaDestinationIdentity
	Plan                usbcopy.ImportPlan
	DuplicateSummary    usbcopy.DuplicateSummary
	EventID             string
	SourceMountID       string
	SourceDevice        string
	SourceRoot          string
	DestinationID       string
	DestinationMountID  string
	DestinationDevice   string
	DestinationRoot     string
	CreatedAt           time.Time
}

var mediaPreviewFileMu sync.Mutex

const mediaPreviewLifetime = 30 * time.Minute

func (s *server) handleMediaImportEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if !s.mediaImportEnabled() {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "USB Copy is not enabled and trusted"})
		return
	}
	if r.Method == http.MethodPost {
		s.handleMediaImportDecision(w, r)
		return
	}
	volumes, err := readMountedBlockVolumes()
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "media state unavailable"})
		return
	}
	events, err := s.updateMediaInsertEvents(volumes)
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "media event unavailable"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": events})
}

func (s *server) handleMediaImportPreview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodDelete {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if !s.mediaImportEnabled() {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "USB Copy is not enabled and trusted"})
		return
	}
	if r.Method == http.MethodDelete {
		data, err := io.ReadAll(io.LimitReader(r.Body, 4097))
		var request struct {
			EventID string `json:"eventId"`
		}
		if err != nil || len(data) > 4096 || decodeStrictJSON(data, &request) != nil || !validMediaEventID(request.EventID) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid preview request"})
			return
		}
		s.mediaMu.Lock()
		defer s.mediaMu.Unlock()
		if !s.hasPendingMediaPreview(request.EventID) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "import preview not found"})
			return
		}
		s.clearMediaPreview(request.EventID)
		for key, event := range s.mediaEvents {
			if event.ID == request.EventID {
				event.Resolved = true
				s.mediaEvents[key] = event
				break
			}
		}
		removeMediaPreview(request.EventID)
		writeJSON(w, http.StatusOK, map[string]bool{"accepted": true})
		return
	}
	eventID := r.Header.Get("X-USB-Copy-Event")
	if !validMediaEventID(eventID) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "preview event id required"})
		return
	}
	s.mediaMu.Lock()
	defer s.mediaMu.Unlock()
	var state mediaPreviewState
	if err := readMediaPreview(eventID, &state); err != nil || subtle.ConstantTimeCompare([]byte(eventID), []byte(state.EventID)) != 1 || time.Since(state.CreatedAt) > mediaPreviewLifetime {
		s.clearMediaPreview(eventID)
		removeMediaPreview(eventID)
		writeJSON(w, http.StatusGone, map[string]string{"error": "import preview expired"})
		return
	}
	if !s.hasPendingMediaPreview(eventID) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "import preview not found"})
		return
	}
	if time.Since(state.CreatedAt) > mediaPreviewLifetime {
		s.clearMediaPreview(eventID)
		removeMediaPreview(eventID)
		writeJSON(w, http.StatusGone, map[string]string{"error": "import preview expired"})
		return
	}
	volumes, err := readMountedBlockVolumes()
	if err != nil || !matchingMediaMount(volumes, state.SourceMountID, state.SourceDevice, state.SourceRoot, true) || !matchingMediaMount(volumes, state.DestinationMountID, state.DestinationDevice, state.DestinationRoot, false) {
		s.clearMediaPreview(eventID)
		removeMediaPreview(eventID)
		writeJSON(w, http.StatusGone, map[string]string{"error": "source or destination drive was removed"})
		return
	}
	source, sourceMounted := findVolumeByKey(volumes, state.SourceMountID+":"+state.SourceDevice)
	destination, destinationMounted := findVolumeByKey(volumes, state.DestinationMountID+":"+state.DestinationDevice)
	if !sourceMounted || !destinationMounted || !isInternalMediaDestination(destination) || destination.mountPath != state.DestinationRoot || !isReadableMediaSource(source) {
		writeJSON(w, http.StatusGone, map[string]string{"error": "source or destination is no longer eligible"})
		return
	}
	currentIdentity, identityErr := readMediaDestinationIdentity(r.Context(), destination)
	if identityErr != nil || !state.DestinationIdentity.valid() || currentIdentity != state.DestinationIdentity {
		writeJSON(w, 409, map[string]string{"error": "internal library changed; prepare a new preview"})
		return
	}
	stateSnapshot, err := usbcopy.Scan(source.mountPath)
	if err != nil {
		writeJSON(w, http.StatusGone, map[string]string{"error": "source content can no longer be scanned"})
		return
	}
	engineDB, err := internalEngineDatabase(destination.mountPath)
	if err != nil {
		writeJSON(w, http.StatusGone, map[string]string{"error": "destination Engine database is unavailable"})
		return
	}
	playlistNames, err := usbcopy.ReadEnginePlaylistNames(engineDB)
	if err != nil {
		writeJSON(w, http.StatusGone, map[string]string{"error": "destination Engine database could not be inspected"})
		return
	}
	destinationMusicRoot, err := internalMusicRoot(destination.mountPath)
	if err != nil {
		writeJSON(w, http.StatusGone, map[string]string{"error": "destination Engine music directory is unavailable"})
		return
	}
	rebuiltPlan, err := usbcopy.BuildImportPlan(source.mountPath, destinationMusicRoot, mediaVolumeDisplayName(source.mountPath), state.CreatedAt, stateSnapshot, playlistNames)
	if err != nil || !sameImportPlan(state.Plan, rebuiltPlan) {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "USB content or destination changed after preview; prepare a new preview"})
		return
	}
	duplicates, err := usbcopy.SummarizeImportDuplicates(r.Context(), source.mountPath, destination.mountPath, engineDB, stateSnapshot.Tracks)
	if err != nil || duplicates != state.DuplicateSummary {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "duplicate state changed; prepare a new preview"})
		return
	}
	stats, err := destinationSpace(state.DestinationRoot)
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "destination space unavailable"})
		return
	}
	freeBytes := statfsBytes(stats.Bavail, int64(stats.Bsize))
	if duplicates.BytesToCopy < 0 || uint64(duplicates.BytesToCopy) > freeBytes || uint64(duplicates.BytesToCopy) > freeBytes-(freeBytes/20) {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "selected drive no longer has enough free space"})
		return
	}
	result := mediaPreviewFromPlan(state.Plan, duplicates)
	result.CommitAvailable = s.mediaCommitAvailable() && mediaMP3Plan(state.Plan)
	writeJSON(w, http.StatusOK, result)
}

func (s *server) hasPendingMediaPreview(eventID string) bool {
	for _, event := range s.mediaEvents {
		if event.ID == eventID && !event.Resolved {
			return s.mediaPreviews != nil && s.mediaPreviews[event.mountKey] != nil
		}
	}
	return false
}

func sameImportPlan(left, right usbcopy.ImportPlan) bool {
	leftJSON, leftErr := json.Marshal(left)
	rightJSON, rightErr := json.Marshal(right)
	return leftErr == nil && rightErr == nil && subtle.ConstantTimeCompare(leftJSON, rightJSON) == 1
}

func matchingMediaMount(volumes []mountedBlockVolume, mountID, deviceID, root string, usb bool) bool {
	for _, volume := range volumes {
		isRemovable := isRemovableMediaSource(volume)
		if volume.mountID == mountID && volume.deviceID == deviceID && filepath.Clean(volume.mountPath) == filepath.Clean(root) && isRemovable == usb {
			return true
		}
	}
	return false
}

func (s *server) handleMediaImportDecision(w http.ResponseWriter, r *http.Request) {
	data, err := io.ReadAll(io.LimitReader(r.Body, 4097))
	var request mediaImportDecision
	if err != nil || len(data) > 4096 || decodeStrictJSON(data, &request) != nil || !validMediaEventID(request.EventID) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid media decision"})
		return
	}
	s.mediaMu.Lock()
	defer s.mediaMu.Unlock()
	var eventKey string
	for key, event := range s.mediaEvents {
		if event.ID == request.EventID && !event.Resolved {
			eventKey = key
			break
		}
	}
	if eventKey == "" {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "media event expired"})
		return
	}
	if request.Decision == "copy" {
		event := s.mediaEvents[eventKey]
		if request.DestinationID == "" || !eventHasDestination(event, request.DestinationID) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "choose a listed internal destination"})
			return
		}
		if _, alreadyPrepared := s.mediaPreviews[eventKey]; alreadyPrepared {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "import preview already prepared for this USB insertion"})
			return
		}
		destination, ok := s.mediaDestinationsForEvent(event, request.DestinationID)
		if !ok {
			writeJSON(w, http.StatusGone, map[string]string{"error": "internal destination is no longer mounted"})
			return
		}
		destinationMountID, destinationDevice, ok := splitMediaMountKey(destination.mountKey)
		if !ok {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "internal destination identity is invalid"})
			return
		}
		volume, mounted := s.mediaEventVolume(event)
		if !mounted {
			writeJSON(w, http.StatusGone, map[string]string{"error": "USB volume is no longer mounted"})
			return
		}
		if destination.FreeBytes == 0 || volume.mountPath == destination.mountPath {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "invalid media destination"})
			return
		}
		if !isReadableMediaSource(volume) {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "source is not an eligible readable media volume"})
			return
		}
		s.setMediaProgress(event.ID, "scan", 0, 0)
		defer s.setMediaProgress("", "", 0, 0)
		snapshot, err := usbcopy.Scan(volume.mountPath)
		if err != nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "USB content could not be scanned"})
			return
		}
		if len(snapshot.Tracks) == 0 {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "no supported audio tracks found"})
			return
		}
		musicRoot, err := internalMusicRoot(destination.mountPath)
		if err != nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "internal Engine music folder was not found"})
			return
		}
		if _, err := privateMediaRoot(); err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "runtime preview storage is unavailable"})
			return
		}
		internalDB, err := internalEngineDatabase(destination.mountPath)
		if err != nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "destination Engine database was not found"})
			return
		}
		enginePlaylists, err := usbcopy.ReadEnginePlaylistNames(internalDB)
		if err != nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "destination Engine database could not be inspected safely"})
			return
		}
		s.setMediaProgress(event.ID, "plan", 0, 0)
		destinationIdentity, identityErr := readMediaIdentityForDestination(r.Context(), destination.mountKey)
		if identityErr != nil {
			writeJSON(w, 409, map[string]string{"error": "internal library identity unavailable"})
			return
		}
		createdAt := time.Now()
		plan, err := usbcopy.BuildImportPlan(volume.mountPath, musicRoot, event.DisplayName, createdAt, snapshot, enginePlaylists)
		if err != nil {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "USB import plan does not fit on the selected drive"})
			return
		}
		duplicates, err := usbcopy.SummarizeImportDuplicatesWithProgress(r.Context(), volume.mountPath, destination.mountPath, internalDB, snapshot.Tracks, func(stage string, done, total int) { s.setMediaProgress(event.ID, stage, done, total) })
		if err != nil {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "duplicate check could not be completed"})
			return
		}
		if uint64(duplicates.BytesToCopy) > destination.FreeBytes {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "USB import plan does not fit on the selected drive"})
			return
		}
		if s.mediaPreviews == nil {
			s.mediaPreviews = make(map[string]*usbcopy.ImportPlan)
		}
		previewState := mediaPreviewState{DestinationIdentity: destinationIdentity,
			Plan: plan, DuplicateSummary: duplicates, EventID: event.ID, SourceMountID: volume.mountID, SourceDevice: volume.deviceID, SourceRoot: volume.mountPath,
			DestinationID: destination.ID, DestinationMountID: destinationMountID,
			DestinationDevice: destinationDevice,
			DestinationRoot:   destination.mountPath, CreatedAt: createdAt,
		}
		if err := writeMediaPreview(event.ID, previewState); err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "import preview could not be saved safely"})
			return
		}
		s.mediaPreviews[eventKey] = &previewState.Plan
		result := mediaPreviewFromPlan(plan, duplicates)
		result.CommitAvailable = s.mediaCommitAvailable() && mediaMP3Plan(plan)
		writeJSON(w, http.StatusOK, result)
		return
	}
	if request.Decision != "decline" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported media decision"})
		return
	}
	event := s.mediaEvents[eventKey]
	event.Resolved = true
	s.mediaEvents[eventKey] = event
	delete(s.mediaPreviews, eventKey)
	removeMediaPreview(event.ID)
	writeJSON(w, http.StatusOK, map[string]bool{"accepted": true})
}

func mediaPreviewFromPlan(plan usbcopy.ImportPlan, duplicates usbcopy.DuplicateSummary) mediaImportPreview {
	warnings := append([]string(nil), plan.Warnings...)
	if len(warnings) > 10 {
		warnings = warnings[:10]
	}
	return mediaImportPreview{
		Accepted: true, Prepared: true, TrackCount: len(plan.Tracks),
		PlaylistCount: len(plan.SourcePlaylists) + 1, BytesToCopy: duplicates.BytesToCopy,
		NewFiles: duplicates.NewFiles, ExistingFiles: duplicates.ExistingFiles, SourceDuplicates: duplicates.SourceDuplicates,
		Warnings: warnings,
	}
}

func splitMediaMountKey(key string) (string, string, bool) {
	mountID, deviceID, ok := strings.Cut(key, ":")
	return mountID, deviceID, ok && mountID != "" && validDeviceNumber(deviceID)
}

func (s *server) mediaEventVolume(event mediaInsertEvent) (mountedBlockVolume, bool) {
	volumes, err := readMountedBlockVolumes()
	if err != nil {
		return mountedBlockVolume{}, false
	}
	return findVolumeByKey(volumes, event.mountKey)
}

func (s *server) mediaDestinationsForEvent(event mediaInsertEvent, id string) (mediaImportDestination, bool) {
	for _, destination := range event.Destinations {
		if destination.ID == id {
			return destination, true
		}
	}
	return mediaImportDestination{}, false
}

func internalMusicRoot(volumeRoot string) (string, error) {
	entries, err := os.ReadDir(volumeRoot)
	if err != nil {
		return "", err
	}
	for _, entry := range entries {
		if !entry.IsDir() || !strings.EqualFold(entry.Name(), "Engine Library") {
			continue
		}
		candidate := filepath.Join(volumeRoot, entry.Name(), "Music")
		info, err := os.Lstat(candidate)
		if err == nil && info.IsDir() && info.Mode()&os.ModeSymlink == 0 {
			return candidate, nil
		}
	}
	return "", errors.New("Engine music directory unavailable")
}

func internalEngineDatabase(volumeRoot string) (string, error) {
	candidates := []string{
		filepath.Join(volumeRoot, "Engine Library", "Database2", "m.db"),
		filepath.Join(volumeRoot, "Engine Library", "Database", "m.db"),
	}
	for _, candidate := range candidates {
		info, err := os.Lstat(candidate)
		if err == nil && info.Mode().IsRegular() && info.Mode()&os.ModeSymlink == 0 {
			return candidate, nil
		}
	}
	return "", errors.New("Engine database unavailable")
}

func (s *server) mediaImportEnabled() bool {
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	state, err := loadRegistry(filepath.Join(s.root, "registry", "state.json"))
	if err != nil {
		return false
	}
	safety, err := loadSafetyState(s.root)
	if err != nil || safety.SafeMode {
		return false
	}
	for id, item := range state.Mods {
		if !item.Enabled || item.Overlay == "" || item.Service == nil || item.Service.Capability != "media-import" || safety.Mods[id].Quarantined {
			continue
		}
		if s.serviceApproved(item) {
			return true
		}
	}
	return false
}

func writeMediaPreview(eventID string, value mediaPreviewState) error {
	if !validMediaEventID(eventID) {
		return errors.New("invalid media event id")
	}
	mediaPreviewFileMu.Lock()
	defer mediaPreviewFileMu.Unlock()
	root, err := privateMediaRoot()
	if err != nil {
		return err
	}
	data, err := json.Marshal(value)
	if err != nil || len(data) > 64*1024*1024 {
		return errors.New("preview data exceeds limit")
	}
	path := filepath.Join(root, eventID+".json")
	temporary, err := os.CreateTemp(root, ".preview-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0600); err != nil {
		temporary.Close()
		return err
	}
	if _, err = temporary.Write(data); err == nil {
		err = temporary.Sync()
	}
	if closeErr := temporary.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return err
	}
	return syncDirectory(root)
}

func privateMediaRoot() (string, error) {
	const base = "/run/prime-unlimited"
	if err := os.MkdirAll(base, 0700); err != nil {
		return "", err
	}
	baseInfo, err := os.Lstat(base)
	if err != nil || !baseInfo.IsDir() || baseInfo.Mode()&os.ModeSymlink != 0 || baseInfo.Mode().Perm()&0002 != 0 {
		return "", errors.New("runtime state directory is unsafe")
	}
	root := filepath.Join(base, "media-import")
	if err := os.MkdirAll(root, 0700); err != nil {
		return "", err
	}
	info, err := os.Lstat(root)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0700 {
		return "", errors.New("preview directory is unsafe")
	}
	return root, nil
}

func validMediaEventID(eventID string) bool {
	if len(eventID) != 64 {
		return false
	}
	for _, char := range eventID {
		if !strings.ContainsRune("0123456789abcdef", char) {
			return false
		}
	}
	return true
}

func readMediaPreview(eventID string, value *mediaPreviewState) error {
	if !validMediaEventID(eventID) {
		return errors.New("invalid media event id")
	}
	mediaPreviewFileMu.Lock()
	defer mediaPreviewFileMu.Unlock()
	root, err := privateMediaRoot()
	if err != nil {
		return err
	}
	path := filepath.Join(root, eventID+".json")
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > 64*1024*1024 || info.Mode().Perm() != 0600 {
		return errors.New("preview data is unsafe")
	}
	data, err := io.ReadAll(io.LimitReader(file, 64*1024*1024+1))
	if err != nil || len(data) > 64*1024*1024 || decodeStrictJSON(data, value) != nil {
		return errors.New("preview data is invalid")
	}
	return nil
}

func removeMediaPreview(eventID string) {
	if !validMediaEventID(eventID) {
		return
	}
	mediaPreviewFileMu.Lock()
	defer mediaPreviewFileMu.Unlock()
	root, err := privateMediaRoot()
	if err == nil {
		_ = os.Remove(filepath.Join(root, eventID+".json"))
	}
}

func (s *server) updateMediaInsertEvents(volumes []mountedBlockVolume) ([]mediaInsertEvent, error) {
	s.mediaMu.Lock()
	defer s.mediaMu.Unlock()
	if s.mediaEvents == nil {
		s.mediaEvents = make(map[string]mediaInsertEvent)
	}
	if s.mediaDestinations == nil {
		s.mediaDestinations = make(map[string]mediaImportDestination)
	}
	destinations := make([]mediaImportDestination, 0)
	activeDestinations := make(map[string]bool)
	for _, volume := range volumes {
		if !isInternalMediaDestination(volume) {
			continue
		}
		key := volume.mountID + ":" + volume.deviceID
		activeDestinations[key] = true
		stats, err := destinationSpace(volume.mountPath)
		if err != nil {
			continue
		}
		destination, exists := s.mediaDestinations[key]
		if !exists {
			id, err := newServiceToken()
			if err != nil {
				return nil, errors.New("destination id unavailable")
			}
			destination = mediaImportDestination{ID: id, mountKey: key, mountPath: volume.mountPath}
		}
		destination.Name = mediaVolumeDisplayName(volume.mountPath)
		destination.FreeBytes = statfsBytes(stats.Bavail, int64(stats.Bsize))
		destination.TotalBytes = statfsBytes(stats.Blocks, int64(stats.Bsize))
		s.mediaDestinations[key] = destination
		destinations = append(destinations, destination)
	}
	for key := range s.mediaDestinations {
		if !activeDestinations[key] {
			delete(s.mediaDestinations, key)
		}
	}
	sort.Slice(destinations, func(i, j int) bool {
		return strings.ToLower(destinations[i].Name) < strings.ToLower(destinations[j].Name)
	})
	for key, event := range s.mediaEvents {
		if _, mounted := findVolumeByKey(volumes, key); !mounted {
			removeMediaPreview(event.ID)
			delete(s.mediaEvents, key)
			delete(s.mediaPreviews, key)
		}
	}
	active := make(map[string]bool)
	for _, volume := range volumes {
		if !isReadableMediaSource(volume) {
			continue
		}
		key := volume.mountID + ":" + volume.deviceID
		active[key] = true
		if _, exists := s.mediaEvents[key]; exists {
			continue
		}
		id, err := newServiceToken()
		if err != nil {
			return nil, errors.New("media event id unavailable")
		}
		s.mediaEvents[key] = mediaInsertEvent{
			ID: id, DisplayName: mediaVolumeDisplayName(volume.mountPath), Destinations: append([]mediaImportDestination(nil), destinations...),
			mountKey: key, mountPath: volume.mountPath,
		}
	}
	for key, event := range s.mediaEvents {
		if !active[key] {
			removeMediaPreview(event.ID)
			delete(s.mediaEvents, key)
			delete(s.mediaPreviews, key)
			continue
		}
		event.Destinations = append([]mediaImportDestination(nil), destinations...)
		s.mediaEvents[key] = event
	}
	result := make([]mediaInsertEvent, 0, len(s.mediaEvents))
	for _, event := range s.mediaEvents {
		if !event.Resolved {
			result = append(result, mediaInsertEvent{ID: event.ID, DisplayName: event.DisplayName, Destinations: publicDestinations(event.Destinations)})
		}
	}
	sort.Slice(result, func(i, j int) bool {
		left, right := strings.ToLower(result[i].DisplayName), strings.ToLower(result[j].DisplayName)
		if left == right {
			return result[i].ID < result[j].ID
		}
		return left < right
	})
	return result, nil
}

func findVolumeByKey(volumes []mountedBlockVolume, key string) (mountedBlockVolume, bool) {
	for _, volume := range volumes {
		if volume.mountID+":"+volume.deviceID == key {
			return volume, true
		}
	}
	return mountedBlockVolume{}, false
}

func eventHasDestination(event mediaInsertEvent, id string) bool {
	for _, destination := range event.Destinations {
		if destination.ID == id {
			return true
		}
	}
	return false
}

func publicDestinations(destinations []mediaImportDestination) []mediaImportDestination {
	result := make([]mediaImportDestination, 0, len(destinations))
	for _, destination := range destinations {
		result = append(result, mediaImportDestination{ID: destination.ID, Name: destination.Name, FreeBytes: destination.FreeBytes, TotalBytes: destination.TotalBytes})
	}
	return result
}

func isInternalMediaDestination(volume mountedBlockVolume) bool {
	if isRemovableMediaSource(volume) || !isInternalDrivePath(volume.sysfsPath) || volume.options["ro"] || !volume.options["rw"] {
		return false
	}
	if volume.fsType != "exfat" && volume.fsType != "vfat" && volume.fsType != "fat" {
		return false
	}
	if filepath.Clean(volume.mountPath) == string(filepath.Separator) || !withinMediaDirectory(volume.mountPath) {
		return false
	}
	if !safeRealDirectory(volume.mountPath) {
		return false
	}
	if _, err := internalMusicRoot(volume.mountPath); err != nil {
		return false
	}
	if _, err := internalEngineDatabase(volume.mountPath); err != nil {
		return false
	}
	return true
}

func isRemovableMediaSource(volume mountedBlockVolume) bool {
	return volume.usb || volume.sdCard
}

func withinMediaDirectory(path string) bool {
	clean := filepath.Clean(path)
	relative, err := filepath.Rel("/media", clean)
	return err == nil && relative != "." && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func safeRealDirectory(path string) bool {
	clean := filepath.Clean(path)
	relative, err := filepath.Rel(string(filepath.Separator), clean)
	if err != nil {
		return false
	}
	current := string(filepath.Separator)
	for _, component := range strings.Split(relative, string(filepath.Separator)) {
		if component == "" || component == "." {
			continue
		}
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return false
		}
	}
	return true
}

func destinationSpace(path string) (syscall.Statfs_t, error) {
	var stats syscall.Statfs_t
	if err := syscall.Statfs(path, &stats); err != nil {
		return stats, err
	}
	return stats, nil
}

func statfsBytes(blocks uint64, blockSize int64) uint64 {
	if blockSize <= 0 {
		return 0
	}
	size := uint64(blockSize)
	if blocks > ^uint64(0)/size {
		return ^uint64(0)
	}
	return blocks * size
}

func mediaVolumeDisplayName(mountPath string) string {
	label := filepath.Base(mountPath)
	if label == "." || label == string(filepath.Separator) || label == "" {
		return "USB drive"
	}
	var result strings.Builder
	for _, char := range label {
		if unicode.IsControl(char) || char == '/' || char == '\\' {
			continue
		}
		result.WriteRune(char)
		if result.Len() >= 48 {
			break
		}
	}
	if value := strings.TrimSpace(result.String()); value != "" {
		return value
	}
	return "USB drive"
}

// Caller holds mediaMu. Plans are indexed by mount key, not event ID.
func (s *server) clearMediaPreview(eventID string) {
	for key, event := range s.mediaEvents {
		if event.ID == eventID {
			delete(s.mediaPreviews, key)
		}
	}
}

// Source media need read access only; a locked SD card remains importable.
func isReadableMediaSource(volume mountedBlockVolume) bool {
	return isRemovableMediaSource(volume) && (volume.options["ro"] || volume.options["rw"]) && withinMediaDirectory(volume.mountPath) && safeRealDirectory(volume.mountPath)
}
