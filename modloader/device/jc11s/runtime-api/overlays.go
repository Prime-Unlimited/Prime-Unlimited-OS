package main

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
)

type overlaySettings struct {
	X int `json:"x"`
	Y int `json:"y"`
}

type overlayRecord struct {
	ID      string `json:"id"`
	Version string `json:"version"`
	Source  string `json:"source"`
}

func (s *server) handleOverlaySettings(w http.ResponseWriter, r *http.Request, id string) {
	if !modIDPattern.MatchString(id) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid mod id"})
		return
	}
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	state, err := loadRegistry(filepath.Join(s.root, "registry", "state.json"))
	item, exists := state.Mods[id]
	if err != nil || !exists || item.Overlay == "" {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "overlay mod not installed"})
		return
	}
	path := filepath.Join(s.root, "settings", id+".json")
	if r.Method == http.MethodGet {
		data, readErr := readOwnedFile(path, 4096, "overlay settings")
		if os.IsNotExist(readErr) {
			writeJSON(w, http.StatusOK, overlaySettings{X: -1, Y: -1})
			return
		}
		var value overlaySettings
		if readErr != nil || decodeStrictJSON(data, &value) != nil || value.X < -1 || value.X > 4096 || value.Y < -1 || value.Y > 4096 {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "overlay settings unavailable"})
			return
		}
		writeJSON(w, http.StatusOK, value)
		return
	}
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	data, readErr := io.ReadAll(io.LimitReader(r.Body, 4097))
	var value overlaySettings
	if readErr != nil || len(data) > 4096 || json.Unmarshal(data, &value) != nil || decodeStrictJSON(data, &value) != nil || value.X < 0 || value.X > 4096 || value.Y < 0 || value.Y > 4096 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid overlay settings"})
		return
	}
	if err := writeAtomicJSON(path, value); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "overlay settings write failed"})
		return
	}
	writeJSON(w, http.StatusOK, value)
}

func enabledOverlays(root string) ([]overlayRecord, error) {
	safety, err := loadSafetyState(root)
	if err != nil {
		return nil, err
	}
	if safety.SafeMode {
		return []overlayRecord{}, nil
	}
	state, err := loadRegistry(filepath.Join(root, "registry", "state.json"))
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(state.Mods))
	for id, item := range state.Mods {
		if item.Enabled && item.Overlay != "" && !safety.Mods[id].Quarantined {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	result := make([]overlayRecord, 0, len(ids))
	for _, id := range ids {
		item := state.Mods[id]
		path := filepath.Join(root, filepath.FromSlash(item.Root), filepath.FromSlash(item.Overlay))
		info, statErr := os.Lstat(path)
		if statErr != nil || validateOwnedRegularFile(info, 0o600) != nil {
			return nil, errors.New("enabled overlay is missing or unsafe")
		}
		result = append(result, overlayRecord{ID: id, Version: item.Version, Source: "file://" + path})
	}
	return result, nil
}
