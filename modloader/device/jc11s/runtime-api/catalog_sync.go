package main

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type catalogSyncResult struct {
	Status          string `json:"status"`
	Source          string `json:"source"`
	CatalogID       string `json:"catalogId"`
	CatalogSHA256   string `json:"catalogSha256"`
	LastSync        int64  `json:"lastSync"`
	OfflineFallback bool   `json:"offlineFallback"`
}

func migrateCatalogPermissions(catalogPath, signaturePath, trustPath string) error {
	seen := map[string]bool{}
	for _, directory := range []string{filepath.Dir(catalogPath), filepath.Dir(signaturePath), filepath.Dir(trustPath)} {
		if seen[directory] {
			continue
		}
		seen[directory] = true
		if err := migratePrivateDirectory(directory); err != nil {
			return err
		}
	}
	for _, path := range []string{catalogPath, signaturePath, catalogPath + ".last-good", signaturePath + ".last-good", trustPath} {
		if err := migratePrivateFile(path); err != nil {
			return err
		}
	}
	return nil
}

func verifyCatalogData(catalogData, signatureData, trustData []byte) catalogState {
	var trust catalogTrustKey
	var signature catalogSignature
	if decodeStrictJSON(trustData, &trust) != nil || decodeStrictJSON(signatureData, &signature) != nil {
		return catalogState{Status: "invalid", Error: "invalid catalog trust metadata"}
	}
	if trust.Algorithm != "ed25519" || signature.Algorithm != "ed25519" ||
		!modIDPattern.MatchString(trust.KeyID) || signature.KeyID != trust.KeyID {
		return catalogState{Status: "invalid", Error: "catalog trust metadata mismatch"}
	}
	publicKey, err := base64.StdEncoding.DecodeString(trust.PublicKey)
	if err != nil || len(publicKey) != ed25519.PublicKeySize {
		return catalogState{Status: "invalid", Error: "invalid catalog public key"}
	}
	signatureBytes, err := base64.StdEncoding.DecodeString(signature.Signature)
	if err != nil || len(signatureBytes) != ed25519.SignatureSize || !ed25519.Verify(ed25519.PublicKey(publicKey), catalogData, signatureBytes) {
		return catalogState{Status: "invalid", Error: "catalog signature verification failed"}
	}
	state := parseCatalog(catalogData)
	if state.Status != "valid" {
		return state
	}
	fingerprint := sha256.Sum256(publicKey)
	state.Trusted, state.KeyID, state.KeyFingerprint = true, trust.KeyID, hex.EncodeToString(fingerprint[:])
	return state
}

