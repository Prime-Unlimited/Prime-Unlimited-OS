package usbcopy

import (
	"path/filepath"
	"strings"
)

// ImportTrackTitle keeps embedded titles and otherwise uses the source filename,
// including its extension, without adding the copy operation's numeric prefix.
func ImportTrackTitle(tags Tags, sourceRelative string) string {
	if strings.TrimSpace(tags.Title) != "" {
		return tags.Title
	}
	return cleanTag(filepath.Base(filepath.FromSlash(sourceRelative)))
}
