package usbcopy

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
)

// RequireOfflineLibrary is a precondition check, not a supervisor or lock.
func RequireOfflineLibrary(procRoot, path string) error {
	entries, err := os.ReadDir(procRoot)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		exe, _ := os.Readlink(filepath.Join(procRoot, entry.Name(), "exe"))
		if exe == "/usr/Engine/Engine" || exe == "/usr/Engine/Engine (deleted)" {
			return errors.New("Engine is still running")
		}
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	// Refuse another process holding this database, before opening it ourselves.
	for _, entry := range entries {
		if _, err := strconv.Atoi(entry.Name()); err != nil || !entry.IsDir() {
			continue
		}
		fds, e := os.ReadDir(filepath.Join(procRoot, entry.Name(), "fd"))
		if e != nil {
			if os.IsNotExist(e) {
				continue
			}
			return errors.New("cannot inspect database users")
		}
		for _, fd := range fds {
			other, e := os.Stat(filepath.Join(procRoot, entry.Name(), "fd", fd.Name()))
			if e == nil && os.SameFile(info, other) {
				return errors.New("database is open in another process")
			}
		}
	}

	return nil
}
