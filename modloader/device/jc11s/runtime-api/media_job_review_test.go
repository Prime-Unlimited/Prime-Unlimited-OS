package main

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMediaReviewRejectsUnsafeRequests(t *testing.T) {
	token := strings.Repeat("b", 64)
	s := &server{root: t.TempDir(), authToken: token}
	base := "/api/v1/media-import/review/"
	for _, tc := range []struct {
		method, path string
		auth         bool
		want         int
	}{{"GET", base + token, false, 401}, {"POST", base + token, true, 405}, {"GET", base + "bad", true, 400}, {"GET", base + token + "?path=/media/SD", true, 400}, {"GET", base + token, true, 403}} {
		r := httptest.NewRequest(tc.method, tc.path, nil)
		if tc.auth {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if w.Code != tc.want {
			t.Fatalf("%s %s = %d", tc.method, tc.path, w.Code)
		}
	}
}
