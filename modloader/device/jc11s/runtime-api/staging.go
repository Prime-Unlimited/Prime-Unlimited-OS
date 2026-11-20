package main

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"unicode/utf8"
)

type stageLimits struct {
	maxEntries    int
	maxDepth      int
	maxPathBytes  int
	maxFileBytes  uint64
	maxTotalBytes uint64
}

var defaultStageLimits = stageLimits{4096, 32, 240, 128 * 1024 * 1024, 256 * 1024 * 1024}

type stageResult struct {
	ID         string `json:"id"`
	Version    string `json:"version"`
	SHA256     string `json:"sha256"`
	FileCount  int    `json:"fileCount"`
	TotalBytes uint64 `json:"totalBytes"`
	Staged     bool   `json:"staged"`
}

type stagedFile struct {
	Path   string `json:"path"`
	Size   uint64 `json:"size"`
	SHA256 string `json:"sha256"`
}

type stageMetadata struct {
	stageResult
	Files []stagedFile `json:"files"`
}

type inspectionResult struct {
	CatalogID    string `json:"catalogId"`
	ID           string `json:"id"`
	Version      string `json:"version"`
	SHA256       string `json:"sha256"`
	Architecture string `json:"architecture"`
	ABI          string `json:"abi"`
	Entrypoint   string `json:"entrypoint"`
	Overlay      string `json:"overlay,omitempty"`
	Eligible     bool   `json:"eligible"`
}

const stageMarker = ".prime-unlimited-stage.json"

func secureDirectory(root string) error {
	if err := os.MkdirAll(root, 0o700); err != nil {
		return err
	}
	info, err := os.Lstat(root)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0o700 {
		return errors.New("staging root must be a real directory with mode 0700")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != uint32(os.Geteuid()) {
		return errors.New("staging root has wrong owner")
	}
	return nil
}

func safeArchivePath(name string, limits stageLimits) (string, error) {
	if name == "" || !utf8.ValidString(name) || strings.ContainsRune(name, 0) || strings.Contains(name, "\\") || strings.HasPrefix(name, "/") {
		return "", errors.New("unsafe archive path")
	}
	clean := path.Clean(name)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || clean != strings.TrimSuffix(name, "/") || len(clean) > limits.maxPathBytes {
		return "", errors.New("unsafe archive path")
	}
	first := strings.SplitN(clean, "/", 2)[0]
	if strings.Contains(first, ":") || strings.Count(clean, "/")+1 > limits.maxDepth || clean == stageMarker {
		return "", errors.New("unsafe archive path")
	}
	return clean, nil
}

func makePrivateDirectories(root, slashPath string) error {
	current := root
	if slashPath == "." || slashPath == "" {
		return nil
	}
	for _, component := range strings.Split(slashPath, "/") {
		current = filepath.Join(current, component)
		if err := os.Mkdir(current, 0o700); err != nil && !os.IsExist(err) {
			return err
		}
		info, err := os.Lstat(current)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("archive parent is not a real directory")
		}
		if err := os.Chmod(current, 0o700); err != nil {
			return err
		}
	}
	return nil
}

