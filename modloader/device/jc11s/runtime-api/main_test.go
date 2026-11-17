package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func validMod() mod {
	return mod{
		ID: "catalog-smoke-test", Name: "Catalog Smoke Test", Version: "0.0.0",
		Description: "Non-installable validation entry.", Models: []string{"JC11S"},
		Firmware: []string{"5.3.0"}, DownloadURL: "https://example.invalid/not-downloadable.zip",
		SHA256: strings.Repeat("0", 64), Size: 1,
	}
}

func writeCatalog(t *testing.T, value any) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "catalog.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestReadOnlyAPIContract(t *testing.T) {
	handler := &server{model: "JC11S", firmware: "5.3.0", catalog: catalogState{
		Catalog: catalog{SchemaVersion: 1, Mods: []mod{}}, Status: "valid",
	}}
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

func TestMutationAuthorizationFailsClosed(t *testing.T) {
	token := strings.Repeat("a", 64)
	handler := &server{authToken: token}
	tests := []struct {
		name       string
		path       string
		headers    []string
		wantStatus int
	}{
		{"missing", "/api/v1/install", nil, http.StatusUnauthorized},
		{"cache missing", "/api/v1/cache", nil, http.StatusUnauthorized},
		{"wrong", "/api/v1/install", []string{"Bearer " + strings.Repeat("b", 64)}, http.StatusUnauthorized},
		{"wrong scheme", "/api/v1/install", []string{token}, http.StatusUnauthorized},
		{"duplicate", "/api/v1/install", []string{"Bearer " + token, "Bearer " + token}, http.StatusUnauthorized},
		{"valid but unavailable", "/api/v1/install", []string{"Bearer " + token}, http.StatusNotImplemented},
		{"unknown mutation", "/api/v1/unknown", []string{"Bearer " + token}, http.StatusNotFound},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, test.path, strings.NewReader(`{"id":"catalog-smoke-test"}`))
			for _, value := range test.headers {
				request.Header.Add("Authorization", value)
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != test.wantStatus {
				t.Fatalf("status=%d, want=%d body=%s", response.Code, test.wantStatus, response.Body.String())
			}
			if strings.Contains(response.Body.String(), token) {
				t.Fatal("response leaked authorization token")
			}
		})
	}
}

func TestCacheEndpointUsesOnlyCatalogMetadata(t *testing.T) {
	payload := []byte("verified package bytes")
	digest := sha256.Sum256(payload)
	packageServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(payload)
	}))
	defer packageServer.Close()
	item := validMod()
	item.DownloadURL = packageServer.URL + "/package.zip"
	item.SHA256 = hex.EncodeToString(digest[:])
	item.Size = int64(len(payload))
	handler := &server{
		model: "JC11S", firmware: "5.3.0", authToken: strings.Repeat("a", 64),
		catalog:   catalogState{Status: "valid", Catalog: catalog{SchemaVersion: 1, Mods: []mod{item}}},
		cacheRoot: filepath.Join(t.TempDir(), "cache"), httpClient: packageServer.Client(),
	}
	tests := []struct {
		name string
		body string
		want int
	}{
		{"catalog id", `{"id":"catalog-smoke-test"}`, http.StatusOK},
		{"unknown id", `{"id":"missing"}`, http.StatusNotFound},
		{"client URL rejected", `{"id":"catalog-smoke-test","downloadUrl":"https://attacker.invalid/file"}`, http.StatusBadRequest},
		{"client path rejected", `{"id":"catalog-smoke-test","path":"/tmp/file"}`, http.StatusBadRequest},
		{"oversized request rejected", `{"id":"catalog-smoke-test"}` + strings.Repeat(" ", 4096), http.StatusBadRequest},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/api/v1/cache", strings.NewReader(test.body))
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Authorization", "Bearer "+handler.authToken)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != test.want {
				t.Fatalf("status=%d want=%d body=%s", response.Code, test.want, response.Body.String())
			}
		})
	}
}

func TestDownloadPackageVerifiesAndReusesAtomicCache(t *testing.T) {
	payload := []byte("PK\x03\x04test package")
	digest := sha256.Sum256(payload)
	requests := 0
	packageServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		_, _ = w.Write(payload)
	}))
	item := validMod()
	item.DownloadURL = packageServer.URL + "/package.zip"
	item.SHA256 = hex.EncodeToString(digest[:])
	item.Size = int64(len(payload))
	cacheRoot := filepath.Join(t.TempDir(), "packages")
	result, err := downloadPackage(t.Context(), packageServer.Client(), item, cacheRoot)
	if err != nil || !result.Cached {
		t.Fatalf("valid package rejected: result=%#v err=%v", result, err)
	}
	finalPath := filepath.Join(cacheRoot, item.SHA256+".zip")
	info, err := os.Stat(finalPath)
	if err != nil || info.Mode().Perm() != 0o600 || info.Size() != item.Size {
		t.Fatalf("unexpected cache file: info=%#v err=%v", info, err)
	}
	packageServer.Close()
	if _, err := downloadPackage(t.Context(), packageServer.Client(), item, cacheRoot); err != nil {
		t.Fatalf("valid cached package was not reused: %v", err)
	}
	if requests != 1 {
		t.Fatalf("package fetched %d times, want 1", requests)
	}
}

