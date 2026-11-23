package main

import (
	usbcopy "prime-unlimited.usb-copy/worker"
	"testing"
)

func TestLiveAdmissionNeverFallsBackToOfflineRestart(t *testing.T) {
	for _, tc := range []struct {
		enabled       bool
		mode          string
		copy, restart bool
		want          string
	}{
		{true, "live", true, false, "live"}, {true, "", false, true, "live"},
		{true, "offline", false, true, ""}, {true, "live", false, false, ""},
		{false, "live", true, true, ""}, {false, "", true, false, ""},
		{false, "", false, true, "offline"}, {false, "offline", false, true, "offline"},
		{true, "unknown", true, true, ""},
	} {
		got, e := mediaRequestedImportMode(tc.enabled, tc.mode, tc.copy, tc.restart)
		if tc.want == "" {
			if e == nil {
				t.Fatalf("unsafe mode accepted %+v: %s", tc, got)
			}
		} else if e != nil || got != tc.want {
			t.Fatalf("mode %+v: %s %v", tc, got, e)
		}
	}
}
func TestLivePreviewRejectsUnverifiedDeviceProfile(t *testing.T) {
	s := &server{mediaImportWrites: true, mediaImportLive: true, model: "JP24", firmware: "5.3.0", architecture: "aarch64"}
	p := mediaImportPreview{CommitAvailable: true}
	s.setMediaPreviewMode(&p)
	if p.Mode != "live" || !p.CommitAvailable {
		t.Fatal("verified profile rejected")
	}
	s.firmware = "unknown"
	p.CommitAvailable = true
	s.setMediaPreviewMode(&p)
	if p.Mode != "live" || p.CommitAvailable {
		t.Fatal("unverified profile silently used offline mode")
	}
}

func TestAdditionalAudioContainersRequireVerifiedLiveProfile(t *testing.T) {
	s := &server{mediaImportWrites: true, mediaImportLive: true, model: "JP24", firmware: "5.3.0", architecture: "aarch64"}
	for _, ext := range []string{"mp3", "WAV", "flac", "aiff"} {
		p := usbcopy.ImportPlan{Tracks: []usbcopy.PlannedTrack{{SourceRelative: "song." + ext}}}
		if s.mediaSupportedPlan(p) != (ext != "aiff") {
			t.Fatal("live format gate", ext)
		}
		s.mediaImportLive = false
		if s.mediaSupportedPlan(p) != (ext == "mp3") {
			t.Fatal("offline format gate expanded", ext)
		}
		s.mediaImportLive = true
		s.model = "JC11S"
		if s.mediaSupportedPlan(p) {
			t.Fatal("unverified profile accepted", ext)
		}
		s.model = "JP24"
	}
}
