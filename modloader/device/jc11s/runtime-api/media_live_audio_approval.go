package main

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	usbcopy "prime-unlimited.usb-copy/worker"
	"strings"
	"time"
)

type mediaLiveAudioApproval struct {
	SourceDigest string    `json:"sourceDigest"`
	ExpiresAt    time.Time `json:"expiresAt"`
	Files        []struct {
		SHA256 string `json:"sha256"`
		Bytes  int64  `json:"bytes"`
	} `json:"files"`
}

// Root-owned, time-limited approval for exact files independently checked and
// explicitly accepted by the owner. It is not supplied by QML or an HTTP body.
// Only an otherwise rejected partial MP3 EOF can be approved; all copying stays
// byte-identical and Engine remains responsible for decoding and analysis.
func approvedLiveAudioEOF(root, digest string, fp usbcopy.FileFingerprint, relative string, validation error, now time.Time) bool {
	if !errors.Is(validation, io.ErrUnexpectedEOF) || !strings.EqualFold(filepath.Ext(relative), ".mp3") || !validMediaEventID(digest) || !validMediaEventID(fp.SHA256) || fp.Bytes <= 0 {
		return false
	}
	data, e := readOwnedFile(filepath.Join(root, "media-jobs", "approvals", "partial-mp3-eof.json"), 4096, "owner audio approval")
	if e != nil {
		return false
	}
	var a mediaLiveAudioApproval
	if decodeStrictJSON(data, &a) != nil || a.SourceDigest != digest || !now.Before(a.ExpiresAt) || a.ExpiresAt.Sub(now) > 24*time.Hour || len(a.Files) == 0 || len(a.Files) > 16 {
		return false
	}
	seen := map[string]bool{}
	matched := false
	for _, file := range a.Files {
		if !validMediaEventID(file.SHA256) || file.Bytes <= 0 || seen[file.SHA256] {
			return false
		}
		seen[file.SHA256] = true
		if file.SHA256 == fp.SHA256 && file.Bytes == fp.Bytes {
			matched = true
		}
	}
	return matched
}
func validateMediaLiveAudio(ctx context.Context, root, source, digest string, t usbcopy.PlannedTrack, fp usbcopy.FileFingerprint) error {
	e := usbcopy.ValidateLiveAudioFile(ctx, source, t.SourceRelative)
	if e == nil {
		return nil
	}
	if !approvedLiveAudioEOF(root, digest, fp, t.SourceRelative, e, time.Now()) {
		return e
	}
	// Recheck the exact approved bytes after the rejected decoder read. The
	// normal pre-copy and post-copy hash checks still run without exceptions.
	current, err := usbcopy.FingerprintAudioFile(ctx, source, t.SourceRelative)
	if err != nil {
		return err
	}
	if current != fp {
		return errors.New("approved audio bytes changed")
	}
	return nil
}
