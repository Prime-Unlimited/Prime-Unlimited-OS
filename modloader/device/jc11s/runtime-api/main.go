package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const maxCatalogBytes = 1024 * 1024
const maxPackageBytes = 128 * 1024 * 1024

var modIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)
var versionPattern = regexp.MustCompile(`^[0-9A-Za-z][0-9A-Za-z._+-]{0,63}$`)

type catalog struct {
	SchemaVersion int       `json:"schemaVersion"`
	CatalogID     string    `json:"catalogId,omitempty"`
	Authenticity  any       `json:"authenticity,omitempty"`
	Mods          []mod     `json:"mods"`
	Libraries     []library `json:"libraries,omitempty"`
}

type mod struct {
	ID           string       `json:"id"`
	Name         string       `json:"name"`
	Version      string       `json:"version"`
	Description  string       `json:"description,omitempty"`
	Models       []string     `json:"models"`
	Firmware     []string     `json:"firmware"`
	DownloadURL  string       `json:"downloadUrl"`
	SHA256       string       `json:"sha256"`
	Size         int64        `json:"size"`
	Architecture string       `json:"architecture,omitempty"`
	ABI          string       `json:"abi,omitempty"`
	Entrypoint   string       `json:"entrypoint,omitempty"`
	Approval     *approval    `json:"approval,omitempty"`
	Dependencies []dependency `json:"dependencies,omitempty"`
	Libraries    []dependency `json:"libraries,omitempty"`
	Conflicts    []string     `json:"conflicts,omitempty"`
	Revoked      bool         `json:"revoked,omitempty"`
}

type approval struct {
	Maintainer string   `json:"maintainer"`
	Devices    []string `json:"devices"`
}

type dependency struct {
	ID         string `json:"id"`
	Constraint string `json:"constraint"`
	Type       string `json:"type,omitempty"`
}

type library struct {
	ID                 string       `json:"id"`
	Name               string       `json:"name"`
	Version            string       `json:"version"`
	Models             []string     `json:"models"`
	Firmware           []string     `json:"firmware"`
	FirmwareConstraint string       `json:"firmwareConstraint,omitempty"`
	Architecture       string       `json:"architecture"`
	ABI                string       `json:"abi"`
	DownloadURL        string       `json:"downloadUrl"`
	SHA256             string       `json:"sha256"`
	Size               int64        `json:"size"`
	Approval           *approval    `json:"approval"`
	Dependencies       []dependency `json:"dependencies,omitempty"`
	Conflicts          []string     `json:"conflicts,omitempty"`
	Revoked            bool         `json:"revoked,omitempty"`
}

type modResponse struct {
	mod
	Compatible bool `json:"compatible"`
}

type catalogState struct {
	Catalog        catalog
	Status         string
	Error          string
	SHA256         string
	Trusted        bool
	KeyID          string
	KeyFingerprint string
}

type catalogTrustKey struct {
	Algorithm string `json:"algorithm"`
	KeyID     string `json:"keyId"`
	PublicKey string `json:"publicKey"`
}

type catalogSignature struct {
	Algorithm string `json:"algorithm"`
	KeyID     string `json:"keyId"`
	Signature string `json:"signature"`
}

