package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"image/png"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
)

// A signed service declaration names one native executable and one scoped
// capability. It cannot supply shell commands, unit properties, users or paths.
type serviceSpec struct {
	Binary     string `json:"binary"`
	SHA256     string `json:"sha256"`
	Capability string `json:"capability"`
}

type serviceCredentials struct {
	TLS         serviceTLS `json:"tls"`
	BrokerURL   string     `json:"brokerUrl"`
	BrokerToken string     `json:"brokerToken"`
	WebToken    string     `json:"webToken"`
}

type serviceRecord struct {
	ID            string             `json:"id"`
	PackageSHA256 string             `json:"packageSha256"`
	Credentials   serviceCredentials `json:"credentials"`
}

var serviceProvisioningAllowed = func() bool { return os.Geteuid() == 0 }

var serviceRunBase = "/run/prime-unlimited-mod-services"
var serviceCommand = func(name string, args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	// Credential contents never appear in arguments or logs.
	output, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %w: %.1024s", name, err, output)
	}
	return nil
}

func validateServiceSpec(spec *serviceSpec) error {
	if spec == nil {
		return nil
	}
	path, err := safeArchivePath(spec.Binary, defaultStageLimits)
	digest, decodeErr := hex.DecodeString(spec.SHA256)
	if err != nil || path != spec.Binary || len(digest) != 32 || decodeErr != nil || strings.ToLower(spec.SHA256) != spec.SHA256 || spec.Capability != "splash-image" {
		return errors.New("invalid native service declaration")
	}
	return nil
}

func serviceName(id string) string {
	digest := sha256.Sum256([]byte(id))
	return "pu-mod-" + hex.EncodeToString(digest[:12])
}

func serviceRecordPath(root, id string) string {
	return filepath.Join(root, "services", serviceName(id)+".json")
}

func loadServiceRecord(root, id string) (serviceRecord, error) {
	var record serviceRecord
	data, err := readOwnedFile(serviceRecordPath(root, id), 8192, "service credentials")
	if err != nil {
		return record, err
	}
	if decodeStrictJSON(data, &record) != nil || record.ID != id {
		return record, errors.New("invalid service record")
	}
	return record, nil
}

// Verify every path component before reading. A mutable package never becomes
// an executable through symlinks, writable directories or a digest mismatch.
func verifiedServiceBinary(root string, item installedMod) ([]byte, error) {
	if err := validateServiceSpec(item.Service); err != nil || item.Service == nil {
		return nil, errors.New("invalid service")
	}
	path := filepath.Join(root, filepath.FromSlash(item.Root), filepath.FromSlash(item.Service.Binary))
	relative, err := filepath.Rel(root, path)
	if err != nil || strings.HasPrefix(relative, "..") {
		return nil, errors.New("unsafe service binary")
	}
	current := root
	for _, part := range strings.Split(filepath.Dir(relative), string(filepath.Separator)) {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0022 != 0 {
			return nil, errors.New("unsafe service parent")
		}
	}
	data, err := readOwnedFile(path, 16<<20, "service binary")
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(data)
	if hex.EncodeToString(digest[:]) != item.Service.SHA256 {
		return nil, errors.New("service binary digest mismatch")
	}
	class, machine := byte(1), byte(40)
	if item.Architecture == "aarch64" {
		class, machine = 2, 183
	} else if item.Architecture != "armv7l" {
		return nil, errors.New("unsupported service architecture")
	}
	if len(data) < 64 || !bytes.Equal(data[:4], []byte{0x7f, 'E', 'L', 'F'}) || data[4] != class || data[5] != 1 || data[18] != machine || data[19] != 0 {
		return nil, errors.New("service ELF target mismatch")
	}
	return data, nil
}

func stopManagedService(root, id string) error {
	if !modIDPattern.MatchString(id) {
		return errors.New("invalid service id")
	}
	_, err := loadServiceRecord(root, id)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	// Stop is idempotent: an absent transient unit is already stopped. Other
	// failures must not be mistaken for a successful stop.
	unit := serviceName(id) + ".service"
	if err := serviceCommand("systemctl", "stop", unit); err != nil {
		// Explicit state handling is done by serviceStopped, not exit-code guessing.
		if !serviceStopped(unit) {
			return err
		}
	}
	_ = serviceCommand("systemctl", "reset-failed", unit)
	if err := os.Remove(serviceRecordPath(root, id)); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.Remove(filepath.Join(root, "services", serviceName(id)+".stdin.json")); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.RemoveAll(filepath.Join(serviceRunBase, serviceName(id))); err != nil {
		return err
	}
	return nil
}

var serviceStopped = func(unit string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	data, err := exec.CommandContext(ctx, "systemctl", "show", unit, "--property=ActiveState", "--property=LoadState").Output()
	state := string(data)
	return (err == nil && (strings.Contains(state, "ActiveState=inactive\n") || strings.Contains(state, "ActiveState=failed\n"))) || strings.Contains(state, "LoadState=not-found\n")
}

