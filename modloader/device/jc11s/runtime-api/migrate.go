package main

import (
	"errors"
	"os"
)

func migratePrivateDirectory(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return errors.New("private directory is unsafe")
	}
	if !info.IsDir() {
		return errors.New("private directory is unsafe")
	}
	if info.Mode().Perm() != 0o700 {
		if err := os.Chmod(path, 0o700); err != nil {
			return err
		}
	}
	return nil
}

func migratePrivateFile(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return errors.New("private file is unsafe")
	}
	if !info.Mode().IsRegular() {
		return errors.New("private file is unsafe")
	}
	if info.Mode().Perm() != 0o600 {
		if err := os.Chmod(path, 0o600); err != nil {
			return err
		}
	}
	return nil
}