type server struct {
	model        string
	firmware     string
	architecture string
	abi          string
	catalog      catalogState
	authToken    string
	cacheRoot    string
	stagingRoot  string
	root         string
	httpClient   *http.Client
	cacheMu      sync.Mutex
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func (s *server) compatible(item mod) bool {
	if !contains(item.Models, s.model) || !contains(item.Firmware, s.firmware) || item.Revoked {
		return false
	}
	if s.catalog.Catalog.SchemaVersion == 2 {
		return item.Architecture == s.architecture && item.ABI == s.abi && item.Approval != nil && contains(item.Approval.Devices, s.model)
	}
	return true
}

func (s *server) authorized(r *http.Request) bool {
	values := r.Header.Values("Authorization")
	if len(values) != 1 || s.authToken == "" {
		return false
	}
	expected := "Bearer " + s.authToken
	return len(values[0]) == len(expected) && subtle.ConstantTimeCompare([]byte(values[0]), []byte(expected)) == 1
}

func (s *server) authorizationStatus() string {
	if s.authToken == "" {
		return "unavailable"
	}
	return "ready"
}

func isMutationPath(path string) bool {
	switch path {
	case "/api/v1/cache", "/api/v1/stage", "/api/v1/inspect", "/api/v1/install", "/api/v1/enable", "/api/v1/disable", "/api/v1/uninstall", "/api/v1/rollback":
		return true
	default:
		return false
	}
}

type cacheRequest struct {
	ID string `json:"id"`
}

type cacheResult struct {
	ID      string `json:"id"`
	Version string `json:"version"`
	SHA256  string `json:"sha256"`
	Size    int64  `json:"size"`
	Cached  bool   `json:"cached"`
}

func (s *server) catalogMod(id string) (mod, bool) {
	if s.catalog.Status != "valid" || !s.catalog.Trusted {
		return mod{}, false
	}
	for _, item := range s.catalog.Catalog.Mods {
		if item.ID == id && s.compatible(item) {
			return item, true
		}
	}
	return mod{}, false
}

func decodeCacheRequest(r *http.Request) (cacheRequest, error) {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return cacheRequest{}, errors.New("Content-Type must be application/json")
	}
	data, err := io.ReadAll(io.LimitReader(r.Body, 4097))
	if err != nil || len(data) > 4096 {
		return cacheRequest{}, errors.New("request body exceeds 4096 bytes")
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	var request cacheRequest
	if err := decoder.Decode(&request); err != nil {
		return cacheRequest{}, errors.New("invalid request JSON")
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return cacheRequest{}, errors.New("request contains trailing JSON data")
	}
	if !modIDPattern.MatchString(request.ID) {
		return cacheRequest{}, errors.New("invalid mod id")
	}
	return request, nil
}

func (s *server) handleCache(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	request, err := decodeCacheRequest(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	item, ok := s.catalogMod(request.ID)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "compatible catalog mod not found"})
		return
	}
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	result, err := downloadPackage(r.Context(), s.httpClient, item, s.cacheRoot)
	if err != nil {
		log.Printf("package cache failed for %s", item.ID)
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "package download or verification failed"})
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *server) handleStage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	request, err := decodeCacheRequest(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	item, ok := s.catalogMod(request.ID)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "compatible catalog mod not found"})
		return
	}
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	result, err := stagePackage(item, s.cacheRoot, s.stagingRoot, defaultStageLimits)
	if err != nil {
		log.Printf("package staging failed for %s", item.ID)
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "cached package missing, invalid, or unsafe"})
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *server) handleInspect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	request, err := decodeCacheRequest(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if s.catalog.Catalog.SchemaVersion != 2 {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "trusted install metadata unavailable"})
		return
	}
	item, ok := s.catalogMod(request.ID)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "compatible approved catalog mod not found"})
		return
	}
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	result, err := inspectStagedPackage(item, s.catalog.Catalog.CatalogID, s.cacheRoot, s.stagingRoot)
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "verified staged package is unavailable or ineligible"})
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *server) handleInstall(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	request, err := decodeCacheRequest(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if s.catalog.Catalog.SchemaVersion != 2 {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "trusted install metadata unavailable"})
		return
	}
	item, ok := s.catalogMod(request.ID)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "compatible approved catalog mod not found"})
		return
	}
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	result, err := installDisabledMod(item, s.catalog.Catalog.CatalogID, s.cacheRoot, s.stagingRoot, s.root)
	if errors.Is(err, errAlreadyInstalled) {
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	if err != nil {
		log.Printf("disabled install failed for %s", item.ID)
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "verified package installation failed"})
		return
	}
	writeJSON(w, http.StatusAccepted, result)
}

