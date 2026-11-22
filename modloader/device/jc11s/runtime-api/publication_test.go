package main

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPublicationStatusDefaultsToReadyAndRejectsUnknown(t *testing.T) {
	item := validV2Mod()
	if item.publicationStatus() != "ready" {
		t.Fatal("missing status was not treated as ready")
	}
	state := parseCatalog([]byte(validV2CatalogJSON(t, item)))
	if state.Status != "valid" || state.Catalog.Mods[0].publicationStatus() != "ready" {
		t.Fatalf("legacy v2 mod rejected: %#v", state)
	}
	item.Status = "development"
	item.Release = &releaseInfo{HardwareVerification: "not-performed", ArtifactSHA256: item.SHA256}
	if state = parseCatalog([]byte(validV2CatalogJSON(t, item))); state.Status != "valid" {
		t.Fatalf("development mod rejected: %#v", state)
	}
	item.Status = "beta"
	if state = parseCatalog([]byte(validV2CatalogJSON(t, item))); state.Status != "invalid" {
		t.Fatal("unknown status accepted")
	}
	base := mustJSON(t, validMod())
	v1 := `{"schemaVersion":1,"mods":[` + strings.TrimSuffix(base, "}") + `,"status":"development"}]}`
	if state = parseCatalog([]byte(v1)); state.Status != "invalid" {
		t.Fatalf("schema v1 status accepted: %#v", state)
	}
}