func TestDownloadPackageRejectsHashSizeAndRedirects(t *testing.T) {
	payload := []byte("package")
	digest := sha256.Sum256(payload)
	packageServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, "/package", http.StatusFound)
			return
		}
		_, _ = w.Write(payload)
	}))
	defer packageServer.Close()
	client := packageServer.Client()
	client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
	tests := []struct {
		name   string
		mutate func(*mod)
	}{
		{"hash mismatch", func(item *mod) { item.SHA256 = strings.Repeat("0", 64) }},
		{"size mismatch", func(item *mod) { item.Size++ }},
		{"redirect", func(item *mod) { item.DownloadURL = packageServer.URL + "/redirect" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			item := validMod()
			item.DownloadURL = packageServer.URL + "/package"
			item.SHA256 = hex.EncodeToString(digest[:])
			item.Size = int64(len(payload))
			test.mutate(&item)
			cacheRoot := filepath.Join(t.TempDir(), "packages")
			if _, err := downloadPackage(t.Context(), client, item, cacheRoot); err == nil {
				t.Fatal("invalid package was accepted")
			}
			entries, err := os.ReadDir(cacheRoot)
			if err != nil || len(entries) != 0 {
				t.Fatalf("failed download left cache files: entries=%v err=%v", entries, err)
			}
		})
	}
}

func TestPackageNetworkPolicy(t *testing.T) {
	for address, want := range map[string]bool{
		"127.0.0.1": false, "10.0.0.1": false, "169.254.1.1": false,
		"224.0.0.1": false, "0.0.0.0": false, "8.8.8.8": true,
	} {
		if got := publicPackageIP(net.ParseIP(address)); got != want {
			t.Errorf("publicPackageIP(%s)=%v want=%v", address, got, want)
		}
	}
	client := newPackageHTTPClient()
	request, _ := http.NewRequest(http.MethodGet, "https://example.invalid/next", nil)
	if err := client.CheckRedirect(request, []*http.Request{{}}); err != http.ErrUseLastResponse {
		t.Fatalf("redirect policy returned %v", err)
	}
}

func TestHealthReportsAuthorizationAvailability(t *testing.T) {
	for token, want := range map[string]string{"": "unavailable", strings.Repeat("a", 64): "ready"} {
		handler := &server{authToken: token}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/health", nil))
		if !strings.Contains(response.Body.String(), `"authorizationStatus":"`+want+`"`) {
			t.Fatalf("token-present=%v body=%s", token != "", response.Body.String())
		}
	}
}

func TestAuthTokenCreationAndPersistence(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "auth")
	path := filepath.Join(directory, "mutation.token")
	token, err := loadOrCreateAuthToken(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(token) != 64 || token != strings.ToLower(token) {
		t.Fatalf("unexpected generated token format: length=%d", len(token))
	}
	directoryInfo, err := os.Stat(directory)
	if err != nil || directoryInfo.Mode().Perm() != 0o700 {
		t.Fatalf("authorization directory mode=%v err=%v", directoryInfo.Mode().Perm(), err)
	}
	fileInfo, err := os.Stat(path)
	if err != nil || fileInfo.Mode().Perm() != 0o600 {
		t.Fatalf("authorization token mode=%v err=%v", fileInfo.Mode().Perm(), err)
	}
	reloaded, err := loadOrCreateAuthToken(path)
	if err != nil || reloaded != token {
		t.Fatalf("token was not persistent: equal=%v err=%v", reloaded == token, err)
	}
}

