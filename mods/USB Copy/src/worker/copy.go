package usbcopy

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/sys/unix"
)

var ErrCopyCanceled = errors.New("USB copy canceled")

type CopyProgress struct {
	FilesDone  int   `json:"filesDone"`
	FilesTotal int   `json:"filesTotal"`
	BytesDone  int64 `json:"bytesDone"`
	BytesTotal int64 `json:"bytesTotal"`
}

type CopyResult struct {
	Directory   string `json:"directory"`
	FilesCopied int    `json:"filesCopied"`
	BytesCopied int64  `json:"bytesCopied"`
	Committed   bool   `json:"committed"`
}

// CopyImportPlan copies all planned audio into a hidden staging directory,
// then atomically publishes the complete import folder without replacing an
// existing destination. Roots must be resolved by the trusted runtime; this
// function must not receive paths supplied by a mod or QML.
func CopyImportPlan(sourceRoot, destinationMusicRoot, jobID string, plan ImportPlan, cancel <-chan struct{}, progress func(CopyProgress)) (CopyResult, error) {
	var result CopyResult
	if len(plan.Tracks) == 0 || plan.ImportDirectory == "" {
		return result, errors.New("import plan contains no tracks or target directory")
	}
	var plannedBytes int64
	for _, track := range plan.Tracks {
		if track.Bytes <= 0 || track.Bytes > math.MaxInt64-plannedBytes || !supportedAudioPath(track.SourceRelative) {
			return result, errors.New("import plan contains an invalid audio track")
		}
		plannedBytes += track.Bytes
	}
	if plannedBytes != plan.BytesToCopy {
		return result, errors.New("import plan byte total is inconsistent")
	}
	if !validJobID(jobID) {
		return result, errors.New("invalid copy job id")
	}
	resolvedSource, err := resolveExistingRoot(sourceRoot)
	if err != nil {
		return result, errors.New("source volume is unavailable")
	}
	sourceInfo, err := os.Stat(resolvedSource)
	if err != nil || !sourceInfo.IsDir() {
		return result, errors.New("source volume is unavailable")
	}
	destinationRoot, err := resolveExistingRoot(destinationMusicRoot)
	if err != nil {
		return result, errors.New("destination volume is unavailable")
	}
	destinationInfo, err := os.Stat(destinationRoot)
	if err != nil || !destinationInfo.IsDir() {
		return result, errors.New("destination music directory is unavailable")
	}
	importDirectory, err := cleanRelative(plan.ImportDirectory)
	if err != nil || filepath.Dir(filepath.FromSlash(importDirectory)) != "." {
		return result, errors.New("invalid import directory")
	}
	finalPath := filepath.Join(destinationRoot, filepath.FromSlash(importDirectory))
	if _, err := os.Lstat(finalPath); err == nil {
		return result, errors.New("import directory already exists")
	} else if !os.IsNotExist(err) {
		return result, errors.New("destination could not be checked")
	}
	stageName := ".usb-copy-staging-" + jobID
	stagePath := filepath.Join(destinationRoot, stageName)
	if err := os.Mkdir(stagePath, 0700); err != nil {
		return result, errors.New("could not create staging directory")
	}
	defer os.RemoveAll(stagePath)

	result.Directory = importDirectory
	result.FilesCopied = 0
	progressState := CopyProgress{FilesTotal: len(plan.Tracks), BytesTotal: plan.BytesToCopy}
	if progress != nil {
		progress(progressState)
	}
	for _, track := range plan.Tracks {
		if canceled(cancel) {
			return result, ErrCopyCanceled
		}
		sourceRelative, err := cleanRelative(track.SourceRelative)
		if err != nil {
			return result, errors.New("import plan contains an unsafe source path")
		}
		destinationRelative, err := cleanRelative(track.DestinationRelative)
		if err != nil || !strings.HasPrefix(destinationRelative, importDirectory+"/") {
			return result, errors.New("import plan contains an unsafe destination path")
		}
		insideDestination := strings.TrimPrefix(destinationRelative, importDirectory+"/")
		if !supportedAudioPath(insideDestination) {
			return result, errors.New("import plan contains an unsupported destination file")
		}
		if err := copyPlannedTrack(resolvedSource, stagePath, sourceRelative, insideDestination, track, cancel, func(bytes int64) {
			progressState.BytesDone += bytes
			if progress != nil {
				progress(progressState)
			}
		}); err != nil {
			return result, err
		}
		result.FilesCopied++
		result.BytesCopied += track.Bytes
		progressState.FilesDone = result.FilesCopied
		if progress != nil {
			progress(progressState)
		}
	}
	if canceled(cancel) {
		return result, ErrCopyCanceled
	}
	if err := syncDirectoryTree(stagePath); err != nil {
		return result, fmt.Errorf("sync staged import: %w", err)
	}
	if canceled(cancel) {
		return result, ErrCopyCanceled
	}
	if err := os.Chmod(stagePath, 0755); err != nil {
		return result, errors.New("could not prepare completed import folder")
	}
	if err := syncDirectory(stagePath); err != nil {
		return result, fmt.Errorf("sync import folder metadata: %w", err)
	}
	if err := publishImportDirectory(stagePath, finalPath); err != nil {
		return result, fmt.Errorf("publish import folder without overwrite: %w", err)
	}
	result.Committed = true
	if err := syncDirectory(destinationRoot); err != nil {
		return result, fmt.Errorf("import committed but parent sync failed: %w", err)
	}
	return result, nil
}

