package main

import (
	"context"
	"errors"
	"golang.org/x/sys/unix"
	"os"
	"path/filepath"
	usbcopy "prime-unlimited.usb-copy/worker"
	"strings"
	"testing"
	"time"
)

func TestAbandonedMediaJobsRequireInactiveWorkerAndExclusiveLeases(t *testing.T) {
	for _, tc := range []struct {
		name       string
		inactive   bool
		probeError bool
		lock       string
		fresh      bool
		want       usbcopy.JobPhase
		wantError  bool
	}{
		{name: "abandoned", inactive: true, want: usbcopy.JobInterrupted},
		{name: "active unit", want: usbcopy.JobQueued},
		{name: "unknown unit", probeError: true, want: usbcopy.JobQueued, wantError: true},
		{name: "writer lease", inactive: true, lock: "supervisor.lock", want: usbcopy.JobQueued},
		{name: "launch lease", inactive: true, lock: "admission.lock", want: usbcopy.JobQueued},
		{name: "fresh launch", inactive: true, fresh: true, want: usbcopy.JobQueued},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			store, err := usbcopy.OpenJobStore(filepath.Join(root, "media-jobs"))
			if err != nil {
				t.Fatal(err)
			}
			job, err := store.Save(usbcopy.ImportJobStatus{ID: strings.Repeat("a", 64), Phase: usbcopy.JobQueued})
			if err != nil {
				t.Fatal(err)
			}
			old := mediaJobUnitInactive
			defer func() { mediaJobUnitInactive = old }()
			calls := 0
			mediaJobUnitInactive = func(context.Context, string) (bool, error) {
				calls++
				if tc.probeError {
					return false, errors.New("systemd unavailable")
				}
				return tc.inactive, nil
			}
			if tc.lock != "" {
				f, e := os.OpenFile(filepath.Join(root, "media-jobs", tc.lock), os.O_CREATE|os.O_RDWR, 0600)
				if e != nil {
					t.Fatal(e)
				}
				defer f.Close()
				if e = unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); e != nil {
					t.Fatal(e)
				}
				defer unix.Flock(int(f.Fd()), unix.LOCK_UN)
			}
			now := time.Now().Add(time.Minute)
			if tc.fresh {
				now = time.Now()
			}
			err = reconcileAbandonedMediaJobs(root, now)
			if (err != nil) != tc.wantError {
				t.Fatalf("error=%v", err)
			}
			saved, err := store.Read(job.ID)
			if err != nil || saved.Phase != tc.want {
				t.Fatalf("phase=%s err=%v", saved.Phase, err)
			}
			if tc.want == usbcopy.JobInterrupted {
				if saved.ErrorCode != "recovery_required" {
					t.Fatal("missing recovery marker")
				}
				if err = store.Acknowledge(saved.ID); err != nil {
					t.Fatal(err)
				}
				again, _ := store.Read(saved.ID)
				if again.Phase != usbcopy.JobInterrupted {
					t.Fatal("ack cleared recovery requirement")
				}
			}
			if (tc.lock != "" || tc.fresh) && calls != 0 {
				t.Fatal("probed worker before obtaining safe evidence")
			}
		})
	}
}
