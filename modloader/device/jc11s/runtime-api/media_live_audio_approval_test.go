package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	usbcopy "prime-unlimited.usb-copy/worker"
	"strings"
	"testing"
	"time"
)

func TestAudioEOFApprovalIsExactAndTimeLimited(t *testing.T) {
	root, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	dir := filepath.Join(root, "media-jobs", "approvals")
	if e = os.MkdirAll(dir, 0700); e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(dir, "partial-mp3-eof.json")
	now := time.Now().UTC()
	digest := strings.Repeat("a", 64)
	fp := usbcopy.FileFingerprint{SHA256: strings.Repeat("b", 64), Bytes: 100}
	write := func(expiry time.Time) {
		t.Helper()
		b, e := json.Marshal(map[string]any{"sourceDigest": digest, "expiresAt": expiry, "files": []map[string]any{{"sha256": fp.SHA256, "bytes": fp.Bytes}}})
		if e != nil {
			t.Fatal(e)
		}
		if e = os.WriteFile(path, b, 0600); e != nil {
			t.Fatal(e)
		}
	}
	write(now.Add(time.Hour))
	if !approvedLiveAudioEOF(root, digest, fp, "song.mp3", io.ErrUnexpectedEOF, now) {
		t.Fatal("exact owner approval rejected")
	}
	for _, tc := range []struct {
		digest, path string
		fp           usbcopy.FileFingerprint
		err          error
	}{
		{strings.Repeat("c", 64), "song.mp3", fp, io.ErrUnexpectedEOF},
		{digest, "song.wav", fp, io.ErrUnexpectedEOF},
		{digest, "song.mp3", usbcopy.FileFingerprint{SHA256: strings.Repeat("c", 64), Bytes: 100}, io.ErrUnexpectedEOF},
		{digest, "song.mp3", usbcopy.FileFingerprint{SHA256: fp.SHA256, Bytes: 101}, io.ErrUnexpectedEOF},
		{digest, "song.mp3", fp, errors.New("discontinuous MPEG stream")},
	} {
		if approvedLiveAudioEOF(root, tc.digest, tc.fp, tc.path, tc.err, now) {
			t.Fatal("approval scope expanded", tc)
		}
	}
	write(now.Add(-time.Second))
	if approvedLiveAudioEOF(root, digest, fp, "song.mp3", io.ErrUnexpectedEOF, now) {
		t.Fatal("expired approval accepted")
	}
	write(now.Add(25 * time.Hour))
	if approvedLiveAudioEOF(root, digest, fp, "song.mp3", io.ErrUnexpectedEOF, now) {
		t.Fatal("unbounded approval accepted")
	}
	write(now.Add(time.Hour))
	if e = os.Chmod(path, 0666); e != nil {
		t.Fatal(e)
	}
	if approvedLiveAudioEOF(root, digest, fp, "song.mp3", io.ErrUnexpectedEOF, now) {
		t.Fatal("writable approval accepted")
	}
}
func TestApprovedEOFStillRequiresUnchangedSourceBytes(t *testing.T) {
	root, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	source, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	frame := make([]byte, 144000*128/44100)
	copy(frame, []byte{255, 251, 144, 0})
	data := append(append(append([]byte{}, frame...), frame...), frame[:100]...)
	path := filepath.Join(source, "song.mp3")
	if e = os.WriteFile(path, data, 0600); e != nil {
		t.Fatal(e)
	}
	fp, e := usbcopy.FingerprintAudioFile(context.Background(), source, "song.mp3")
	if e != nil {
		t.Fatal(e)
	}
	digest := strings.Repeat("a", 64)
	dir := filepath.Join(root, "media-jobs", "approvals")
	if e = os.MkdirAll(dir, 0700); e != nil {
		t.Fatal(e)
	}
	if e = writeAtomicJSON(filepath.Join(dir, "partial-mp3-eof.json"), map[string]any{"sourceDigest": digest, "expiresAt": time.Now().Add(time.Hour), "files": []map[string]any{{"sha256": fp.SHA256, "bytes": fp.Bytes}}}); e != nil {
		t.Fatal(e)
	}
	tr := usbcopy.PlannedTrack{SourceRelative: "song.mp3", Bytes: fp.Bytes}
	if e = validateMediaLiveAudio(context.Background(), root, source, digest, tr, fp); e != nil {
		t.Fatal("exact approved original rejected", e)
	}
	data[20] ^= 1
	if e = os.WriteFile(path, data, 0600); e != nil {
		t.Fatal(e)
	}
	if e = validateMediaLiveAudio(context.Background(), root, source, digest, tr, fp); e == nil {
		t.Fatal("modified bytes accepted through old approval")
	}
}
