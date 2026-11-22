package usbcopy

import (
	"context"
	"encoding/json"
	"errors"
	"golang.org/x/sys/unix"
	"io"
	"os"
	"path/filepath"
)

// FileLiveJournal stores private intent records separately from public job JSON.
// A reservation survives failed initial saves and crashes; Create never resumes it.
// The trusted supervisor must hold its shared admission/writer lease throughout.
type FileLiveJournal struct{ root string }

func OpenFileLiveJournal(root string) (*FileLiveJournal, error) {
	if !filepath.IsAbs(root) || filepath.Clean(root) != root {
		return nil, errors.New("invalid live journal root")
	}
	resolved, err := resolveExistingRoot(root)
	if err != nil || resolved != root {
		return nil, errors.New("indirect live journal root")
	}
	if err = os.MkdirAll(root, 0700); err != nil {
		return nil, err
	}
	st, err := os.Lstat(root)
	if err != nil || !st.IsDir() || st.Mode()&os.ModeSymlink != 0 || st.Mode().Perm() != 0700 {
		return nil, errors.New("live journal must be private")
	}
	return &FileLiveJournal{root: root}, nil
}
func (j *FileLiveJournal) syncDirectory() error {
	f, e := os.Open(j.root)
	if e != nil {
		return e
	}
	defer f.Close()
	return f.Sync()
}
func (j *FileLiveJournal) Create(ctx context.Context, s LiveSequenceState) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !validJobStatusID(s.JobID) || s.Phase != "prepared" || !validLiveBinding(s.Binding) {
		return errors.New("invalid initial live record")
	}
	f, err := os.OpenFile(filepath.Join(j.root, s.JobID+".reserved"), os.O_WRONLY|os.O_CREATE|os.O_EXCL|unix.O_NOFOLLOW, 0600)
	if err != nil {
		return err
	}
	_, err = f.WriteString(s.JobID)
	if err == nil {
		err = f.Sync()
	}
	ce := f.Close()
	if err == nil {
		err = ce
	}
	if err == nil {
		err = j.syncDirectory()
	}
	if err != nil {
		return err
	}
	return j.Save(ctx, s)
}
func (j *FileLiveJournal) Save(ctx context.Context, s LiveSequenceState) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !validJobStatusID(s.JobID) || !validLiveBinding(s.Binding) {
		return errors.New("invalid live record")
	}
	f, err := os.OpenFile(filepath.Join(j.root, s.JobID+".reserved"), os.O_RDONLY|unix.O_NOFOLLOW, 0)
	if err != nil {
		return err
	}
	st, err := f.Stat()
	f.Close()
	if err != nil || !st.Mode().IsRegular() || st.Mode().Perm() != 0600 {
		return errors.New("invalid reservation")
	}
	data, err := json.Marshal(s)
	if err != nil {
		return err
	}
	if len(data) > 8<<20 {
		return errors.New("live record too large")
	}
	out, err := os.CreateTemp(j.root, ".live-")
	if err != nil {
		return err
	}
	name := out.Name()
	defer os.Remove(name)
	if err = out.Chmod(0600); err == nil {
		_, err = out.Write(data)
	}
	if err == nil {
		err = out.Sync()
	}
	ce := out.Close()
	if err == nil {
		err = ce
	}
	if err != nil {
		return err
	}
	if err = os.Rename(name, filepath.Join(j.root, s.JobID+".json")); err != nil {
		return err
	}
	return j.syncDirectory()
}
func (j *FileLiveJournal) Read(id string) (LiveSequenceState, error) {
	var s LiveSequenceState
	if !validJobStatusID(id) {
		return s, errors.New("invalid job ID")
	}
	f, err := os.OpenFile(filepath.Join(j.root, id+".json"), os.O_RDONLY|unix.O_NOFOLLOW, 0)
	if err != nil {
		return s, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() || st.Mode().Perm() != 0600 || st.Size() > 8<<20 {
		return s, errors.New("invalid live journal file")
	}
	decoder := json.NewDecoder(io.LimitReader(f, 8<<20))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&s); err != nil {
		return s, err
	}
	if decoder.Decode(new(any)) != io.EOF || s.JobID != id || !validLiveBinding(s.Binding) {
		return s, errors.New("invalid live journal data")
	}
	return s, nil
}
