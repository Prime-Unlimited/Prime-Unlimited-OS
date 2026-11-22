package usbcopy

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type PlannedTrack struct {
	SourceRelative      string   `json:"sourceRelative"`
	DestinationRelative string   `json:"destinationRelative"`
	Bytes               int64    `json:"bytes"`
	SourceModifiedAt    int64    `json:"sourceModifiedAt"`
	Tags                Tags     `json:"tags"`
	Artwork             []byte   `json:"artwork,omitempty"`
	SourcePlaylists     []string `json:"sourcePlaylists,omitempty"`
	Warnings            []string `json:"warnings,omitempty"`
}

type PlannedPlaylist struct {
	Name      string   `json:"name"`
	Source    string   `json:"source,omitempty"`
	Tracks    []string `json:"tracks"`
	Missing   int      `json:"missing"`
	Generated bool     `json:"generated,omitempty"`
}

type ImportPlan struct {
	VolumeLabel      string            `json:"volumeLabel"`
	ImportDirectory  string            `json:"importDirectory"`
	Tracks           []PlannedTrack    `json:"tracks"`
	SourcePlaylists  []PlannedPlaylist `json:"sourcePlaylists,omitempty"`
	CopyPlaylist     PlannedPlaylist   `json:"copyPlaylist"`
	BytesToCopy      int64             `json:"bytesToCopy"`
	DestinationFiles int               `json:"destinationFiles"`
	Warnings         []string          `json:"warnings,omitempty"`
}

