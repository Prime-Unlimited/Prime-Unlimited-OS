package usbcopy

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

// LiveCommand contains only the supported native operations. Paths are relative
// to the already approved internal Engine Library, never arbitrary actor URLs.
type LiveCommand struct {
	ID        string                 `json:"id"`
	JobID     string                 `json:"jobId"`
	Intent    string                 `json:"intent"`
	Operation string                 `json:"operation"`
	Binding   LiveBinding            `json:"binding"`
	ExpiresAt time.Time              `json:"expiresAt"`
	Paths     []string               `json:"paths,omitempty"`
	LocalIDs  []int64                `json:"localIds,omitempty"`
	Tracks    []LiveTrackReference   `json:"tracks,omitempty"`
	TrackURIs []string               `json:"trackUris,omitempty"`
	Playlist  *LivePlaylistReference `json:"playlist,omitempty"`
	Title     string                 `json:"title,omitempty"`
}

// An actor acknowledgement is evidence, not verification. Only the trusted
// worker may verify it against the library and current native binding.
type LiveCommandObservation struct {
	Reason           string                 `json:"reason,omitempty"`
	SourceDevice     string                 `json:"sourceDevice,omitempty"`
	CollectionDevice string                 `json:"collectionDevice,omitempty"`
	LibraryUUID      string                 `json:"libraryUuid,omitempty"`
	AllTracks        bool                   `json:"allTracks,omitempty"`
	Outcome          string                 `json:"outcome"`
	Binding          LiveBinding            `json:"binding"`
	Tracks           []LiveTrackReference   `json:"tracks,omitempty"`
	Playlist         *LivePlaylistReference `json:"playlist,omitempty"`
	URIs             []string               `json:"uris,omitempty"`
}
type LiveCommandRecord struct {
	Command     LiveCommand             `json:"command"`
	Phase       string                  `json:"phase"`
	Observation *LiveCommandObservation `json:"observation,omitempty"`
}

// One queue belongs to one supervised job. Its caller must retain the shared
// import/writer lease. Claims survive actor/API restarts and cannot be replayed.
// Authentication, physical mount checks and parent-job admission remain the
// trusted runtime's responsibility; this store does not expose an HTTP endpoint.
type FileLiveCommands struct{ root, jobID string }