func (s *server) handleUninstall(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	request, err := decodeCacheRequest(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	result, err := uninstallDisabledMod(request.ID, s.root)
	if errors.Is(err, errNotInstalled) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	if err != nil {
		log.Printf("disabled uninstall failed for %s", request.ID)
		writeJSON(w, http.StatusConflict, map[string]string{"error": "disabled mod uninstallation failed"})
		return
	}
	writeJSON(w, http.StatusAccepted, result)
}

func (s *server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.RawQuery != "" || r.URL.Fragment != "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "query parameters are not supported"})
		return
	}
	if r.Method != http.MethodGet {
		if !isMutationPath(r.URL.Path) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		if !s.authorized(r) {
			w.Header().Set("WWW-Authenticate", "Bearer")
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "mutation authorization required"})
			return
		}
		if r.URL.Path == "/api/v1/cache" {
			s.handleCache(w, r)
			return
		}
		if r.URL.Path == "/api/v1/stage" {
			s.handleStage(w, r)
			return
		}
		if r.URL.Path == "/api/v1/inspect" {
			s.handleInspect(w, r)
			return
		}
		if r.URL.Path == "/api/v1/install" {
			s.handleInstall(w, r)
			return
		}
		if r.URL.Path == "/api/v1/uninstall" {
			s.handleUninstall(w, r)
			return
		}
		writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "mutation engine unavailable"})
		return
	}
	if strings.HasPrefix(r.URL.Path, "/api/v1/transactions/") {
		id := strings.TrimPrefix(r.URL.Path, "/api/v1/transactions/")
		decoded, err := hex.DecodeString(id)
		if err != nil || len(decoded) != 16 || hex.EncodeToString(decoded) != id {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid transaction id"})
			return
		}
		records, err := listTransactions(s.root)
		if err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "transaction registry unavailable"})
			return
		}
		for _, record := range records {
			if record.ID == id {
				writeJSON(w, http.StatusOK, record)
				return
			}
		}
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "transaction not found"})
		return
	}
	switch r.URL.Path {
	case "/api/v1/health":
		writeJSON(w, http.StatusOK, map[string]any{
			"service": "prime-unlimited-mod-loader", "mode": "authenticated-disabled-lifecycle",
			"catalogStatus": s.catalog.Status, "catalogError": s.catalog.Error,
			"catalogTrust": map[bool]string{true: "verified", false: "unverified"}[s.catalog.Trusted],
			"catalogKeyId": s.catalog.KeyID, "catalogKeyFingerprint": s.catalog.KeyFingerprint,
			"catalogSchemaVersion": s.catalog.Catalog.SchemaVersion, "catalogId": s.catalog.Catalog.CatalogID,
			"authorizationStatus": s.authorizationStatus(),
		})
	case "/api/v1/mods":
		if s.catalog.Status == "invalid" {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{
				"error": "catalog validation failed", "catalogStatus": s.catalog.Status,
				"catalogError": s.catalog.Error,
			})
			return
		}
		mods := make([]modResponse, 0, len(s.catalog.Catalog.Mods))
		for _, item := range s.catalog.Catalog.Mods {
			mods = append(mods, modResponse{mod: item, Compatible: s.compatible(item)})
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"model": s.model, "firmware": s.firmware, "mods": mods,
			"catalogStatus": s.catalog.Status, "catalogSha256": s.catalog.SHA256,
			"catalogTrust":         map[bool]string{true: "verified", false: "unverified"}[s.catalog.Trusted],
			"catalogKeyId":         s.catalog.KeyID,
			"catalogSchemaVersion": s.catalog.Catalog.SchemaVersion, "catalogId": s.catalog.Catalog.CatalogID,
		})
	case "/api/v1/installed":
		state, err := loadRegistry(filepath.Join(s.root, "registry", "state.json"))
		if err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "installed registry unavailable"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"mods": state.Mods})
	case "/api/v1/transactions":
		records, err := listTransactions(s.root)
		if err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "transaction registry unavailable"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"transactions": records})
	case "/api/v1/libraries":
		writeJSON(w, http.StatusOK, map[string]any{"libraries": []any{}})
	default:
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
	}
}

func validateList(name string, values []string) error {
	if len(values) == 0 || len(values) > 64 {
		return fmt.Errorf("%s must contain 1 to 64 entries", name)
	}
	seen := make(map[string]bool, len(values))
	for _, value := range values {
		if value == "" || len(value) > 100 || strings.TrimSpace(value) != value {
			return fmt.Errorf("invalid %s entry", name)
		}
		if seen[value] {
			return fmt.Errorf("duplicate %s entry %q", name, value)
		}
		seen[value] = true
	}
	return nil
}