func stagePackage(item mod, cacheRoot, stagingRoot string, limits stageLimits) (stageResult, error) {
	result := stageResult{ID: item.ID, Version: item.Version, SHA256: item.SHA256}
	archivePath := filepath.Join(cacheRoot, item.SHA256+".zip")
	if !verifyCachedPackage(archivePath, item) {
		return result, errors.New("verified cached package unavailable")
	}
	if err := secureDirectory(stagingRoot); err != nil {
		return result, err
	}
	finalPath := filepath.Join(stagingRoot, item.SHA256)
	if existing, err := readValidStage(finalPath, result); err == nil {
		return existing, nil
	} else if _, statErr := os.Lstat(finalPath); statErr == nil {
		return result, errors.New("existing stage is invalid")
	} else if !os.IsNotExist(statErr) {
		return result, statErr
	}

	archive, err := os.Open(archivePath)
	if err != nil {
		return result, err
	}
	defer archive.Close()
	reader, err := zip.NewReader(archive, item.Size)
	if err != nil {
		return result, err
	}
	if len(reader.File) > limits.maxEntries {
		return result, errors.New("archive contains too many entries")
	}
	temporary, err := os.MkdirTemp(stagingRoot, ".stage-*")
	if err != nil {
		return result, err
	}
	defer os.RemoveAll(temporary)
	if err := os.Chmod(temporary, 0o700); err != nil {
		return result, err
	}
	seen := make(map[string]bool, len(reader.File)) // value is true for a directory
	files := make([]stagedFile, 0, len(reader.File))
	for _, entry := range reader.File {
		if entry.NonUTF8 || entry.Flags&1 != 0 {
			return result, errors.New("unsupported ZIP entry encoding or encryption")
		}
		name, err := safeArchivePath(entry.Name, limits)
		_, duplicate := seen[name]
		if err != nil || duplicate {
			return result, errors.New("unsafe or duplicate ZIP path")
		}
		for parent := path.Dir(name); parent != "."; parent = path.Dir(parent) {
			if directory, exists := seen[parent]; exists && !directory {
				return result, errors.New("file and parent path conflict")
			}
		}
		mode := entry.Mode()
		target := filepath.Join(temporary, filepath.FromSlash(name))
		if mode&(os.ModeSymlink|os.ModeDevice|os.ModeNamedPipe|os.ModeSocket|os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 {
			return result, errors.New("ZIP contains a special entry")
		}
		if entry.FileInfo().IsDir() {
			seen[name] = true
			if err := makePrivateDirectories(temporary, name); err != nil {
				return result, err
			}
			continue
		}
		for existing := range seen {
			if strings.HasPrefix(existing, name+"/") {
				return result, errors.New("file and child path conflict")
			}
		}
		seen[name] = false
		if !mode.IsRegular() {
			return result, errors.New("ZIP contains a non-regular entry")
		}
		if entry.UncompressedSize64 > limits.maxFileBytes || result.TotalBytes+entry.UncompressedSize64 > limits.maxTotalBytes {
			return result, errors.New("extracted size limit exceeded")
		}
		if err := makePrivateDirectories(temporary, path.Dir(name)); err != nil {
			return result, err
		}
		input, err := entry.Open()
		if err != nil {
			return result, err
		}
		output, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			input.Close()
			return result, err
		}
		digest := sha256.New()
		written, copyErr := io.Copy(io.MultiWriter(output, digest), io.LimitReader(input, int64(entry.UncompressedSize64)+1))
		closeInErr := input.Close()
		syncErr := output.Sync()
		closeOutErr := output.Close()
		if copyErr != nil || closeInErr != nil || syncErr != nil || closeOutErr != nil || uint64(written) != entry.UncompressedSize64 {
			return result, errors.New("ZIP entry failed integrity validation")
		}
		result.FileCount++
		result.TotalBytes += uint64(written)
		files = append(files, stagedFile{Path: name, Size: uint64(written), SHA256: hex.EncodeToString(digest.Sum(nil))})
	}
	result.Staged = true
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	markerData, err := json.Marshal(stageMetadata{stageResult: result, Files: files})
	if err != nil {
		return result, err
	}
	markerData = append(markerData, '\n')
	markerPath := filepath.Join(temporary, stageMarker)
	marker, err := os.OpenFile(markerPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return result, err
	}
	if _, err = marker.Write(markerData); err == nil {
		err = marker.Sync()
	}
	if closeErr := marker.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return result, err
	}
	dir, err := os.Open(temporary)
	if err != nil {
		return result, err
	}
	err = dir.Sync()
	_ = dir.Close()
	if err != nil {
		return result, err
	}
	if err := os.Rename(temporary, finalPath); err != nil {
		return result, err
	}
	root, err := os.Open(stagingRoot)
	if err != nil {
		return result, err
	}
	err = root.Sync()
	_ = root.Close()
	return result, err
}

