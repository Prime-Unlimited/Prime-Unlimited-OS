package main

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type zipEntry struct {
	name string
	data string
	mode os.FileMode
}

func makeZIP(t *testing.T, entries []zipEntry) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for _, entry := range entries {
		header := &zip.FileHeader{Name: entry.name, Method: zip.Deflate}
		if entry.mode == 0 {
			entry.mode = 0o644
		}
		header.SetMode(entry.mode)
		part, err := writer.CreateHeader(header)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write([]byte(entry.data)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func cachedZIP(t *testing.T, payload []byte) (mod, string) {
	t.Helper()
	digest := sha256.Sum256(payload)
	item := validMod()
	item.SHA256 = hex.EncodeToString(digest[:])
	item.Size = int64(len(payload))
	root := filepath.Join(t.TempDir(), "cache")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, item.SHA256+".zip"), payload, 0o600); err != nil {
		t.Fatal(err)
	}
	return item, root
}

func TestStagePackageExtractsAtomicallyWithPrivateModes(t *testing.T) {
	payload := makeZIP(t, []zipEntry{
		{name: "mod/", mode: os.ModeDir | 0o777},
		{name: "mod/readme.txt", data: "hello", mode: 0o777},
		{name: "mod/data/config.json", data: "{}", mode: 0o666},
	})
	item, cacheRoot := cachedZIP(t, payload)
	stagingRoot := filepath.Join(t.TempDir(), "staging")
	result, err := stagePackage(item, cacheRoot, stagingRoot, defaultStageLimits)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Staged || result.FileCount != 2 || result.TotalBytes != 7 {
		t.Fatalf("unexpected result: %#v", result)
	}
	finalRoot := filepath.Join(stagingRoot, item.SHA256)
	for path, mode := range map[string]os.FileMode{
		finalRoot: 0o700, filepath.Join(finalRoot, "mod"): 0o700,
		filepath.Join(finalRoot, "mod", "data"):       0o700,
		filepath.Join(finalRoot, "mod", "readme.txt"): 0o600,
		filepath.Join(finalRoot, stageMarker):         0o600,
	} {
		info, err := os.Lstat(path)
		if err != nil || info.Mode().Perm() != mode {
			t.Fatalf("%s mode=%v err=%v", path, info, err)
		}
	}
	second, err := stagePackage(item, cacheRoot, stagingRoot, defaultStageLimits)
	if err != nil || second != result {
		t.Fatalf("valid existing stage not reused: %#v %v", second, err)
	}
	stable := item
	stable.Version = "1.0.0"
	promoted, err := stagePackage(stable, cacheRoot, stagingRoot, defaultStageLimits)
	if err != nil || promoted.Version != stable.Version || promoted.SHA256 != item.SHA256 {
		t.Fatalf("same-payload stable promotion did not reuse stage: %#v %v", promoted, err)
	}
	entries, _ := os.ReadDir(stagingRoot)
	if len(entries) != 1 {
		t.Fatalf("temporary staging data remained: %v", entries)
	}
	if err := os.WriteFile(filepath.Join(finalRoot, "mod", "readme.txt"), []byte("HELLO"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := stagePackage(item, cacheRoot, stagingRoot, defaultStageLimits); err == nil {
		t.Fatal("same-size staged content tampering was accepted")
	}
}

func TestStagePackageRejectsUnsafeArchives(t *testing.T) {
	tests := []struct {
		name    string
		entries []zipEntry
		limits  stageLimits
	}{
		{"parent traversal", []zipEntry{{name: "../escape", data: "x"}}, defaultStageLimits},
		{"absolute", []zipEntry{{name: "/escape", data: "x"}}, defaultStageLimits},
		{"windows", []zipEntry{{name: `C:\escape`, data: "x"}}, defaultStageLimits},
		{"backslash", []zipEntry{{name: `dir\escape`, data: "x"}}, defaultStageLimits},
		{"symlink", []zipEntry{{name: "link", data: "target", mode: os.ModeSymlink | 0o777}}, defaultStageLimits},
		{"duplicate", []zipEntry{{name: "same", data: "a"}, {name: "same", data: "b"}}, defaultStageLimits},
		{"file parent", []zipEntry{{name: "parent", data: "a"}, {name: "parent/child", data: "b"}}, defaultStageLimits},
		{"file after child", []zipEntry{{name: "parent/child", data: "b"}, {name: "parent", data: "a"}}, defaultStageLimits},
		{"reserved marker", []zipEntry{{name: stageMarker, data: "fake"}}, defaultStageLimits},
		{"too deep", []zipEntry{{name: "a/b/c", data: "x"}}, stageLimits{10, 2, 240, 100, 100}},
		{"file too large", []zipEntry{{name: "large", data: "12345"}}, stageLimits{10, 10, 240, 4, 100}},
		{"total too large", []zipEntry{{name: "one", data: "123"}, {name: "two", data: "456"}}, stageLimits{10, 10, 240, 10, 5}},
		{"too many", []zipEntry{{name: "one", data: "1"}, {name: "two", data: "2"}}, stageLimits{1, 10, 240, 10, 10}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			item, cacheRoot := cachedZIP(t, makeZIP(t, test.entries))
			stagingRoot := filepath.Join(t.TempDir(), "staging")
			if _, err := stagePackage(item, cacheRoot, stagingRoot, test.limits); err == nil {
				t.Fatal("unsafe archive was accepted")
			}
			entries, err := os.ReadDir(stagingRoot)
			if err != nil || len(entries) != 0 {
				t.Fatalf("failed stage left data: %v, %v", entries, err)
			}
		})
	}
}

func TestStagePackageRequiresValidCacheAndRejectsCorruptZIP(t *testing.T) {
	for name, payload := range map[string][]byte{"not zip": []byte("not a zip"), "truncated": makeZIP(t, []zipEntry{{name: "file", data: "payload"}})[:20]} {
		t.Run(name, func(t *testing.T) {
			item, cacheRoot := cachedZIP(t, payload)
			if _, err := stagePackage(item, cacheRoot, filepath.Join(t.TempDir(), "stage"), defaultStageLimits); err == nil {
				t.Fatal("invalid ZIP accepted")
			}
		})
	}
	item := validMod()
	item.SHA256 = strings.Repeat("1", 64)
	item.Size = 10
	if _, err := stagePackage(item, t.TempDir(), filepath.Join(t.TempDir(), "stage"), defaultStageLimits); err == nil {
		t.Fatal("missing cache accepted")
	}
}

func TestStageEndpointIsAuthenticatedCatalogBoundAndCacheOnly(t *testing.T) {
	payload := makeZIP(t, []zipEntry{{name: "mod.txt", data: "safe"}})
	item, cacheRoot := cachedZIP(t, payload)
	token := strings.Repeat("a", 64)
	handler := &server{model: "JC11S", firmware: "5.3.0", authToken: token, cacheRoot: cacheRoot,
		stagingRoot: filepath.Join(t.TempDir(), "stage"), catalog: catalogState{Status: "valid", Trusted: true, Catalog: catalog{SchemaVersion: 1, Mods: []mod{item}}}}
	for _, test := range []struct {
		name, body string
		auth       bool
		want       int
	}{
		{"unauthorized", `{"id":"catalog-smoke-test"}`, false, http.StatusUnauthorized},
		{"client path rejected", `{"id":"catalog-smoke-test","path":"/tmp/evil"}`, true, http.StatusBadRequest},
		{"unknown", `{"id":"unknown"}`, true, http.StatusNotFound},
		{"catalog entry", `{"id":"catalog-smoke-test"}`, true, http.StatusOK},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/api/v1/stage", strings.NewReader(test.body))
			request.Header.Set("Content-Type", "application/json")
			if test.auth {
				request.Header.Set("Authorization", "Bearer "+token)
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != test.want {
				t.Fatalf("status=%d want=%d body=%s", response.Code, test.want, response.Body.String())
			}
		})
	}
}
