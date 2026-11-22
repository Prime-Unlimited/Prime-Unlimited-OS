//go:build linux

package usbcopy

import "golang.org/x/sys/unix"

func publishImportDirectory(stagePath, finalPath string) error {
	return unix.Renameat2(unix.AT_FDCWD, stagePath, unix.AT_FDCWD, finalPath, unix.RENAME_NOREPLACE)
}
