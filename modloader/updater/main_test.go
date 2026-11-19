package main

import (
	"archive/zip"
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSignedManifestAndTamperDetection(t *testing.T) {
	m := []byte(`{"schemaVersion":1,"version":"0.2.0","revision":1,"artifacts":[{"model":"JC11S","architecture":"armv7l","abi":"gnueabihf","url":"https://api.github.com/repos/Prime-Unlimited/Prime-Unlimited-OS/releases/assets/1","sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","size":1}]}`)
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	trust, _ := json.Marshal(trustKey{Algorithm: "ed25519", KeyID: "test", PublicKey: base64.StdEncoding.EncodeToString(pub)})
	sig, _ := json.Marshal(signature{Algorithm: "ed25519", KeyID: "test", Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(priv, m))})
	if _, e := verify(m, sig, trust); e != nil {
		t.Fatal(e)
	}
	m[10] ^= 1
	if _, e := verify(m, sig, trust); e == nil {
		t.Fatal("tampered manifest accepted")
	}
}

func TestPublishedManifestSignatureAndDeviceIsolation(t *testing.T) {
	manifestData, e := os.ReadFile(filepath.Join("..", "update", "manifest.json"))
	if e != nil {
		t.Fatal(e)
	}
	signatureData, e := os.ReadFile(filepath.Join("..", "update", "manifest.sig.json"))
	if e != nil {
		t.Fatal(e)
	}
	trustData, e := os.ReadFile(filepath.Join("..", "catalog", "trust", "prime-unlimited-2026-1.json"))
	if e != nil {
		t.Fatal(e)
	}
	m, e := verify(manifestData, signatureData, trustData)
	if e != nil {
		t.Fatal(e)
	}
	if m.Version != "0.2.0" || m.Revision < 1 || len(m.Artifacts) != 2 {
		t.Fatalf("manifest=%#v", m)
	}
	seen := map[string]bool{}
	for _, a := range m.Artifacts {
		if seen[a.Model] || a.Size < 1024 || len(a.SHA256) != 64 {
			t.Fatalf("artifact=%#v", a)
		}
		seen[a.Model] = true
	}
	if !seen["JC11S"] || !seen["JP24"] {
		t.Fatal("device artifact missing")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestReleaseAssetRequestsBinaryContent(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if got := r.Header.Get("Accept"); got != "application/octet-stream" {
			t.Fatalf("Accept=%q", got)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("zip")), Header: make(http.Header)}, nil
	})}
	s := &server{client: client}
	data, err := s.fetch(t.Context(), "https://api.github.com/repos/Prime-Unlimited/Prime-Unlimited-OS/releases/assets/1", 10)
	if err != nil || string(data) != "zip" {
		t.Fatalf("data=%q err=%v", data, err)
	}
}

func bundle(t *testing.T, extra string) []byte {
	t.Helper()
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	for name := range allowed {
		w, _ := z.Create(name)
		_, _ = w.Write([]byte(name))
	}
	if extra != "" {
		w, _ := z.Create(extra)
		_, _ = w.Write([]byte("bad"))
	}
	if e := z.Close(); e != nil {
		t.Fatal(e)
	}
	return b.Bytes()
}
func TestBundleExtractionIsAllowlisted(t *testing.T) {
	if e := extract(bundle(t, ""), t.TempDir()); e != nil {
		t.Fatal(e)
	}
	if e := extract(bundle(t, "etc/passwd"), t.TempDir()); e == nil {
		t.Fatal("unapproved bundle entry accepted")
	}
}

func TestRevision8BridgeRemainsRevision7AllowlistCompatible(t *testing.T) {
	legacy := map[string]string{
		"bin/prime-unlimited-runtime-api":  "bin/prime-unlimited-runtime-api",
		"bin/prime-unlimited-self-updater": "bin/prime-unlimited-self-updater",
		"bin/modloader-api-start.sh":       "bin/modloader-api-start.sh",
		"bin/modloader-updater-start.sh":   "bin/modloader-updater-start.sh",
		"ui/ModLoader.template.qml":        "ui/ModLoader.template.qml",
		"ui/OverlayHost.template.qml":      "ui/OverlayHost.template.qml",
		"VERSION.json":                     "updater/version.json",
	}
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	for name := range legacy {
		w, _ := z.Create(name)
		_, _ = w.Write([]byte(name))
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	if err := extractWithAllowlist(b.Bytes(), t.TempDir(), legacy); err != nil {
		t.Fatal("revision 7 rejected the revision 8 bridge bundle:", err)
	}
}

func TestEmbeddedControlCenterPayloadMatchesCanonicalHook(t *testing.T) {
	canonical, err := os.ReadFile(filepath.Join("..", "device", "jc11s", "ui-hook", "ControlCenterTab.qml"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(canonical, []byte(controlCenterTabPayload)) {
		t.Fatal("embedded ControlCenterTab.qml differs from canonical QRC patch")
	}
	if len(canonical) > 1156 {
		t.Fatalf("ControlCenterTab.qml exceeds QRC slot: %d", len(canonical))
	}
}

func TestControlCenterMigrationBacksUpStagesAndInvalidatesCache(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, controlCenterTarget)
	cache := filepath.Join(root, "qml-cache")
	old := []byte("old control center")
	if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, old, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(cache, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cache, "compiled.qmlc"), []byte("cache"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := installControlCenterPatch(root, cache); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(target)
	if !bytes.Equal(got, []byte(controlCenterTabPayload)) {
		t.Fatal("Control Center patch was not staged")
	}
	backup, _ := os.ReadFile(filepath.Join(root, "updater", "backup", controlCenterTarget))
	if !bytes.Equal(backup, old) {
		t.Fatalf("backup=%q", backup)
	}
	if _, err := os.Stat(cache); !os.IsNotExist(err) {
		t.Fatalf("QML cache still exists: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, controlCenterMarker)); err != nil {
		t.Fatal("migration marker missing", err)
	}
	s := &server{root: root}
	if err := s.restore(); err != nil {
		t.Fatal(err)
	}
	restored, _ := os.ReadFile(target)
	if !bytes.Equal(restored, old) {
		t.Fatalf("restored=%q", restored)
	}
}

func TestUpdaterMutationAuthorization(t *testing.T) {
	root := t.TempDir()
	tokenPath := filepath.Join(root, "token")
	if e := os.WriteFile(tokenPath, []byte(strings.Repeat("a", 64)), 0600); e != nil {
		t.Fatal(e)
	}
	s := &server{tokenPath: tokenPath, state: status{Installed: versionState{"0.2.0", 1}}}
	r := httptest.NewRequest(http.MethodPost, "/api/v1/check", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatalf("status=%d", w.Code)
	}
	r = httptest.NewRequest(http.MethodGet, "/api/v1/status", nil)
	w = httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"version":"0.2.0"`) {
		t.Fatalf("status response=%s", w.Body.String())
	}
}
