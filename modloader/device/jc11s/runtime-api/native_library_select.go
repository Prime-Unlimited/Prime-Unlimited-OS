package main

import (
	"io"
	"net/http"
	"path/filepath"
	"time"
)

func nativeSelectionMatches(o *nativePlaylistObservation, state nativeCreateState, current bool) bool {
	if o == nil {
		return false
	}
	n := 0
	for _, d := range o.Devices {
		if d.Path == state.SourceDevice && d.UUID == state.Identity.LibraryUUID && d.CanSelect && (!current || d.Current) {
			n++
		}
	}
	return n == 1 && (!current || (o.CollectionIsLibrary && o.CanRegisterFiles))
}
func (s *server) nativeSelectionConfirmed(state nativeCreateState) bool {
	s.nativeProbeMu.Lock()
	defer s.nativeProbeMu.Unlock()
	return time.Now().Before(s.nativeProbeUntil) && time.Since(s.nativeProbeReceivedAt) < 15*time.Second && nativeSelectionMatches(s.nativeProbeObservation, state, true)
}
func (s *server) queueNativeLibrarySelection(w http.ResponseWriter, r *http.Request, previous nativeCreateState, loadErr error, operation string) {
	data, e := io.ReadAll(io.LimitReader(r.Body, 33))
	if e != nil || string(data) != "{\"confirm\":true}" {
		writeJSON(w, 400, map[string]string{"error": "explicit library selection required"})
		return
	}
	if loadErr != nil || nativeCreateActive(previous) {
		writeJSON(w, 409, map[string]string{"error": "previous native test requires review"})
		return
	}
	release, e := nativeCreateLease(s.root)
	if e != nil {
		writeJSON(w, 409, map[string]string{"error": "another import is active"})
		return
	}
	defer release()
	target, e := nativeCreateTarget(r.Context(), previous)
	if e != nil {
		var ok bool
		target, ok = resolveMediaReviewDestination(r.Context(), mediaJobRequest{DestinationIdentity: previous.Identity})
		if !ok {
			writeJSON(w, 409, map[string]string{"error": "internal library changed"})
			return
		}
	}
	binding := previous
	binding.SourceDevice = target.mountPath
	binding.Mount = mediaIdentity(target)
	s.nativeProbeMu.Lock()
	matches := time.Now().Before(s.nativeProbeUntil) && time.Since(s.nativeProbeReceivedAt) < 15*time.Second && nativeSelectionMatches(s.nativeProbeObservation, binding, operation == "all_tracks")
	s.nativeProbeMu.Unlock()
	if !matches {
		writeJSON(w, 409, map[string]string{"error": "exact native internal device model required"})
		return
	}
	id, e := newServiceToken()
	if e != nil {
		writeJSON(w, 503, map[string]string{"error": "command identity unavailable"})
		return
	}
	state := nativeCreateState{ID: id, Operation: operation, SourceDevice: binding.SourceDevice, Identity: binding.Identity, Mount: binding.Mount, Phase: "queued", CreatedAt: time.Now().UTC(), RootRow: -2}
	state.ExpiresAt = state.CreatedAt.Add(time.Minute)
	state.EngineIdentity, e = nativeEngineIdentity()
	if e != nil {
		writeJSON(w, 409, map[string]string{"error": "engine unavailable"})
		return
	}
	directory, e := nativeCreateDirectory(s.root)
	if e == nil {
		e = writeAtomicJSON(filepath.Join(directory, previous.ID+".completed.json"), previous)
	}
	if e == nil {
		e = saveNativeCreate(s.root, state)
	}
	if e != nil {
		writeJSON(w, 503, map[string]string{"error": "command could not be saved"})
		return
	}
	writeJSON(w, 202, state)
}

func (s *server) nativeAllTracksConfirmed(state nativeCreateState) bool {
	s.nativeProbeMu.Lock()
	defer s.nativeProbeMu.Unlock()
	o := s.nativeProbeObservation
	return o != nil && time.Now().Before(s.nativeProbeUntil) && time.Since(s.nativeProbeReceivedAt) < 15*time.Second && nativeSelectionMatches(o, state, true) && o.CollectionPath == state.SourceDevice && (o.CollectionListID == "0" || o.CollectionListID == "-1")
}
