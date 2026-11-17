package main

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const maxCatalogBytes = 1024 * 1024

var modIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

type catalog struct {
	SchemaVersion int   `json:"schemaVersion"`
	Mods          []mod `json:"mods"`
}

type mod struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Version     string   `json:"version"`
	Description string   `json:"description,omitempty"`
	Models      []string `json:"models"`
	Firmware    []string `json:"firmware"`
	DownloadURL string   `json:"downloadUrl"`
	SHA256      string   `json:"sha256"`
}

type modResponse struct {
	mod
	Compatible bool `json:"compatible"`
}

type catalogState struct {
	Catalog catalog
	Status  string
	Error   string
	SHA256  string
}

type server struct {
	model     string
	firmware  string
	catalog   catalogState
	authToken string
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
	return contains(item.Models, s.model) && contains(item.Firmware, s.firmware)
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
	case "/api/v1/install", "/api/v1/enable", "/api/v1/disable", "/api/v1/uninstall", "/api/v1/rollback":
		return true
	default:
		return false
	}
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
		writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "mutation engine unavailable"})
		return
	}
	switch r.URL.Path {
	case "/api/v1/health":
		writeJSON(w, http.StatusOK, map[string]any{
			"service": "prime-unlimited-mod-loader", "mode": "authenticated-read-only",
			"catalogStatus": s.catalog.Status, "catalogError": s.catalog.Error,
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
		})
	case "/api/v1/installed":
		writeJSON(w, http.StatusOK, map[string]any{"mods": map[string]any{}})
	case "/api/v1/transactions":
		writeJSON(w, http.StatusOK, map[string]any{"transactions": []any{}})
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

func validateMod(item mod) error {
	if !modIDPattern.MatchString(item.ID) {
		return errors.New("invalid id")
	}
	if item.Name == "" || len(item.Name) > 100 || strings.TrimSpace(item.Name) != item.Name {
		return errors.New("invalid name")
	}
	if item.Version == "" || len(item.Version) > 40 || strings.TrimSpace(item.Version) != item.Version {
		return errors.New("invalid version")
	}
	if len(item.Description) > 2000 {
		return errors.New("description exceeds 2000 bytes")
	}
	if err := validateList("models", item.Models); err != nil {
		return err
	}
	if err := validateList("firmware", item.Firmware); err != nil {
		return err
	}
	parsed, err := url.Parse(item.DownloadURL)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" {
		return errors.New("downloadUrl must be an absolute HTTPS URL without credentials or fragment")
	}
	if len(item.SHA256) != 64 {
		return errors.New("sha256 must contain 64 lowercase hexadecimal characters")
	}
	decoded, err := hex.DecodeString(item.SHA256)
	if err != nil || hex.EncodeToString(decoded) != item.SHA256 {
		return errors.New("sha256 must contain 64 lowercase hexadecimal characters")
	}
	return nil
}

func loadCatalog(path string) catalogState {
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return catalogState{Catalog: catalog{SchemaVersion: 1, Mods: []mod{}}, Status: "missing"}
		}
		return catalogState{Status: "invalid", Error: "catalog cannot be opened"}
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxCatalogBytes+1))
	if err != nil {
		return catalogState{Status: "invalid", Error: "catalog cannot be read"}
	}
	if len(data) > maxCatalogBytes {
		return catalogState{Status: "invalid", Error: "catalog exceeds 1 MiB"}
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	var result catalog
	if err := decoder.Decode(&result); err != nil {
		return catalogState{Status: "invalid", Error: "invalid catalog JSON: " + err.Error()}
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return catalogState{Status: "invalid", Error: "catalog contains trailing JSON data"}
	}
	if result.SchemaVersion != 1 {
		return catalogState{Status: "invalid", Error: "only catalog schemaVersion 1 is supported"}
	}
	if result.Mods == nil || len(result.Mods) > 2048 {
		return catalogState{Status: "invalid", Error: "mods must be an array with at most 2048 entries"}
	}
	ids := make(map[string]bool, len(result.Mods))
	for index, item := range result.Mods {
		if err := validateMod(item); err != nil {
			return catalogState{Status: "invalid", Error: fmt.Sprintf("mods[%d]: %v", index, err)}
		}
		if ids[item.ID] {
			return catalogState{Status: "invalid", Error: fmt.Sprintf("duplicate mod id %q", item.ID)}
		}
		ids[item.ID] = true
	}
	digest := sha256.Sum256(data)
	return catalogState{Catalog: result, Status: "valid", SHA256: hex.EncodeToString(digest[:])}
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
	catalogPath := flag.String("catalog", "/data/prime-unlimited/catalog/catalog.json", "catalog file")
	authTokenPath := flag.String("auth-token", "/data/prime-unlimited/auth/mutation.token", "device-owned mutation token")
	flag.Parse()
	if !validListenAddress(*listen) {
		log.Fatal("refusing non-loopback listen address")
	}
	state := loadCatalog(*catalogPath)
	if state.Status == "invalid" {
		log.Printf("catalog rejected: %s", state.Error)
	}
	authToken, err := loadOrCreateAuthToken(*authTokenPath)
	if err != nil {
		log.Fatalf("authorization setup failed: %v", err)
	}
	httpServer := &http.Server{
		Addr:              *listen,
		Handler:           &server{model: *model, firmware: *firmware, catalog: state, authToken: authToken},
		ReadHeaderTimeout: 3 * time.Second,
		ReadTimeout:       5 * time.Second,
		WriteTimeout:      5 * time.Second,
		IdleTimeout:       15 * time.Second,
		MaxHeaderBytes:    16 * 1024,
	}
	log.Printf("Prime Unlimited runtime API listening on http://%s (catalog: %s)", *listen, state.Status)
	log.Fatal(httpServer.ListenAndServe())
}
