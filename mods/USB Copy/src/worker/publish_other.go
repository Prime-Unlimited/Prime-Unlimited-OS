//go:build !linux

package usbcopy

import "errors"

func publishImportDirectory(_, _ string) error {
	return errors.New("atomic no-overwrite directory publish requires Linux renameat2")
}
