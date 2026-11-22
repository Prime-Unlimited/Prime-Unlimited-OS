package usbcopy

import (
	"context"
	"errors"
)

// Internal causes stay private; only a stable code is exposed to the dialog.
type importStageError struct {
	code  string
	cause error
}

func (e *importStageError) Error() string { return e.code + ": " + e.cause.Error() }
func (e *importStageError) Unwrap() error { return e.cause }
func withImportCode(code string, err error) error {
	if err == nil {
		return nil
	}
	return &importStageError{code, err}
}
func importErrorCode(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return "import_timeout"
	}
	if errors.Is(err, context.Canceled) {
		return "import_cancelled"
	}
	var stage *importStageError
	if errors.As(err, &stage) {
		return stage.code
	}
	return "import_failed"
}
