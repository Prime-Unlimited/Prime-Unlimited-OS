package main

import (
	"context"
	"errors"
	"golang.org/x/sys/unix"
	"os"
	"os/exec"
	"path/filepath"
	usbcopy "prime-unlimited.usb-copy/worker"
	"strings"
	"time"
)

// Only explicit inactive/not-found evidence permits marking a worker abandoned.
var mediaJobUnitInactive = func(ctx context.Context, id string) (bool, error) {
	data, err := exec.CommandContext(ctx, "systemctl", "show", "pu-usb-job-"+id+".service", "--property=LoadState", "--property=ActiveState").CombinedOutput()
	values := map[string]string{}
	for _, line := range strings.Split(string(data), "\n") {
		k, v, ok := strings.Cut(line, "=")
		if ok {
			values[k] = v
		}
	}
	if values["LoadState"] == "not-found" && values["ActiveState"] == "inactive" {
		return true, nil
	}
	if err != nil {
		return false, errors.New("import worker state unavailable")
	}
	switch values["ActiveState"] {
	case "inactive", "failed":
		return true, nil
	case "active", "activating", "deactivating", "reloading":
		return false, nil
	default:
		return false, errors.New("import worker state is ambiguous")
	}
}

// Never infer abandonment from an API restart: workers run in separate units.
// Admission and supervisor leases prevent racing either job launch or a writer.
// This marks uncertainty only; it never replays an import or edits media/SQLite.
func reconcileAbandonedMediaJobs(root string, now time.Time) error {
	store, err := usbcopy.OpenJobStore(filepath.Join(root, "media-jobs"))
	if err != nil {
		return err
	}
	locks := []*os.File{}
	defer func() {
		for i := len(locks) - 1; i >= 0; i-- {
			unix.Flock(int(locks[i].Fd()), unix.LOCK_UN)
			locks[i].Close()
		}
	}()
	for _, name := range []string{"admission.lock", "supervisor.lock"} {
		file, e := os.OpenFile(filepath.Join(root, "media-jobs", name), os.O_CREATE|os.O_RDWR|unix.O_NOFOLLOW, 0600)
		if e != nil {
			return e
		}
		if e = unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB); e != nil {
			file.Close()
			if e == unix.EWOULDBLOCK || e == unix.EAGAIN {
				return nil
			}
			return e
		}
		locks = append(locks, file)
	}
	jobs, err := store.List()
	if err != nil {
		return err
	}
	pending := false
	for _, job := range jobs {
		switch job.Phase {
		case usbcopy.JobCompleted, usbcopy.JobFailed, usbcopy.JobCancelled, usbcopy.JobInterrupted:
			continue
		}
		pending = true
		// Let a just-launched systemd unit acquire its supervisor lease first.
		if now.Sub(job.UpdatedAt) < 30*time.Second {
			return nil
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		inactive, e := mediaJobUnitInactive(ctx, job.ID)
		cancel()
		if e != nil {
			return e
		}
		if !inactive {
			return nil
		}
	}
	if !pending {
		return nil
	}
	return store.RecoverInterrupted()
}
