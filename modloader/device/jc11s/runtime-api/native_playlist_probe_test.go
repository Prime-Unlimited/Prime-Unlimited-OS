package main

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func nativeProbeFixture(t *testing.T) *server {
	root, item := serviceFixture(t)
	item.Service.Capability = "media-import"
	item.Overlay = "mod/ui.qml"
	saveServiceFixture(t, root, item)
	return &server{root: root, authToken: strings.Repeat("a", 64), model: "JP24", firmware: "5.3.0", catalog: catalogState{Status: "valid", Trusted: true, Catalog: catalog{SchemaVersion: 1, Mods: []mod{{ID: item.ID, Version: item.Version, SHA256: item.PackageSHA256, Service: item.Service, Models: []string{"JP24"}, Firmware: []string{"5.3.0"}}}}}}
}
func TestNativeProbeRequiresAuthorizationAndExpires(t *testing.T) {
	s := nativeProbeFixture(t)
	base := "/api/v1/native-playlist/probe"
	call := func(method, path, body string, auth bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		if auth {
			r.Header.Set("Authorization", "Bearer "+s.authToken)
		}
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		return w
	}
	if w := call("GET", base, "", false); w.Code != 401 {
		t.Fatal("unauthorized probe exposed")
	}
	if w := call("POST", base+"/arm", `{"action":"create"}`, true); w.Code != 400 {
		t.Fatal("mutation instructions accepted")
	}
	body := `{"mainScreen":true,"visited":2,"rootDepth":1,"durationMs":0,"truncated":false,"canRefreshCollection":true,"canImportTracks":true,"canAddTracksToDatabase":false,"instances":[],"ids":[{"type":"number","value":"16700","length":5}]}`
	if w := call("POST", base+"/observation", body, true); w.Code != 409 {
		t.Fatal("unarmed observation accepted")
	}
	if w := call("POST", base+"/arm", "{}", true); w.Code != 200 {
		t.Fatal("arm failed", w.Body.String())
	}
	if w := call("POST", base+"/observation", body, true); w.Code != 200 {
		t.Fatal("observation rejected", w.Body.String())
	}
	if w := call("POST", base+"/observation", strings.Replace(body, "16700", "/private/music/song.mp3", 1), true); w.Code != 400 {
		t.Fatal("private path accepted")
	}
	w := call("GET", base, "", true)
	var response map[string]any
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &response) != nil || response["enabled"] != true || response["observation"] == nil {
		t.Fatal("observation unavailable")
	}
	s.nativeProbeUntil = time.Now().Add(-time.Second)
	if w = call("POST", base+"/observation", body, true); w.Code != 409 {
		t.Fatal("expired probe accepted")
	}
	if w = call("DELETE", base, "", true); w.Code != 200 {
		t.Fatal("disarm failed")
	}
}