func developerFixture(t *testing.T, item mod) (*server, string) {
	t.Helper()
	root := t.TempDir()
	auth := filepath.Join(root, "auth")
	if err := os.MkdirAll(auth, 0o700); err != nil {
		t.Fatal(err)
	}
	token := strings.Repeat("ab", 32)
	if err := os.WriteFile(filepath.Join(auth, "developer.token"), []byte(token+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	handler := &server{
		model: "JC11S", firmware: "5.3.0", architecture: "armv7l", abi: "gnueabihf",
		authToken:          strings.Repeat("cd", 32),
		developerModePath:  filepath.Join(auth, "developer.mode"),
		developerTokenPath: filepath.Join(auth, "developer.token"),
		catalog: catalogState{Status: "valid", Trusted: true, Catalog: catalog{
			SchemaVersion: 2, CatalogID: "prime-unlimited-test", Mods: []mod{item}, Libraries: []library{},
		}},
		cacheRoot: filepath.Join(root, "cache"),
	}
	return handler, token
}

func TestDevelopmentModIsHiddenAndNotInstallableUntilAuthorized(t *testing.T) {
	item := validV2Mod()
	item.Status = "development"
	item.Release = &releaseInfo{HardwareVerification: "not-performed", ArtifactSHA256: item.SHA256}
	handler, developerToken := developerFixture(t, item)
	list := httptest.NewRecorder()
	handler.ServeHTTP(list, httptest.NewRequest(http.MethodGet, "/api/v1/mods", nil))
	if list.Code != http.StatusOK || strings.Contains(list.Body.String(), item.ID) {
		t.Fatalf("development mod visible without developer mode: %d %s", list.Code, list.Body.String())
	}
	install := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/install", strings.NewReader(`{"id":"`+item.ID+`"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+handler.authToken)
	handler.ServeHTTP(install, request)
	if install.Code != http.StatusForbidden || !strings.Contains(install.Body.String(), "mod is installable only in developer mode") {
		t.Fatalf("direct install was not refused: %d %s", install.Code, install.Body.String())
	}
	unprotected := httptest.NewRecorder()
	enable := httptest.NewRequest(http.MethodPost, "/api/v1/developer-mode", strings.NewReader(`{"enabled":true}`))
	enable.Header.Set("Content-Type", "application/json")
	enable.Header.Set("Authorization", "Bearer "+handler.authToken)
	enable.Header.Set("X-Prime-Developer-Token", developerToken)
	handler.ServeHTTP(unprotected, enable)
	if unprotected.Code != http.StatusForbidden || handler.developerEnabled() {
		t.Fatalf("local token enabled developer mode: %d %s", unprotected.Code, unprotected.Body.String())
	}
	if err := handler.setDeveloperEnabled(true); err != nil {
		t.Fatal(err)
	}
	list = httptest.NewRecorder()
	handler.ServeHTTP(list, httptest.NewRequest(http.MethodGet, "/api/v1/mods", nil))
	if !strings.Contains(list.Body.String(), `"status":"development"`) || !strings.Contains(list.Body.String(), `"developerMode":true`) {
		t.Fatalf("development mod hidden while developer mode is on: %s", list.Body.String())
	}
}

func TestReadyModStaysVisibleWithoutDeveloperMode(t *testing.T) {
	item := validV2Mod()
	item.Status = "ready"
	item.Release = &releaseInfo{HardwareVerification: "recorded-separately", ArtifactSHA256: item.SHA256}
	handler, _ := developerFixture(t, item)
	list := httptest.NewRecorder()
	handler.ServeHTTP(list, httptest.NewRequest(http.MethodGet, "/api/v1/mods", nil))
	if list.Code != http.StatusOK || !strings.Contains(list.Body.String(), item.ID) || strings.Contains(list.Body.String(), `"developerMode":true`) {
		t.Fatalf("ready mod missing: %d %s", list.Code, list.Body.String())
	}
}

func TestCacheReportsConcreteDownloadFailures(t *testing.T) {
	item := validV2Mod()
	handler, _ := developerFixture(t, item)
	handler.authToken = strings.Repeat("cd", 32)
	handler.httpClient = newPackageHTTPClient()
	item.DownloadURL = "https://example.invalid/package.zip"
	handler.catalog.Catalog.Mods[0] = item
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/cache", strings.NewReader(`{"id":"`+item.ID+`"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+handler.authToken)
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadGateway || !strings.Contains(response.Body.String(), "download URL is not supported") {
		t.Fatalf("unsupported URL was not named: %d %s", response.Code, response.Body.String())
	}
}

func TestLegacyDownloadURLIsClassifiedAndHashed(t *testing.T) {
	legacy := "https://github.com/Prime-Unlimited/Prime-Unlimited-OS/releases/download/splash-manager-v1.1.2/splash-manager-v1.1.2-jc11s.zip"
	if err := classifyPackageURL(legacy); err != nil {
		t.Fatal(err)
	}
	asset := "https://api.github.com/repos/Prime-Unlimited/Prime-Unlimited-OS/releases/assets/773325277"
	if err := classifyPackageURL(asset); err != nil {
		t.Fatal(err)
	}
	if err := classifyPackageURL("https://evil.example/releases/download/x/y.zip"); err == nil {
		t.Fatal("arbitrary host accepted")
	}
	payload := []byte("package-bytes")
	digest := sha256.Sum256(payload)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, "https://evil.example/stolen", http.StatusFound)
			return
		}
		_, _ = w.Write(payload)
	}))
	defer server.Close()
	client := server.Client()
	client.CheckRedirect = allowPackageRedirect
	item := validMod()
	item.DownloadURL = server.URL + "/redirect"
	item.SHA256 = hex.EncodeToString(digest[:])
	item.Size = int64(len(payload))
	if _, err := downloadPackage(t.Context(), client, item, filepath.Join(t.TempDir(), "cache")); err == nil || !strings.Contains(err.Error(), "HTTP redirect blocked") {
		t.Fatalf("blocked redirect error = %v", err)
	}
	item.DownloadURL = server.URL + "/package"
	item.SHA256 = strings.Repeat("ab", 32)
	if _, err := downloadPackage(t.Context(), client, item, filepath.Join(t.TempDir(), "cache")); err == nil || !strings.Contains(err.Error(), "package SHA-256 mismatch") {
		t.Fatalf("hash error = %v", err)
	}
}

func TestArchitectureAndFirmwareReasons(t *testing.T) {
	item := validV2Mod()
	handler, _ := developerFixture(t, item)
	item.Architecture = "aarch64"
	if reason := handler.accessReason(item, 2); reason != "architecture is not compatible" {
		t.Fatalf("architecture reason = %q", reason)
	}
	item = validV2Mod()
	item.Firmware = []string{"9.9.9"}
	if reason := handler.accessReason(item, 2); reason != "firmware is not supported" {
		t.Fatalf("firmware reason = %q", reason)
	}
}

func TestInvalidSignatureBlocksInstall(t *testing.T) {
	item := validV2Mod()
	handler, _ := developerFixture(t, item)
	handler.catalog.Status = "invalid"
	handler.catalog.Trusted = false
	handler.catalog.Error = "catalog signature verification failed"
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/install", strings.NewReader(`{"id":"`+item.ID+`"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+handler.authToken)
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "catalog signature verification failed") {
		t.Fatalf("bad signature did not block install: %d %s", response.Code, response.Body.String())
	}
}