func validatePackageFields(id, name, version, downloadURL, digest string, size int64, models, firmware []string) error {
	if !modIDPattern.MatchString(id) {
		return errors.New("invalid id")
	}
	if name == "" || len(name) > 100 || strings.TrimSpace(name) != name {
		return errors.New("invalid name")
	}
	if version == "" || len(version) > 40 || strings.TrimSpace(version) != version {
		return errors.New("invalid version")
	}
	if err := validateList("models", models); err != nil {
		return err
	}
	if err := validateList("firmware", firmware); err != nil {
		return err
	}
	parsed, err := url.Parse(downloadURL)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" {
		return errors.New("downloadUrl must be an absolute HTTPS URL without credentials or fragment")
	}
	if len(digest) != 64 {
		return errors.New("sha256 must contain 64 lowercase hexadecimal characters")
	}
	decoded, err := hex.DecodeString(digest)
	if err != nil || hex.EncodeToString(decoded) != digest {
		return errors.New("sha256 must contain 64 lowercase hexadecimal characters")
	}
	if size < 1 || size > maxPackageBytes {
		return errors.New("size must be between 1 byte and 128 MiB")
	}
	return nil
}

func validateMod(item mod) error {
	if err := validatePackageFields(item.ID, item.Name, item.Version, item.DownloadURL, item.SHA256, item.Size, item.Models, item.Firmware); err != nil {
		return err
	}
	if len(item.Description) > 2000 {
		return errors.New("description exceeds 2000 bytes")
	}
	if item.Architecture != "" || item.ABI != "" || item.Entrypoint != "" || item.Approval != nil || item.Dependencies != nil || item.Libraries != nil || item.Conflicts != nil || item.Revoked {
		return errors.New("schema v2 fields are not allowed in schema v1")
	}
	return nil
}

func validateApproval(value *approval) error {
	if value == nil || value.Maintainer == "" || len(value.Maintainer) > 100 || strings.TrimSpace(value.Maintainer) != value.Maintainer {
		return errors.New("invalid approval maintainer")
	}
	return validateList("approval devices", value.Devices)
}

func validateDependencies(name string, values []dependency) error {
	if len(values) > 256 {
		return fmt.Errorf("%s exceeds 256 entries", name)
	}
	seen := make(map[string]bool, len(values))
	for _, value := range values {
		if !modIDPattern.MatchString(value.ID) || value.Constraint == "" || len(value.Constraint) > 100 || strings.TrimSpace(value.Constraint) != value.Constraint || (value.Type != "" && value.Type != "mod" && value.Type != "library") {
			return fmt.Errorf("invalid %s entry", name)
		}
		key := value.Type + "\x00" + value.ID
		if seen[key] {
			return fmt.Errorf("duplicate %s entry", name)
		}
		seen[key] = true
	}
	return nil
}

func validateV2Mod(item mod) error {
	if err := validatePackageFields(item.ID, item.Name, item.Version, item.DownloadURL, item.SHA256, item.Size, item.Models, item.Firmware); err != nil {
		return err
	}
	if len(item.Description) > 2000 {
		return errors.New("description exceeds 2000 bytes")
	}
	if !versionPattern.MatchString(item.Version) {
		return errors.New("version is not a safe path component")
	}
	if item.Architecture == "" || len(item.Architecture) > 40 || item.ABI == "" || len(item.ABI) > 40 {
		return errors.New("invalid architecture or ABI")
	}
	entrypoint, err := safeArchivePath(item.Entrypoint, defaultStageLimits)
	if err != nil || entrypoint != item.Entrypoint || strings.HasSuffix(item.Entrypoint, "/") {
		return errors.New("invalid entrypoint")
	}
	if err := validateApproval(item.Approval); err != nil {
		return err
	}
	if err := validateDependencies("dependencies", item.Dependencies); err != nil {
		return err
	}
	if err := validateDependencies("libraries", item.Libraries); err != nil {
		return err
	}
	if err := validateListOptional("conflicts", item.Conflicts); err != nil {
		return err
	}
	return nil
}

func validateListOptional(name string, values []string) error {
	if values == nil {
		return nil
	}
	if len(values) > 256 {
		return fmt.Errorf("%s exceeds 256 entries", name)
	}
	seen := make(map[string]bool, len(values))
	for _, value := range values {
		if !modIDPattern.MatchString(value) || seen[value] {
			return fmt.Errorf("invalid or duplicate %s entry", name)
		}
		seen[value] = true
	}
	return nil
}

