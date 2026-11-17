package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"syscall"
)

var uiAuthorizationMarker = []byte("__PRIME_UNLIMITED_AUTH_TOKEN__")

func writeAtomicPrivateFile(path string, data []byte) error {
	directory := filepath.Dir(path)
	info, err := os.Lstat(directory)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o022 != 0 {
		return errors.New("unsafe UI directory")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != uint32(os.Geteuid()) {
		return errors.New("UI directory has wrong owner")
	}
	temporary, err := os.CreateTemp(directory, ".authorized-ui-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err = temporary.Chmod(0o600); err == nil {
		_, err = temporary.Write(data)
	}
	if err == nil {
		err = temporary.Sync()
	}
	if closeErr := temporary.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err = os.Rename(temporaryPath, path); err != nil {
		return err
	}
	return syncDirectory(directory)
}

func provisionAuthorizedUI(root, token string) error {
	templatePath := filepath.Join(root, "ui", "ModLoader.template.qml")
	template, err := readOwnedFile(templatePath, 1024*1024, "manager UI template")
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if bytes.Count(template, uiAuthorizationMarker) != 1 {
		return errors.New("manager UI template has invalid authorization marker")
	}
	authorized := bytes.Replace(template, uiAuthorizationMarker, []byte(token), 1)
	return writeAtomicPrivateFile(filepath.Join(root, "ui", "ModLoader.qml"), authorized)
}