func startManagedService(root string, item installedMod) error {
	binary, err := verifiedServiceBinary(root, item)
	if err != nil {
		return err
	}
	old, err := loadServiceRecord(root, item.ID)
	if err == nil && old.PackageSHA256 == item.PackageSHA256 {
		if serviceCommand("systemctl", "is-active", "--quiet", serviceName(item.ID)+".service") == nil {
			return nil
		}
		if !serviceCleanlyStopped(serviceName(item.ID) + ".service") {
			return errors.New("managed service exited unexpectedly")
		}

	}
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if err == nil {
		if err := stopManagedService(root, item.ID); err != nil {
			return err
		}
	}
	if !serviceProvisioningAllowed() {
		return errors.New("service provisioning requires the trusted root runtime")
	}
	if err := ensurePrivateDirectory(filepath.Join(root, "services")); err != nil {
		return err
	}
	// Public traversal only for root-owned immutable executable copies. All
	// credentials stay in the private runtime root and are passed on stdin.
	if err := ensureTraverseDirectory(serviceRunBase); err != nil {
		return err
	}
	dir := filepath.Join(serviceRunBase, serviceName(item.ID))
	if err := ensureTraverseDirectory(dir); err != nil {
		return err
	}
	executable := filepath.Join(dir, "server")
	if info, err := os.Lstat(executable); err == nil && !info.Mode().IsRegular() {
		return errors.New("unsafe service executable")
	}
	if err := os.WriteFile(executable, binary, 0700); err != nil {
		return err
	}
	if err := os.Chmod(executable, 0755); err != nil {
		return err
	}
	broker, err := newServiceToken()
	if err != nil {
		return err
	}
	web, err := newServiceToken()
	if err != nil {
		return err
	}
	tlsConfig, err := loadServiceTLS(root, item.ID)
	if err != nil {
		return err
	}
	credentials := serviceCredentials{BrokerURL: "http://127.0.0.1:8098/api/v1/service-splash", BrokerToken: broker, WebToken: web, TLS: tlsConfig}
	record := serviceRecord{ID: item.ID, PackageSHA256: item.PackageSHA256, Credentials: credentials}
	if err := writeAtomicJSON(serviceRecordPath(root, item.ID), record); err != nil {
		return err
	}
	stdin := filepath.Join(root, "services", serviceName(item.ID)+".stdin.json")
	if err := writeAtomicJSON(stdin, credentials); err != nil {
		return err
	}
	args := []string{"--quiet", "--unit=" + serviceName(item.ID),
		"--property=Type=exec", "--property=DynamicUser=yes",
		"--property=StandardInput=file:" + stdin,
		"--property=ProtectSystem=strict", "--property=ProtectHome=yes",
		"--property=PrivateTmp=yes", "--property=PrivateDevices=yes",
		"--property=NoNewPrivileges=yes", "--property=RestrictSUIDSGID=yes",
		"--property=CapabilityBoundingSet=", "--property=LockPersonality=yes",
		"--property=RestrictAddressFamilies=AF_INET AF_UNIX",

		"--property=MemoryMax=128M", "--property=TasksMax=32",
		"--property=KillMode=control-group", "--property=TimeoutStopSec=10s",
		"--property=BindsTo=prime-unlimited-modloader-api.service",
		"--property=After=prime-unlimited-modloader-api.service",
		executable, "--managed"}
	if err := serviceCommand("systemd-run", args...); err != nil {
		// Keep the record so the reconciler can stop a partially started unit.
		return err
	}
	return nil
}

func reconcileServices(root string) error { return reconcileServicesPolicy(root, nil) }

func reconcileServicesPolicy(root string, approve func(installedMod) bool) error {
	state, err := loadRegistry(filepath.Join(root, "registry", "state.json"))
	if err != nil {
		return err
	}
	safety, err := loadSafetyState(root)
	if err != nil {
		return err
	}
	ids := make([]string, 0)
	for id, item := range state.Mods {
		if item.Service != nil {
			ids = append(ids, id)
		}
	}
	// Include orphan records (e.g. interrupted uninstall), never unknown unit names.
	entries, err := os.ReadDir(filepath.Join(root, "services"))
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".json") || entry.Name() == "tls.json" || strings.HasSuffix(entry.Name(), ".stdin.json") {
			continue
		}
		data, err := readOwnedFile(filepath.Join(root, "services", entry.Name()), 8192, "service record")
		if err != nil {
			return err
		}
		var record serviceRecord
		if decodeStrictJSON(data, &record) != nil || !modIDPattern.MatchString(record.ID) || entry.Name() != serviceName(record.ID)+".json" {
			return errors.New("invalid service record")
		}
		if item, ok := state.Mods[record.ID]; !ok || item.Service == nil {
			if err := stopManagedService(root, record.ID); err != nil {
				return err
			}
		}
	}
	sort.Strings(ids)
	// Exactly one writer for each exclusive capability; never race two splash servers.
	owner := ""
	for _, id := range ids {
		item := state.Mods[id]
		if !item.Enabled || safety.SafeMode || safety.Mods[id].Quarantined || (approve != nil && !approve(item)) {
			if err := stopManagedService(root, id); err != nil {
				return err
			}
			continue
		}
		if owner != "" {
			return errors.New("exclusive service capability conflict")
		}
		owner = id
	}
	for _, id := range ids {
		item := state.Mods[id]
		if item.Enabled && !safety.SafeMode && !safety.Mods[id].Quarantined && (approve == nil || approve(item)) {
			if err := startManagedService(root, item); err != nil {
				stopErr := stopManagedService(root, id)
				failure := safety.Mods[id]
				if failure.FailureCount < failureQuarantineThreshold {
					failure.FailureCount++
				}
				failure.Quarantined = failure.FailureCount >= failureQuarantineThreshold
				failure.LastFailure = time.Now().Unix()
				safety.Mods[id] = failure
				if writeErr := writeSafetyState(root, safety); writeErr != nil {
					return writeErr
				}
				if stopErr != nil {
					return stopErr
				}
				return err
			}
		}
	}
	return nil
}