// BuildImportPlan resolves source tracks into a destination music directory
// without creating files or changing an Engine database. Existing destination
// paths are preserved: a conflicting track gets a new numbered filename.
// sourceRoot and destinationMusicRoot must be trusted runtime-resolved roots,
// never paths supplied by a mod package or directly by the UI.
func BuildImportPlan(sourceRoot, destinationMusicRoot, volumeLabel string, copiedAt time.Time, snapshot Snapshot, existingPlaylistNames []string) (ImportPlan, error) {
	plan := ImportPlan{
		VolumeLabel: volumeLabel,
		Tracks:      make([]PlannedTrack, 0, len(snapshot.Tracks)),
		Warnings:    append([]string(nil), snapshot.Warnings...),
	}
	resolvedSource, err := filepath.EvalSymlinks(sourceRoot)
	if err != nil {
		return plan, fmt.Errorf("resolve source volume: %w", err)
	}
	sourceInfo, err := os.Stat(resolvedSource)
	if err != nil || !sourceInfo.IsDir() {
		return plan, errors.New("source volume is not a directory")
	}
	destinationRoot, err := resolveExistingRoot(destinationMusicRoot)
	if err != nil {
		return plan, fmt.Errorf("resolve destination music directory: %w", err)
	}
	existingPaths, err := indexDestination(destinationRoot)
	if err != nil {
		return plan, err
	}
	importDirectoryName := fmt.Sprintf("USB Copy - %s - %s", cleanImportLabel(volumeLabel), copiedAt.Format("2006-01-02"))
	plan.ImportDirectory, err = allocateDestination(importDirectoryName, existingPaths)
	if err != nil {
		return plan, fmt.Errorf("allocate import directory: %w", err)
	}
	existingPaths[pathKey(plan.ImportDirectory)] = true

	destinationBySource := make(map[string]string, len(snapshot.Tracks))
	sortedTracks := append([]Track(nil), snapshot.Tracks...)
	sort.Slice(sortedTracks, func(i, j int) bool {
		return strings.ToLower(sortedTracks[i].RelativePath) < strings.ToLower(sortedTracks[j].RelativePath)
	})
	for _, track := range sortedTracks {
		sourceRelative, err := cleanRelative(track.RelativePath)
		if err != nil {
			plan.Warnings = append(plan.Warnings, "an unsafe source path was omitted")
			continue
		}
		sourcePath := filepath.Join(resolvedSource, filepath.FromSlash(sourceRelative))
		if !noSymlinkPath(resolvedSource, sourcePath) {
			plan.Warnings = append(plan.Warnings, "a source file changed or became unsafe after scanning")
			continue
		}
		info, err := os.Stat(sourcePath)
		if err != nil || !info.Mode().IsRegular() || info.Size() != track.Bytes || (track.ModifiedAt != 0 && info.ModTime().UnixNano() != track.ModifiedAt) {
			plan.Warnings = append(plan.Warnings, "a source file changed after scanning")
			continue
		}
		destinationPath := filepath.ToSlash(filepath.Join(filepath.FromSlash(plan.ImportDirectory), filepath.FromSlash(sourceRelative)))
		destinationRelative, err := allocateDestination(destinationPath, existingPaths)
		if err != nil {
			plan.Warnings = append(plan.Warnings, "a destination path could not be allocated")
			continue
		}
		planned := PlannedTrack{
			SourceRelative: sourceRelative, DestinationRelative: destinationRelative,
			Bytes: info.Size(), SourceModifiedAt: track.ModifiedAt, Tags: track.Tags,
			Artwork:         append([]byte(nil), track.Artwork...),
			SourcePlaylists: append([]string(nil), track.Playlists...),
			Warnings:        append([]string(nil), track.Warnings...),
		}
		plan.Tracks = append(plan.Tracks, planned)
		destinationBySource[filepath.Clean(filepath.FromSlash(sourceRelative))] = destinationRelative
		plan.BytesToCopy += info.Size()
	}
	plan.DestinationFiles = len(plan.Tracks)

	playlistNames := append([]string(nil), existingPlaylistNames...)
	for _, sourcePlaylist := range snapshot.Playlists {
		members := make([]string, 0, len(sourcePlaylist.Tracks))
		missing := sourcePlaylist.Missing
		for _, member := range sourcePlaylist.Tracks {
			cleaned, err := cleanRelative(member)
			if err != nil {
				missing++
				continue
			}
			if destination, ok := destinationBySource[filepath.Clean(filepath.FromSlash(cleaned))]; ok {
				members = append(members, destination)
			} else {
				missing++
			}
		}
		if len(members) == 0 {
			continue
		}
		name := uniquePlaylistName(sourcePlaylist.Name, playlistNames)
		playlistNames = append(playlistNames, name)
		plan.SourcePlaylists = append(plan.SourcePlaylists, PlannedPlaylist{
			Name: name, Source: sourcePlaylist.RelativePath, Tracks: members,
			Missing: missing,
		})
	}

	orderedPaths := orderDestinationTracks(snapshot, destinationBySource)
	copyName := BuildCopyPlaylist(volumeLabel, copiedAt, []Track{}, playlistNames).Name
	plan.CopyPlaylist = PlannedPlaylist{Name: copyName, Tracks: orderedPaths, Generated: true}
	return plan, nil
}

func cleanImportLabel(value string) string {
	label := cleanPlaylistLabel(value)
	if label == "" {
		return "USB drive"
	}
	return label
}

func resolveExistingRoot(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	volume := filepath.VolumeName(absolute)
	current := volume + string(filepath.Separator)
	remainder := strings.TrimPrefix(absolute, current)
	for _, component := range strings.Split(remainder, string(filepath.Separator)) {
		if component == "" {
			continue
		}
		current = filepath.Join(current, component)
		info, statErr := os.Lstat(current)
		if os.IsNotExist(statErr) {
			continue
		}
		if statErr != nil {
			return "", statErr
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return "", errors.New("destination path contains a symlink or non-directory")
		}
	}
	return filepath.Clean(absolute), nil
}

func indexDestination(root string) (map[string]bool, error) {
	paths := make(map[string]bool)
	// A unique top-level import directory cannot collide with existing descendants.
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return paths, nil
	}
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if entry.Type()&os.ModeSymlink != 0 {
			return nil, errors.New("destination contains a symlink")
		}
		paths[pathKey(entry.Name())] = entry.IsDir()
	}
	return paths, nil
}

