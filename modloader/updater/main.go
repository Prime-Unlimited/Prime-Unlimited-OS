package main

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const maxManifest = 1024 * 1024
const maxBundle = 64 * 1024 * 1024

type versionState struct {
	Version  string `json:"version"`
	Revision int    `json:"revision"`
}
type artifact struct {
	Model, Architecture, ABI, URL, SHA256 string
	Size                                  int64
}

func (a *artifact) UnmarshalJSON(data []byte) error {
	var v struct {
		Model        string `json:"model"`
		Architecture string `json:"architecture"`
		ABI          string `json:"abi"`
		URL          string `json:"url"`
		SHA256       string `json:"sha256"`
		Size         int64  `json:"size"`
	}
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	a.Model, a.Architecture, a.ABI, a.URL, a.SHA256, a.Size = v.Model, v.Architecture, v.ABI, v.URL, v.SHA256, v.Size
	return nil
}

type manifest struct {
	SchemaVersion int        `json:"schemaVersion"`
	Version       string     `json:"version"`
	Revision      int        `json:"revision"`
	Artifacts     []artifact `json:"artifacts"`
}
type signature struct {
	Algorithm string `json:"algorithm"`
	KeyID     string `json:"keyId"`
	Signature string `json:"signature"`
}
type trustKey struct {
	Algorithm string `json:"algorithm"`
	KeyID     string `json:"keyId"`
	PublicKey string `json:"publicKey"`
}
type status struct {
	Installed         versionState  `json:"installed"`
	Available         *versionState `json:"available,omitempty"`
	UpdateAvailable   bool          `json:"updateAvailable"`
	Checking          bool          `json:"checking"`
	Busy              bool          `json:"busy"`
	LastCheck         int64         `json:"lastCheck,omitempty"`
	Error             string        `json:"error,omitempty"`
	RollbackAvailable bool          `json:"rollbackAvailable"`
}
type server struct {
	root, model, architecture, abi, manifestURL, signatureURL, trustPath, tokenPath string
	client                                                                          *http.Client
	mu                                                                              sync.Mutex
	state                                                                           status
	selected                                                                        artifact
	manifest                                                                        manifest
}

var allowed = map[string]string{
	"bin/prime-unlimited-runtime-api":  "bin/prime-unlimited-runtime-api",
	"bin/prime-unlimited-self-updater": "bin/prime-unlimited-self-updater",
	"bin/modloader-api-start.sh":       "bin/modloader-api-start.sh",
	"bin/modloader-updater-start.sh":   "bin/modloader-updater-start.sh",
	"ui/ModLoader.template.qml":        "ui/ModLoader.template.qml",
	"ui/OverlayHost.template.qml":      "ui/OverlayHost.template.qml",
	"VERSION.json":                     "updater/version.json",
}

