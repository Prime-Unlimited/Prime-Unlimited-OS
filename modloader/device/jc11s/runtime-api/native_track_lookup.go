package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func nativeLookupURIMatches(raw, expected string) bool {
	var uris []string
	if json.Unmarshal([]byte(raw), &uris) != nil {
		var one string
		if json.Unmarshal([]byte(raw), &one) == nil {
			uris = []string{one}
		} else {
			uris = []string{raw}
		}
	}
	if len(uris) != 1 {
		return false
	}
	normalize := func(value string) (string, bool) {
		if filepath.IsAbs(value) {
			return filepath.Clean(value), filepath.Clean(value) == value
		}
		u, e := url.Parse(value)
		if e != nil || u.Scheme != "file" || (u.Host != "" && u.Host != "localhost") || u.RawQuery != "" || u.Fragment != "" || !filepath.IsAbs(u.Path) || filepath.Clean(u.Path) != u.Path {
			return "", false
		}
		return u.Path, true
	}
	a, ok := normalize(uris[0])
	b, other := normalize(expected)
	return ok && other && a == b
}
func verifyNativeLookupResult(state *nativeCreateState, id, uri string) error {
	native, e := strconv.ParseUint(id, 10, 64)
	if e != nil || native == 0 || native > (1<<53)-1 || native&((1<<48)-1) != uint64(state.LookupLocalID) {
		return errors.New("unsupported or different native track identity")
	}
	state.LookupURI = uri
	normalized, valid := checkedNativeLookupURI(*state, uri)
	if !valid || !nativeLookupURIMatches(normalized, state.LookupFileURL) {
		state.Phase = "lookup_failed"
		return nil
	}
	state.NativeTrackID = id
	state.Phase = "verified"
	return nil
}
func nativeLookupFileURL(ctx context.Context, database, root string, id int64) (string, error) {
	db, e := openNativeReadDB(database)
	if e != nil {
		return "", e
	}
	defer db.Close()
	var relative string
	if e = db.QueryRowContext(ctx, "SELECT path FROM Track WHERE id=?", id).Scan(&relative); e != nil {
		return "", e
	}
	clean := filepath.Clean(filepath.FromSlash(relative))
	if clean == "." || filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", errors.New("unsupported track location")
	}
	path := filepath.Join(root, "Engine Library", clean)
	return (&url.URL{Scheme: "file", Path: path}).String(), nil
}
func (s *server) queueNativeTrackLookup(w http.ResponseWriter, r *http.Request, previous nativeCreateState, loadErr error) {
	var input struct {
		Confirm bool  `json:"confirm"`
		TrackID int64 `json:"trackId"`
	}
	data, e := io.ReadAll(io.LimitReader(r.Body, 257))
	if e != nil || len(data) > 256 || decodeStrictJSON(data, &input) != nil || !input.Confirm || input.TrackID <= 0 || input.TrackID >= (1<<48) {
		writeJSON(w, 400, map[string]string{"error": "one local track ID required"})
		return
	}
	if loadErr != nil || nativeCreateActive(previous) {
		writeJSON(w, 409, map[string]string{"error": "previous command requires review"})
		return
	}
	release, e := nativeCreateLease(s.root)
	if e != nil {
		writeJSON(w, 409, map[string]string{"error": "another import is active"})
		return
	}
	defer release()
	target, e := nativeCreateTarget(r.Context(), previous)
	if e != nil || !s.nativeAllTracksConfirmed(previous) {
		writeJSON(w, 409, map[string]string{"error": "verified internal All Tracks collection required"})
		return
	}
	database, e := internalEngineDatabase(target.mountPath)
	fileURL := ""
	if e == nil {
		fileURL, e = nativeLookupFileURL(r.Context(), database, target.mountPath, input.TrackID)
	}
	if e != nil {
		writeJSON(w, 409, map[string]string{"error": "internal track location unavailable"})
		return
	}
	id, e := newServiceToken()
	if e != nil {
		writeJSON(w, 503, map[string]string{"error": "command identity unavailable"})
		return
	}
	state := nativeCreateState{ID: id, Operation: "lookup", Phase: "queued", SourceDevice: target.mountPath, Identity: previous.Identity, Mount: mediaIdentity(target), LookupLocalID: input.TrackID, LookupFileURL: fileURL, CreatedAt: time.Now().UTC(), RootRow: -2}
	state.ExpiresAt = state.CreatedAt.Add(time.Minute)
	if e = populateNativeLookupOrigin(r.Context(), &state, database); e != nil {
		writeJSON(w, 409, map[string]string{"error": "internal origin reference unavailable"})
		return
	}
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
		writeJSON(w, 503, map[string]string{"error": "lookup command could not be saved"})
		return
	}
	writeJSON(w, 202, state)
}

func populateNativeLookupOrigin(ctx context.Context, state *nativeCreateState, database string) error {
	db, e := openNativeReadDB(database)
	if e != nil {
		return e
	}
	defer db.Close()
	if e = db.QueryRowContext(ctx, "SELECT originDatabaseUuid,originTrackId FROM Track WHERE id=?", state.LookupLocalID).Scan(&state.LookupOriginUUID, &state.LookupOriginID); e != nil {
		return e
	}
	if state.LookupOriginUUID == "" || state.LookupOriginID <= 0 {
		return errors.New("origin reference unavailable")
	}
	state.LookupOriginUUID = strings.ToLower(state.LookupOriginUUID)
	return nil
}

// Engine's local URI embeds the origin reference in a query. Validate each
// known field before comparing its local path; never discard arbitrary queries.
func checkedNativeLookupURI(state nativeCreateState, raw string) (string, bool) {
	var uris []string
	if json.Unmarshal([]byte(raw), &uris) != nil {
		var one string
		if json.Unmarshal([]byte(raw), &one) == nil {
			uris = []string{one}
		} else {
			uris = []string{raw}
		}
	}
	if len(uris) != 1 {
		return "", false
	}
	u, e := url.Parse(uris[0])
	if e != nil {
		return "", false
	}
	if u.RawQuery == "" {
		return uris[0], true
	}
	if u.Scheme != "file" || (u.Host != "" && u.Host != "localhost") || u.Fragment != "" || state.LookupOriginUUID == "" || state.LookupOriginID <= 0 {
		return "", false
	}
	q, e := url.ParseQuery(u.RawQuery)
	if e != nil {
		return "", false
	}
	for key, values := range q {
		if (key != "databaseUuid" && key != "trackId" && key != "uri") || len(values) != 1 {
			return "", false
		}
	}
	if !strings.EqualFold(q.Get("databaseUuid"), state.LookupOriginUUID) || q.Get("uri") != "" {
		return "", false
	}
	fields := strings.Fields(q.Get("trackId"))
	if len(fields) != 2 || !strings.EqualFold(fields[1], state.LookupOriginUUID) {
		return "", false
	}
	id, e := strconv.ParseInt(fields[0], 10, 64)
	if e != nil || id != state.LookupOriginID {
		return "", false
	}
	u.RawQuery = ""
	u.ForceQuery = false
	u.Fragment = ""
	u.RawFragment = ""
	return u.String(), true
}
