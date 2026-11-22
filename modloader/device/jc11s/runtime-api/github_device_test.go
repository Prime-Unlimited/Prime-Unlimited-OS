package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestGitHubAdminLoginEnablesDeveloperModeWithoutStoringTheToken(t *testing.T) {
	item := validV2Mod()
	handler, _ := developerFixture(t, item)
	var polls int
	var hold sync.Mutex
	release := make(chan struct{})
	git := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/code":
			_, _ = w.Write([]byte(`{"device_code":"device-secret","user_code":"WDJB-MJHT","verification_uri":"https://github.com/login/device","expires_in":30,"interval":1}`))
		case "/token":
			hold.Lock()
			polls++
			current := polls
			hold.Unlock()
			if current == 1 {
				_, _ = w.Write([]byte(`{"error":"authorization_pending"}`))
				return
			}
			select {
			case <-r.Context().Done():
				return
			case <-release:
			}
			_, _ = w.Write([]byte(`{"access_token":"gho_test_admin_token","token_type":"bearer"}`))
		case "/repo":
			if r.Header.Get("Authorization") != "Bearer gho_test_admin_token" {
				http.Error(w, "missing token", http.StatusUnauthorized)
				return
			}
			_, _ = w.Write([]byte(`{"permissions":{"admin":true,"push":true,"pull":true}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer git.Close()
	handler.github = &githubDeviceClient{clientID: "test", codeURL: git.URL + "/code", tokenURL: git.URL + "/token", repoURL: git.URL + "/repo", client: git.Client()}
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/developer-mode", strings.NewReader(`{"enabled":true}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+handler.authToken)
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || handler.developerEnabled() || !strings.Contains(response.Body.String(), `"status":"pending"`) || !strings.Contains(response.Body.String(), "WDJB-MJHT") || strings.Contains(response.Body.String(), "device-secret") || strings.Contains(response.Body.String(), "gho_test_admin_token") {
		t.Fatalf("login did not stay pending: %d %s enabled=%v", response.Code, response.Body.String(), handler.developerEnabled())
	}
	close(release)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && !handler.developerEnabled() {
		time.Sleep(20 * time.Millisecond)
	}
	if !handler.developerEnabled() {
		t.Fatal("repository admin was not enabled")
	}
	status := httptest.NewRecorder()
	handler.ServeHTTP(status, httptest.NewRequest(http.MethodGet, "/api/v1/developer-mode", nil))
	if status.Code != http.StatusOK || !strings.Contains(status.Body.String(), `"enabled":true`) || strings.Contains(status.Body.String(), "gho_test_admin_token") {
		t.Fatalf("status after approval: %d %s", status.Code, status.Body.String())
	}
	root := filepath.Dir(handler.developerModePath)
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		data, readErr := os.ReadFile(filepath.Join(root, entry.Name()))
		if readErr != nil {
			t.Fatal(readErr)
		}
		if strings.Contains(string(data), "gho_test_admin_token") || strings.Contains(string(data), "device-secret") {
			t.Fatalf("secret stored in %s", entry.Name())
		}
	}
	qr := httptest.NewRecorder()
	handler.ServeHTTP(qr, httptest.NewRequest(http.MethodGet, "/api/v1/developer-mode/qr.png", nil))
	if qr.Code != http.StatusOK || qr.Header().Get("Content-Type") != "image/png" || !strings.HasPrefix(qr.Body.String(), "\x89PNG") {
		t.Fatalf("qr response: %d %s", qr.Code, qr.Header().Get("Content-Type"))
	}
	off := httptest.NewRecorder()
	offRequest := httptest.NewRequest(http.MethodPost, "/api/v1/developer-mode", strings.NewReader(`{"enabled":false}`))
	offRequest.Header.Set("Content-Type", "application/json")
	offRequest.Header.Set("Authorization", "Bearer "+handler.authToken)
	handler.ServeHTTP(off, offRequest)
	if off.Code != http.StatusOK || handler.developerEnabled() {
		t.Fatalf("disable failed: %d %s", off.Code, off.Body.String())
	}
}

func TestGitHubLoginRejectsNonAdminAndCancel(t *testing.T) {
	handler, _ := developerFixture(t, validV2Mod())
	phase := "cancel"
	git := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/code":
			_, _ = w.Write([]byte(`{"device_code":"device-secret","user_code":"ABCD-EFGH","verification_uri":"https://github.com/login/device","expires_in":30,"interval":5}`))
		case "/token":
			if phase == "cancel" {
				_, _ = w.Write([]byte(`{"error":"authorization_pending"}`))
				return
			}
			_, _ = w.Write([]byte(`{"access_token":"gho_not_admin","token_type":"bearer"}`))
		case "/repo":
			_, _ = w.Write([]byte(`{"permissions":{"admin":false}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer git.Close()
	handler.github = &githubDeviceClient{clientID: "test", codeURL: git.URL + "/code", tokenURL: git.URL + "/token", repoURL: git.URL + "/repo", client: git.Client()}
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/developer-mode", strings.NewReader(`{"enabled":true}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+handler.authToken)
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "ABCD-EFGH") {
		t.Fatalf("login did not start: %d %s", response.Code, response.Body.String())
	}
	cancel := httptest.NewRecorder()
	cancelRequest := httptest.NewRequest(http.MethodPost, "/api/v1/developer-mode", strings.NewReader(`{"enabled":false}`))
	cancelRequest.Header.Set("Content-Type", "application/json")
	cancelRequest.Header.Set("Authorization", "Bearer "+handler.authToken)
	handler.ServeHTTP(cancel, cancelRequest)
	if handler.developerEnabled() || !strings.Contains(cancel.Body.String(), `"status":"idle"`) {
		t.Fatalf("cancelled login enabled the device: %s", cancel.Body.String())
	}

	phase = "deny"
	handler, _ = developerFixture(t, validV2Mod())
	handler.github = &githubDeviceClient{clientID: "test", codeURL: git.URL + "/code", tokenURL: git.URL + "/token", repoURL: git.URL + "/repo", client: git.Client()}
	denied := httptest.NewRecorder()
	deniedRequest := httptest.NewRequest(http.MethodPost, "/api/v1/developer-mode", strings.NewReader(`{"enabled":true}`))
	deniedRequest.Header.Set("Content-Type", "application/json")
	deniedRequest.Header.Set("Authorization", "Bearer "+handler.authToken)
	handler.ServeHTTP(denied, deniedRequest)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(handler.developerModeStatus()["status"].(string), "denied") {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if handler.developerEnabled() || handler.developerModeStatus()["status"] != "denied" {
		t.Fatalf("non-admin was not refused: %#v", handler.developerModeStatus())
	}
}
