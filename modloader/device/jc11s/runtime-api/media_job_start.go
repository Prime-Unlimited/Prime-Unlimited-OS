package main

import (
	"context"
	"encoding/json"
	"errors"
	"golang.org/x/sys/unix"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	usbcopy "prime-unlimited.usb-copy/worker"
	"strings"
	"time"
)

type mediaJobRequest struct {
	Mode                string                   `json:"mode,omitempty"`
	DestinationIdentity mediaDestinationIdentity `json:"destinationIdentity,omitempty"`
	ID                  string                   `json:"id"`
	EventID             string                   `json:"eventId"`
	Mounts              mediaJobMounts           `json:"mounts"`
	SourceDigest        string                   `json:"sourceDigest"`
	CreatedAt           time.Time                `json:"createdAt"`
}

func mediaIdentity(v mountedBlockVolume) mediaMountIdentity {
	return mediaMountIdentity{Generation: v.deviceGeneration, MountID: v.mountID, DeviceID: v.deviceID, Root: v.mountPath, SysfsPath: v.sysfsPath}
}
func (s *server) mediaCommitAvailable() bool { return s.mediaImportWrites && s.model == "JP24" }
func mediaMP3Plan(plan usbcopy.ImportPlan) bool {
	for _, track := range plan.Tracks {
		if !strings.EqualFold(filepath.Ext(track.SourceRelative), ".mp3") {
			return false
		}
	}
	return len(plan.Tracks) > 0
}

