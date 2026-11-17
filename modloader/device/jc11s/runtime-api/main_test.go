package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadOnlyAPIContract(t *testing.T) {
	handler := &server{model: "JC11S", firmware: "5.3.0", catalog: catalog{Mods: []map[string]any{}}}
	tests := []struct {
		method string
		path   string
		status int
	}{
		{http.MethodGet, "/api/v1/health", http.StatusOK},
		{http.MethodGet, "/api/v1/mods", http.StatusOK},
		{http.MethodGet, "/api/v1/installed", http.StatusOK},
		{http.MethodGet, "/api/v1/transactions", http.StatusOK},
		{http.MethodGet, "/api/v1/libraries", http.StatusOK},
		{http.MethodGet, "/api/v1/missing", http.StatusNotFound},
		{http.MethodGet, "/api/v1/health?unexpected=true", http.StatusBadRequest},
		{http.MethodPost, "/api/v1/install", http.StatusUnauthorized},
	}
	for _, test := range tests {
		t.Run(test.method+" "+test.path, func(t *testing.T) {
			request := httptest.NewRequest(test.method, test.path, nil)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != test.status {
				t.Fatalf("status = %d, want %d; body: %s", response.Code, test.status, response.Body.String())
			}
			if response.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("response is missing Cache-Control: no-store")
			}
		})
	}
}

func TestCatalogCompatibility(t *testing.T) {
	handler := &server{model: "JC11S", firmware: "5.3.0", catalog: catalog{Mods: []map[string]any{
		{"id": "compatible", "models": []any{"JC11S"}, "firmware": []any{"5.3.0"}},
		{"id": "revoked", "models": []any{"JC11S"}, "firmware": []any{"5.3.0"}, "revoked": true},
	}}}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/mods", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	var body struct {
		Mods []map[string]any `json:"mods"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Mods[0]["compatible"] != true || body.Mods[1]["compatible"] != false {
		t.Fatalf("unexpected compatibility result: %#v", body.Mods)
	}
}

func TestLoadCatalogValidation(t *testing.T) {
	directory := t.TempDir()
	missing, err := loadCatalog(filepath.Join(directory, "missing.json"))
	if err != nil || len(missing.Mods) != 0 {
		t.Fatalf("missing catalog should be empty and valid: %#v, %v", missing, err)
	}

	valid := `{"schemaVersion":1,"mods":[{"id":"demo","name":"Demo","version":"1","downloadUrl":"https://example.invalid/demo.zip","sha256":"abc"}]}`
	path := filepath.Join(directory, "catalog.json")
	if err := os.WriteFile(path, []byte(valid), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := loadCatalog(path)
	if err != nil || len(loaded.Mods) != 1 {
		t.Fatalf("valid catalog rejected: %#v, %v", loaded, err)
	}

	if err := os.WriteFile(path, []byte(strings.Repeat(" ", 1024*1024+1)), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadCatalog(path); err == nil {
		t.Fatal("oversized catalog was accepted")
	}
}