func LiveCommandID(jobID, intent string) string {
	sum := sha256.Sum256([]byte(jobID + "\x00" + intent))
	return hex.EncodeToString(sum[:])
}
func OpenFileLiveCommands(root, jobID string) (*FileLiveCommands, error) {
	if !validJobStatusID(jobID) {
		return nil, errors.New("invalid live command job")
	}
	j, err := OpenFileLiveJournal(root)
	if err != nil {
		return nil, err
	}
	return &FileLiveCommands{root: j.root, jobID: jobID}, nil
}
func (q *FileLiveCommands) locked(ctx context.Context, call func() error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(q.root, "commands.lock"), os.O_CREATE|os.O_RDWR|unix.O_NOFOLLOW, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() || st.Mode().Perm() != 0600 {
		return errors.New("unsafe command lock")
	}
	// Wait briefly for metadata readers. This retries only lock acquisition,
	// never a claimed command or a native operation.
	lockCtx, cancel := context.WithTimeout(ctx, 250*time.Millisecond)
	defer cancel()
	for {
		err = unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			break
		}
		if !errors.Is(err, unix.EWOULDBLOCK) && !errors.Is(err, unix.EAGAIN) {
			return err
		}
		timer := time.NewTimer(5 * time.Millisecond)
		select {
		case <-lockCtx.Done():
			timer.Stop()
			return lockCtx.Err()
		case <-timer.C:
		}
	}
	defer unix.Flock(int(f.Fd()), unix.LOCK_UN)
	if err = lockCtx.Err(); err != nil {
		return err
	}
	return call()
}
func validLiveCommand(c LiveCommand) bool {
	if !validJobStatusID(c.JobID) || c.ID != LiveCommandID(c.JobID, c.Intent) || !strings.HasPrefix(c.Intent, c.JobID+":") || len(c.Intent) > 256 || !validLiveBinding(c.Binding) || c.ExpiresAt.IsZero() {
		return false
	}
	if len(c.Paths) > 512 || len(c.LocalIDs) > 512 || len(c.Tracks) > 512 {
		return false
	}
	if len(c.TrackURIs) > 512 {
		return false
	}
	for _, uri := range c.TrackURIs {
		if uri == "" || len(uri) > 8192 {
			return false
		}
	}
	seen := map[string]bool{}
	for _, p := range c.Paths {
		if !safeRelative(p) || filepath.ToSlash(filepath.Clean(p)) != p || !strings.HasPrefix(p, "Music/") || !LiveAudioExtension(p) || seen[p] {
			return false
		}
		seen[p] = true
	}
	ids := map[int64]bool{}
	for _, id := range c.LocalIDs {
		if id <= 0 || ids[id] {
			return false
		}
		ids[id] = true
	}
	native := map[string]bool{}
	trackLocals := map[int64]bool{}
	trackSources := map[string]bool{}
	for _, t := range c.Tracks {
		if t.Binding != c.Binding || !safeRelative(t.SourceRelative) || !validLiveNativeTrackID(t.NativeID) || t.LocalID <= 0 || native[t.NativeID] || trackLocals[t.LocalID] || trackSources[t.SourceRelative] {
			return false
		}
		native[t.NativeID] = true
		trackLocals[t.LocalID] = true
		trackSources[t.SourceRelative] = true
	}
	if c.Playlist != nil && (c.Playlist.Binding != c.Binding || c.Playlist.LocalID <= 0 || !validLiveNativeTrackID(c.Playlist.NativeID) || strings.TrimSpace(c.Playlist.Title) == "" || len(c.Playlist.Title) > 512) {
		return false
	}
	noArgs := len(c.Paths) == 0 && len(c.LocalIDs) == 0 && len(c.Tracks) == 0 && len(c.TrackURIs) == 0 && c.Playlist == nil && c.Title == ""
	switch c.Operation {
	case "select_library", "all_tracks":
		return noArgs
	case "register_files":
		return len(c.Paths) > 0 && len(c.LocalIDs) == 0 && len(c.Tracks) == 0 && len(c.TrackURIs) == 0 && c.Playlist == nil && c.Title == ""
	case "lookup_tracks":
		return len(c.LocalIDs) > 0 && len(c.Paths) == 0 && len(c.Tracks) == 0 && len(c.TrackURIs) == 0 && c.Playlist == nil && c.Title == ""
	case "create_playlist":
		return strings.TrimSpace(c.Title) != "" && len(c.Title) <= 512 && !strings.ContainsAny(c.Title, "\x00\r\n") && len(c.Paths) == 0 && len(c.LocalIDs) == 0 && len(c.Tracks) == 0 && len(c.TrackURIs) == 0 && c.Playlist == nil
	case "add_tracks":
		return len(c.Tracks) > 0 && len(c.TrackURIs) == len(c.Tracks) && c.Playlist != nil && len(c.Paths) == 0 && len(c.LocalIDs) == 0 && c.Title == ""
	}
	return false
}
func (q *FileLiveCommands) read() (LiveCommandRecord, error) {
	var r LiveCommandRecord
	f, err := os.OpenFile(filepath.Join(q.root, "command.json"), os.O_RDONLY|unix.O_NOFOLLOW, 0)
	if err != nil {
		return r, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() || st.Mode().Perm() != 0600 || st.Size() > 1<<20 {
		return r, errors.New("unsafe native command record")
	}
	d := json.NewDecoder(io.LimitReader(f, 1<<20))
	d.DisallowUnknownFields()
	if err = d.Decode(&r); err != nil {
		return r, err
	}
	if d.Decode(new(any)) != io.EOF || !validLiveCommand(r.Command) || r.Command.JobID != q.jobID {
		return r, errors.New("invalid native command record")
	}
	switch r.Phase {
	case "queued", "claimed", "observed", "verified", "review_required":
	default:
		return r, errors.New("invalid command phase")
	}
	return r, nil
}
func (q *FileLiveCommands) save(r LiveCommandRecord) error {
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}
	if len(b) > 1<<20 {
		return errors.New("native command record too large")
	}
	f, err := os.CreateTemp(q.root, ".command-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(0600); err == nil {
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
		return err
	}
	if err = os.Rename(f.Name(), filepath.Join(q.root, "command.json")); err != nil {
		return err
	}
	return (&FileLiveJournal{root: q.root}).syncDirectory()
}
func (q *FileLiveCommands) Read(ctx context.Context) (r LiveCommandRecord, err error) {
	err = q.locked(ctx, func() error { var e error; r, e = q.read(); return e })
	return
}
func (q *FileLiveCommands) Enqueue(ctx context.Context, c LiveCommand, now time.Time) error {
	if !validLiveCommand(c) || c.JobID != q.jobID || !now.Before(c.ExpiresAt) || c.ExpiresAt.Sub(now) > 5*time.Minute {
		return errors.New("invalid or expired native command")
	}
	return q.locked(ctx, func() error {
		if !time.Now().Before(c.ExpiresAt) {
			return errors.New("native command expired while waiting for publication")
		}
		previous, err := q.read()
		if err == nil && previous.Phase != "verified" {
			return errors.New("previous native command requires review")
		}
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		if err == nil {
			// Preserve the verified evidence before publishing the next command.
			archive := filepath.Join(q.root, previous.Command.ID+".completed.json")
			if e := os.Link(filepath.Join(q.root, "command.json"), archive); e != nil {
				if !os.IsExist(e) {
					return e
				}
				f, e := os.OpenFile(archive, os.O_RDONLY|unix.O_NOFOLLOW, 0)
				if e != nil {
					return e
				}
				st, e := f.Stat()
				if e != nil || !st.Mode().IsRegular() || st.Mode().Perm() != 0600 || st.Size() > 1<<20 {
					f.Close()
					return errors.New("unsafe completed command")
				}
				data, e := io.ReadAll(io.LimitReader(f, 1<<20))
				f.Close()
				expected, _ := json.Marshal(previous)
				if e != nil || !bytes.Equal(data, expected) {
					return errors.New("completed command evidence differs")
				}
			}
			if e := (&FileLiveJournal{root: q.root}).syncDirectory(); e != nil {
				return e
			}
		}
		// Reserve before publishing. An interrupted publication cannot be retried.
		f, err := os.OpenFile(filepath.Join(q.root, c.ID+".reserved"), os.O_CREATE|os.O_EXCL|os.O_WRONLY|unix.O_NOFOLLOW, 0600)
		if err != nil {
			return err
		}
		_, err = f.WriteString(c.ID)
		if err == nil {
			err = f.Sync()
		}
		ce := f.Close()
		if err == nil {
			err = ce
		}
		if err != nil {
			return err
		}
		if err = (&FileLiveJournal{root: q.root}).syncDirectory(); err != nil {
			return err
		}
		return q.save(LiveCommandRecord{Command: c, Phase: "queued"})
	})
}

