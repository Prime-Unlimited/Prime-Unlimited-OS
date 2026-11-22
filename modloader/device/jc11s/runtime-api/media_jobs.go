package main

import (
	"io"
	"net/http"
	"path/filepath"
	usbcopy "prime-unlimited.usb-copy/worker"
)

// This endpoint exposes persisted progress only. Starting a privileged import
// remains unavailable until the supervised commit/recovery path is integrated.
func (s *server) handleMediaJobs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if !s.mediaImportEnabled() {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "USB Copy is not enabled and trusted"})
		return
	}
	store, err := usbcopy.OpenJobStore(filepath.Join(s.root, "media-jobs"))
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "import job storage unavailable"})
		return
	}
	jobs, err := store.List()
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "import job status unavailable"})
		return
	}
	if len(jobs) > 20 {
		jobs = jobs[:20]
	}
	writeJSON(w, http.StatusOK, map[string]any{"jobs": jobs})
}

func (s *server) handleMediaJobAcknowledge(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, 405, map[string]string{"error": "method not allowed"})
		return
	}
	if !s.mediaImportEnabled() {
		writeJSON(w, 403, map[string]string{"error": "USB Copy is not enabled and trusted"})
		return
	}
	var request struct {
		JobID string `json:"jobId"`
	}
	data, e := io.ReadAll(io.LimitReader(r.Body, 4097))
	if e != nil || len(data) > 4096 || decodeStrictJSON(data, &request) != nil || !validMediaEventID(request.JobID) {
		writeJSON(w, 400, map[string]string{"error": "invalid job acknowledgement"})
		return
	}
	store, e := usbcopy.OpenJobStore(filepath.Join(s.root, "media-jobs"))
	if e != nil {
		writeJSON(w, 503, map[string]string{"error": "job storage unavailable"})
		return
	}
	if e = store.Acknowledge(request.JobID); e != nil {
		writeJSON(w, 409, map[string]string{"error": "job cannot be acknowledged"})
		return
	}
	writeJSON(w, 200, map[string]bool{"accepted": true})
}
