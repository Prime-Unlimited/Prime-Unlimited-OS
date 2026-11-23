package main

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"errors"
	"path/filepath"
)

// The UI actor travels inside the trusted runtime binary. It is never accepted
// from a mod ZIP, and does not add entries to the existing updater ZIP format.
//
//go:embed assets/NativeMediaImport.qml
var nativeMediaActor []byte

func nativeMediaActorRevision() string {
	sum := sha256.Sum256(nativeMediaActor)
	return hex.EncodeToString(sum[:])
}
func provisionNativeMediaUI(root, token string) error {
	dir := filepath.Join(root, "ui")
	if !safeRealDirectory(dir) {
		return errors.New("unsafe native media UI directory")
	}
	if bytes.Count(nativeMediaActor, uiAuthorizationMarker) != 1 || !validMediaEventID(token) {
		return errors.New("invalid native UI authorization")
	}
	return writeAtomicPrivateFile(filepath.Join(dir, "NativeMediaImport.qml"), bytes.Replace(nativeMediaActor, uiAuthorizationMarker, []byte(token), 1))
}

func (s *server) nativeMediaOverlay() (overlayRecord, bool, error) {
	var empty overlayRecord
	if !s.mediaLiveAvailable() || !s.mediaImportEnabled() {
		return empty, false, nil
	}
	path := filepath.Join(s.root, "ui", "NativeMediaImport.qml")
	data, e := readOwnedFile(path, 1<<20, "trusted native media actor")
	if e != nil {
		return empty, false, e
	}
	expected := bytes.Replace(nativeMediaActor, uiAuthorizationMarker, []byte(s.authToken), 1)
	if !bytes.Equal(data, expected) {
		return empty, false, errors.New("native media actor differs from trusted runtime")
	}
	rev := nativeMediaActorRevision()
	return overlayRecord{ID: "usb-copy-native", Version: rev, Source: "file://" + path + "?native=" + rev}, true, nil
}