// AwaitObservation waits without resubmitting or reclaiming a command. A caller
// timeout leaves durable evidence for review; it never starts an offline fallback.
func (q *FileLiveCommands) AwaitObservation(ctx context.Context, c LiveCommand) (LiveCommandObservation, error) {
	if !validLiveCommand(c) || c.JobID != q.jobID {
		return LiveCommandObservation{}, errors.New("invalid awaited command")
	}
	deadline, cancel := context.WithDeadline(ctx, c.ExpiresAt)
	defer cancel()
	ticker := time.NewTicker(75 * time.Millisecond)
	defer ticker.Stop()
	for {
		r, e := q.Read(deadline)
		if e == nil {
			if r.Command.ID != c.ID || r.Command.Binding != c.Binding {
				return LiveCommandObservation{}, errors.New("native command was replaced")
			}
			switch r.Phase {
			case "observed":
				if r.Observation == nil || r.Observation.Outcome != "applied" || r.Observation.Binding != c.Binding {
					return LiveCommandObservation{}, errors.New("invalid native observation")
				}
				return *r.Observation, nil
			case "review_required", "verified":
				return LiveCommandObservation{}, errors.New("native command is no longer awaiting verification")
			}
		} else if !errors.Is(e, unix.EWOULDBLOCK) && !errors.Is(e, unix.EAGAIN) {
			return LiveCommandObservation{}, e
		}
		select {
		case <-deadline.Done():
			return LiveCommandObservation{}, deadline.Err()
		case <-ticker.C:
		}
	}
}
func (q *FileLiveCommands) Claim(ctx context.Context, id string, binding LiveBinding, now time.Time) (command LiveCommand, err error) {
	err = q.locked(ctx, func() error {
		r, e := q.read()
		if e != nil {
			return e
		}
		if r.Command.ID != id || r.Phase != "queued" || r.Command.Binding != binding || !now.Before(r.Command.ExpiresAt) || !time.Now().Before(r.Command.ExpiresAt) {
			return errors.New("native command cannot be claimed")
		}
		r.Phase = "claimed"
		if e = q.save(r); e != nil {
			return e
		}
		command = r.Command
		return nil
	})
	return
}
func (q *FileLiveCommands) Observe(ctx context.Context, id string, o LiveCommandObservation, now time.Time) error {
	return q.locked(ctx, func() error {
		r, e := q.read()
		if e != nil {
			return e
		}
		if r.Command.ID != id || r.Phase != "claimed" || o.Binding != r.Command.Binding || !now.Before(r.Command.ExpiresAt) || !time.Now().Before(r.Command.ExpiresAt) {
			return errors.New("stale native acknowledgement")
		}
		if o.Outcome != "applied" && o.Outcome != "not_invoked" && o.Outcome != "uncertain" {
			return errors.New("invalid native outcome")
		}
		r.Observation = &o
		r.Phase = "observed"
		if o.Outcome != "applied" {
			r.Phase = "review_required"
		}
		return q.save(r)
	})
}

// Verify never trusts an actor's success flag. The trusted caller supplies the
// actual current binding and checks persisted native/SQL evidence in verify.
func (q *FileLiveCommands) Verify(ctx context.Context, id string, binding LiveBinding, verify func(LiveCommand, LiveCommandObservation) error) error {
	return q.locked(ctx, func() error {
		r, e := q.read()
		if e != nil {
			return e
		}
		if verify == nil || r.Command.ID != id || r.Phase != "observed" || r.Observation == nil || binding != r.Command.Binding {
			return errors.New("native command is not verifiable")
		}
		if e = verify(r.Command, *r.Observation); e != nil {
			r.Phase = "review_required"
			return errors.Join(e, q.save(r))
		}
		r.Phase = "verified"
		return q.save(r)
	})
}
