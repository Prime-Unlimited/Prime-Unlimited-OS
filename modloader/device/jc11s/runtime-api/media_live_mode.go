package main

import "errors"

func (s *server) setMediaPreviewMode(p *mediaImportPreview) {
	p.Mode = "offline"
	if s.mediaImportLive {
		p.Mode = "live"
		p.CommitAvailable = p.CommitAvailable && s.mediaLiveAvailable()
	}
}

// A legacy trusted bridge's broader restart confirmation also authorizes copy.
// The explicitly enabled server selects live mode; errors never fall back to
// stopping Engine. New clients send mode=live and confirmCopy instead.
func mediaRequestedImportMode(live bool, mode string, copy, restart bool) (string, error) {
	if live {
		if (mode == "" || mode == "live") && (copy || restart) {
			return "live", nil
		}
	} else {
		if (mode == "" || mode == "offline") && restart {
			return "offline", nil
		}
	}
	return "", errors.New("import mode or confirmation is not permitted")
}