func readLimited(path string, limit int64) ([]byte, error) {
	f, e := os.Open(path)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	d, e := io.ReadAll(io.LimitReader(f, limit+1))
	if e != nil || int64(len(d)) > limit {
		return nil, errors.New("file invalid or too large")
	}
	return d, nil
}
func writeAtomic(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, e := os.CreateTemp(filepath.Dir(path), ".update-*")
	if e != nil {
		return e
	}
	name := f.Name()
	defer os.Remove(name)
	if e = f.Chmod(mode); e == nil {
		_, e = f.Write(data)
	}
	if e == nil {
		e = f.Sync()
	}
	if ce := f.Close(); e == nil {
		e = ce
	}
	if e != nil {
		return e
	}
	return os.Rename(name, path)
}
func decode(data []byte, v any) error {
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if e := d.Decode(v); e != nil {
		return e
	}
	return nil
}
func compare(a, b versionState) int {
	parse := func(v string) ([3]int, bool) {
		var out [3]int
		parts := strings.Split(v, ".")
		if len(parts) > 3 {
			return out, false
		}
		for i, p := range parts {
			n, e := strconv.Atoi(p)
			if e != nil {
				return out, false
			}
			out[i] = n
		}
		return out, true
	}
	left, lok := parse(a.Version)
	right, rok := parse(b.Version)
	if lok && rok {
		for i := 0; i < 3; i++ {
			if left[i] < right[i] {
				return -1
			}
			if left[i] > right[i] {
				return 1
			}
		}
	} else {
		if a.Version < b.Version {
			return -1
		}
		if a.Version > b.Version {
			return 1
		}
	}
	if a.Revision < b.Revision {
		return -1
	}
	if a.Revision > b.Revision {
		return 1
	}
	return 0
}
func (s *server) installed() (versionState, error) {
	d, e := readLimited(filepath.Join(s.root, "updater/version.json"), 4096)
	if e != nil {
		return versionState{}, e
	}
	var v versionState
	e = decode(d, &v)
	return v, e
}
func (s *server) fetch(ctx context.Context, url string, limit int64) ([]byte, error) {
	if !strings.HasPrefix(url, "https://raw.githubusercontent.com/Prime-Unlimited/Prime-Unlimited-OS/") && !strings.HasPrefix(url, "https://api.github.com/repos/Prime-Unlimited/Prime-Unlimited-OS/releases/assets/") {
		return nil, errors.New("URL host or path refused")
	}
	r, e := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if e != nil {
		return nil, e
	}
	resp, e := s.client.Do(r)
	if e != nil {
		return nil, e
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	d, e := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if e != nil || int64(len(d)) > limit {
		return nil, errors.New("response too large")
	}
	return d, nil
}
func verify(manifestData, signatureData, trustData []byte) (manifest, error) {
	var m manifest
	var sig signature
	var trust trustKey
	if decode(manifestData, &m) != nil || decode(signatureData, &sig) != nil || decode(trustData, &trust) != nil {
		return m, errors.New("invalid update metadata")
	}
	pub, e := base64.StdEncoding.DecodeString(trust.PublicKey)
	sb, se := base64.StdEncoding.DecodeString(sig.Signature)
	if e != nil || se != nil || trust.Algorithm != "ed25519" || sig.Algorithm != "ed25519" || sig.KeyID != trust.KeyID || len(pub) != ed25519.PublicKeySize || !ed25519.Verify(pub, manifestData, sb) {
		return m, errors.New("update signature invalid")
	}
	if m.SchemaVersion != 1 || m.Version == "" || m.Revision < 0 || len(m.Artifacts) == 0 {
		return m, errors.New("update manifest invalid")
	}
	return m, nil
}
func (s *server) check(ctx context.Context) error {
	s.mu.Lock()
	s.state.Checking = true
	s.mu.Unlock()
	defer func() { s.mu.Lock(); s.state.Checking = false; s.state.LastCheck = time.Now().Unix(); s.mu.Unlock() }()
	md, e := s.fetch(ctx, s.manifestURL, maxManifest)
	if e != nil {
		return e
	}
	sd, e := s.fetch(ctx, s.signatureURL, 4096)
	if e != nil {
		return e
	}
	td, e := readLimited(s.trustPath, 4096)
	if e != nil {
		return e
	}
	m, e := verify(md, sd, td)
	if e != nil {
		return e
	}
	var selected artifact
	found := false
	for _, a := range m.Artifacts {
		if a.Model == s.model && a.Architecture == s.architecture && a.ABI == s.abi && len(a.SHA256) == 64 && a.Size > 0 && a.Size <= maxBundle {
			selected = a
			found = true
			break
		}
	}
	if !found {
		return errors.New("no compatible updater artifact")
	}
	installed, e := s.installed()
	if e != nil {
		return e
	}
	available := versionState{m.Version, m.Revision}
	s.mu.Lock()
	s.manifest = m
	s.selected = selected
	s.state.Installed = installed
	s.state.Available = &available
	s.state.UpdateAvailable = compare(installed, available) < 0
	s.state.Error = ""
	_, e = os.Stat(filepath.Join(s.root, "updater/backup/version.json"))
	s.state.RollbackAvailable = e == nil
	s.mu.Unlock()
	return nil
}
func digest(data []byte) string { v := sha256.Sum256(data); return hex.EncodeToString(v[:]) }
func extract(data []byte, destination string) error {
	z, e := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if e != nil {
		return e
	}
	seen := map[string]bool{}
	for _, f := range z.File {
		target, ok := allowed[f.Name]
		if !ok || seen[f.Name] || f.FileInfo().IsDir() || f.Mode()&os.ModeSymlink != 0 {
			return errors.New("bundle contains unapproved entry")
		}
		seen[f.Name] = true
		r, e := f.Open()
		if e != nil {
			return e
		}
		b, e := io.ReadAll(io.LimitReader(r, 16*1024*1024))
		r.Close()
		if e != nil {
			return e
		}
		mode := os.FileMode(0644)
		if strings.HasPrefix(target, "bin/") {
			mode = 0755
		}
		if e = writeAtomic(filepath.Join(destination, target), b, mode); e != nil {
			return e
		}
	}
	if len(seen) != len(allowed) {
		return errors.New("bundle is incomplete")
	}
	return nil
}
func copyFile(src, dst string) error {
	d, e := readLimited(src, 32*1024*1024)
	if e != nil {
		return e
	}
	info, e := os.Stat(src)
	if e != nil {
		return e
	}
	return writeAtomic(dst, d, info.Mode().Perm())
}
func (s *server) backup() error {
	base := filepath.Join(s.root, "updater/backup")
	_ = os.RemoveAll(base)
	for _, target := range allowed {
		src := filepath.Join(s.root, target)
		if _, e := os.Stat(src); e == nil {
			if e = copyFile(src, filepath.Join(base, target)); e != nil {
				return e
			}
		}
	}
	return nil
}
func (s *server) restore() error {
	base := filepath.Join(s.root, "updater/backup")
	for _, target := range allowed {
		src := filepath.Join(base, target)
		if _, e := os.Stat(src); e == nil {
			if e = copyFile(src, filepath.Join(s.root, target)); e != nil {
				return e
			}
		}
	}
	return nil
}
func restartRuntime() error {
	cmd := exec.Command("systemctl", "restart", "prime-unlimited-modloader-api.service")
	return cmd.Run()
}
func health() error {
	client := &http.Client{Timeout: 2 * time.Second}
	for i := 0; i < 10; i++ {
		resp, e := client.Get("http://127.0.0.1:8098/api/v1/health")
		if e == nil {
			resp.Body.Close()
			if resp.StatusCode == 200 {
				return nil
			}
		}
		time.Sleep(time.Second)
	}
	return errors.New("updated runtime health check failed")
}
func (s *server) install(ctx context.Context) error {
	s.mu.Lock()
	if s.state.Busy {
		s.mu.Unlock()
		return errors.New("updater busy")
	}
	s.state.Busy = true
	a := s.selected
	available := s.state.Available
	s.mu.Unlock()
	defer func() { s.mu.Lock(); s.state.Busy = false; s.mu.Unlock() }()
	if available == nil || !s.state.UpdateAvailable {
		return errors.New("no update available")
	}
	data, e := s.fetch(ctx, a.URL, maxBundle)
	if e != nil {
		return e
	}
	if int64(len(data)) != a.Size || digest(data) != a.SHA256 {
		return errors.New("update artifact verification failed")
	}
	stage := filepath.Join(s.root, "updater/stage")
	_ = os.RemoveAll(stage)
	if e = extract(data, stage); e != nil {
		return e
	}
	if e = s.backup(); e != nil {
		return e
	}
	for _, target := range allowed {
		if e = copyFile(filepath.Join(stage, target), filepath.Join(s.root, target)); e != nil {
			_ = s.restore()
			return e
		}
	}
	if e = restartRuntime(); e == nil {
		e = health()
	}
	if e != nil {
		_ = s.restore()
		_ = restartRuntime()
		return errors.New("update failed and previous version was restored")
	}
	s.mu.Lock()
	s.state.Installed = *available
	s.state.UpdateAvailable = false
	s.state.RollbackAvailable = true
	s.mu.Unlock()
	return nil
}
func (s *server) rollback() error {
	s.mu.Lock()
	if s.state.Busy {
		s.mu.Unlock()
		return errors.New("updater busy")
	}
	s.state.Busy = true
	s.mu.Unlock()
	defer func() { s.mu.Lock(); s.state.Busy = false; s.mu.Unlock() }()
	if e := s.restore(); e != nil {
		return e
	}
	if e := restartRuntime(); e != nil {
		return e
	}
	if e := health(); e != nil {
		return e
	}
	installed, e := s.installed()
	s.mu.Lock()
	s.state.Installed = installed
	s.state.RollbackAvailable = false
	s.mu.Unlock()
	return e
}
func token(path string) (string, error) {
	d, e := readLimited(path, 256)
	if e != nil {
		return "", e
	}
	v := strings.TrimSpace(string(d))
	if len(v) != 64 {
		return "", errors.New("invalid auth token")
	}
	return v, nil
}
func (s *server) authorized(r *http.Request) bool {
	t, e := token(s.tokenPath)
	if e != nil {
		return false
	}
	values := r.Header.Values("Authorization")
	expected := "Bearer " + t
	return len(values) == 1 && len(values[0]) == len(expected) && subtle.ConstantTimeCompare([]byte(values[0]), []byte(expected)) == 1
}
func jsonResponse(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
func (s *server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.RawQuery != "" {
		jsonResponse(w, 400, map[string]string{"error": "query refused"})
		return
	}
	if r.Method == http.MethodGet && r.URL.Path == "/api/v1/status" {
		s.mu.Lock()
		v := s.state
		s.mu.Unlock()
		jsonResponse(w, 200, v)
		return
	}
	if r.Method != http.MethodPost || !s.authorized(r) {
		jsonResponse(w, 401, map[string]string{"error": "authorization required"})
		return
	}
	var e error
	switch r.URL.Path {
	case "/api/v1/check":
		e = s.check(r.Context())
	case "/api/v1/update":
		e = s.install(r.Context())
	case "/api/v1/rollback":
		e = s.rollback()
	default:
		jsonResponse(w, 404, map[string]string{"error": "not found"})
		return
	}
	if e != nil {
		s.mu.Lock()
		s.state.Error = e.Error()
		s.mu.Unlock()
		jsonResponse(w, 409, map[string]string{"error": e.Error()})
		return
	}
	jsonResponse(w, 200, map[string]bool{"ok": true})
	if r.URL.Path == "/api/v1/update" || r.URL.Path == "/api/v1/rollback" {
		go func() {
			time.Sleep(2 * time.Second)
			_ = exec.Command("systemctl", "restart", "prime-unlimited-modloader-updater.service").Run()
		}()
	}
}
func main() {
	listen := flag.String("listen", "127.0.0.1:8099", "")
	root := flag.String("root", "/data/prime-unlimited", "")
	model := flag.String("model", "JC11S", "")
	arch := flag.String("architecture", "armv7l", "")
	abi := flag.String("abi", "gnueabihf", "")
	manifestURL := flag.String("manifest-url", "", "")
	signatureURL := flag.String("signature-url", "", "")
	trust := flag.String("trust-key", "/data/prime-unlimited/trust/catalog-key.json", "")
	tokenPath := flag.String("auth-token", "/data/prime-unlimited/auth/mutation.token", "")
	flag.Parse()
	client := &http.Client{Timeout: 10 * time.Minute, CheckRedirect: func(request *http.Request, via []*http.Request) error {
		if len(via) == 1 && via[0].URL.Hostname() == "api.github.com" && request.URL.Hostname() == "release-assets.githubusercontent.com" {
			return nil
		}
		return http.ErrUseLastResponse
	}}
	s := &server{root: *root, model: *model, architecture: *arch, abi: *abi, manifestURL: *manifestURL, signatureURL: *signatureURL, trustPath: *trust, tokenPath: *tokenPath, client: client}
	if v, e := s.installed(); e == nil {
		s.state.Installed = v
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if e := s.check(ctx); e != nil {
			s.mu.Lock()
			s.state.Error = e.Error()
			s.mu.Unlock()
		}
	}()
	httpServer := &http.Server{Addr: *listen, Handler: s, ReadHeaderTimeout: 3 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 12 * time.Minute}
	fmt.Println(httpServer.ListenAndServe())
}
