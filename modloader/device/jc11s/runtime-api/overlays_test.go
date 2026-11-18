package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func overlayFixture(t *testing.T) (string, installedMod) {
	t.Helper()
	root := t.TempDir()
	item := installedMod{ID: "clock-test", Version: "0.1.0", PackageSHA256: strings.Repeat("a", 64), CatalogID: "test", Architecture: "armv7l", ABI: "gnueabihf", Entrypoint: "start.sh", Overlay: "ui/PrimeClock.qml", Root: "mods/installed/clock-test/0.1.0", Enabled: true, InstalledAt: 1}
	overlay := filepath.Join(root, filepath.FromSlash(item.Root), "ui", "PrimeClock.qml")
	if err := os.MkdirAll(filepath.Dir(overlay), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(overlay, []byte("import QtQuick 2.12\nItem {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeAtomicJSON(filepath.Join(root, "registry", "state.json"), registryState{SchemaVersion: 1, Mods: map[string]installedMod{item.ID: item}}); err != nil {
		t.Fatal(err)
	}
	return root, item
}

func TestOverlaySettingsRequireAuthorizationAndPersist(t *testing.T) {
	root, item := overlayFixture(t)
	token := strings.Repeat("b", 64)
	handler := &server{root: root, authToken: token}

	request := httptest.NewRequest(http.MethodPost, "/api/v1/overlay-settings/"+item.ID, strings.NewReader(`{"x":123,"y":45}`))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized settings write: %d", response.Code)
	}
	request = httptest.NewRequest(http.MethodPost, "/api/v1/overlay-settings/"+item.ID, strings.NewReader(`{"x":123,"y":45}`))
	request.Header.Set("Authorization", "Bearer "+token)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("settings write: %d %s", response.Code, response.Body.String())
	}
	request = httptest.NewRequest(http.MethodGet, "/api/v1/overlay-settings/"+item.ID, nil)
	request.Header.Set("Authorization", "Bearer "+token)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"x":123`) {
		t.Fatalf("settings read: %d %s", response.Code, response.Body.String())
	}
}

func TestMonotonicEndpoint(t *testing.T) {
	response := httptest.NewRecorder()
	(&server{}).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/monotonic", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "milliseconds") {
		t.Fatalf("monotonic response: %d %s", response.Code, response.Body.String())
	}
}

func TestEnabledOverlaysReturnsValidatedEnabledSources(t *testing.T) {
	root, item := overlayFixture(t)
	overlays, err := enabledOverlays(root)
	if err != nil || len(overlays) != 1 || overlays[0].ID != item.ID || overlays[0].Source != "file://"+filepath.Join(root, filepath.FromSlash(item.Root), filepath.FromSlash(item.Overlay)) {
		t.Fatalf("overlays: %#v %v", overlays, err)
	}
	if _, err := setSafeMode(root, true); err != nil {
		t.Fatal(err)
	}
	overlays, err = enabledOverlays(root)
	if err != nil || len(overlays) != 0 {
		t.Fatalf("safe mode overlays: %#v %v", overlays, err)
	}
}

func TestEnabledOverlaysRejectsUnsafePayload(t *testing.T) {
	root, item := overlayFixture(t)
	path := filepath.Join(root, filepath.FromSlash(item.Root), filepath.FromSlash(item.Overlay))
	if err := os.Chmod(path, 0o666); err != nil {
		t.Fatal(err)
	}
	if _, err := enabledOverlays(root); err == nil {
		t.Fatal("unsafe overlay was exposed")
	}
}