func readValidStage(root string, expected stageResult) (stageResult, error) {
	var metadata stageMetadata
	data, err := readOwnedFile(filepath.Join(root, stageMarker), 2*1024*1024, "stage marker")
	if err != nil || decodeStrictJSON(data, &metadata) != nil || !metadata.Staged || metadata.ID != expected.ID || metadata.Version != expected.Version || metadata.SHA256 != expected.SHA256 || len(metadata.Files) != metadata.FileCount {
		return metadata.stageResult, errors.New("invalid stage marker")
	}
	expectedFiles := make(map[string]stagedFile, len(metadata.Files))
	for _, file := range metadata.Files {
		if _, exists := expectedFiles[file.Path]; exists || file.Path == "" || len(file.SHA256) != 64 {
			return metadata.stageResult, errors.New("invalid stage inventory")
		}
		expectedFiles[file.Path] = file
	}
	count := 0
	var total uint64
	err = filepath.Walk(root, func(current string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return errors.New("symlink in stage")
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || stat.Uid != uint32(os.Geteuid()) {
			return errors.New("stage path has wrong owner")
		}
		if info.IsDir() {
			if info.Mode().Perm() != 0o700 {
				return fmt.Errorf("unsafe directory mode")
			}
			return nil
		}
		if !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
			return errors.New("unsafe staged file")
		}
		if current != filepath.Join(root, stageMarker) {
			relative, relErr := filepath.Rel(root, current)
			if relErr != nil {
				return relErr
			}
			name := filepath.ToSlash(relative)
			expectedFile, exists := expectedFiles[name]
			if !exists || expectedFile.Size != uint64(info.Size()) {
				return errors.New("unexpected staged file")
			}
			file, openErr := os.Open(current)
			if openErr != nil {
				return openErr
			}
			digest := sha256.New()
			_, copyErr := io.Copy(digest, file)
			closeErr := file.Close()
			if copyErr != nil || closeErr != nil || hex.EncodeToString(digest.Sum(nil)) != expectedFile.SHA256 {
				return errors.New("staged file digest mismatch")
			}
			count++
			total += uint64(info.Size())
		}
		return nil
	})
	if err != nil || count != metadata.FileCount || total != metadata.TotalBytes {
		return metadata.stageResult, errors.New("stage inventory mismatch")
	}
	return metadata.stageResult, nil
}

func inspectStagedPackage(item mod, catalogID, cacheRoot, stagingRoot string) (inspectionResult, error) {
	result := inspectionResult{CatalogID: catalogID, ID: item.ID, Version: item.Version, SHA256: item.SHA256,
		Architecture: item.Architecture, ABI: item.ABI, Entrypoint: item.Entrypoint, Overlay: item.Overlay}
	if len(item.Libraries) != 0 {
		return result, errors.New("library installation is not available")
	}
	for _, dep := range item.Dependencies {
		if dep.Type != "" && dep.Type != "mod" {
			return result, errors.New("library installation is not available")
		}
	}
	if !verifyCachedPackage(filepath.Join(cacheRoot, item.SHA256+".zip"), item) {
		return result, errors.New("verified cache unavailable")
	}
	stageRoot := filepath.Join(stagingRoot, item.SHA256)
	if _, err := readValidStage(stageRoot, stageResult{ID: item.ID, Version: item.Version, SHA256: item.SHA256}); err != nil {
		return result, err
	}
	entrypoint := filepath.Join(stageRoot, filepath.FromSlash(item.Entrypoint))
	info, err := os.Lstat(entrypoint)
	if err != nil || validateOwnedRegularFile(info, 0o600) != nil {
		return result, errors.New("declared entrypoint is missing or unsafe")
	}
	if item.Overlay != "" {
		overlay := filepath.Join(stageRoot, filepath.FromSlash(item.Overlay))
		overlayInfo, overlayErr := os.Lstat(overlay)
		if overlayErr != nil || validateOwnedRegularFile(overlayInfo, 0o600) != nil {
			return result, errors.New("declared overlay is missing or unsafe")
		}
	}
	if item.Service != nil {
		probe := installedMod{Architecture: item.Architecture, Service: item.Service}
		if _, err := verifiedServiceBinary(stageRoot, probe); err != nil {
			return result, err
		}
	}
	result.Eligible = true
	return result, nil
}
