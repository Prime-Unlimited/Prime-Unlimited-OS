package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"golang.org/x/sys/unix"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
)

type mediaDestinationIdentity struct {
	FilesystemUUID string `json:"filesystemUuid"`
	LibraryUUID    string `json:"libraryUuid"`
}

var mediaFilesystemUUIDPattern = regexp.MustCompile(`^(?:[0-9a-f]{4}-[0-9a-f]{4}|[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})$`)
var mediaLibraryUUIDPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func (i mediaDestinationIdentity) valid() bool {
	return mediaFilesystemUUIDPattern.MatchString(i.FilesystemUUID) && mediaLibraryUUIDPattern.MatchString(i.LibraryUUID)
}
func (i mediaDestinationIdentity) empty() bool { return i == mediaDestinationIdentity{} }

// Resolve aliases only; never read arbitrary block-device contents.
func filesystemUUIDForDevice(directory, source string) (string, error) {
	real, err := filepath.EvalSymlinks(source)
	if err != nil {
		return "", err
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		return "", err
	}
	found := ""
	for _, entry := range entries {
		name := strings.ToLower(entry.Name())
		if !mediaFilesystemUUIDPattern.MatchString(name) || entry.Type()&os.ModeSymlink == 0 {
			continue
		}
		target, e := filepath.EvalSymlinks(filepath.Join(directory, entry.Name()))
		if e != nil || target != real {
			continue
		}
		if found != "" {
			return "", errors.New("ambiguous filesystem identity")
		}
		found = name
	}
	if found == "" {
		return "", errors.New("filesystem identity unavailable")
	}
	return found, nil
}
func readMediaDestinationIdentity(ctx context.Context, volume mountedBlockVolume) (mediaDestinationIdentity, error) {
	var identity mediaDestinationIdentity
	if !isInternalMediaDestination(volume) {
		return identity, errors.New("destination is not internal")
	}
	node, err := os.Stat(volume.source)
	if err != nil {
		return identity, err
	}
	stat, ok := node.Sys().(*syscall.Stat_t)
	if !ok || node.Mode()&os.ModeDevice == 0 || node.Mode()&os.ModeCharDevice != 0 || fmt.Sprintf("%d:%d", unix.Major(uint64(stat.Rdev)), unix.Minor(uint64(stat.Rdev))) != volume.deviceID {
		return identity, errors.New("destination device identity mismatch")
	}
	identity.FilesystemUUID, err = filesystemUUIDForDevice("/dev/disk/by-uuid", volume.source)
	if err != nil {
		return identity, err
	}
	database, err := internalEngineDatabase(volume.mountPath)
	if err != nil {
		return identity, err
	}
	uri := (&url.URL{Scheme: "file", Path: database, RawQuery: "mode=ro"}).String()
	db, err := sql.Open("sqlite", uri)
	if err != nil {
		return identity, err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if _, err = db.ExecContext(ctx, "PRAGMA query_only=ON"); err != nil {
		return identity, err
	}
	if err = db.QueryRowContext(ctx, "SELECT uuid FROM Information ORDER BY id LIMIT 1").Scan(&identity.LibraryUUID); err != nil {
		return identity, err
	}
	identity.LibraryUUID = strings.ToLower(identity.LibraryUUID)
	if !identity.valid() {
		return identity, errors.New("library identity is invalid")
	}
	return identity, nil
}

func readMediaIdentityForDestination(ctx context.Context, key string) (mediaDestinationIdentity, error) {
	volumes, err := readMountedBlockVolumes()
	if err != nil {
		return mediaDestinationIdentity{}, err
	}
	volume, ok := findVolumeByKey(volumes, key)
	if !ok {
		return mediaDestinationIdentity{}, errors.New("destination disappeared")
	}
	return readMediaDestinationIdentity(ctx, volume)
}