func allocateDestination(sourceRelative string, existing map[string]bool) (string, error) {
	cleaned, err := cleanRelative(sourceRelative)
	if err != nil {
		return "", err
	}
	parent := filepath.Dir(filepath.FromSlash(cleaned))
	for current := parent; current != "." && current != ""; current = filepath.Dir(current) {
		if isDirectory, ok := existing[pathKey(filepath.ToSlash(current))]; ok && !isDirectory {
			return "", errors.New("destination parent is occupied by a file")
		}
	}
	if _, ok := existing[pathKey(cleaned)]; !ok {
		existing[pathKey(cleaned)] = false
		return cleaned, nil
	}
	dir, filename := pathSplit(cleaned)
	ext := filepath.Ext(filename)
	stem := strings.TrimSuffix(filename, ext)
	for number := 2; number <= 10000; number++ {
		candidateName := fmt.Sprintf("%s (USB Copy %d)%s", stem, number, ext)
		candidate := candidateName
		if dir != "" {
			candidate = dir + "/" + candidateName
		}
		if _, exists := existing[pathKey(candidate)]; exists {
			continue
		}
		existing[pathKey(candidate)] = false
		return candidate, nil
	}
	return "", errors.New("too many destination name conflicts")
}

func orderDestinationTracks(snapshot Snapshot, destinationBySource map[string]string) []string {
	ordered := make([]string, 0, len(destinationBySource))
	seen := make(map[string]struct{}, len(destinationBySource))
	appendSource := func(relative string) {
		cleaned, err := cleanRelative(relative)
		if err != nil {
			return
		}
		key := filepath.Clean(filepath.FromSlash(cleaned))
		if destination, ok := destinationBySource[key]; ok {
			if _, already := seen[key]; !already {
				seen[key] = struct{}{}
				ordered = append(ordered, destination)
			}
		}
	}
	for _, playlist := range snapshot.Playlists {
		for _, member := range playlist.Tracks {
			appendSource(member)
		}
	}
	remaining := make([]string, 0, len(destinationBySource)-len(seen))
	for key := range destinationBySource {
		if _, ok := seen[key]; !ok {
			remaining = append(remaining, key)
		}
	}
	sort.Slice(remaining, func(i, j int) bool { return strings.ToLower(remaining[i]) < strings.ToLower(remaining[j]) })
	for _, key := range remaining {
		appendSource(filepath.ToSlash(key))
	}
	return ordered
}

func cleanRelative(value string) (string, error) {
	if strings.ContainsRune(value, '\x00') {
		return "", errors.New("path contains a NUL byte")
	}
	value = filepath.ToSlash(value)
	if !safeRelative(filepath.FromSlash(value)) {
		return "", errors.New("path is not relative to its media root")
	}
	return filepath.ToSlash(filepath.Clean(filepath.FromSlash(value))), nil
}

func pathKey(value string) string {
	return strings.ToLower(filepath.ToSlash(filepath.Clean(filepath.FromSlash(value))))
}

func pathSplit(value string) (string, string) {
	directory, filename := filepath.Split(filepath.FromSlash(value))
	return filepath.ToSlash(filepath.Clean(directory)), filename
}

func uniquePlaylistName(candidate string, existing []string) string {
	base := strings.TrimSpace(strings.ToValidUTF8(candidate, "�"))
	if base == "" {
		base = "USB Playlist"
	}
	if !containsName(existing, base) {
		return base
	}
	for number := 2; number <= 10000; number++ {
		name := fmt.Sprintf("%s (USB Copy %d)", base, number)
		if !containsName(existing, name) {
			return name
		}
	}
	return fmt.Sprintf("USB Playlist %d", time.Now().Unix())
}
