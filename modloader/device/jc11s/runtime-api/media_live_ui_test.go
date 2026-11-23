package main

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNativeMediaStatusRequiresAuthAndExplicitEnablement(t *testing.T) {
	s := nativeProbeFixture(t)
	s.mediaImportWrites = true
	s.architecture = "aarch64"
	call := func(method, path string, auth bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, nil)
		if auth {
			r.Header.Set("Authorization", "Bearer "+s.authToken)
		}
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		return w
	}
	path := "/api/v1/media-import/native-status"
	if w := call("GET", path, false); w.Code != 401 {
		t.Fatal("unauthorized UI status", w.Code)
	}
	if w := call("POST", path, true); w.Code != 405 {
		t.Fatal("status mutation accepted", w.Code)
	}
	if w := call("GET", path+"?enabled=true", true); w.Code != 400 {
		t.Fatal("query override accepted", w.Code)
	}
	for _, enabled := range []bool{false, true} {
		s.mediaImportLive = enabled
		w := call("GET", path, true)
		var response struct {
			Enabled  bool   `json:"enabled"`
			Revision string `json:"scriptRevision"`
		}
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &response) != nil || response.Enabled != enabled || response.Revision != nativeMediaActorRevision() {
			t.Fatalf("status/revision mismatch: %d %s", w.Code, w.Body.String())
		}
		if w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("native actor state may be cached")
		}
	}
	s.model = "JC11S"
	if w := call("GET", path, true); strings.Contains(w.Body.String(), `"enabled":true`) {
		t.Fatal("unverified model enabled live actor")
	}
}
func TestEmbeddedNativeActorProvisionIsExactPrivateAndRejectsIndirectDirectory(t *testing.T) {
	root, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	dir := filepath.Join(root, "ui")
	if e = os.Mkdir(dir, 0700); e != nil {
		t.Fatal(e)
	}
	token := strings.Repeat("a", 64)
	if e = provisionNativeMediaUI(root, token); e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(dir, "NativeMediaImport.qml")
	data, e := os.ReadFile(path)
	if e != nil || !bytes.Equal(data, bytes.Replace(nativeMediaActor, uiAuthorizationMarker, []byte(token), 1)) {
		t.Fatal("embedded actor differs", e)
	}
	st, e := os.Lstat(path)
	if e != nil || st.Mode().Perm() != 0600 {
		t.Fatal("unsafe native actor mode", e)
	}
	if len(nativeMediaActorRevision()) != 64 {
		t.Fatal("invalid actor revision")
	}
	other := t.TempDir()
	linkRoot := t.TempDir()
	if e = os.Symlink(other, filepath.Join(linkRoot, "ui")); e != nil {
		t.Fatal(e)
	}
	if e = provisionNativeMediaUI(linkRoot, token); e == nil {
		t.Fatal("native actor written through indirect UI directory")
	}
	if _, e = os.Stat(filepath.Join(other, "NativeMediaImport.qml")); !os.IsNotExist(e) {
		t.Fatal("indirect target was modified")
	}
}

func TestTrustedNativeActorDeliveredThroughExistingOverlayLoader(t *testing.T) {
	s := nativeProbeFixture(t)
	s.mediaImportWrites = true
	s.mediaImportLive = true
	s.architecture = "aarch64"
	registry, e := loadRegistry(filepath.Join(s.root, "registry", "state.json"))
	if e != nil {
		t.Fatal(e)
	}
	for _, item := range registry.Mods {
		if item.Overlay != "" {
			path := filepath.Join(s.root, filepath.FromSlash(item.Root), filepath.FromSlash(item.Overlay))
			if e = os.MkdirAll(filepath.Dir(path), 0700); e != nil {
				t.Fatal(e)
			}
			if e = os.WriteFile(path, []byte("import QtQuick 2.12\nItem {}\n"), 0600); e != nil {
				t.Fatal(e)
			}
		}
	}
	if e := os.MkdirAll(filepath.Join(s.root, "ui"), 0700); e != nil {
		t.Fatal(e)
	}
	if e := provisionNativeMediaUI(s.root, s.authToken); e != nil {
		t.Fatal(e)
	}
	r := httptest.NewRequest("GET", "/api/v1/overlays", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var body struct {
		Overlays []overlayRecord `json:"overlays"`
	}
	if e := json.Unmarshal(w.Body.Bytes(), &body); e != nil {
		t.Fatal(e)
	}
	found := 0
	for _, o := range body.Overlays {
		if o.ID == "usb-copy-native" {
			found++
			if !strings.HasPrefix(o.Source, "file://"+s.root+"/ui/NativeMediaImport.qml?native=") {
				t.Fatal("actor not from trusted core", o.Source)
			}
		}
	}
	if found != 1 || strings.Contains(w.Body.String(), s.authToken) {
		t.Fatal("missing native overlay or leaked authorization")
	}
	s.mediaImportLive = false
	_, enabled, e := s.nativeMediaOverlay()
	if e != nil || enabled {
		t.Fatal("disabled native actor still advertised")
	}
}
