package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const testKeyID = "test-catalog-1"

type signedFixture struct {
	catalog   string
	private   string
	signature string
	trust     string
}

func newSignedFixture(t *testing.T) signedFixture {
	t.Helper()
	directory := t.TempDir()
	fixture := signedFixture{
		catalog:   filepath.Join(directory, "catalog.json"),
		private:   filepath.Join(directory, "catalog.key"),
		signature: filepath.Join(directory, "catalog.sig.json"),
		trust:     filepath.Join(directory, "trust.json"),
	}
	if err := os.WriteFile(fixture.catalog, []byte("{\"schemaVersion\":1,\"mods\":[]}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fixture.private, []byte(hex.EncodeToString(privateKey)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := signCatalog(fixture.catalog, fixture.private, testKeyID, fixture.signature, fixture.trust, false); err != nil {
		t.Fatal(err)
	}
	return fixture
}

func rewriteJSON(t *testing.T, path string, value any) {
	t.Helper()
	data, err := marshalDocument(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestSignAndVerifyCatalog(t *testing.T) {
	fixture := newSignedFixture(t)
	result, err := verifyCatalog(fixture.catalog, fixture.signature, fixture.trust)
	if err != nil {
		t.Fatal(err)
	}
	if result.KeyID != testKeyID || len(result.KeyFingerprint) != 64 || len(result.CatalogDigest) != 64 {
		t.Fatalf("unexpected verification result: %#v", result)
	}
}

func TestVerifyRejectsTamperedCatalog(t *testing.T) {
	fixture := newSignedFixture(t)
	if err := os.WriteFile(fixture.catalog, []byte("{\"schemaVersion\":1,\"mods\":[{}]}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := verifyCatalog(fixture.catalog, fixture.signature, fixture.trust); err == nil || !strings.Contains(err.Error(), "verification failed") {
		t.Fatalf("expected signature failure, got %v", err)
	}
}

func TestVerifyRejectsWrongKeyID(t *testing.T) {
	fixture := newSignedFixture(t)
	data, err := os.ReadFile(fixture.signature)
	if err != nil {
		t.Fatal(err)
	}
	var document signature
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	document.KeyID = "other-test-key"
	rewriteJSON(t, fixture.signature, document)
	if _, err := verifyCatalog(fixture.catalog, fixture.signature, fixture.trust); err == nil || !strings.Contains(err.Error(), "metadata mismatch") {
		t.Fatalf("expected key ID mismatch, got %v", err)
	}
}

func TestVerifyRejectsWrongPublicKey(t *testing.T) {
	fixture := newSignedFixture(t)
	publicKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	rewriteJSON(t, fixture.trust, trustKey{
		Algorithm: "ed25519",
		KeyID:     testKeyID,
		PublicKey: base64.StdEncoding.EncodeToString(publicKey),
	})
	if _, err := verifyCatalog(fixture.catalog, fixture.signature, fixture.trust); err == nil || !strings.Contains(err.Error(), "verification failed") {
		t.Fatalf("expected wrong-key failure, got %v", err)
	}
}

func TestVerifyRejectsMalformedSignature(t *testing.T) {
	for name, encoded := range map[string]string{
		"invalid base64": "%%%",
		"wrong length":   base64.StdEncoding.EncodeToString([]byte("short")),
	} {
		t.Run(name, func(t *testing.T) {
			fixture := newSignedFixture(t)
			rewriteJSON(t, fixture.signature, signature{Algorithm: "ed25519", KeyID: testKeyID, Signature: encoded})
			if _, err := verifyCatalog(fixture.catalog, fixture.signature, fixture.trust); err == nil {
				t.Fatal("expected malformed signature to fail")
			}
		})
	}
}

func TestVerifyRejectsDuplicateMetadataKeys(t *testing.T) {
	for name, field := range map[string]string{"signature": "signature", "trust": "publicKey"} {
		t.Run(name, func(t *testing.T) {
			fixture := newSignedFixture(t)
			path := fixture.signature
			if name == "trust" {
				path = fixture.trust
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			closing := strings.LastIndexByte(string(data), '}')
			duplicate := append([]byte{}, data[:closing]...)
			duplicate = append(duplicate, []byte(",\n  \""+field+"\": \"attacker-controlled\"\n}\n")...)
			if err := os.WriteFile(path, duplicate, 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := verifyCatalog(fixture.catalog, fixture.signature, fixture.trust); err == nil {
				t.Fatal("expected duplicate metadata key rejection")
			}
		})
	}
}

func TestVerifyRejectsMalformedOrMissingTrustKey(t *testing.T) {
	for name, update := range map[string]func(*testing.T, signedFixture){
		"missing": func(t *testing.T, fixture signedFixture) {
			if err := os.Remove(fixture.trust); err != nil {
				t.Fatal(err)
			}
		},
		"malformed": func(t *testing.T, fixture signedFixture) {
			if err := os.WriteFile(fixture.trust, []byte("not-json\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		},
		"bad public key": func(t *testing.T, fixture signedFixture) {
			rewriteJSON(t, fixture.trust, trustKey{Algorithm: "ed25519", KeyID: testKeyID, PublicKey: "%%%"})
		},
	} {
		t.Run(name, func(t *testing.T) {
			fixture := newSignedFixture(t)
			update(t, fixture)
			if _, err := verifyCatalog(fixture.catalog, fixture.signature, fixture.trust); err == nil {
				t.Fatal("expected invalid trust key to fail")
			}
		})
	}
}

func TestLoadPrivateKeyRejectsMissingMalformedAndUnsafeFiles(t *testing.T) {
	directory := t.TempDir()
	missing := filepath.Join(directory, "missing.key")
	if _, err := loadPrivateKey(missing, false); err == nil {
		t.Fatal("expected missing key to fail")
	}
	malformed := filepath.Join(directory, "malformed.key")
	if err := os.WriteFile(malformed, []byte("not-a-private-key\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadPrivateKey(malformed, false); err == nil {
		t.Fatal("expected malformed key to fail")
	}
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	unsafe := filepath.Join(directory, "unsafe.key")
	if err := os.WriteFile(unsafe, []byte(hex.EncodeToString(privateKey)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(unsafe, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadPrivateKey(unsafe, false); err == nil || !strings.Contains(err.Error(), "mode 0600") {
		t.Fatalf("expected unsafe permissions to fail, got %v", err)
	}
}

func TestVerifyRejectsUnsafeMetadataPermissions(t *testing.T) {
	for _, field := range []string{"signature", "trust"} {
		t.Run(field, func(t *testing.T) {
			fixture := newSignedFixture(t)
			path := fixture.signature
			if field == "trust" {
				path = fixture.trust
			}
			if err := os.Chmod(path, 0o666); err != nil {
				t.Fatal(err)
			}
			if _, err := verifyCatalog(fixture.catalog, fixture.signature, fixture.trust); err == nil || !strings.Contains(err.Error(), "unsafe") {
				t.Fatalf("expected unsafe metadata to fail, got %v", err)
			}
		})
	}
}

func TestOfficialCatalogFixturesStillVerify(t *testing.T) {
	trust := filepath.Join("..", "trust", "prime-unlimited-2026-1.json")
	fixtures, err := filepath.Glob(filepath.Join("..", "*.sig.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(fixtures) == 0 {
		t.Fatal("no official signature fixtures found")
	}
	for _, signaturePath := range fixtures {
		name := strings.TrimSuffix(filepath.Base(signaturePath), ".sig.json")
		t.Run(name, func(t *testing.T) {
			catalogPath := strings.TrimSuffix(signaturePath, ".sig.json") + ".json"
			if _, err := verifyCatalog(catalogPath, signaturePath, trust); err != nil {
				t.Fatalf("official fixture no longer verifies: %v", err)
			}
		})
	}
}

func buildCLI(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "sign-catalog")
	command := exec.Command("go", "build", "-o", path, ".")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build CLI: %v\n%s", err, output)
	}
	return path
}

func TestVerifyCLIMutualExclusionExitCode(t *testing.T) {
	fixture := newSignedFixture(t)
	command := exec.Command(buildCLI(t), "-verify", "-catalog", fixture.catalog, "-signature", fixture.signature, "-public-key", fixture.trust, "-private-key", fixture.private)
	err := command.Run()
	exit, ok := err.(*exec.ExitError)
	if !ok || exit.ExitCode() != 2 {
		t.Fatalf("expected usage exit code 2, got %v", err)
	}
}

func TestVerifyCLIStdoutDoesNotDiscloseKeyMaterial(t *testing.T) {
	fixture := newSignedFixture(t)
	output, err := exec.Command(buildCLI(t), "-verify", "-catalog", fixture.catalog, "-signature", fixture.signature, "-public-key", fixture.trust).Output()
	if err != nil {
		t.Fatal(err)
	}
	privateData, _ := os.ReadFile(fixture.private)
	trustData, _ := os.ReadFile(fixture.trust)
	signatureData, _ := os.ReadFile(fixture.signature)
	var trust trustKey
	var signed signature
	if err := json.Unmarshal(trustData, &trust); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(signatureData, &signed); err != nil {
		t.Fatal(err)
	}
	stdout := string(output)
	for label, material := range map[string]string{"private key": strings.TrimSpace(string(privateData)), "public key": trust.PublicKey, "signature": signed.Signature} {
		if strings.Contains(stdout, material) {
			t.Fatalf("stdout disclosed %s", label)
		}
	}
	if !strings.Contains(stdout, "verified catalog key-id="+testKeyID) {
		t.Fatalf("unexpected stdout: %q", stdout)
	}
}
