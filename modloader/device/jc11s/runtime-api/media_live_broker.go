package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/unix"
	usbcopy "prime-unlimited.usb-copy/worker"
)

type mediaLiveSession struct {
	JobID   string              `json:"jobId"`
	Binding usbcopy.LiveBinding `json:"binding"`
}
type mediaLiveCommandContext struct {
	session mediaLiveSession
	request mediaJobRequest
	queue   *usbcopy.FileLiveCommands
}
type mediaLiveEnvelope struct {
	Command       usbcopy.LiveCommand `json:"command"`
	SourceDevice  string              `json:"sourceDevice"`
	FileURLs      []string            `json:"fileUrls,omitempty"`
	ExpiresUnixMS int64               `json:"expiresUnixMs"`
}

func mediaLiveDirectory(root, id string) (string, error) {
	if !validMediaEventID(id) {
		return "", errors.New("invalid live job")
	}
	dir := filepath.Join(root, "media-jobs", "live", id)
	if err := ensurePrivateDirectory(dir); err != nil {
		return "", err
	}
	return dir, nil
}
func mediaLiveMountBinding(request mediaJobRequest) string {
	b, _ := json.Marshal(struct {
		Mounts   mediaJobMounts
		Identity mediaDestinationIdentity
	}{request.Mounts, request.DestinationIdentity})
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
func readMediaLiveSession(root, id string) (mediaLiveSession, error) {
	var session mediaLiveSession
	dir, err := mediaLiveDirectory(root, id)
	if err != nil {
		return session, err
	}
	b, err := readOwnedFile(filepath.Join(dir, "session.json"), 4096, "live import session")
	if err != nil {
		return session, err
	}
	if decodeStrictJSON(b, &session) != nil || session.JobID != id || session.Binding.EngineIdentity == "" || !validMediaEventID(session.Binding.MountIdentity) || !mediaLibraryUUIDPattern.MatchString(session.Binding.LibraryUUID) {
		return session, errors.New("invalid live import session")
	}
	return session, nil
}

// The caller must already hold the shared supervisor lease. Session publication
// is exclusive: a worker restart cannot silently bind a job to a new Engine.
func createMediaLiveSession(root string, request mediaJobRequest, engine string) (mediaLiveSession, error) {
	session := mediaLiveSession{JobID: request.ID, Binding: usbcopy.LiveBinding{LibraryUUID: request.DestinationIdentity.LibraryUUID, MountIdentity: mediaLiveMountBinding(request), EngineIdentity: engine}}
	if request.Mode != "live" || !request.DestinationIdentity.valid() || engine == "" {
		return session, errors.New("invalid live session configuration")
	}
	dir, err := mediaLiveDirectory(root, request.ID)
	if err != nil {
		return session, err
	}
	f, err := os.OpenFile(filepath.Join(dir, "session.json"), os.O_CREATE|os.O_EXCL|os.O_WRONLY|unix.O_NOFOLLOW, 0600)
	if err != nil {
		return session, err
	}
	b, err := json.Marshal(session)
	if err == nil {
		_, err = f.Write(b)
	}
	if err == nil {
		err = f.Sync()
	}
	ce := f.Close()
	if err == nil {
		err = ce
	}
	if err != nil {
		return session, err
	}
	return session, syncDirectory(dir)
}
func mediaLiveSupervisorHeld(root string) error {
	f, err := os.OpenFile(filepath.Join(root, "media-jobs", "supervisor.lock"), os.O_RDWR|unix.O_NOFOLLOW, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() || st.Mode().Perm() != 0600 {
		return errors.New("unsafe supervisor lease")
	}
	err = unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
	if err == nil {
		unix.Flock(int(f.Fd()), unix.LOCK_UN)
		return errors.New("live supervisor is not holding its lease")
	}
	if !errors.Is(err, unix.EWOULDBLOCK) && !errors.Is(err, unix.EAGAIN) {
		return err
	}
	return nil
}

// Recheck the private parent job, physical drives, library identity and actual
// native process before every claim/ack, not identities supplied by QML.
func (s *server) resolveMediaLiveCommand(ctx context.Context, id string) (mediaLiveCommandContext, error) {
	var out mediaLiveCommandContext
	request, err := readMediaJobRequest(s.root, id)
	if err != nil {
		return out, err
	}
	if request.Mode != "live" || !request.DestinationIdentity.valid() {
		return out, errors.New("parent job is not a live import")
	}
	store, err := usbcopy.OpenJobStore(filepath.Join(s.root, "media-jobs"))
	if err != nil {
		return out, err
	}
	job, err := store.Read(id)
	if err != nil {
		return out, err
	}
	if job.Mode != "live" {
		return out, errors.New("parent status is not a live import")
	}
	if job.Phase != usbcopy.JobCopying && job.Phase != usbcopy.JobCommitting {
		return out, errors.New("parent live job is not active")
	}
	session, err := readMediaLiveSession(s.root, id)
	if err != nil {
		return out, err
	}
	if session.Binding.LibraryUUID != request.DestinationIdentity.LibraryUUID || session.Binding.MountIdentity != mediaLiveMountBinding(request) {
		return out, errors.New("live session belongs to different media")
	}
	control, err := newMediaEngineController(s.model, request.Mounts)
	if err != nil {
		return out, err
	}
	control.validateDestination = func(ctx context.Context, v mountedBlockVolume) error {
		actual, e := readMediaDestinationIdentity(ctx, v)
		if e != nil {
			return e
		}
		if actual != request.DestinationIdentity {
			return errors.New("live library changed")
		}
		return nil
	}
	if err = control.ValidateVolumes(ctx); err != nil {
		return out, err
	}
	engine, err := nativeEngineIdentity()
	if err != nil || engine != session.Binding.EngineIdentity {
		return out, errors.New("live Engine instance changed")
	}
	if _, err = mediaEngineCommand(ctx, "/bin/systemctl", "is-active", "--quiet", "pu-usb-job-"+id+".service"); err != nil {
		return out, errors.New("parent live worker is inactive")
	}
	if err = mediaLiveSupervisorHeld(s.root); err != nil {
		return out, err
	}
	dir, err := mediaLiveDirectory(s.root, id)
	if err != nil {
		return out, err
	}
	queue, err := usbcopy.OpenFileLiveCommands(filepath.Join(dir, "commands"), id)
	if err != nil {
		return out, err
	}
	return mediaLiveCommandContext{session: session, request: request, queue: queue}, nil
}
func liveCommandEnvelope(c usbcopy.LiveCommand, parent mediaLiveCommandContext) mediaLiveEnvelope {
	envelope := mediaLiveEnvelope{Command: c, SourceDevice: parent.request.Mounts.Destination.Root, ExpiresUnixMS: c.ExpiresAt.UnixMilli()}
	for _, p := range c.Paths {
		envelope.FileURLs = append(envelope.FileURLs, (&url.URL{Scheme: "file", Path: filepath.Join(envelope.SourceDevice, "Engine Library", filepath.FromSlash(p))}).String())
	}
	return envelope
}
func (s *server) handleNativeMediaCommand(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !s.authorized(r) {
		writeJSON(w, 401, map[string]string{"error": "trusted UI authorization required"})
		return
	}
	if r.URL.RawQuery != "" || r.URL.Fragment != "" {
		writeJSON(w, 400, map[string]string{"error": "query parameters are not supported"})
		return
	}
	if !s.mediaLiveAvailable() || !s.mediaImportEnabled() {
		writeJSON(w, 403, map[string]string{"error": "live import is not enabled"})
		return
	}
	serveNativeMediaCommand(w, r, s.resolveMediaLiveCommand)
}

func (s *server) handleNativeMediaStatus(w http.ResponseWriter, r *http.Request) {
	if !s.authorized(r) {
		writeJSON(w, 401, map[string]string{"error": "trusted UI authorization required"})
		return
	}
	if r.URL.RawQuery != "" || r.URL.Fragment != "" {
		writeJSON(w, 400, map[string]string{"error": "query parameters are not supported"})
		return
	}
	if r.Method != http.MethodGet {
		writeJSON(w, 405, map[string]string{"error": "method not allowed"})
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, map[string]any{"enabled": s.mediaLiveAvailable() && s.mediaImportEnabled(), "scriptRevision": nativeMediaActorRevision()})
}
func serveNativeMediaCommand(w http.ResponseWriter, r *http.Request, resolve func(context.Context, string) (mediaLiveCommandContext, error)) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/v1/media-import/native/"), "/")
	if len(parts) != 2 || !validMediaEventID(parts[0]) {
		writeJSON(w, 404, map[string]string{"error": "unknown native import endpoint"})
		return
	}
	operation := parts[1]
	if operation != "pending" && operation != "claim" && operation != "observe" {
		writeJSON(w, 404, map[string]string{"error": "unknown native import endpoint"})
		return
	}
	if (operation == "pending" && r.Method != http.MethodGet) || (operation != "pending" && r.Method != http.MethodPost) {
		writeJSON(w, 405, map[string]string{"error": "method not allowed"})
		return
	}
	var input struct {
		ID          string                          `json:"id"`
		Observation *usbcopy.LiveCommandObservation `json:"observation,omitempty"`
	}
	if operation != "pending" {
		limit := int64(1 << 20)
		if operation == "claim" {
			limit = 256
		}
		b, e := io.ReadAll(io.LimitReader(r.Body, limit+1))
		if e != nil || int64(len(b)) > limit || decodeStrictJSON(b, &input) != nil || !validMediaEventID(input.ID) || (operation == "claim" && input.Observation != nil) || (operation == "observe" && input.Observation == nil) {
			writeJSON(w, 400, map[string]string{"error": "invalid native command acknowledgement"})
			return
		}
	}
	parent, err := resolve(r.Context(), parts[0])
	if err != nil {
		writeJSON(w, 409, map[string]string{"error": "live parent job or media changed"})
		return
	}
	if operation == "pending" {
		record, e := parent.queue.Read(r.Context())
		if os.IsNotExist(e) {
			w.WriteHeader(204)
			return
		}
		if e != nil {
			writeJSON(w, 409, map[string]string{"error": "native command state unavailable"})
			return
		}
		if record.Command.Binding != parent.session.Binding {
			writeJSON(w, 409, map[string]string{"error": "native command binding changed"})
			return
		}
		if record.Phase != "queued" || !time.Now().Before(record.Command.ExpiresAt) {
			w.WriteHeader(204)
			return
		}
		writeJSON(w, 200, liveCommandEnvelope(record.Command, parent))
		return
	}
	if operation == "claim" {
		command, e := parent.queue.Claim(r.Context(), input.ID, parent.session.Binding, time.Now())
		if e != nil {
			writeJSON(w, 409, map[string]string{"error": "native command cannot be claimed"})
			return
		}
		writeJSON(w, 200, liveCommandEnvelope(command, parent))
		return
	}
	if input.Observation.Binding != parent.session.Binding {
		writeJSON(w, 409, map[string]string{"error": "native observation binding changed"})
		return
	}
	if err = parent.queue.Observe(r.Context(), input.ID, *input.Observation, time.Now()); err != nil {
		writeJSON(w, 409, map[string]string{"error": "native observation is stale or invalid"})
		return
	}
	writeJSON(w, 202, map[string]bool{"recorded": true})
}
