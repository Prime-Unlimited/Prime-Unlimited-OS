package main

import (
	"context"
	"net/http"
	"path/filepath"
	usbcopy "prime-unlimited.usb-copy/worker"
	"time"
)

type mediaIdentityReader func(context.Context, mountedBlockVolume) (mediaDestinationIdentity, error)

func resolveMediaReviewFromVolumes(ctx context.Context, request mediaJobRequest, boot string, volumes []mountedBlockVolume, readIdentity mediaIdentityReader, eligible func(mountedBlockVolume) bool) (mountedBlockVolume, bool) {
	if request.DestinationIdentity.empty() {
		if boot != request.Mounts.BootID {
			return mountedBlockVolume{}, false
		}
		volume, ok := findVolumeByKey(volumes, request.Mounts.Destination.MountID+":"+request.Mounts.Destination.DeviceID)
		return volume, ok && volume.mountPath == request.Mounts.Destination.Root && volume.sysfsPath == request.Mounts.Destination.SysfsPath && eligible(volume)
	}
	if !request.DestinationIdentity.valid() {
		return mountedBlockVolume{}, false
	}
	var matched mountedBlockVolume
	count := 0
	for _, volume := range volumes {
		if !eligible(volume) {
			continue
		}
		identity, err := readIdentity(ctx, volume)
		if err == nil && identity == request.DestinationIdentity {
			matched = volume
			count++
		}
	}
	return matched, count == 1
}
func resolveMediaReviewDestination(ctx context.Context, request mediaJobRequest) (mountedBlockVolume, bool) {
	boot, err := mediaBootID()
	if err != nil {
		return mountedBlockVolume{}, false
	}
	volumes, err := readMountedBlockVolumes()
	if err != nil {
		return mountedBlockVolume{}, false
	}
	return resolveMediaReviewFromVolumes(ctx, request, boot, volumes, readMediaDestinationIdentity, isInternalMediaDestination)
}
func (s *server) handleMediaJobReview(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodGet {
		writeJSON(w, 405, map[string]string{"error": "method not allowed"})
		return
	}
	if !validMediaEventID(id) {
		writeJSON(w, 400, map[string]string{"error": "invalid job id"})
		return
	}
	if !s.mediaImportEnabled() {
		writeJSON(w, 403, map[string]string{"error": "USB Copy is not enabled and trusted"})
		return
	}
	store, err := usbcopy.OpenJobStore(filepath.Join(s.root, "media-jobs"))
	if err != nil {
		writeJSON(w, 503, map[string]string{"error": "job state unavailable"})
		return
	}
	job, err := store.Read(id)
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": "job unavailable"})
		return
	}
	switch job.Phase {
	case usbcopy.JobCompleted, usbcopy.JobFailed, usbcopy.JobCancelled, usbcopy.JobInterrupted:
	default:
		writeJSON(w, 409, map[string]string{"error": "import is still active"})
		return
	}
	request, err := readMediaJobRequest(s.root, id)
	if err != nil {
		writeJSON(w, 409, map[string]string{"error": "trusted job request unavailable"})
		return
	}
	destination, matched := resolveMediaReviewDestination(r.Context(), request)
	if !matched {
		writeJSON(w, 409, map[string]string{"error": "original internal destination identity requires manual verification"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	review, err := usbcopy.ReviewImportArtifacts(ctx, destination.mountPath, id)
	if err != nil {
		writeJSON(w, 409, map[string]string{"error": "import artifacts could not be inspected safely"})
		return
	}
	current, matched := resolveMediaReviewDestination(r.Context(), request)
	if !matched || current.mountID != destination.mountID || current.deviceID != destination.deviceID || current.mountPath != destination.mountPath || current.sysfsPath != destination.sysfsPath {
		writeJSON(w, 409, map[string]string{"error": "internal destination changed during review"})
		return
	}
	writeJSON(w, 200, map[string]any{"jobId": id, "review": review})
}
