package usbcopy

import (
	"context"
	"errors"
	"golang.org/x/sys/unix"
	"os"
	"path/filepath"
	"time"
)

// ImportControl must be implemented by the trusted runtime. Recovery must be
// independent of Engine and this process. StartEngine must confirm native
// Engine recovery; ValidateVolumes must recheck the original mount identities.
type ImportControl interface {
	ValidateVolumes(context.Context) error
	ArmRecovery(context.Context, string) error
	StopEngine(context.Context) error
	StartEngine(context.Context) error
	DisarmRecovery(context.Context, string) error
}

// SuperviseOfflineImport is a single-job lifecycle, not an HTTP authorization
// boundary. Only the trusted broker may resolve its source/destination roots.
// It does not implement post-power-loss file/database reconciliation.
func SuperviseOfflineImport(ctx context.Context, store *JobStore, id, source, destination, procRoot string, control ImportControl) (OfflineBatchResult, error) {
	return SuperviseApprovedOfflineImport(ctx, store, id, source, destination, procRoot, "", control)
}
func SuperviseApprovedOfflineImport(ctx context.Context, store *JobStore, id, source, destination, procRoot, expectedDigest string, control ImportControl) (result OfflineBatchResult, err error) {
	if store == nil || control == nil || !validJobStatusID(id) {
		return result, errors.New("invalid import supervisor configuration")
	}
	lease, e := os.OpenFile(filepath.Join(store.root, "supervisor.lock"), os.O_CREATE|os.O_RDWR|unix.O_NOFOLLOW, 0600)
	if e != nil {
		return result, e
	}
	defer lease.Close()
	if e = unix.Flock(int(lease.Fd()), unix.LOCK_EX|unix.LOCK_NB); e != nil {
		return result, errors.New("another import supervisor is active")
	}
	defer unix.Flock(int(lease.Fd()), unix.LOCK_UN)
	job, e := store.Read(id)
	if e != nil {
		return result, e
	}
	if job.Phase != JobQueued {
		return result, errors.New("import is not queued")
	}
	armed, touched, committed := false, false, false
	defer func() {
		recoveryCtx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		restored := !touched
		if touched {
			startErr := control.StartEngine(recoveryCtx)
			if startErr != nil {
				err = errors.Join(err, startErr)
			} else {
				restored = true
			}
		}
		recoveryUncertain := false
		if armed && restored {
			if e := control.DisarmRecovery(recoveryCtx, id); e != nil {
				err = errors.Join(err, e)
				recoveryUncertain = true
			}
		}
		job.EngineRestored = restored
		if err == nil {
			job.Phase = JobCompleted
			job.ErrorCode = ""
		} else if committed || errors.Is(err, ErrCommitUncertain) || !restored || recoveryUncertain {
			job.Phase = JobInterrupted
			job.ErrorCode = "recovery_required"
		} else {
			job.Phase = JobFailed
			job.ErrorCode = importErrorCode(err)
		}
		saved, saveErr := store.Save(job)
		if saveErr == nil {
			job = saved
		} else {
			err = errors.Join(err, saveErr)
		}
	}()
	if err = control.ValidateVolumes(ctx); err != nil {
		return result, withImportCode("volumes_changed", err)
	}
	if err = control.ArmRecovery(ctx, id); err != nil {
		return result, withImportCode("recovery_unavailable", err)
	}
	armed = true
	job.Phase = JobStopping
	job, err = store.Save(job)
	if err != nil {
		return result, err
	}
	touched = true
	if err = control.StopEngine(ctx); err != nil {
		return result, withImportCode("engine_stop_failed", err)
	}
	if err = control.ValidateVolumes(ctx); err != nil {
		return result, withImportCode("volumes_changed", err)
	}
	result, err = RunOfflineBatch(ctx, source, destination, procRoot, OfflineBatchOptions{JobID: id, ExpectedSourceDigest: expectedDigest, Progress: func(phase JobPhase, progress CopyProgress) error {
		if phase == JobCommitting {
			if e := control.ValidateVolumes(ctx); e != nil {
				return e
			}
		}
		next := job
		next.Phase = phase
		next.FilesDone = progress.FilesDone
		next.FilesTotal = progress.FilesTotal
		next.BytesDone = progress.BytesDone
		next.BytesTotal = progress.BytesTotal
		saved, e := store.Save(next)
		if e != nil {
			return e
		}
		job = saved
		return nil
	}})
	if err != nil {
		return result, err
	}
	committed = true
	job.ResultAvailable = true
	job.CopiedFiles = result.Copied
	job.SkippedFiles = result.Reused + result.SourceDuplicates
	job.CreatedPlaylists = result.Playlists
	job.PlaylistName = result.PlaylistName
	job.Phase = JobRestarting
	job, err = store.Save(job)
	return result, err
}
