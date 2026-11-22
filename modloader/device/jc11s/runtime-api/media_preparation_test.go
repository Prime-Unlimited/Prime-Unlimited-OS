package main

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMediaPreparationAuthenticationAndPolicy(t *testing.T) {
	token := strings.Repeat("a", 64)
	s := &server{root: t.TempDir(), authToken: token}
	for _, tc := range []struct {
		method, path string
		auth         bool
		want         int
	}{{"GET", "/api/v1/media-import/preparation", false, 401}, {"POST", "/api/v1/media-import/preparation", true, 405}, {"GET", "/api/v1/media-import/preparation?path=/media/Denon", true, 400}, {"GET", "/api/v1/media-import/preparation", true, 403}} {
		r := httptest.NewRequest(tc.method, tc.path, nil)
		if tc.auth {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if w.Code != tc.want {
			t.Fatalf("%s %s: %d", tc.method, tc.path, w.Code)
		}
	}
}
func TestMediaPreparationCanBeUpdatedWhileMediaLockIsHeld(t *testing.T) {
	s := &server{}
	s.mediaMu.Lock()
	defer s.mediaMu.Unlock()
	s.setMediaProgress("opaque-event", "index", 128, 16000)
	s.mediaProgressMu.Lock()
	p := s.mediaProgress
	s.mediaProgressMu.Unlock()
	if p.Done != 128 || p.Total != 16000 || p.Stage != "index" {
		t.Fatalf("progress: %#v", p)
	}
	s.setMediaProgress("", "", 0, 0)
	if s.mediaProgress.EventID != "" {
		t.Fatal("finished progress retained")
	}
}
