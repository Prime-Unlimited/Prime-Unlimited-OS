package usbcopy

import (
	"bytes"
	"encoding/json"
	"errors"
	"golang.org/x/sys/unix"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type JobPhase string

const (
	JobQueued      JobPhase = "queued"
	JobStopping    JobPhase = "stopping_engine"
	JobCopying     JobPhase = "copying"
	JobCommitting  JobPhase = "committing_library"
	JobRestarting  JobPhase = "restarting_engine"
	JobCompleted   JobPhase = "completed"
	JobFailed      JobPhase = "failed"
	JobCancelled   JobPhase = "cancelled"
	JobInterrupted JobPhase = "interrupted"
)

// ImportJobStatus contains public progress and a playlist label, never file paths or tags.
type ImportJobStatus struct {
	ResultAvailable  bool      `json:"resultAvailable,omitempty"`
	CopiedFiles      int       `json:"copiedFiles,omitempty"`
	SkippedFiles     int       `json:"skippedFiles,omitempty"`
	CreatedPlaylists int       `json:"createdPlaylists,omitempty"`
	PlaylistName     string    `json:"playlistName,omitempty"`
	Acknowledged     bool      `json:"acknowledged"`
	EngineRestored   bool      `json:"engineRestored"`
	ID               string    `json:"id"`
	Revision         uint64    `json:"revision"`
	Phase            JobPhase  `json:"phase"`
	FilesDone        int       `json:"filesDone"`
	FilesTotal       int       `json:"filesTotal"`
	BytesDone        int64     `json:"bytesDone"`
	BytesTotal       int64     `json:"bytesTotal"`
	ErrorCode        string    `json:"errorCode,omitempty"`
	UpdatedAt        time.Time `json:"updatedAt"`
}
type JobStore struct{ root string }

func OpenJobStore(root string) (*JobStore, error) {
	if !filepath.IsAbs(root) || filepath.Clean(root) != root {
		return nil, errors.New("invalid job directory")
	}
	resolved, e := resolveExistingRoot(root)
	if e != nil || resolved != root {
		return nil, errors.New("unsafe job directory")
	}
	if e = os.MkdirAll(root, 0700); e != nil {
		return nil, e
	}
	st, e := os.Lstat(root)
	if e != nil || !st.IsDir() || st.Mode()&os.ModeSymlink != 0 || st.Mode().Perm() != 0700 {
		return nil, errors.New("job directory is not private")
	}
	return &JobStore{root}, nil
}
func validJobStatusID(id string) bool {
	if len(id) != 64 {
		return false
	}
	for _, c := range id {
		if !strings.ContainsRune("0123456789abcdef", c) {
			return false
		}
	}
	return true
}
func terminalJobPhase(phase JobPhase) bool {
	return phase == JobCompleted || phase == JobFailed || phase == JobCancelled || phase == JobInterrupted
}
func validJobTransition(old, next JobPhase) bool {
	if old == next {
		return !terminalJobPhase(old)
	}
	if terminalJobPhase(old) {
		return false
	}
	if next == JobFailed || next == JobInterrupted {
		return true
	}
	switch old {
	case JobQueued:
		return next == JobStopping || next == JobCancelled
	case JobStopping:
		return next == JobCopying
	case JobCopying:
		return next == JobCommitting || next == JobCancelled
	case JobCommitting:
		return next == JobRestarting
	case JobRestarting:
		return next == JobCompleted
	}
	return false
}
func validJobProgress(s ImportJobStatus) bool {
	if s.FilesDone < 0 || s.FilesTotal < 0 || s.FilesDone > s.FilesTotal || s.BytesDone < 0 || s.BytesTotal < 0 || s.BytesDone > s.BytesTotal || len(s.ErrorCode) > 80 {
		return false
	}
	if s.ResultAvailable {
		if (s.Phase != JobRestarting && s.Phase != JobCompleted && s.Phase != JobInterrupted) || s.CopiedFiles < 0 || s.SkippedFiles < 0 || s.CopiedFiles > s.FilesTotal || s.SkippedFiles != s.FilesTotal-s.CopiedFiles || s.CreatedPlaylists < 0 || len(s.PlaylistName) > 512 {
			return false
		}
		if s.CopiedFiles == 0 && (s.CreatedPlaylists != 0 || s.PlaylistName != "") {
			return false
		}
		if s.CopiedFiles > 0 && (s.CreatedPlaylists == 0 || s.PlaylistName == "") {
			return false
		}
	} else if s.CopiedFiles != 0 || s.SkippedFiles != 0 || s.CreatedPlaylists != 0 || s.PlaylistName != "" {
		return false
	}
	for _, c := range s.ErrorCode {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_') {
			return false
		}
	}
	return true
}
func (s *JobStore) Read(id string) (ImportJobStatus, error) {
	var status ImportJobStatus
	if !validJobStatusID(id) {
		return status, errors.New("invalid job id")
	}
	f, e := os.OpenFile(filepath.Join(s.root, id+".json"), os.O_RDONLY|unix.O_NOFOLLOW, 0)
	if e != nil {
		return status, e
	}
	defer f.Close()
	st, e := f.Stat()
	if e != nil || !st.Mode().IsRegular() || st.Size() > 16384 || st.Mode().Perm() != 0600 {
		return status, errors.New("unsafe job record")
	}
	data, e := io.ReadAll(io.LimitReader(f, 16385))
	if e != nil || len(data) > 16384 {
		return status, errors.New("job record too large")
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if e = d.Decode(&status); e != nil {
		return status, e
	}
	var extra any
	if e = d.Decode(&extra); e != io.EOF {
		return status, errors.New("trailing job data")
	}
	if status.ID != id || status.Revision == 0 || !validJobProgress(status) || status.UpdatedAt.IsZero() {
		return status, errors.New("invalid job record")
	}
	switch status.Phase {
	case JobQueued, JobStopping, JobCopying, JobCommitting, JobRestarting, JobCompleted, JobFailed, JobCancelled, JobInterrupted:
	default:
		return status, errors.New("invalid job phase")
	}
	return status, nil
}

// Save uses a revision check and a per-job OS lock across the API and worker.
// Successful return means the atomic record and its directory were synced.
func (s *JobStore) Save(next ImportJobStatus) (ImportJobStatus, error) {
	if !validJobStatusID(next.ID) || !validJobProgress(next) {
		return next, errors.New("invalid job status")
	}
	lock, e := os.OpenFile(filepath.Join(s.root, next.ID+".lock"), os.O_CREATE|os.O_RDWR|unix.O_NOFOLLOW, 0600)
	if e != nil {
		return next, e
	}
	defer lock.Close()
	if e = unix.Flock(int(lock.Fd()), unix.LOCK_EX); e != nil {
		return next, e
	}
	defer unix.Flock(int(lock.Fd()), unix.LOCK_UN)
	ackOnly := false
	previous, e := s.Read(next.ID)
	if os.IsNotExist(e) {
		if next.Revision != 0 || next.Phase != JobQueued || next.FilesDone != 0 || next.BytesDone != 0 {
			return next, errors.New("new job must be queued")
		}
	} else if e != nil {
		return next, e
	} else {
		ackOnly = false
		if terminalJobPhase(previous.Phase) && next.Phase == previous.Phase && !previous.Acknowledged && next.Acknowledged {
			compare := next
			compare.Acknowledged = previous.Acknowledged
			ackOnly = compare == previous
		}
		if next.Revision != previous.Revision || (!validJobTransition(previous.Phase, next.Phase) && !ackOnly) {
			return next, errors.New("stale revision or invalid job transition")
		}
		if next.FilesTotal != previous.FilesTotal || next.BytesTotal != previous.BytesTotal || next.FilesDone < previous.FilesDone || next.BytesDone < previous.BytesDone {
			return next, errors.New("job totals or progress changed inconsistently")
		}
	}
	if e == nil && !ackOnly && terminalJobPhase(next.Phase) && next.Phase != JobInterrupted && previous.Phase != JobQueued && !next.EngineRestored {
		return next, errors.New("terminal job requires confirmed Engine recovery")
	}
	if next.Phase == JobCompleted && (next.FilesDone != next.FilesTotal || next.BytesDone != next.BytesTotal) {
		return next, errors.New("incomplete progress cannot complete")
	}
	next.Revision++
	next.UpdatedAt = time.Now().UTC()
	data, e := json.Marshal(next)
	if e != nil {
		return next, e
	}
	f, e := os.CreateTemp(s.root, ".job-")
	if e != nil {
		return next, e
	}
	name := f.Name()
	defer os.Remove(name)
	if e = f.Chmod(0600); e == nil {
		_, e = f.Write(data)
	}
	if e == nil {
		e = f.Sync()
	}
	ce := f.Close()
	if e != nil {
		return next, e
	}
	if ce != nil {
		return next, ce
	}
	if e = os.Rename(name, filepath.Join(s.root, next.ID+".json")); e != nil {
		return next, e
	}
	dir, e := os.Open(s.root)
	if e != nil {
		return next, e
	}
	defer dir.Close()
	if e = dir.Sync(); e != nil {
		return next, e
	}
	return next, nil
}
func (s *JobStore) List() ([]ImportJobStatus, error) {
	files, e := os.ReadDir(s.root)
	if e != nil {
		return nil, e
	}
	out := []ImportJobStatus{}
	for _, file := range files {
		if !strings.HasSuffix(file.Name(), ".json") {
			continue
		}
		id := strings.TrimSuffix(file.Name(), ".json")
		status, e := s.Read(id)
		if e != nil {
			return nil, e
		}
		out = append(out, status)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt.After(out[j].UpdatedAt) })
	return out, nil
}

// RecoverInterrupted marks abandoned jobs for manual reconciliation only.
// Call solely after an exclusive supervisor has established no worker survives.
// It never deletes media, rolls back a database or resumes an uncertain commit.
func (s *JobStore) RecoverInterrupted() error {
	jobs, e := s.List()
	if e != nil {
		return e
	}
	for _, job := range jobs {
		if terminalJobPhase(job.Phase) {
			continue
		}
		job.Phase = JobInterrupted
		job.ErrorCode = "recovery_required"
		if _, e = s.Save(job); e != nil {
			return e
		}
	}
	return nil
}

func (s *JobStore) Acknowledge(id string) error {
	status, e := s.Read(id)
	if e != nil {
		return e
	}
	if !terminalJobPhase(status.Phase) {
		return errors.New("running job cannot be acknowledged")
	}
	if status.Acknowledged {
		return nil
	}
	status.Acknowledged = true
	_, e = s.Save(status)
	return e
}
