package usbcopy

import (
	"fmt"
	"strings"
	"time"
)

type GeneratedPlaylist struct {
	Name   string   `json:"name"`
	Tracks []string `json:"tracks"`
}

// BuildCopyPlaylist names and orders the playlist created for one copy run.
// Existing destination playlists are never replaced; a numbered suffix makes
// the generated name unique when the same USB drive is copied again.
func BuildCopyPlaylist(volume string, copiedAt time.Time, tracks []Track, existingNames []string) GeneratedPlaylist {
	label := cleanPlaylistLabel(volume)
	if label == "" {
		label = "USB drive"
	}
	base := fmt.Sprintf("USB Copy - %s - %s", label, copiedAt.Format("2006-01-02"))
	name := base
	for suffix := 2; containsName(existingNames, name); suffix++ {
		name = fmt.Sprintf("%s (%d)", base, suffix)
	}
	paths := make([]string, 0, len(tracks))
	for _, track := range tracks {
		if safeRelative(track.RelativePath) {
			paths = append(paths, track.RelativePath)
		}
	}
	return GeneratedPlaylist{Name: name, Tracks: paths}
}

func cleanPlaylistLabel(value string) string {
	var label strings.Builder
	for _, char := range strings.TrimSpace(value) {
		if char < 0x20 || strings.ContainsRune("/\\:*?\"<>|", char) {
			continue
		}
		label.WriteRune(char)
		if label.Len() >= 48 {
			break
		}
	}
	return strings.TrimSpace(label.String())
}

func containsName(names []string, candidate string) bool {
	for _, name := range names {
		if strings.EqualFold(strings.TrimSpace(name), candidate) {
			return true
		}
	}
	return false
}
