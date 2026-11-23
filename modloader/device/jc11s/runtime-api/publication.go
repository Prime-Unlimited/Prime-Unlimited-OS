package main

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
)

var errRedirectBlocked = errors.New("HTTP redirect blocked")

var assetPathPattern = regexp.MustCompile(`^/repos/Prime-Unlimited/Prime-Unlimited-OS/releases/assets/[0-9]+$`)
var legacyDownloadPattern = regexp.MustCompile(`^/Prime-Unlimited/Prime-Unlimited-OS/releases/download/[A-Za-z0-9._+-]+/[A-Za-z0-9._+-]+$`)

func classifyPackageURL(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" || parsed.RawQuery != "" {
		return errors.New("download URL is not supported")
	}
	host := parsed.Hostname()
	if host == "127.0.0.1" || host == "::1" {
		return nil
	}
	if host == "api.github.com" && assetPathPattern.MatchString(parsed.EscapedPath()) {
		return nil
	}
	if host == "github.com" && legacyDownloadPattern.MatchString(parsed.EscapedPath()) {
		return nil
	}
	return errors.New("download URL is not supported")
}

func allowPackageRedirect(request *http.Request, via []*http.Request) error {
	if len(via) != 1 || request.URL == nil || request.URL.Scheme != "https" || request.URL.User != nil || request.URL.Hostname() != "release-assets.githubusercontent.com" {
		return http.ErrUseLastResponse
	}
	origin := via[0].URL
	if origin == nil || origin.Scheme != "https" || origin.User != nil || origin.RawQuery != "" {
		return http.ErrUseLastResponse
	}
	if origin.Hostname() == "api.github.com" && assetPathPattern.MatchString(origin.EscapedPath()) {
		return nil
	}
	if origin.Hostname() == "github.com" && legacyDownloadPattern.MatchString(origin.EscapedPath()) {
		return nil
	}
	return http.ErrUseLastResponse
}

func (s *server) developerEnabled() bool {
	if s.developerModePath == "" {
		return false
	}
	data, err := readOwnedFile(s.developerModePath, 16, "developer mode")
	return err == nil && string(data) == "enabled\n"
}

func (s *server) handleDeveloperQR(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(githubDeviceQR)
}

func writeEnabledFile(path string) error {
	directory := filepath.Dir(path)
	info, err := os.Lstat(directory)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0o700 {
		return errors.New("developer mode directory is unsafe")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != uint32(os.Geteuid()) {
		return errors.New("developer mode directory is unsafe")
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return err
	}
	if _, err = file.WriteString("enabled\n"); err == nil {
		err = file.Chmod(0o600)
	}
	if err == nil {
		err = file.Sync()
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	return err
}

func (s *server) setDeveloperEnabled(enabled bool) error {
	if s.developerModePath == "" {
		return errors.New("developer mode directory is unsafe")
	}
	if enabled {
		return writeEnabledFile(s.developerModePath)
	}
	info, err := os.Lstat(s.developerModePath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("developer mode file is unsafe")
	}
	return os.Remove(s.developerModePath)
}

func (s *server) handleDeveloperMode(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Content-Type must be application/json"})
		return
	}
	data, err := io.ReadAll(io.LimitReader(r.Body, 129))
	if err != nil || len(data) > 128 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "request body exceeds 128 bytes"})
		return
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	var body struct {
		Enabled bool `json:"enabled"`
	}
	if err := decoder.Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request JSON"})
		return
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "request contains trailing JSON data"})
		return
	}
	if !body.Enabled {
		s.cancelDeveloperLogin()
		if err := s.setDeveloperEnabled(false); err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "developer mode update failed"})
			return
		}
		writeJSON(w, http.StatusOK, s.developerModeStatus())
		return
	}
	if s.developerEnabled() {
		writeJSON(w, http.StatusOK, s.developerModeStatus())
		return
	}
	status, payload := s.beginDeveloperLogin()
	writeJSON(w, status, payload)
}

func (s *server) accessReason(item mod, schemaVersion int) string {
	if item.publicationStatus() == "development" && !s.developerEnabled() {
		return "mod is installable only in developer mode"
	}
	if !contains(item.Models, s.model) {
		return "mod is not compatible with this device"
	}
	if item.Revoked {
		return "mod is revoked"
	}
	if !contains(item.Firmware, s.firmware) {
		return "firmware is not supported"
	}
	if schemaVersion == 2 {
		if item.Architecture != s.architecture || item.ABI != s.abi {
			return "architecture is not compatible"
		}
		if item.Approval == nil || !contains(item.Approval.Devices, s.model) {
			return "mod is not approved for this device"
		}
	}
	return ""
}

func (s *server) requireInstallable(w http.ResponseWriter, id string) (mod, bool) {
	s.catalogMu.RLock()
	status := s.catalog.Status
	trusted := s.catalog.Trusted
	catalogError := s.catalog.Error
	schemaVersion := s.catalog.Catalog.SchemaVersion
	s.catalogMu.RUnlock()
	if status != "valid" {
		message := catalogError
		if message == "" {
			message = "catalog signature verification failed"
		}
		writeJSON(w, http.StatusConflict, map[string]string{"error": message})
		return mod{}, false
	}
	if !trusted {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "compatible catalog mod not found"})
		return mod{}, false
	}
	found, ok := s.lookupMod(id)
	if !ok && !s.developerEnabled() {
		// Preserve the explicit authorization error for a known development
		// release without exposing it in the normal catalog view.
		found, ok = s.lookupCatalogCandidate(id, true)
	}
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "compatible catalog mod not found"})
		return mod{}, false
	}
	if reason := s.accessReason(found, schemaVersion); reason != "" {
		code := http.StatusConflict
		if reason == "mod is installable only in developer mode" {
			code = http.StatusForbidden
		}
		writeJSON(w, code, map[string]string{"error": reason})
		return mod{}, false
	}
	return found, true
}

func publicInstallError(err error, fallback string) string {
	if err == nil {
		return fallback
	}
	if errors.Is(err, errRedirectBlocked) {
		return errRedirectBlocked.Error()
	}
	message := err.Error()
	switch message {
	case "download URL is not supported",
		"HTTP redirect blocked",
		"release asset unreachable",
		"package size mismatch",
		"package SHA-256 mismatch",
		"mod is installable only in developer mode",
		"firmware is not supported",
		"architecture is not compatible",
		"mod is not compatible with this device",
		"mod is revoked",
		"mod is not approved for this device",
		"catalog signature verification failed",
		"unsafe or duplicate ZIP path",
		"unsupported ZIP entry encoding or encryption",
		"ZIP contains a special entry",
		"ZIP contains a non-regular entry",
		"ZIP entry failed integrity validation",
		"archive contains too many entries",
		"extracted size limit exceeded",
		"file and parent path conflict",
		"file and child path conflict",
		"declared entrypoint is missing or unsafe",
		"entrypoint is missing or unsafe",
		"service binary digest mismatch",
		"unsupported service architecture",
		"service ELF target mismatch",
		"verified cached package unavailable",
		"ZIP is invalid",
		"native service file is invalid":
		return message
	}
	if strings.HasPrefix(message, "zip:") {
		return "ZIP is invalid"
	}
	switch message {
	case "invalid service", "invalid service record", "unsafe service binary", "unsafe service parent":
		return "native service file is invalid"
	}
	return fallback
}