func fetchCatalogFile(ctx context.Context, client *http.Client, rawURL string, limit int64) ([]byte, error) {
	if client == nil || rawURL == "" {
		return nil, errors.New("catalog sync is not configured")
	}
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() != "raw.githubusercontent.com" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, errors.New("catalog URL must use raw.githubusercontent.com HTTPS")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("catalog HTTP status %d", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil || int64(len(data)) > limit {
		return nil, errors.New("catalog response exceeds size limit")
	}
	return data, nil
}

func writeAtomicFile(path string, data []byte) error {
	directory := filepath.Dir(path)
	if err := ensurePrivateDirectory(directory); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(directory, ".catalog-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err = temporary.Chmod(0o600); err == nil {
		_, err = temporary.Write(data)
	}
	if err == nil {
		err = temporary.Sync()
	}
	if closeErr := temporary.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err = os.Rename(temporaryPath, path); err != nil {
		return err
	}
	return syncDirectory(directory)
}

func (s *server) syncCatalog(ctx context.Context) (catalogSyncResult, error) {
	result := catalogSyncResult{Status: "failed", Source: s.catalogSource, LastSync: s.catalogLastSync}
	catalogData, err := fetchCatalogFile(ctx, s.httpClient, s.catalogURL, maxCatalogBytes)
	if err != nil {
		return result, err
	}
	signatureData, err := fetchCatalogFile(ctx, s.httpClient, s.catalogSignatureURL, 4096)
	if err != nil {
		return result, err
	}
	trustData, err := readOwnedFile(s.catalogTrustKeyPath, 4096, "catalog trust key")
	if err != nil {
		return result, err
	}
	state := verifyCatalogData(catalogData, signatureData, trustData)
	if state.Status != "valid" || !state.Trusted {
		return result, errors.New(state.Error)
	}
	// Preserve the currently verified pair before replacing either primary
	// file. If power is lost between the two primary renames, startup can
	// still recover this complete last-known-good pair.
	if current := loadVerifiedCatalog(s.catalogPath, s.catalogSignaturePath, s.catalogTrustKeyPath); current.Status == "valid" && current.Trusted {
		if oldCatalog, readErr := readOwnedFile(s.catalogPath, maxCatalogBytes, "catalog"); readErr == nil {
			if oldSignature, signatureErr := readOwnedFile(s.catalogSignaturePath, 4096, "catalog signature"); signatureErr == nil {
				if err := writeAtomicFile(s.catalogSignaturePath+".last-good", oldSignature); err != nil {
					return result, err
				}
				if err := writeAtomicFile(s.catalogPath+".last-good", oldCatalog); err != nil {
					return result, err
				}
			}
		}
	}
	// Persist the signature first. Until the catalog rename, the in-memory
	// last-known-good state remains active; a crash can never activate unsigned bytes.
	if err := writeAtomicFile(s.catalogSignaturePath, signatureData); err != nil {
		return result, err
	}
	if err := writeAtomicFile(s.catalogPath, catalogData); err != nil {
		return result, err
	}
	s.catalogMu.Lock()
	s.catalog = state
	s.catalogLastSync = time.Now().Unix()
	s.catalogSource = "github"
	s.catalogSyncError = ""
	result = catalogSyncResult{Status: "updated", Source: "github", CatalogID: state.Catalog.CatalogID, CatalogSHA256: state.SHA256, LastSync: s.catalogLastSync}
	s.catalogMu.Unlock()
	return result, nil
}

func loadVerifiedCatalogWithFallback(catalogPath, signaturePath, trustPath string) (catalogState, bool) {
	primary := loadVerifiedCatalog(catalogPath, signaturePath, trustPath)
	if primary.Status == "valid" && primary.Trusted {
		return primary, false
	}
	fallback := loadVerifiedCatalog(catalogPath+".last-good", signaturePath+".last-good", trustPath)
	if fallback.Status == "valid" && fallback.Trusted {
		return fallback, true
	}
	return primary, false
}

func (s *server) handleCatalogSync(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if data, _ := io.ReadAll(io.LimitReader(r.Body, 1)); len(data) != 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "catalog sync request body must be empty"})
		return
	}
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	result, err := s.syncCatalog(r.Context())
	if err != nil {
		s.catalogMu.Lock()
		s.catalogSyncError = err.Error()
		result.OfflineFallback = s.catalog.Status == "valid" && s.catalog.Trusted
		result.Source = s.catalogSource
		s.catalogMu.Unlock()
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": "catalog sync failed", "offlineFallback": result.OfflineFallback, "source": result.Source})
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func compareCatalogVersions(left, right string) int {
	a, aerr := parseSemanticVersion(strings.TrimPrefix(left, "v"))
	b, berr := parseSemanticVersion(strings.TrimPrefix(right, "v"))
	if aerr == nil && berr == nil {
		return compareVersions(a, b)
	}
	if left < right {
		return -1
	}
	if left > right {
		return 1
	}
	return 0
}

func updateDetails(item mod, installed installedMod) (bool, string) {
	comparison := compareCatalogVersions(installed.Version, item.Version)
	if comparison < 0 {
		return true, "version"
	}
	if comparison == 0 && installed.Revision < item.Revision {
		return true, "revision"
	}
	if comparison == 0 && installed.Revision == item.Revision && installed.PackageSHA256 != item.SHA256 {
		return true, "sha256"
	}
	return false, ""
}