func validateV2Library(item library) error {
	if item.FirmwareConstraint != "" {
		return errors.New("firmwareConstraint is not supported by this runtime")
	}
	if err := validatePackageFields(item.ID, item.Name, item.Version, item.DownloadURL, item.SHA256, item.Size, item.Models, item.Firmware); err != nil {
		return err
	}
	if !versionPattern.MatchString(item.Version) {
		return errors.New("version is not a safe path component")
	}
	if item.Architecture == "" || len(item.Architecture) > 40 || item.ABI == "" || len(item.ABI) > 40 {
		return errors.New("invalid architecture or ABI")
	}
	if err := validateApproval(item.Approval); err != nil {
		return err
	}
	if err := validateDependencies("dependencies", item.Dependencies); err != nil {
		return err
	}
	return validateListOptional("conflicts", item.Conflicts)
}

func readOwnedFile(path string, limit int64, label string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !info.Mode().IsRegular() || !ok || stat.Uid != uint32(os.Geteuid()) || info.Mode().Perm()&0o022 != 0 {
		return nil, fmt.Errorf("%s must be a service-owned regular file without group or world write access", label)
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("%s cannot be opened", label)
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, fmt.Errorf("%s cannot be read", label)
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("%s exceeds size limit", label)
	}
	return data, nil
}

func decodeStrictJSON(data []byte, target any) error {
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return errors.New("trailing JSON data")
	}
	return nil
}

func parseCatalog(data []byte) catalogState {
	var result catalog
	if err := decodeStrictJSON(data, &result); err != nil {
		return catalogState{Status: "invalid", Error: "invalid catalog JSON: " + err.Error()}
	}
	if result.Mods == nil || len(result.Mods) > 2048 {
		return catalogState{Status: "invalid", Error: "mods must be an array with at most 2048 entries"}
	}
	if result.SchemaVersion != 1 && result.SchemaVersion != 2 {
		return catalogState{Status: "invalid", Error: "only catalog schemaVersion 1 and 2 are supported"}
	}
	var rootFields map[string]json.RawMessage
	if err := json.Unmarshal(data, &rootFields); err != nil {
		return catalogState{Status: "invalid", Error: "invalid catalog object"}
	}
	if _, present := rootFields["authenticity"]; present {
		return catalogState{Status: "invalid", Error: "embedded authenticity is unsupported; use the detached signature"}
	}
	if result.SchemaVersion == 1 && (result.CatalogID != "" || result.Libraries != nil) {
		return catalogState{Status: "invalid", Error: "schema v2 root fields are not allowed in schema v1"}
	}
	if result.SchemaVersion == 1 {
		var raw struct {
			SchemaVersion int               `json:"schemaVersion"`
			Mods          []json.RawMessage `json:"mods"`
		}
		if err := json.Unmarshal(data, &raw); err != nil {
			return catalogState{Status: "invalid", Error: "invalid schema v1 shape"}
		}
		allowed := map[string]bool{"id": true, "name": true, "version": true, "description": true, "models": true, "firmware": true, "downloadUrl": true, "sha256": true, "size": true}
		for index, encoded := range raw.Mods {
			var fields map[string]json.RawMessage
			if json.Unmarshal(encoded, &fields) != nil {
				return catalogState{Status: "invalid", Error: "invalid schema v1 mod"}
			}
			for field := range fields {
				if !allowed[field] {
					return catalogState{Status: "invalid", Error: fmt.Sprintf("mods[%d]: schema v2 field %q is not allowed in schema v1", index, field)}
				}
			}
		}
		for field := range rootFields {
			if field != "schemaVersion" && field != "mods" {
				return catalogState{Status: "invalid", Error: fmt.Sprintf("schema v2 root field %q is not allowed in schema v1", field)}
			}
		}
	}
	if result.SchemaVersion == 2 {
		if !modIDPattern.MatchString(result.CatalogID) {
			return catalogState{Status: "invalid", Error: "invalid catalogId"}
		}
		if result.Libraries == nil || len(result.Libraries) > 2048 {
			return catalogState{Status: "invalid", Error: "libraries must be an array with at most 2048 entries"}
		}
	}
	ids := make(map[string]bool, len(result.Mods))
	for index, item := range result.Mods {
		var err error
		if result.SchemaVersion == 1 {
			err = validateMod(item)
		} else {
			err = validateV2Mod(item)
		}
		if err != nil {
			return catalogState{Status: "invalid", Error: fmt.Sprintf("mods[%d]: %v", index, err)}
		}
		if ids[item.ID] {
			return catalogState{Status: "invalid", Error: fmt.Sprintf("duplicate mod id %q", item.ID)}
		}
		ids[item.ID] = true
	}
	libraryIDs := make(map[string]bool, len(result.Libraries))
	for index, item := range result.Libraries {
		if err := validateV2Library(item); err != nil {
			return catalogState{Status: "invalid", Error: fmt.Sprintf("libraries[%d]: %v", index, err)}
		}
		if libraryIDs[item.ID] {
			return catalogState{Status: "invalid", Error: fmt.Sprintf("duplicate library id %q", item.ID)}
		}
		libraryIDs[item.ID] = true
	}
	digest := sha256.Sum256(data)
	return catalogState{Catalog: result, Status: "valid", SHA256: hex.EncodeToString(digest[:])}
}