// CopyImportFiles copies audio files into a hidden, per-job staging directory
// and returns that unpublished path. The Engine importer can validate and
// transact its metadata before PublishImportFiles makes the directory visible.
// A failed caller must remove stagingPath with DiscardImportFiles.
func CopyImportFiles(sourceRoot, destinationMusicRoot, jobID string, plan ImportPlan, cancel <-chan struct{}, progress func(CopyProgress)) (CopyResult, string, error) {
	var result CopyResult
	if len(plan.Tracks) == 0 || !validJobID(jobID) {
		return result, "", errors.New("invalid import job")
	}
	var plannedBytes int64
	for _, track := range plan.Tracks {
		if track.Bytes <= 0 || track.Bytes > math.MaxInt64-plannedBytes || !supportedAudioPath(track.SourceRelative) {
			return result, "", errors.New("import plan contains an invalid audio track")
		}
		plannedBytes += track.Bytes
	}
	if plannedBytes != plan.BytesToCopy {
		return result, "", errors.New("import plan byte total is inconsistent")
	}
	sourceRoot, err := resolveExistingRoot(sourceRoot)
	if err != nil {
		return result, "", errors.New("source volume is unavailable")
	}
	destinationRoot, err := resolveExistingRoot(destinationMusicRoot)
	if err != nil {
		return result, "", errors.New("destination music directory is unavailable")
	}
	importDirectory, err := cleanRelative(plan.ImportDirectory)
	if err != nil || filepath.Dir(filepath.FromSlash(importDirectory)) != "." {
		return result, "", errors.New("invalid import directory")
	}
	stageName := ".usb-copy-staging-" + jobID
	stagePath := filepath.Join(destinationRoot, stageName)
	if err := os.Mkdir(stagePath, 0700); err != nil {
		return result, "", errors.New("could not create staging directory")
	}
	keep := false
	defer func() {
		if !keep {
			_ = os.RemoveAll(stagePath)
		}
	}()
	progressState := CopyProgress{FilesTotal: len(plan.Tracks), BytesTotal: plan.BytesToCopy}
	if progress != nil {
		progress(progressState)
	}
	for _, track := range plan.Tracks {
		if canceled(cancel) {
			return result, "", ErrCopyCanceled
		}
		sourceRelative, err := cleanRelative(track.SourceRelative)
		if err != nil {
			return result, "", errors.New("import plan contains an unsafe source path")
		}
		destinationRelative, err := cleanRelative(track.DestinationRelative)
		if err != nil || !strings.HasPrefix(destinationRelative, importDirectory+"/") {
			return result, "", errors.New("import plan contains an unsafe destination path")
		}
		insideDestination := strings.TrimPrefix(destinationRelative, importDirectory+"/")
		if !supportedAudioPath(insideDestination) {
			return result, "", errors.New("import plan contains an unsupported destination file")
		}
		if err := copyPlannedTrack(sourceRoot, stagePath, sourceRelative, insideDestination, track, cancel, func(bytes int64) {
			progressState.BytesDone += bytes
			if progress != nil {
				progress(progressState)
			}
		}); err != nil {
			return result, "", err
		}
		result.FilesCopied++
		result.BytesCopied += track.Bytes
		progressState.FilesDone = result.FilesCopied
		if progress != nil {
			progress(progressState)
		}
	}
	if canceled(cancel) {
		return result, "", ErrCopyCanceled
	}
	if err := syncDirectoryTree(stagePath); err != nil {
		return result, "", fmt.Errorf("sync staged import: %w", err)
	}
	keep = true
	result.Directory = importDirectory
	return result, stagePath, nil
}