func TestAuthTokenRejectsUnsafePathsAndContent(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T, directory, path string)
	}{
		{"world readable", func(t *testing.T, _, path string) {
			if err := os.WriteFile(path, []byte(strings.Repeat("a", 64)+"\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}},
		{"invalid content", func(t *testing.T, _, path string) {
			if err := os.WriteFile(path, []byte(strings.Repeat("Z", 64)+"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		{"symlink", func(t *testing.T, directory, path string) {
			target := filepath.Join(directory, "target")
			if err := os.WriteFile(target, []byte(strings.Repeat("a", 64)+"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(target, path); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			directory := filepath.Join(t.TempDir(), "auth")
			if err := os.Mkdir(directory, 0o700); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(directory, "mutation.token")
			test.setup(t, directory, path)
			if _, err := loadOrCreateAuthToken(path); err == nil {
				t.Fatal("unsafe authorization token was accepted")
			}
		})
	}
}

func TestCatalogCompatibilityAndStatus(t *testing.T) {
	compatible := validMod()
	incompatible := validMod()
	incompatible.ID = "wrong-firmware"
	incompatible.Firmware = []string{"0.0.0"}
	handler := &server{model: "JC11S", firmware: "5.3.0", catalog: catalogState{
		Catalog: catalog{SchemaVersion: 1, Mods: []mod{compatible, incompatible}}, Status: "valid", SHA256: "abc",
	}}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/mods", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	var body struct {
		CatalogStatus string        `json:"catalogStatus"`
		Mods          []modResponse `json:"mods"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.CatalogStatus != "valid" || !body.Mods[0].Compatible || body.Mods[1].Compatible {
		t.Fatalf("unexpected response: %#v", body)
	}
}

func TestInvalidCatalogKeepsHealthAvailable(t *testing.T) {
	handler := &server{catalog: catalogState{Status: "invalid", Error: "test failure"}}
	for path, want := range map[string]int{"/api/v1/health": 200, "/api/v1/mods": 503} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != want || !strings.Contains(response.Body.String(), "invalid") {
			t.Fatalf("%s: status=%d body=%s", path, response.Code, response.Body.String())
		}
	}
}

func TestListenAddressIsStrictlyLoopback(t *testing.T) {
	for address, want := range map[string]bool{
		"127.0.0.1:8098":      true,
		"0.0.0.0:8098":        false,
		"127.0.0.2:8098":      false,
		"[::1]:8098":          false,
		"localhost:8098":      false,
		"127.0.0.1":           false,
		"127.0.0.1:8098/path": false,
	} {
		if got := validListenAddress(address); got != want {
			t.Errorf("validListenAddress(%q) = %v, want %v", address, got, want)
		}
	}
}

func TestLoadCatalogValidation(t *testing.T) {
	if state := loadCatalog(filepath.Join(t.TempDir(), "missing.json")); state.Status != "missing" || len(state.Catalog.Mods) != 0 {
		t.Fatalf("missing catalog should be empty: %#v", state)
	}
	valid := catalog{SchemaVersion: 1, Mods: []mod{validMod()}}
	if state := loadCatalog(writeCatalog(t, valid)); state.Status != "valid" || state.SHA256 == "" {
		t.Fatalf("valid catalog rejected: %#v", state)
	}

	tests := []struct {
		name string
		data string
	}{
		{"unknown field", `{"schemaVersion":1,"mods":[],"extra":true}`},
		{"unsupported schema", `{"schemaVersion":2,"mods":[]}`},
		{"duplicate id", `{"schemaVersion":1,"mods":[` + mustJSON(t, validMod()) + `,` + mustJSON(t, validMod()) + `]}`},
		{"uppercase sha", strings.Replace(mustJSON(t, valid), strings.Repeat("0", 64), strings.Repeat("A", 64), 1)},
		{"URL credentials", strings.Replace(mustJSON(t, valid), "https://example.invalid", "https://user@example.invalid", 1)},
		{"missing size", strings.Replace(mustJSON(t, valid), `,"size":1`, "", 1)},
		{"zero size", strings.Replace(mustJSON(t, valid), `"size":1`, `"size":0`, 1)},
		{"oversized package", strings.Replace(mustJSON(t, valid), `"size":1`, `"size":134217729`, 1)},
		{"trailing JSON", mustJSON(t, valid) + `{}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "catalog.json")
			if err := os.WriteFile(path, []byte(test.data), 0o600); err != nil {
				t.Fatal(err)
			}
			if state := loadCatalog(path); state.Status != "invalid" || state.Error == "" {
				t.Fatalf("invalid catalog accepted: %#v", state)
			}
		})
	}

	path := filepath.Join(t.TempDir(), "large.json")
	if err := os.WriteFile(path, []byte(strings.Repeat(" ", maxCatalogBytes+1)), 0o600); err != nil {
		t.Fatal(err)
	}
	if state := loadCatalog(path); state.Status != "invalid" || !strings.Contains(state.Error, "1 MiB") {
		t.Fatalf("oversized catalog accepted: %#v", state)
	}

	t.Run("world writable catalog", func(t *testing.T) {
		path := writeCatalog(t, valid)
		if err := os.Chmod(path, 0o666); err != nil {
			t.Fatal(err)
		}
		if state := loadCatalog(path); state.Status != "invalid" {
			t.Fatalf("writable catalog accepted: %#v", state)
		}
	})
	t.Run("catalog symlink", func(t *testing.T) {
		target := writeCatalog(t, valid)
		link := filepath.Join(t.TempDir(), "catalog.json")
		if err := os.Symlink(target, link); err != nil {
			t.Fatal(err)
		}
		if state := loadCatalog(link); state.Status != "invalid" {
			t.Fatalf("catalog symlink accepted: %#v", state)
		}
	})
}

func mustJSON(t *testing.T, value any) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
