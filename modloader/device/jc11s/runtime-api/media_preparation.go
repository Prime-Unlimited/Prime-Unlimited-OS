package main

import "net/http"

type mediaPreparationProgress struct {
	EventID string `json:"eventId"`
	Stage   string `json:"stage"`
	Done    int    `json:"done"`
	Total   int    `json:"total"`
}

func (s *server) setMediaProgress(eventID, stage string, done, total int) {
	s.mediaProgressMu.Lock()
	defer s.mediaProgressMu.Unlock()
	s.mediaProgress = mediaPreparationProgress{eventID, stage, done, total}
}

// Independent of mediaMu so status polling never waits behind a media scan.
func (s *server) handleMediaPreparation(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, 405, map[string]string{"error": "method not allowed"})
		return
	}
	if !s.mediaImportEnabled() {
		writeJSON(w, 403, map[string]string{"error": "USB Copy is not enabled and trusted"})
		return
	}
	s.mediaProgressMu.Lock()
	progress := s.mediaProgress
	s.mediaProgressMu.Unlock()
	writeJSON(w, 200, progress)
}