func loadCatalog(path string) catalogState {
	data, err := readOwnedFile(path, maxCatalogBytes, "catalog")
	if err != nil {
		if os.IsNotExist(err) {
			return catalogState{Catalog: catalog{SchemaVersion: 1, Mods: []mod{}}, Status: "missing"}
		}
		return catalogState{Status: "invalid", Error: err.Error()}
	}
	return parseCatalog(data)
}

func loadVerifiedCatalog(catalogPath, signaturePath, trustKeyPath string) catalogState {
	catalogData, err := readOwnedFile(catalogPath, maxCatalogBytes, "catalog")
	if err != nil {
		if os.IsNotExist(err) {
			return catalogState{Catalog: catalog{SchemaVersion: 1, Mods: []mod{}}, Status: "missing"}
		}
		return catalogState{Status: "invalid", Error: err.Error()}
	}
	trustData, err := readOwnedFile(trustKeyPath, 4096, "catalog trust key")
	if err != nil {
		return catalogState{Status: "invalid", Error: "catalog trust key unavailable or unsafe"}
	}
	signatureData, err := readOwnedFile(signaturePath, 4096, "catalog signature")
	if err != nil {
		return catalogState{Status: "invalid", Error: "catalog signature unavailable or unsafe"}
	}
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
	if err != nil || len(signatureBytes) != ed25519.SignatureSize ||
		!ed25519.Verify(ed25519.PublicKey(publicKey), catalogData, signatureBytes) {
		return catalogState{Status: "invalid", Error: "catalog signature verification failed"}
	}
	state := parseCatalog(catalogData)
	if state.Status != "valid" {
		return state
	}
	fingerprint := sha256.Sum256(publicKey)
	state.Trusted = true
	state.KeyID = trust.KeyID
	state.KeyFingerprint = hex.EncodeToString(fingerprint[:])
	return state
}

func publicPackageIP(ip net.IP) bool {
	return ip != nil && !ip.IsUnspecified() && !ip.IsLoopback() && !ip.IsPrivate() &&
		!ip.IsLinkLocalUnicast() && !ip.IsLinkLocalMulticast() && !ip.IsMulticast()
}

