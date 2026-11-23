package usbcopy

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/dhowden/tag"
)

const (
	maxPlaylistLine = 1 << 20
	maxArtworkBytes = 10 << 20
)

var audioExtensions = map[string]struct{}{
	".aac": {}, ".aif": {}, ".aiff": {}, ".alac": {}, ".flac": {},
	".m4a": {}, ".mp3": {}, ".mp4": {}, ".ogg": {}, ".wav": {},
}

// Track describes a source audio file without modifying it. RelativePath is
// slash-separated so the same scan result can be used on both device targets.
type Track struct {
	RelativePath    string   `json:"relativePath"`
	Bytes           int64    `json:"bytes"`
	ModifiedAt      int64    `json:"modifiedAt"`
	Tags            Tags     `json:"tags"`
	Artwork         []byte   `json:"artwork,omitempty"`
	ArtworkHash     []byte   `json:"artworkHash,omitempty"`
	ArtworkFilename string   `json:"artworkFilename,omitempty"`
	Playlists       []string `json:"playlists,omitempty"`
	Warnings        []string `json:"warnings,omitempty"`
}

type Tags struct {
	Format      string `json:"format,omitempty"`
	Title       string `json:"title,omitempty"`
	Artist      string `json:"artist,omitempty"`
	AlbumArtist string `json:"albumArtist,omitempty"`
	Album       string `json:"album,omitempty"`
	Genre       string `json:"genre,omitempty"`
	Composer    string `json:"composer,omitempty"`
	Comment     string `json:"comment,omitempty"`
	Year        int    `json:"year,omitempty"`
	Track       int    `json:"track,omitempty"`
	TrackTotal  int    `json:"trackTotal,omitempty"`
	Disc        int    `json:"disc,omitempty"`
	DiscTotal   int    `json:"discTotal,omitempty"`
}

type Playlist struct {
	Name         string   `json:"name"`
	RelativePath string   `json:"relativePath"`
	Tracks       []string `json:"tracks"`
	Missing      int      `json:"missing"`
}

type Snapshot struct {
	Tracks    []Track    `json:"tracks"`
	Playlists []Playlist `json:"playlists"`
	Warnings  []string   `json:"warnings,omitempty"`
}

// Scan reads audio metadata and common M3U playlists from a mounted source.
// It rejects symlinks and playlist references that escape the source root.
// It does not write to the source, copy files, or update an Engine database.
func Scan(root string) (Snapshot, error) {
	var result Snapshot
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return result, fmt.Errorf("resolve source: %w", err)
	}
	info, err := os.Stat(resolvedRoot)
	if err != nil || !info.IsDir() {
		return result, errors.New("source is not a readable directory")
	}

	trackByPath := make(map[string]int)
	playlistFiles := make([]string, 0)
	engineDatabases := make([]string, 0, 1)
	err = filepath.WalkDir(resolvedRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			result.Warnings = append(result.Warnings, "some source files could not be read")
			if entry != nil && entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		// macOS writes AppleDouble resource-fork sidecars beside songs. Their
		// .mp3/.m3u suffix belongs to metadata, not to playable content.
		if path != resolvedRoot && (strings.HasPrefix(entry.Name(), "._") || entry.Name() == ".DS_Store") {
			if entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if entry.Type()&fs.ModeSymlink != 0 {
			if entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		relative, err := filepath.Rel(resolvedRoot, path)
		if err != nil || !safeRelative(relative) {
			result.Warnings = append(result.Warnings, "a source path was ignored as unsafe")
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		relative = filepath.ToSlash(relative)
		if isEngineMainDatabase(relative) {
			engineDatabases = append(engineDatabases, path)
			return nil
		}
		if ext == ".m3u" || ext == ".m3u8" {
			playlistFiles = append(playlistFiles, path)
			return nil
		}
		if _, ok := audioExtensions[ext]; !ok {
			return nil
		}
		fileInfo, err := entry.Info()
		if err != nil {
			result.Warnings = append(result.Warnings, "some source files could not be read")
			return nil
		}
		track := Track{RelativePath: relative, Bytes: fileInfo.Size(), ModifiedAt: fileInfo.ModTime().UnixNano()}
		if err := readTags(path, &track); err != nil {
			track.Warnings = append(track.Warnings, "embedded metadata could not be fully read")
		}
		trackByPath[relative] = len(result.Tracks)
		result.Tracks = append(result.Tracks, track)
		return nil
	})
	if err != nil {
		return Snapshot{}, fmt.Errorf("scan source: %w", err)
	}
	sort.Slice(result.Tracks, func(i, j int) bool {
		return strings.ToLower(result.Tracks[i].RelativePath) < strings.ToLower(result.Tracks[j].RelativePath)
	})
	trackByPath = make(map[string]int, len(result.Tracks))
	for i := range result.Tracks {
		trackByPath[filepath.Clean(filepath.FromSlash(result.Tracks[i].RelativePath))] = i
	}
	sort.Strings(playlistFiles)
	for _, path := range playlistFiles {
		playlist, err := readPlaylist(resolvedRoot, path, trackByPath)
		if err != nil {
			result.Warnings = append(result.Warnings, "a playlist could not be read safely")
			continue
		}
		for _, relative := range playlist.Tracks {
			if index, ok := trackByPath[filepath.Clean(filepath.FromSlash(relative))]; ok {
				result.Tracks[index].Playlists = append(result.Tracks[index].Playlists, playlist.Name)
			}
		}
		result.Playlists = append(result.Playlists, playlist)
	}
	for _, databasePath := range engineDatabases {
		playlists, err := readEnginePlaylists(databasePath, trackByPath)
		if err != nil {
			result.Warnings = append(result.Warnings, "Engine playlists could not be read safely")
			continue
		}
		for _, playlist := range playlists {
			for _, relative := range playlist.Tracks {
				if index, ok := trackByPath[filepath.Clean(filepath.FromSlash(relative))]; ok {
					result.Tracks[index].Playlists = append(result.Tracks[index].Playlists, playlist.Name)
				}
			}
			result.Playlists = append(result.Playlists, playlist)
		}
	}
	return result, nil
}

func isEngineMainDatabase(relative string) bool {
	parts := strings.Split(filepath.ToSlash(relative), "/")
	if len(parts) != 3 {
		return false
	}
	return strings.EqualFold(parts[0], "Engine Library") &&
		strings.EqualFold(parts[1], "Database2") &&
		strings.EqualFold(parts[2], "m.db")
}

func readTags(path string, track *Track) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	metadata, err := tag.ReadFrom(file)
	if err != nil {
		if strings.EqualFold(filepath.Ext(path), ".wav") {
			return readWaveMetadata(file, track)
		}
		return err
	}
	return applyAudioMetadata(metadata, track)
}