// PublishImportFiles atomically publishes a staged directory without replacing
// an existing import, after its Engine database transaction has committed.
func PublishImportFiles(destinationMusicRoot, jobID string, stagePath string, plan ImportPlan) (CopyResult, error) {
	var result CopyResult
	if !validJobID(jobID) {
		return result, errors.New("invalid import job")
	}
	root, err := resolveExistingRoot(destinationMusicRoot)
	if err != nil {
		return result, errors.New("destination music directory is unavailable")
	}
	expectedStage := filepath.Join(root, ".usb-copy-staging-"+jobID)
	if filepath.Clean(stagePath) != expectedStage {
		return result, errors.New("staging directory does not match import job")
	}
	info, err := os.Lstat(expectedStage)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return result, errors.New("staged import is unavailable")
	}
	importDirectory, err := cleanRelative(plan.ImportDirectory)
	if err != nil || filepath.Dir(filepath.FromSlash(importDirectory)) != "." {
		return result, errors.New("invalid import directory")
	}
	finalPath := filepath.Join(root, filepath.FromSlash(importDirectory))
	if err := os.Chmod(expectedStage, 0755); err != nil {
		return result, errors.New("could not prepare staged import")
	}
	if err := syncDirectory(expectedStage); err != nil {
		return result, errors.New("could not sync staged import")
	}
	if err := publishImportDirectory(expectedStage, finalPath); err != nil {
		return result, fmt.Errorf("publish import folder without overwrite: %w", err)
	}
	result.Directory, result.FilesCopied, result.BytesCopied, result.Committed = importDirectory, len(plan.Tracks), plan.BytesToCopy, true
	if err := syncDirectory(root); err != nil {
		return result, fmt.Errorf("import committed but parent sync failed: %w", err)
	}
	return result, nil
}

// DiscardImportFiles removes an unpublished staging directory after validating
// its location and per-job name.
func DiscardImportFiles(destinationMusicRoot, jobID string) error {
	if !validJobID(jobID) {
		return errors.New("invalid import job")
	}
	root, err := resolveExistingRoot(destinationMusicRoot)
	if err != nil {
		return errors.New("destination music directory is unavailable")
	}
	stagePath := filepath.Join(root, ".usb-copy-staging-"+jobID)
	info, err := os.Lstat(stagePath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("staging directory is unsafe")
	}
	return os.RemoveAll(stagePath)
}

func validJobID(value string) bool {
	if len(value) < 16 || len(value) > 64 {
		return false
	}
	for _, char := range value {
		if !((char >= '0' && char <= '9') || (char >= 'a' && char <= 'f')) {
			return false
		}
	}
	return true
}

func supportedAudioPath(value string) bool {
	cleaned, err := cleanRelative(value)
	if err != nil {
		return false
	}
	_, ok := audioExtensions[strings.ToLower(filepath.Ext(cleaned))]
	return ok
}

func NewCopyJobID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw[:]), nil
}