// The offline SQL writer remains MP3-only. Additional containers use Engine's
// native registration exclusively, behind the verified live profile gate.
func (s *server) mediaSupportedPlan(plan usbcopy.ImportPlan) bool {
	if !s.mediaImportLive {
		return mediaMP3Plan(plan)
	}
	if !s.mediaLiveAvailable() || len(plan.Tracks) == 0 {
		return false
	}
	for _, t := range plan.Tracks {
		if !usbcopy.LiveAudioExtension(t.SourceRelative) {
			return false
		}
	}
	return true
}
func mediaJobRequestsRoot(root string) (string, error) {
	if _, e := usbcopy.OpenJobStore(filepath.Join(root, "media-jobs")); e != nil {
		return "", e
	}
	dir := filepath.Join(root, "media-jobs", "requests")
	if e := os.MkdirAll(dir, 0700); e != nil {
		return "", e
	}
	st, e := os.Lstat(dir)
	if e != nil || !st.IsDir() || st.Mode()&os.ModeSymlink != 0 || st.Mode().Perm() != 0700 || !safeRealDirectory(dir) {
		return "", errors.New("unsafe job request directory")
	}
	return dir, nil
}
func writeMediaJobRequest(root string, request mediaJobRequest) error {
	if request.Mode != "" && request.Mode != "offline" && request.Mode != "live" {
		return errors.New("invalid import mode")
	}
	if !validMediaEventID(request.ID) || !validMediaEventID(request.EventID) || !validMediaEventID(request.SourceDigest) || (!request.DestinationIdentity.empty() && !request.DestinationIdentity.valid()) {
		return errors.New("invalid private job request")
	}
	dir, e := mediaJobRequestsRoot(root)
	if e != nil {
		return e
	}
	b, e := json.Marshal(request)
	if e != nil {
		return e
	}
	if len(b) > 16384 {
		return errors.New("job request too large")
	}
	f, e := os.OpenFile(filepath.Join(dir, request.ID+".json"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	_, e = f.Write(b)
	if e == nil {
		e = f.Sync()
	}
	ce := f.Close()
	if e != nil {
		return e
	}
	if ce != nil {
		return ce
	}
	return syncDirectory(dir)
}
func readMediaJobRequest(root, id string) (mediaJobRequest, error) {
	var request mediaJobRequest
	if !validMediaEventID(id) {
		return request, errors.New("invalid job id")
	}
	dir, e := mediaJobRequestsRoot(root)
	if e != nil {
		return request, e
	}
	b, e := readOwnedFile(filepath.Join(dir, id+".json"), 16384, "import job request")
	if e != nil {
		return request, e
	}
	if decodeStrictJSON(b, &request) != nil || request.ID != id || !validMediaEventID(request.EventID) || !validMediaEventID(request.SourceDigest) || request.CreatedAt.IsZero() {
		return request, errors.New("invalid import job request")
	}
	if !request.DestinationIdentity.empty() && !request.DestinationIdentity.valid() {
		return request, errors.New("invalid library identity")
	}
	if request.Mode != "" && request.Mode != "offline" && request.Mode != "live" {
		return request, errors.New("invalid import mode")
	}
	return request, nil
}
func launchMediaJob(s *server, id string) error {
	if !validMediaEventID(id) {
		return errors.New("invalid job id")
	}
	exe, e := os.Executable()
	if e != nil {
		return e
	}
	exe, e = filepath.EvalSymlinks(exe)
	if e != nil {
		return e
	}
	info, e := os.Stat(exe)
	if e != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0022 != 0 {
		return errors.New("unsafe runtime executable")
	}
	request, e := readMediaJobRequest(s.root, id)
	if e != nil {
		return e
	}
	maximum := "420s"
	if request.Mode == "live" {
		if !s.mediaLiveAvailable() {
			return errors.New("live job launch is not enabled")
		}
		maximum = "21600s"
	}
	args := []string{"--quiet", "--unit=pu-usb-job-" + id, "--property=Type=exec", "--property=Restart=no", "--property=KillMode=control-group", "--property=RuntimeMaxSec=" + maximum, "--property=TimeoutStopSec=45s", "--property=UMask=0077", exe, "--media-import-job", id, "--model", s.model, "--firmware", s.firmware, "--architecture", s.architecture, "--abi", s.abi, "--root", s.root, "--catalog", s.catalogPath, "--catalog-signature", s.catalogSignaturePath, "--catalog-trust-key", s.catalogTrustKeyPath}
	if request.Mode == "live" {
		args = append(args, "--media-import-live", "--media-import-writes")
	}
	_, e = mediaEngineCommand(context.Background(), "/usr/bin/systemd-run", args...)
	return e
}
func (s *server) handleMediaJobStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if !s.mediaCommitAvailable() || !s.mediaImportEnabled() {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "supervised import is not enabled"})
		return
	}
	if s.mediaImportLive && !s.mediaLiveAvailable() {
		writeJSON(w, 403, map[string]string{"error": "live import is not verified for this device profile"})
		return
	}
	var input struct {
		EventID              string `json:"eventId"`
		Mode                 string `json:"mode,omitempty"`
		ConfirmCopy          bool   `json:"confirmCopy"`
		ConfirmEngineRestart bool   `json:"confirmEngineRestart"`
	}
	data, e := io.ReadAll(io.LimitReader(r.Body, 4097))
	if e != nil || len(data) > 4096 || decodeStrictJSON(data, &input) != nil || !validMediaEventID(input.EventID) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "valid preview and explicit Engine-restart confirmation required"})
		return
	}
	mode, modeErr := mediaRequestedImportMode(s.mediaImportLive, input.Mode, input.ConfirmCopy, input.ConfirmEngineRestart)
	if modeErr != nil {
		writeJSON(w, 400, map[string]string{"error": "valid import mode and explicit copy confirmation required"})
		return
	}
	s.mediaMu.Lock()
	defer s.mediaMu.Unlock()
	store, e := usbcopy.OpenJobStore(filepath.Join(s.root, "media-jobs"))
	if e != nil {
		writeJSON(w, 503, map[string]string{"error": "job storage unavailable"})
		return
	}
	admission, e := os.OpenFile(filepath.Join(s.root, "media-jobs", "admission.lock"), os.O_CREATE|os.O_RDWR|unix.O_NOFOLLOW, 0600)
	if e != nil {
		writeJSON(w, 503, map[string]string{"error": "job admission unavailable"})
		return
	}
	defer admission.Close()
	if e = unix.Flock(int(admission.Fd()), unix.LOCK_EX|unix.LOCK_NB); e != nil {
		writeJSON(w, 409, map[string]string{"error": "another import request is being prepared"})
		return
	}
	defer unix.Flock(int(admission.Fd()), unix.LOCK_UN)
	nativeState, nativeErr := readNativeCreate(s.root)
	if (nativeErr == nil && nativeCreateActive(nativeState)) || (nativeErr != nil && !os.IsNotExist(nativeErr)) {
		writeJSON(w, 409, map[string]string{"error": "native playlist test requires review before another import"})
		return
	}
	jobs, e := store.List()
	if e != nil {
		writeJSON(w, 503, map[string]string{"error": "job state unavailable"})
		return
	}
	for _, job := range jobs {
		saved, err := readMediaJobRequest(s.root, job.ID)
		if err == nil && saved.EventID == input.EventID {
			writeJSON(w, http.StatusAccepted, map[string]any{"job": job})
			return
		}
		if job.Phase != usbcopy.JobCompleted && job.Phase != usbcopy.JobFailed && job.Phase != usbcopy.JobCancelled {
			writeJSON(w, 409, map[string]string{"error": "another import is active or requires recovery"})
			return
		}
	}
	var state mediaPreviewState
	if e = readMediaPreview(input.EventID, &state); e != nil || state.EventID != input.EventID || time.Since(state.CreatedAt) > mediaPreviewLifetime || !s.hasPendingMediaPreview(input.EventID) || !s.mediaSupportedPlan(state.Plan) {
		writeJSON(w, 409, map[string]string{"error": "valid supported preview required"})
		return
	}
	volumes, e := readMountedBlockVolumes()
	if e != nil {
		writeJSON(w, 503, map[string]string{"error": "mount state unavailable"})
		return
	}
	source, sourceOK := findVolumeByKey(volumes, state.SourceMountID+":"+state.SourceDevice)
	target, targetOK := findVolumeByKey(volumes, state.DestinationMountID+":"+state.DestinationDevice)
	if !sourceOK || !targetOK || source.deviceGeneration != state.SourceGeneration || target.deviceGeneration != state.DestinationGeneration || source.mountPath != state.SourceRoot || target.mountPath != state.DestinationRoot || !isReadableMediaSource(source) || !isInternalMediaDestination(target) {
		writeJSON(w, 409, map[string]string{"error": "source or destination changed"})
		return
	}
	destinationIdentity, identityErr := readMediaDestinationIdentity(r.Context(), target)
	if identityErr != nil || !state.DestinationIdentity.valid() || destinationIdentity != state.DestinationIdentity {
		writeJSON(w, 409, map[string]string{"error": "internal library changed; prepare a new preview"})
		return
	}
	boot, e := mediaBootID()
	if e != nil {
		writeJSON(w, 503, map[string]string{"error": "boot identity unavailable"})
		return
	}
	if !validMediaEventID(state.DuplicateSummary.SourceDigest) {
		writeJSON(w, 409, map[string]string{"error": "prepare a new content-verified preview"})
		return
	}
	id, e := newServiceToken()
	if e != nil {
		writeJSON(w, 503, map[string]string{"error": "job identity unavailable"})
		return
	}
	request := mediaJobRequest{Mode: mode, DestinationIdentity: destinationIdentity, ID: id, EventID: input.EventID, Mounts: mediaJobMounts{BootID: boot, Source: mediaIdentity(source), Destination: mediaIdentity(target)}, SourceDigest: state.DuplicateSummary.SourceDigest, CreatedAt: time.Now().UTC()}
	if e = writeMediaJobRequest(s.root, request); e != nil {
		writeJSON(w, 503, map[string]string{"error": "job request could not be saved"})
		return
	}
	job, e := store.Save(usbcopy.ImportJobStatus{Mode: mode, ID: id, Phase: usbcopy.JobQueued, FilesTotal: len(state.Plan.Tracks), BytesTotal: state.DuplicateSummary.BytesToCopy})
	if e != nil {
		writeJSON(w, 503, map[string]string{"error": "job status could not be saved"})
		return
	}
	if e = launchMediaJob(s, id); e != nil {
		latest, readErr := store.Read(id)
		if readErr == nil && latest.Phase == usbcopy.JobQueued {
			latest.Phase = usbcopy.JobFailed
			latest.EngineRestored = true
			latest.ErrorCode = "start_failed"
			_, _ = store.Save(latest)
		}
		writeJSON(w, 503, map[string]any{"error": "import launch could not be confirmed", "jobId": id})
		return
	}
	for key, event := range s.mediaEvents {
		if event.ID == input.EventID {
			if e := s.rememberMediaResolution(event); e != nil {
				log.Printf("accepted media job resolution could not be persisted")
			}
			event.Resolved = true
			s.mediaEvents[key] = event
		}
	}
	s.clearMediaPreview(input.EventID)
	removeMediaPreview(input.EventID)
	writeJSON(w, http.StatusAccepted, map[string]any{"job": job})
}
func (s *server) runMediaJob(ctx context.Context, id string) (err error) {
	if s.model != "JP24" || os.Geteuid() != 0 || s.root != "/data/prime-unlimited" {
		return errors.New("unsupported privileged import worker")
	}
	store, e := usbcopy.OpenJobStore(filepath.Join(s.root, "media-jobs"))
	if e != nil {
		return e
	}
	defer func() {
		if err != nil {
			job, readErr := store.Read(id)
			if readErr == nil && job.Phase == usbcopy.JobQueued {
				job.Phase = usbcopy.JobFailed
				job.ErrorCode = "worker_setup_failed"
				_, _ = store.Save(job)
			}
		}
	}()
	request, e := readMediaJobRequest(s.root, id)
	if e != nil {
		return e
	}
	if request.Mode == "live" {
		return s.runMediaLiveJob(ctx, request, store)
	}
	control, e := newMediaEngineController(s.model, request.Mounts)
	if e != nil {
		return e
	}
	if request.DestinationIdentity.valid() {
		control.validateDestination = func(ctx context.Context, target mountedBlockVolume) error {
			current, err := readMediaDestinationIdentity(ctx, target)
			if err != nil || current != request.DestinationIdentity {
				return errors.New("internal library identity changed")
			}
			return nil
		}
	}
	control.approved = func() bool {
		latest, _ := loadVerifiedCatalogWithFallback(s.catalogPath, s.catalogSignaturePath, s.catalogTrustKeyPath)
		s.catalogMu.Lock()
		s.catalog = latest
		s.catalogMu.Unlock()
		return s.mediaImportEnabled()
	}
	_, e = usbcopy.SuperviseApprovedOfflineImport(ctx, store, id, request.Mounts.Source.Root, request.Mounts.Destination.Root, "/proc", request.SourceDigest, control)
	return e
}