func applyAudioMetadata(metadata tag.Metadata, track *Track) error {
	track.Tags = Tags{
		Format: string(metadata.Format()), Title: cleanTag(metadata.Title()),
		Artist: cleanTag(metadata.Artist()), AlbumArtist: cleanTag(metadata.AlbumArtist()),
		Album: cleanTag(metadata.Album()), Genre: cleanTag(metadata.Genre()),
		Composer: cleanTag(metadata.Composer()), Comment: cleanTag(metadata.Comment()),
		Year: metadata.Year(),
	}
	track.Tags.Track, track.Tags.TrackTotal = metadata.Track()
	track.Tags.Disc, track.Tags.DiscTotal = metadata.Disc()
	if picture := metadata.Picture(); picture != nil {
		if len(picture.Data) == 0 || len(picture.Data) > maxArtworkBytes {
			return errors.New("embedded artwork is empty or exceeds the size limit")
		}
		track.Artwork = append([]byte(nil), picture.Data...)
		track.ArtworkHash, track.ArtworkFilename, _ = EngineArtworkIdentity(track.Artwork)
	}
	return nil
}

func readPlaylist(root, path string, trackByPath map[string]int) (Playlist, error) {
	relativePlaylist, err := filepath.Rel(root, path)
	if err != nil || !safeRelative(relativePlaylist) {
		return Playlist{}, errors.New("unsafe playlist path")
	}
	file, err := os.Open(path)
	if err != nil {
		return Playlist{}, err
	}
	defer file.Close()

	playlist := Playlist{
		Name:         strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)),
		RelativePath: filepath.ToSlash(relativePlaylist),
		Tracks:       make([]string, 0),
	}
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 4096), maxPlaylistLine)
	base := filepath.Dir(path)
	for scanner.Scan() {
		line := strings.TrimSpace(strings.TrimPrefix(scanner.Text(), "\ufeff"))
		if line == "" || strings.HasPrefix(line, "#") || strings.Contains(line, "://") {
			continue
		}
		candidate := line
		if !filepath.IsAbs(candidate) {
			candidate = filepath.Join(base, filepath.FromSlash(candidate))
		}
		candidate, err = filepath.Abs(filepath.Clean(candidate))
		if err != nil || !within(root, candidate) || !noSymlinkPath(root, candidate) {
			playlist.Missing++
			continue
		}
		relative, err := filepath.Rel(root, candidate)
		if err != nil || !safeRelative(relative) {
			playlist.Missing++
			continue
		}
		relative = filepath.ToSlash(relative)
		if _, exists := trackByPath[filepath.Clean(filepath.FromSlash(relative))]; !exists {
			playlist.Missing++
			continue
		}
		playlist.Tracks = append(playlist.Tracks, relative)
	}
	if err := scanner.Err(); err != nil && !errors.Is(err, io.EOF) {
		return Playlist{}, err
	}
	return playlist, nil
}

func cleanTag(value string) string {
	return strings.TrimSpace(strings.ToValidUTF8(value, "�"))
}

func safeRelative(path string) bool {
	if path == "." || path == "" || filepath.IsAbs(path) {
		return false
	}
	clean := filepath.Clean(path)
	return clean != ".." && !strings.HasPrefix(clean, ".."+string(filepath.Separator))
}

func within(root, path string) bool {
	relative, err := filepath.Rel(root, path)
	return err == nil && safeRelative(relative)
}

func noSymlinkPath(root, path string) bool {
	if !within(root, path) {
		return false
	}
	relative, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	current := root
	for _, part := range strings.Split(relative, string(filepath.Separator)) {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err != nil || info.Mode()&os.ModeSymlink != 0 {
			return false
		}
	}
	return true
}
