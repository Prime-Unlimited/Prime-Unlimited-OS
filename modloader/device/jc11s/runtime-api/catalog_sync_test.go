package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCatalogSyncSignatureAndUpdateComparison(t *testing.T) {
	item := validV2Mod()
	item.Revision = 2
	encoded, err := json.Marshal(map[string]any{"schemaVersion": 2, "catalogId": "device-catalog", "mods": []mod{item}, "libraries": []library{}})
	if err != nil {
		t.Fatal(err)
	}
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	trust, _ := json.Marshal(catalogTrustKey{Algorithm: "ed25519", KeyID: "test-key", PublicKey: base64.StdEncoding.EncodeToString(public)})
	signature, _ := json.Marshal(catalogSignature{Algorithm: "ed25519", KeyID: "test-key", Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(private, encoded))})
	state := verifyCatalogData(encoded, signature, trust)
	if state.Status != "valid" || !state.Trusted || state.Catalog.Mods[0].Revision != 2 {
		t.Fatalf("signed catalog rejected: %#v", state)
	}
	encoded[0] ^= 1
	if verifyCatalogData(encoded, signature, trust).Status != "invalid" {
		t.Fatal("tampered catalog accepted")
	}
	installed := installedMod{Version: item.Version, Revision: 1, PackageSHA256: item.SHA256}
	if available, reason := updateDetails(item, installed); !available || reason != "revision" {
		t.Fatalf("revision update missing: %v %s", available, reason)
	}
	installed.Revision = 2
	installed.PackageSHA256 = strings.Repeat("f", 64)
	if available, reason := updateDetails(item, installed); !available || reason != "sha256" {
		t.Fatalf("hash update missing: %v %s", available, reason)
	}
}

func TestCatalogStartupUsesOnlySignedLastGoodFallback(t *testing.T) {
	directory := t.TempDir()
	catalogPath := filepath.Join(directory, "catalog.json")
	signaturePath := filepath.Join(directory, "catalog.sig.json")
	trustPath := filepath.Join(directory, "trust.json")
	item := validV2Mod()
	encoded, _ := json.Marshal(map[string]any{"schemaVersion": 2, "catalogId": "device-catalog", "mods": []mod{item}, "libraries": []library{}})
	public, private, _ := ed25519.GenerateKey(rand.Reader)
	trust, _ := json.Marshal(catalogTrustKey{Algorithm: "ed25519", KeyID: "test-key", PublicKey: base64.StdEncoding.EncodeToString(public)})
	signature, _ := json.Marshal(catalogSignature{Algorithm: "ed25519", KeyID: "test-key", Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(private, encoded))})
	for path, data := range map[string][]byte{catalogPath: []byte("broken"), signaturePath: signature, trustPath: trust, catalogPath + ".last-good": encoded, signaturePath + ".last-good": signature} {
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	state, fallback := loadVerifiedCatalogWithFallback(catalogPath, signaturePath, trustPath)
	if !fallback || state.Status != "valid" || !state.Trusted {
		t.Fatalf("fallback=%v state=%#v", fallback, state)
	}
	if err := os.WriteFile(signaturePath+".last-good", []byte("broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	if state, fallback = loadVerifiedCatalogWithFallback(catalogPath, signaturePath, trustPath); fallback || state.Status == "valid" {
		t.Fatal("invalid fallback was accepted")
	}
}

func TestManualCatalogSyncRequiresAuthorizationAndKeepsOfflineCatalog(t *testing.T) {
	root := t.TempDir()
	token := strings.Repeat("a", 64)
	handler := &server{authToken: token, root: root, catalog: catalogState{Status: "valid", Trusted: true, Catalog: catalog{SchemaVersion: 2, CatalogID: "cached", Mods: []mod{}, Libraries: []library{}}}, catalogSource: "offline-cache", catalogTrustKeyPath: filepath.Join(root, "missing"), httpClient: nil}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/catalog/sync", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized sync=%d", response.Code)
	}
	request = httptest.NewRequest(http.MethodPost, "/api/v1/catalog/sync", nil)
	request.Header.Set("Authorization", "Bearer "+token)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadGateway || !strings.Contains(response.Body.String(), `"offlineFallback":true`) {
		t.Fatalf("fallback response=%d %s", response.Code, response.Body.String())
	}
}