func newPackageHTTPClient() *http.Client {
	dialer := &net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}
	transport := &http.Transport{
		Proxy:               nil,
		DisableCompression:  true,
		MaxIdleConns:        2,
		IdleConnTimeout:     30 * time.Second,
		TLSHandshakeTimeout: 15 * time.Second,
		TLSClientConfig:     &tls.Config{MinVersion: tls.VersionTLS12},
	}
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, errors.New("invalid package address")
		}
		addresses, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil || len(addresses) == 0 {
			return nil, errors.New("package host resolution failed")
		}
		for _, resolved := range addresses {
			if !publicPackageIP(resolved.IP) {
				return nil, errors.New("package host resolved to a non-public address")
			}
		}
		return dialer.DialContext(ctx, network, net.JoinHostPort(addresses[0].IP.String(), port))
	}
	return &http.Client{
		Transport: transport,
		Timeout:   10 * time.Minute,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func verifyCachedPackage(path string, item mod) bool {
	info, err := os.Lstat(path)
	if err != nil || validateOwnedRegularFile(info, 0o600) != nil || info.Size() != item.Size {
		return false
	}
	file, err := os.Open(path)
	if err != nil {
		return false
	}
	digest := sha256.New()
	_, copyErr := io.Copy(digest, file)
	closeErr := file.Close()
	return copyErr == nil && closeErr == nil && hex.EncodeToString(digest.Sum(nil)) == item.SHA256
}

func downloadPackage(ctx context.Context, client *http.Client, item mod, cacheRoot string) (cacheResult, error) {
	result := cacheResult{ID: item.ID, Version: item.Version, SHA256: item.SHA256, Size: item.Size}
	if client == nil {
		return result, errors.New("package HTTP client unavailable")
	}
	if err := os.MkdirAll(cacheRoot, 0o700); err != nil {
		return result, err
	}
	directoryInfo, err := os.Lstat(cacheRoot)
	if err != nil || !directoryInfo.IsDir() || directoryInfo.Mode()&os.ModeSymlink != 0 || directoryInfo.Mode().Perm() != 0o700 {
		return result, errors.New("package cache directory must be a real directory with mode 0700")
	}
	directoryStat, ok := directoryInfo.Sys().(*syscall.Stat_t)
	if !ok || directoryStat.Uid != uint32(os.Geteuid()) {
		return result, errors.New("package cache directory is not owned by the service user")
	}
	finalPath := filepath.Join(cacheRoot, item.SHA256+".zip")
	if verifyCachedPackage(finalPath, item) {
		result.Cached = true
		return result, nil
	}
	if info, err := os.Lstat(finalPath); err == nil {
		if !info.Mode().IsRegular() {
			return result, errors.New("invalid existing package cache path")
		}
		if err := os.Remove(finalPath); err != nil {
			return result, err
		}
	} else if !os.IsNotExist(err) {
		return result, err
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, item.DownloadURL, nil)
	if err != nil {
		return result, err
	}
	request.Header.Set("Accept", "application/zip, application/octet-stream")
	request.Header.Set("User-Agent", "Prime-Unlimited-Runtime/1")
	response, err := client.Do(request)
	if err != nil {
		return result, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return result, fmt.Errorf("unexpected package HTTP status %d", response.StatusCode)
	}
	if response.ContentLength >= 0 && response.ContentLength != item.Size {
		return result, errors.New("package Content-Length mismatch")
	}
	temporary, err := os.CreateTemp(cacheRoot, ".download-*")
	if err != nil {
		return result, err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return result, err
	}
	digest := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(temporary, digest), io.LimitReader(response.Body, item.Size+1))
	if copyErr != nil {
		temporary.Close()
		return result, copyErr
	}
	if written != item.Size {
		temporary.Close()
		return result, errors.New("downloaded package size mismatch")
	}
	if hex.EncodeToString(digest.Sum(nil)) != item.SHA256 {
		temporary.Close()
		return result, errors.New("downloaded package SHA-256 mismatch")
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return result, err
	}
	if err := temporary.Close(); err != nil {
		return result, err
	}
	if err := os.Rename(temporaryPath, finalPath); err != nil {
		return result, err
	}
	directory, err := os.Open(cacheRoot)
	if err != nil {
		return result, err
	}
	if err := directory.Sync(); err != nil {
		directory.Close()
		return result, err
	}
	if err := directory.Close(); err != nil {
		return result, err
	}
	result.Cached = true
	return result, nil
}

