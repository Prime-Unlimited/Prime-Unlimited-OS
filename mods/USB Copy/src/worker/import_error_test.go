package usbcopy

import (
	"context"
	"errors"
	"testing"
)

func TestImportErrorCodesPreservePrivateCausesAndCancellation(t *testing.T) {
	cause := errors.New("private/audio/path.mp3")
	e := withImportCode("invalid_mp3", cause)
	if importErrorCode(e) != "invalid_mp3" || !errors.Is(e, cause) {
		t.Fatal("error classification lost")
	}
	if importErrorCode(withImportCode("invalid_mp3", context.DeadlineExceeded)) != "import_timeout" {
		t.Fatal("timeout hidden by stage")
	}
	if importErrorCode(cause) != "import_failed" {
		t.Fatal("private error exposed")
	}
}