func copyPlannedTrack(sourceRoot, stageRoot, sourceRelative, destinationRelative string, track PlannedTrack, cancel <-chan struct{}, onBytes func(int64)) error {
	source, err := openSourceNoFollow(sourceRoot, sourceRelative)
	if err != nil {
		return errors.New("source track is unavailable or unsafe")
	}
	defer source.Close()
	before, err := source.Stat()
	if err != nil || !before.Mode().IsRegular() || before.Size() != track.Bytes || (track.SourceModifiedAt != 0 && before.ModTime().UnixNano() != track.SourceModifiedAt) {
		return errors.New("source track changed after planning")
	}
	destinationPath, err := createStagedFile(stageRoot, destinationRelative)
	if err != nil {
		return err
	}
	destination, err := os.OpenFile(destinationPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		return errors.New("could not create staged track")
	}
	keep := false
	defer func() {
		_ = destination.Close()
		if !keep {
			_ = os.Remove(destinationPath)
		}
	}()
	writer := io.Writer(destination)
	buffer := make([]byte, 256*1024)
	var copied int64
	for {
		if canceled(cancel) {
			return ErrCopyCanceled
		}
		read, readErr := source.Read(buffer)
		if read > 0 {
			written, writeErr := writer.Write(buffer[:read])
			copied += int64(written)
			if onBytes != nil && written > 0 {
				onBytes(int64(written))
			}
			if writeErr != nil {
				return errors.New("staged track write failed")
			}
			if written != read {
				return io.ErrShortWrite
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return errors.New("source track read failed")
		}
	}
	after, err := source.Stat()
	if err != nil || copied != track.Bytes || after.Size() != before.Size() || !after.ModTime().Equal(before.ModTime()) {
		return errors.New("source track changed during copy")
	}
	if err := destination.Sync(); err != nil {
		return errors.New("staged track sync failed")
	}
	if err := destination.Close(); err != nil {
		return errors.New("staged track close failed")
	}
	keep = true
	return nil
}

func openSourceNoFollow(root, relative string) (*os.File, error) {
	cleaned, err := cleanRelative(relative)
	if err != nil {
		return nil, err
	}
	rootFD, err := unix.Open(root, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	fd := rootFD
	parts := strings.Split(filepath.FromSlash(cleaned), string(filepath.Separator))
	for _, component := range parts[:len(parts)-1] {
		nextFD, openErr := unix.Openat(fd, component, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
		if fd != rootFD {
			_ = unix.Close(fd)
		}
		if openErr != nil {
			_ = unix.Close(rootFD)
			return nil, openErr
		}
		fd = nextFD
	}
	fileFD, err := unix.Openat(fd, parts[len(parts)-1], unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if fd != rootFD {
		_ = unix.Close(fd)
	}
	_ = unix.Close(rootFD)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fileFD), cleaned), nil
}

func createStagedFile(stageRoot, relative string) (string, error) {
	cleaned, err := cleanRelative(relative)
	if err != nil {
		return "", errors.New("invalid staged path")
	}
	parts := strings.Split(filepath.FromSlash(cleaned), string(filepath.Separator))
	current := stageRoot
	for _, component := range parts[:len(parts)-1] {
		current = filepath.Join(current, component)
		if err := os.Mkdir(current, 0755); err != nil && !os.IsExist(err) {
			return "", errors.New("could not create staged directory")
		}
		info, err := os.Lstat(current)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return "", errors.New("staged directory is unsafe")
		}
	}
	return filepath.Join(current, parts[len(parts)-1]), nil
}

func syncDirectoryTree(root string) error {
	directories := make([]string, 0)
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return errors.New("staging tree contains a symlink")
		}
		if entry.IsDir() {
			directories = append(directories, path)
		}
		return nil
	})
	if err != nil {
		return err
	}
	sort.Slice(directories, func(i, j int) bool { return len(directories[i]) > len(directories[j]) })
	for _, directory := range directories {
		if err := syncDirectory(directory); err != nil {
			return err
		}
	}
	return nil
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}

func canceled(cancel <-chan struct{}) bool {
	if cancel == nil {
		return false
	}
	select {
	case <-cancel:
		return true
	default:
		return false
	}
}
