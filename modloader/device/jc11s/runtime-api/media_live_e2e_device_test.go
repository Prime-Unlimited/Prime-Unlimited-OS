package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	usbcopy "prime-unlimited.usb-copy/worker"
)

// Explicit opt-in for the owner-authorized real import trial. Never part of a
// normal test run. All mutations go through normal preview/start admission;
// there is no direct database write or automatic retry of the start request.
func TestMediaLiveImportDeviceEndToEnd(t *testing.T) {
	if os.Getenv("PRIME_USB_LIVE_IMPORT_TEST") != "1" {
		t.Skip("explicit real device import opt-in required")
	}
	if os.Geteuid() != 0 {
		t.Fatal("device import probe requires the trusted root context")
	}
	const root = "/data/prime-unlimited"
	sample := os.Getenv("PRIME_USB_LIVE_SAMPLE")
	sampleSHA := os.Getenv("PRIME_USB_LIVE_SAMPLE_SHA256")
	existing, parseErr := strconv.Atoi(os.Getenv("PRIME_USB_LIVE_EXISTING"))
	if sample == "" || filepath.Base(sample) != sample || len(sampleSHA) != 64 || parseErr != nil || existing < 0 {
		t.Fatal("explicit sample, SHA256 and existing count required")
	}
	duplicateOnly := os.Getenv("PRIME_USB_LIVE_DUPLICATE_ONLY") == "1"
	expectedNew := 1
	if duplicateOnly {
		expectedNew = 0
	}
	counts := func() [3]int64 {
		t.Helper()
		database, err := internalEngineDatabase("/media/Denon")
		if err != nil {
			t.Fatal(err)
		}
		db, err := openNativeReadDB(database)
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close()
		var result [3]int64
		for i, table := range []string{"Track", "Playlist", "PlaylistEntity"} {
			if err = db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&result[i]); err != nil {
				t.Fatal(err)
			}
		}
		return result
	}
	var initialCounts [3]int64
	if duplicateOnly {
		initialCounts = counts()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	before, e := nativeEngineIdentity()
	if e != nil {
		t.Fatal(e)
	}
	fp, e := usbcopy.FingerprintAudioFile(ctx, "/media/MP3 2", sample)
	if e != nil || fp.SHA256 != sampleSHA {
		t.Fatal("approved new sample not present or changed", e)
	}
	snapshot, e := usbcopy.Scan("/media/MP3 2")
	if e != nil {
		t.Fatal(e)
	}
	var source usbcopy.Track
	for _, track := range snapshot.Tracks {
		if track.RelativePath == sample {
			source = track
		}
	}
	if source.Tags.Title == "" || source.Tags.Artist == "" || len(source.Artwork) == 0 {
		t.Fatal("new sample must contain readable ID3 title, artist and embedded cover")
	}
	token, e := readAuthToken(filepath.Join(root, "auth", "mutation.token"))
	if e != nil {
		t.Fatal(e)
	}
	client := &http.Client{Timeout: 120 * time.Second}
	call := func(method, path string, input, output any) {
		t.Helper()
		var body io.Reader
		if input != nil {
			data, e := json.Marshal(input)
			if e != nil {
				t.Fatal(e)
			}
			body = bytes.NewReader(data)
		}
		r, e := http.NewRequestWithContext(ctx, method, "http://127.0.0.1:8098"+path, body)
		if e != nil {
			t.Fatal(e)
		}
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set("Content-Type", "application/json")
		response, e := client.Do(r)
		if e != nil {
			t.Fatal("device API request outcome requires review", e)
		}
		defer response.Body.Close()
		data, e := io.ReadAll(io.LimitReader(response.Body, 1<<20))
		if e != nil {
			t.Fatal(e)
		}
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			t.Fatalf("%s %s: HTTP %d %s", method, path, response.StatusCode, data)
		}
		if output != nil {
			if e = json.Unmarshal(data, output); e != nil {
				t.Fatal(e)
			}
		}
	}
	var status struct {
		Enabled bool `json:"enabled"`
	}
	call("GET", "/api/v1/media-import/native-status", nil, &status)
	if !status.Enabled {
		t.Fatal("live broker is not enabled")
	}
	var events struct {
		Events []mediaInsertEvent `json:"events"`
	}
	call("GET", "/api/v1/media-import/events", nil, &events)
	var eventID, destinationID string
	for _, event := range events.Events {
		if event.DisplayName == "MP3 2" {
			eventID = event.ID
			for _, dest := range event.Destinations {
				if dest.Name == "Denon" {
					destinationID = dest.ID
				}
			}
		}
	}
	if !validMediaEventID(eventID) || !validMediaEventID(destinationID) {
		t.Fatal("fresh MP3 2 insertion and internal Denon destination required")
	}
	var preview mediaImportPreview
	call("POST", "/api/v1/media-import/events", mediaImportDecision{EventID: eventID, Decision: "copy", DestinationID: destinationID}, &preview)
	if preview.Mode != "live" || !preview.CommitAvailable || preview.TrackCount != existing+expectedNew || preview.NewFiles != expectedNew || preview.ExistingFiles != existing || preview.BytesToCopy != int64(expectedNew)*fp.Bytes {
		t.Fatalf("unexpected approved trial preview: %+v", preview)
	}
	t.Logf("preview: %d new, %d skipped, %d bytes; starting one normal live job", preview.NewFiles, preview.ExistingFiles, preview.BytesToCopy)
	var started struct {
		Job usbcopy.ImportJobStatus `json:"job"`
	}
	call("POST", "/api/v1/media-import/start", map[string]any{"eventId": eventID, "mode": "live", "confirmCopy": true}, &started)
	if !validMediaEventID(started.Job.ID) || started.Job.Mode != "live" {
		t.Fatal("trusted live job identity missing")
	}
	id := started.Job.ID
	dir, e := mediaLiveDirectory(root, id)
	if e != nil {
		t.Fatal(e)
	}
	if e = writeAtomicJSON(filepath.Join(dir, "e2e-start.json"), map[string]any{"engineBefore": before, "job": started.Job, "sampleSHA256": fp.SHA256}); e != nil {
		t.Fatal(e)
	}
	t.Logf("live job: %s", id)
	var final usbcopy.ImportJobStatus
	for {
		var jobs struct {
			Jobs []usbcopy.ImportJobStatus `json:"jobs"`
		}
		call("GET", "/api/v1/media-import/jobs", nil, &jobs)
		for _, job := range jobs.Jobs {
			if job.ID == id {
				final = job
			}
		}
		if final.Phase == usbcopy.JobCompleted || final.Phase == usbcopy.JobFailed || final.Phase == usbcopy.JobInterrupted || final.Phase == usbcopy.JobCancelled {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatalf("live job requires review after timeout: %s", id)
		case <-time.After(time.Second):
		}
	}
	after, e := nativeEngineIdentity()
	if e != nil {
		t.Fatal(e)
	}
	if e = writeAtomicJSON(filepath.Join(dir, "e2e-result.json"), map[string]any{"engineBefore": before, "engineAfter": after, "job": final, "sampleSHA256": fp.SHA256}); e != nil {
		t.Fatal(e)
	}
	if before != after {
		t.Fatal("Engine instance changed during live import")
	}
	if final.Phase != usbcopy.JobCompleted || !final.ResultAvailable || final.CopiedFiles != expectedNew || final.SkippedFiles != existing || final.CreatedPlaylists != expectedNew {
		t.Fatalf("live import requires review: %+v", final)
	}
	if duplicateOnly {
		if final.PlaylistName != "" || counts() != initialCounts {
			t.Fatal("duplicate-only import changed library counts or generated a playlist")
		}
		entries, err := os.ReadDir(filepath.Join(dir, "commands"))
		if err != nil || len(entries) != 0 {
			t.Fatal("duplicate-only import emitted native commands", err)
		}
		t.Logf("verified duplicate-only result: %d skipped; zero copies, playlists or native commands; database counts and Engine unchanged", existing)
		return
	}
	database, e := internalEngineDatabase("/media/Denon")
	if e != nil {
		t.Fatal(e)
	}
	db, e := openNativeReadDB(database)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	var localID int64
	var path string
	rows, e := db.QueryContext(ctx, `SELECT t.id,t.path FROM Playlist l JOIN PlaylistEntity p ON p.listId=l.id JOIN Track t ON t.originDatabaseUuid=p.databaseUuid AND t.originTrackId=p.trackId WHERE l.title=?`, final.PlaylistName)
	if e != nil {
		t.Fatal(e)
	}
	n := 0
	for rows.Next() {
		n++
		if e = rows.Scan(&localID, &path); e != nil {
			rows.Close()
			t.Fatal(e)
		}
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		t.Fatal(e)
	}
	if n != 1 || !strings.HasPrefix(path, "Music/") || filepath.Base(path) != sample {
		t.Fatal("playlist does not contain exactly the new copied sample")
	}
	copyFP, e := usbcopy.FingerprintAudioFile(ctx, "/media/Denon/Engine Library", path)
	if e != nil || copyFP.SHA256 != sampleSHA {
		t.Fatal("copied content differs", e)
	}
	planned := usbcopy.PlannedTrack{SourceRelative: sample, DestinationRelative: strings.TrimPrefix(path, "Music/"), Bytes: source.Bytes, Tags: source.Tags, Artwork: source.Artwork}
	if _, e = liveRegistered(ctx, database, "/media/Denon", "647495f9-fd0e-4772-95b9-c69de5aac767", planned); e != nil {
		t.Fatal(e)
	}
	t.Logf("verified playlist: %s; new Track.id=%d; tags, artwork and SHA256 verified; Engine unchanged", final.PlaylistName, localID)
}
