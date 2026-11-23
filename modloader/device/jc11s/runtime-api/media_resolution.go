package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

type mediaResolution struct {
	BootID           string    `json:"bootId"`
	MountKey         string    `json:"mountKey"`
	MountPath        string    `json:"mountPath"`
	EventID          string    `json:"eventId"`
	ResolvedAt       time.Time `json:"resolvedAt"`
	DeviceGeneration string    `json:"deviceGeneration,omitempty"`
}

func mediaResolutionPath(root, boot, key, path string, generation ...string) (string, error) {
	if len(generation) > 1 {
		return "", errors.New("invalid device generation scope")
	}
	gen := ""
	if len(generation) == 1 {
		gen = generation[0]
	}
	if gen != "" {
		n, e := strconv.ParseUint(gen, 10, 64)
		if e != nil || n == 0 || strconv.FormatUint(n, 10) != gen {
			return "", errors.New("invalid device generation")
		}
	}
	if !filepath.IsAbs(root) || len(boot) != 36 || !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return "", errors.New("invalid media resolution scope")
	}
	if _, _, ok := splitMediaMountKey(key); !ok {
		return "", errors.New("invalid resolution mount")
	}
	scope := boot + "\x00" + key + "\x00" + path
	if gen != "" {
		scope += "\x00diskseq:" + gen
	}
	hash := sha256.Sum256([]byte(scope))
	return filepath.Join(root, "media-decisions", hex.EncodeToString(hash[:])+".json"), nil
}
func writeMediaResolution(root, boot string, event mediaInsertEvent) error {
	path, err := mediaResolutionPath(root, boot, event.mountKey, event.mountPath, event.deviceGeneration)
	if err != nil {
		return err
	}
	if !validMediaEventID(event.ID) {
		return errors.New("invalid resolved event")
	}
	if err = ensurePrivateDirectory(filepath.Dir(path)); err != nil {
		return err
	}
	return writeAtomicJSON(path, mediaResolution{DeviceGeneration: event.deviceGeneration, BootID: boot, MountKey: event.mountKey, MountPath: event.mountPath, EventID: event.ID, ResolvedAt: time.Now().UTC()})
}
func readMediaResolution(root, boot, key, mountPath string, generation ...string) (string, bool, error) {
	path, err := mediaResolutionPath(root, boot, key, mountPath, generation...)
	if err != nil {
		return "", false, err
	}
	data, err := readOwnedFile(path, 4096, "media resolution")
	if os.IsNotExist(err) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	var record mediaResolution
	gen := ""
	if len(generation) == 1 {
		gen = generation[0]
	}
	if decodeStrictJSON(data, &record) != nil || record.DeviceGeneration != gen || record.BootID != boot || record.MountKey != key || record.MountPath != mountPath || !validMediaEventID(record.EventID) || record.ResolvedAt.IsZero() {
		return "", false, errors.New("invalid media resolution record")
	}
	return record.EventID, true, nil
}
func (s *server) rememberMediaResolution(event mediaInsertEvent) error {
	// Test-only in-memory servers have no absolute runtime root.
	if !filepath.IsAbs(s.root) {
		return nil
	}
	boot, err := mediaBootID()
	if err != nil {
		return err
	}
	return writeMediaResolution(s.root, boot, event)
}
func (s *server) restoreMediaResolution(event mediaInsertEvent) (mediaInsertEvent, error) {
	if !filepath.IsAbs(s.root) {
		return event, nil
	}
	boot, err := mediaBootID()
	if err != nil {
		return event, err
	}
	id, resolved, err := readMediaResolution(s.root, boot, event.mountKey, event.mountPath, event.deviceGeneration)
	if err != nil {
		return event, err
	}
	if resolved {
		event.ID = id
		event.Resolved = true
	}
	return event, nil
}