func (s *server) handleServiceSplash(w http.ResponseWriter, r *http.Request) {
	if r.URL.RawQuery != "" || (r.Method != http.MethodGet && r.Method != http.MethodPost) {
		http.Error(w, "unsupported request", 400)
		return
	}
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	state, err := loadRegistry(filepath.Join(s.root, "registry", "state.json"))
	if err != nil {
		http.Error(w, "unavailable", 503)
		return
	}
	safety, err := loadSafetyState(s.root)
	if err != nil || safety.SafeMode {
		http.Error(w, "blocked", 403)
		return
	}
	headers := r.Header.Values("Authorization")
	if len(headers) != 1 || !strings.HasPrefix(headers[0], "Bearer ") {
		http.Error(w, "unauthorized", 401)
		return
	}
	provided := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	authorized := false
	for id, item := range state.Mods {
		if !item.Enabled || item.Service == nil || item.Service.Capability != "splash-image" || safety.Mods[id].Quarantined {
			continue
		}
		if !s.serviceApproved(item) {
			continue
		}
		record, err := loadServiceRecord(s.root, id)
		if err == nil && record.PackageSHA256 == item.PackageSHA256 && len(provided) >= 32 && subtle.ConstantTimeCompare([]byte(provided), []byte(record.Credentials.BrokerToken)) == 1 {
			authorized = true
			break
		}
	}
	if !authorized {
		http.Error(w, "unauthorized", 401)
		return
	}
	target := filepath.Join(s.root, "ui", "splash", "SplashScreen.png")
	if r.Method == http.MethodGet {
		data, err := os.ReadFile(target)
		if os.IsNotExist(err) {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			http.Error(w, "unavailable", 503)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(data)
		return
	}
	if r.Header.Get("Content-Type") != "image/png" {
		http.Error(w, "PNG required", 415)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 10<<20)
	data, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "upload exceeds 10 MiB", 413)
		return
	}
	config, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil || config.Width <= 0 || config.Height <= 0 || int64(config.Width)*int64(config.Height) > 16000000 {
		http.Error(w, "invalid PNG dimensions", 400)
		return
	}
	if _, err := png.Decode(bytes.NewReader(data)); err != nil {
		http.Error(w, "invalid PNG", 400)
		return
	}
	// Reject symlinked parents; the service never controls these directories.
	for _, dir := range []string{filepath.Join(s.root, "ui"), filepath.Join(s.root, "ui", "splash")} {
		if err := os.Mkdir(dir, 0755); err != nil && !os.IsExist(err) {
			http.Error(w, "write failed", 500)
			return
		}
		info, err := os.Lstat(dir)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0022 != 0 {
			http.Error(w, "unsafe splash directory", 500)
			return
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || stat.Uid != uint32(os.Geteuid()) {
			http.Error(w, "unsafe splash directory owner", 500)
			return
		}
	}
	file, err := os.CreateTemp(filepath.Dir(target), ".upload-*.png")
	if err != nil {
		http.Error(w, "write failed", 500)
		return
	}
	defer os.Remove(file.Name())
	if _, err = file.Write(data); err == nil {
		err = file.Chmod(0644)
	}
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(file.Name(), target)
	}
	if err != nil {
		http.Error(w, "write failed", 500)
		return
	}
	_, _ = io.WriteString(w, "Splash image saved")
}

func newServiceToken() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}

func (s *server) serviceApproved(item installedMod) bool {
	published, ok := s.catalogMod(item.ID)
	return ok && !published.Revoked && published.Version == item.Version && published.SHA256 == item.PackageSHA256 && published.Service != nil && item.Service != nil && *published.Service == *item.Service
}

// A normal runtime restart/reboot is not counted as a mod crash. Failed
// transient units are not collected automatically, so their Result survives.
var serviceCleanlyStopped = func(unit string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	data, err := exec.CommandContext(ctx, "systemctl", "show", unit, "--property=ActiveState", "--property=Result", "--property=LoadState").Output()
	value := string(data)
	return strings.Contains(value, "LoadState=not-found\n") || (err == nil && strings.Contains(value, "ActiveState=inactive\n") && strings.Contains(value, "Result=success\n"))
}
