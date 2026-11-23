package main

import "testing"

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
