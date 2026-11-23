package main

import (
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type nativePlaylistInstance struct {
	SelectedListID       string `json:"selectedListID"`
	SelectedTestPlaylist bool   `json:"selectedTestPlaylist"`
	SourceDevice         string `json:"sourceDevice"`
	RootRow              int    `json:"rootRow"`
	CurrentRow           int    `json:"currentRow"`
	Visible              bool   `json:"visible"`
	Enabled              bool   `json:"enabled"`
	ControllerPresent    bool   `json:"controllerPresent"`
	ModelPresent         bool   `json:"modelPresent"`
	CanCreate            bool   `json:"canCreate"`
	CanAdd               bool   `json:"canAdd"`
	CanReadStructure     bool   `json:"canReadStructure"`
	RootIndexType        string `json:"rootIndexType"`
	CurrentIndexType     string `json:"currentIndexType"`
}
type nativePlaylistID struct {
	Type   string `json:"type"`
	Value  string `json:"value"`
	Length int    `json:"length"`
}
type nativeDeviceObservation struct {
	Path      string `json:"path"`
	UUID      string `json:"uuid"`
	Row       int    `json:"row"`
	Current   bool   `json:"current"`
	CanSelect bool   `json:"canSelect"`
}
type nativePlaylistObservation struct {
	CollectionPath         string                    `json:"collectionPath,omitempty"`
	CollectionListID       string                    `json:"collectionListId,omitempty"`
	CollectionRows         int                       `json:"collectionRows"`
	Devices                []nativeDeviceObservation `json:"devices,omitempty"`
	CollectionIsLibrary    bool                      `json:"collectionIsLibrary"`
	CanRegisterFiles       bool                      `json:"canRegisterFiles"`
	Errors                 int                       `json:"errors"`
	VisibleVisited         int                       `json:"visibleVisited"`
	ClassHints             []string                  `json:"classHints"`
	MainScreen             bool                      `json:"mainScreen"`
	Visited                int                       `json:"visited"`
	Truncated              bool                      `json:"truncated"`
	DurationMS             int                       `json:"durationMs"`
	RootDepth              int                       `json:"rootDepth"`
	CanRefreshCollection   bool                      `json:"canRefreshCollection"`
	CanImportTracks        bool                      `json:"canImportTracks"`
	CanAddTracksToDatabase bool                      `json:"canAddTracksToDatabase"`
	Instances              []nativePlaylistInstance  `json:"instances"`
	IDs                    []nativePlaylistID        `json:"ids"`
}

var nativeProbeClassPattern = regexp.MustCompile(`^[A-Za-z0-9_]{1,128}$`)
var nativeProbeIDPattern = regexp.MustCompile(`^[0-9a-fA-F:{};-]{0,128}$`)

func validNativeObservation(o nativePlaylistObservation) bool {
	if o.Errors < 0 || o.Errors > 40000 || o.VisibleVisited < 0 || o.VisibleVisited > o.Visited || len(o.ClassHints) > 32 {
		return false
	}
	for _, hint := range o.ClassHints {
		if !nativeProbeClassPattern.MatchString(hint) {
			return false
		}
	}
	if o.Visited < 0 || o.Visited > 20000 || o.DurationMS < 0 || o.DurationMS > 60000 || o.RootDepth < 0 || o.RootDepth > 64 || len(o.Instances) > 16 || len(o.IDs) > 6 {
		return false
	}
	if o.CollectionRows < 0 || o.CollectionRows > 1000000 || len(o.CollectionPath) > 256 || (o.CollectionPath != "" && (!strings.HasPrefix(o.CollectionPath, "/media/") || strings.Contains(o.CollectionPath, "..") || strings.ContainsAny(o.CollectionPath, "\x00\r\n"))) {
		return false
	}
	if o.CollectionListID != "" {
		if _, e := strconv.ParseInt(o.CollectionListID, 10, 64); e != nil {
			return false
		}
	}
	if len(o.Devices) > 32 {
		return false
	}
	for _, d := range o.Devices {
		if !strings.HasPrefix(d.Path, "/media/") || len(d.Path) > 256 || strings.Contains(d.Path, "..") || strings.ContainsAny(d.Path, "\x00\r\n") || d.Row < 0 || d.Row > 31 || len(d.UUID) > 128 || !nativeProbeIDPattern.MatchString(d.UUID) {
			return false
		}
	}
	validType := func(t string) bool {
		return t == "undefined" || t == "object" || t == "number" || t == "string" || t == "boolean"
	}
	for _, i := range o.Instances {
		if i.SelectedListID != "" {
			id, err := strconv.ParseUint(i.SelectedListID, 10, 64)
			if err != nil || id > (1<<53)-1 {
				return false
			}
		}
		if len(i.SourceDevice) > 256 || (i.SourceDevice != "" && (!strings.HasPrefix(i.SourceDevice, "/media/") || strings.Contains(i.SourceDevice, "..") || strings.ContainsAny(i.SourceDevice, "\x00\r\n"))) || i.RootRow < -2 || i.CurrentRow < -2 || !validType(i.RootIndexType) || !validType(i.CurrentIndexType) {
			return false
		}
	}
	for _, i := range o.IDs {
		if !validType(i.Type) || i.Length < 0 || i.Length > 100000 || !nativeProbeIDPattern.MatchString(i.Value) {
			return false
		}
	}
	return true
}
func (s *server) handleNativePlaylistProbe(w http.ResponseWriter, r *http.Request) {
	if !s.authorized(r) {
		writeJSON(w, 401, map[string]string{"error": "trusted UI authorization required"})
		return
	}
	if r.URL.RawQuery != "" || r.URL.Fragment != "" {
		writeJSON(w, 400, map[string]string{"error": "query parameters are not supported"})
		return
	}
	if s.model != "JP24" || !s.mediaImportEnabled() {
		writeJSON(w, 403, map[string]string{"error": "trusted JP24 USB Copy required"})
		return
	}
	suffix := strings.TrimPrefix(r.URL.Path, "/api/v1/native-playlist/probe")
	if suffix == "" && r.Method == http.MethodGet {
		s.nativeProbeMu.Lock()
		defer s.nativeProbeMu.Unlock()
		writeJSON(w, 200, map[string]any{"enabled": time.Now().Before(s.nativeProbeUntil), "scriptRevision": s.nativeProbeRevision, "observation": s.nativeProbeObservation, "receivedAt": s.nativeProbeReceivedAt})
		return
	}
	if suffix == "" && r.Method == http.MethodDelete {
		s.nativeProbeMu.Lock()
		s.nativeProbeUntil = time.Time{}
		s.nativeProbeMu.Unlock()
		writeJSON(w, 200, map[string]bool{"enabled": false})
		return
	}
	if suffix == "/arm" && r.Method == http.MethodPost {
		data, err := io.ReadAll(io.LimitReader(r.Body, 17))
		if err != nil || strings.TrimSpace(string(data)) != "{}" {
			writeJSON(w, 400, map[string]string{"error": "empty JSON object required"})
			return
		}
		s.nativeProbeMu.Lock()
		s.nativeProbeUntil = time.Now().Add(5 * time.Minute)
		s.nativeProbeRevision = strconv.FormatInt(time.Now().UnixNano(), 10)
		s.nativeProbeObservation = nil
		s.nativeProbeReceivedAt = time.Time{}
		s.nativeProbeMu.Unlock()
		writeJSON(w, 200, map[string]bool{"enabled": true})
		return
	}
	if suffix == "/observation" && r.Method == http.MethodPost {
		data, err := io.ReadAll(io.LimitReader(r.Body, 8193))
		var observation nativePlaylistObservation
		if err != nil || len(data) > 8192 || decodeStrictJSON(data, &observation) != nil || !validNativeObservation(observation) {
			writeJSON(w, 400, map[string]string{"error": "invalid passive observation"})
			return
		}
		s.nativeProbeMu.Lock()
		defer s.nativeProbeMu.Unlock()
		if !time.Now().Before(s.nativeProbeUntil) {
			writeJSON(w, 409, map[string]string{"error": "probe is not armed"})
			return
		}
		s.nativeProbeObservation = &observation
		s.nativeProbeReceivedAt = time.Now().UTC()
		writeJSON(w, 200, map[string]bool{"accepted": true})
		return
	}
	writeJSON(w, 405, map[string]string{"error": "method not allowed"})
}
