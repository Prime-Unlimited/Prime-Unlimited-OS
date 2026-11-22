package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMediaJobEndpointRejectsUntrustedAndMutationRequests(t *testing.T) {
	token := strings.Repeat("a", 64)
	s := &server{root: t.TempDir(), authToken: token}
	for _, test := range []struct {
		method, path string
		auth         bool
		want         int
	}{{"GET", "/api/v1/media-import/jobs", false, 401}, {"POST", "/api/v1/media-import/jobs", true, 405}, {"GET", "/api/v1/media-import/jobs?path=/media/Denon", true, 400}, {"GET", "/api/v1/media-import/jobs", true, 403}} {
		r := httptest.NewRequest(test.method, test.path, nil)
		if test.auth {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if w.Code != test.want {
			t.Fatalf("%s %s returned %d want %d", test.method, test.path, w.Code, test.want)
		}
		if w.Code == http.StatusOK {
			t.Fatal("unexpected job disclosure")
		}
	}
}

func TestMediaStartAndAcknowledgementRemainExplicitlyGated(t *testing.T) {
	token := strings.Repeat("b", 64)
	s := &server{root: t.TempDir(), authToken: token, model: "JP24"}
	cases := []struct {
		method, path string
		auth         bool
		want         int
	}{{"POST", "/api/v1/media-import/start", false, 401}, {"GET", "/api/v1/media-import/start", true, 405}, {"POST", "/api/v1/media-import/start", true, 403}, {"POST", "/api/v1/media-import/start?source=/media/Denon", true, 400}, {"POST", "/api/v1/media-import/jobs/acknowledge", false, 401}, {"GET", "/api/v1/media-import/jobs/acknowledge", true, 405}, {"POST", "/api/v1/media-import/jobs/acknowledge", true, 403}}
	for _, test := range cases {
		r := httptest.NewRequest(test.method, test.path, strings.NewReader(`{"eventId":"`+strings.Repeat("a", 64)+`","confirmEngineRestart":true}`))
		if test.auth {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if w.Code != test.want {
			t.Fatalf("%s %s: %d want %d", test.method, test.path, w.Code, test.want)
		}
	}
}
