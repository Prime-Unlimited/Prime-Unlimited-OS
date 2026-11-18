package main

import (
	"archive/zip"
	"bytes"
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
	if m.Version != "0.2.0" || m.Revision != 3 || len(m.Artifacts) != 2 {
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