func validListenAddress(address string) bool {
	host, port, err := net.SplitHostPort(address)
	if err != nil || port == "" {
		return false
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil || portNumber < 1 || portNumber > 65535 {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.Equal(net.ParseIP("127.0.0.1"))
}

func validateOwnedRegularFile(info os.FileInfo, mode os.FileMode) error {
	if !info.Mode().IsRegular() {
		return errors.New("path is not a regular file")
	}
	if info.Mode().Perm() != mode {
		return fmt.Errorf("path permissions must be %04o", mode)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != uint32(os.Geteuid()) {
		return errors.New("path is not owned by the service user")
	}
	return nil
}

func readAuthToken(path string) (string, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if err := validateOwnedRegularFile(info, 0o600); err != nil {
		return "", err
	}
	if info.Size() < 64 || info.Size() > 65 {
		return "", errors.New("authorization token has invalid size")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	token := strings.TrimSuffix(string(data), "\n")
	decoded, err := hex.DecodeString(token)
	if err != nil || len(decoded) != 32 || hex.EncodeToString(decoded) != token {
		return "", errors.New("authorization token must be 64 lowercase hexadecimal characters")
	}
	return token, nil
}

func loadOrCreateAuthToken(path string) (string, error) {
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return "", err
	}
	directoryInfo, err := os.Lstat(directory)
	if err != nil {
		return "", err
	}
	if !directoryInfo.IsDir() || directoryInfo.Mode()&os.ModeSymlink != 0 || directoryInfo.Mode().Perm() != 0o700 {
		return "", errors.New("authorization directory must be a real directory with mode 0700")
	}
	directoryStat, ok := directoryInfo.Sys().(*syscall.Stat_t)
	if !ok || directoryStat.Uid != uint32(os.Geteuid()) {
		return "", errors.New("authorization directory is not owned by the service user")
	}

	token, err := readAuthToken(path)
	if err == nil {
		return token, nil
	}
	if !os.IsNotExist(err) {
		return "", err
	}
	random := make([]byte, 32)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	generated := hex.EncodeToString(random)
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if os.IsExist(err) {
			return readAuthToken(path)
		}
		return "", err
	}
	if _, err := file.WriteString(generated + "\n"); err != nil {
		file.Close()
		return "", err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return "", err
	}
	if err := file.Close(); err != nil {
		return "", err
	}
	return readAuthToken(path)
}

func main() {
	listen := flag.String("listen", "127.0.0.1:8098", "loopback listen address")
	model := flag.String("model", "JC11S", "device model")
	firmware := flag.String("firmware", "5.3.0", "Engine OS version")
	architecture := flag.String("architecture", "armv7l", "runtime architecture identity")
	abi := flag.String("abi", "gnueabihf", "runtime ABI identity")
	catalogPath := flag.String("catalog", "/data/prime-unlimited/catalog/catalog.json", "catalog file")
	catalogSignaturePath := flag.String("catalog-signature", "/data/prime-unlimited/catalog/catalog.sig.json", "detached catalog signature")
	catalogTrustKeyPath := flag.String("catalog-trust-key", "/data/prime-unlimited/trust/catalog-key.json", "deployment-owned catalog trust key")
	authTokenPath := flag.String("auth-token", "/data/prime-unlimited/auth/mutation.token", "device-owned mutation token")
	cacheRoot := flag.String("cache-root", "/data/prime-unlimited/cache/packages", "verified package cache")
	stagingRoot := flag.String("staging-root", "/data/prime-unlimited/staging/packages", "non-executable verified package staging")
	root := flag.String("root", "/data/prime-unlimited", "persistent /data-only runtime root")
	flag.Parse()
	if !validListenAddress(*listen) {
		log.Fatal("refusing non-loopback listen address")
	}
	if filepath.Clean(*root) != "/data/prime-unlimited" || !filepath.IsAbs(*root) {
		log.Fatal("refusing runtime root outside /data/prime-unlimited")
	}
	if err := recoverInstall(*root); err != nil {
		log.Fatalf("install recovery failed: %v", err)
	}
	state := loadVerifiedCatalog(*catalogPath, *catalogSignaturePath, *catalogTrustKeyPath)
	if state.Status == "invalid" {
		log.Printf("catalog rejected: %s", state.Error)
	}
	authToken, err := loadOrCreateAuthToken(*authTokenPath)
	if err != nil {
		log.Fatalf("authorization setup failed: %v", err)
	}
	httpServer := &http.Server{
		Addr: *listen,
		Handler: &server{model: *model, firmware: *firmware, architecture: *architecture, abi: *abi, catalog: state, authToken: authToken, root: *root,
			cacheRoot: *cacheRoot, stagingRoot: *stagingRoot, httpClient: newPackageHTTPClient()},
		ReadHeaderTimeout: 3 * time.Second,
		ReadTimeout:       5 * time.Second,
		WriteTimeout:      10 * time.Minute,
		IdleTimeout:       15 * time.Second,
		MaxHeaderBytes:    16 * 1024,
	}
	log.Printf("Prime Unlimited runtime API listening on http://%s (catalog: %s)", *listen, state.Status)
	log.Fatal(httpServer.ListenAndServe())
}
