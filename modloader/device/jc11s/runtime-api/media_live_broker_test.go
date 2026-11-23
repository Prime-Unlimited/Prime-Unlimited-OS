package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
	usbcopy "prime-unlimited.usb-copy/worker"
)

func liveBrokerFixture(t *testing.T) (mediaLiveCommandContext, usbcopy.LiveCommand) {
	t.Helper()
	root, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	id := strings.Repeat("a", 64)
	q, e := usbcopy.OpenFileLiveCommands(filepath.Join(root, "commands"), id)
	if e != nil {
		t.Fatal(e)
	}
	binding := usbcopy.LiveBinding{LibraryUUID: "647495f9-fd0e-4772-95b9-c69de5aac767", MountIdentity: strings.Repeat("b", 64), EngineIdentity: "boot:pid:start"}
	parent := mediaLiveCommandContext{queue: q, session: mediaLiveSession{JobID: id, Binding: binding}, request: mediaJobRequest{ID: id, Mode: "live", Mounts: mediaJobMounts{Destination: mediaMountIdentity{Root: "/media/Denon"}}}}
	c := usbcopy.LiveCommand{JobID: id, Intent: id + ":register:0", Operation: "register_files", Binding: binding, Paths: []string{"Music/Imported/A & B.mp3"}, ExpiresAt: time.Now().Add(time.Minute)}
	c.ID = usbcopy.LiveCommandID(id, c.Intent)
	if e = q.Enqueue(context.Background(), c, time.Now()); e != nil {
		t.Fatal(e)
	}
	return parent, c
}
func TestLiveBrokerClaimsOnceAndNeverTreatsAckAsVerified(t *testing.T) {
	parent, c := liveBrokerFixture(t)
	resolve := func(_ context.Context, id string) (mediaLiveCommandContext, error) {
		if id != c.JobID {
			return parent, errors.New("wrong parent")
		}
		return parent, nil
	}
	call := func(method, action, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "/api/v1/media-import/native/"+c.JobID+"/"+action, strings.NewReader(body))
		w := httptest.NewRecorder()
		serveNativeMediaCommand(w, r, resolve)
		return w
	}
	w := call("GET", "pending", "")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var envelope mediaLiveEnvelope
	if e := json.Unmarshal(w.Body.Bytes(), &envelope); e != nil {
		t.Fatal(e)
	}
	if len(envelope.FileURLs) != 1 || envelope.FileURLs[0] != "file:///media/Denon/Engine%20Library/Music/Imported/A%20&%20B.mp3" {
		t.Fatalf("untrusted or incorrectly encoded location: %+v", envelope)
	}
	body := `{"id":"` + c.ID + `"}`
	if w = call("POST", "claim", body); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w = call("POST", "claim", body); w.Code != 409 {
		t.Fatal("duplicate native invocation claim", w.Code)
	}
	if w = call("GET", "pending", ""); w.Code != 204 {
		t.Fatal("claimed operation was re-offered", w.Code)
	}
	ack, _ := json.Marshal(map[string]any{"id": c.ID, "observation": usbcopy.LiveCommandObservation{Outcome: "applied", Binding: c.Binding}})
	if w = call("POST", "observe", string(ack)); w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	r, e := parent.queue.Read(context.Background())
	if e != nil || r.Phase != "observed" {
		t.Fatalf("actor bypassed trusted verification: %+v %v", r, e)
	}
	if w = call("POST", "observe", string(ack)); w.Code != 409 {
		t.Fatal("duplicate actor result accepted", w.Code)
	}
}
func TestLiveBrokerRejectsStaleParentBeforeClaim(t *testing.T) {
	parent, c := liveBrokerFixture(t)
	r := httptest.NewRequest("POST", "/api/v1/media-import/native/"+c.JobID+"/claim", strings.NewReader(`{"id":"`+c.ID+`"}`))
	w := httptest.NewRecorder()
	serveNativeMediaCommand(w, r, func(context.Context, string) (mediaLiveCommandContext, error) {
		return parent, errors.New("worker stopped or Engine changed")
	})
	if w.Code != 409 {
		t.Fatal("stale parent accepted", w.Code)
	}
	record, e := parent.queue.Read(context.Background())
	if e != nil || record.Phase != "queued" {
		t.Fatal("invalid parent consumed command")
	}
}
func TestLiveBrokerAuthorizationAndFeatureGate(t *testing.T) {
	s := nativeProbeFixture(t)
	s.mediaImportWrites = true
	path := "/api/v1/media-import/native/" + strings.Repeat("a", 64) + "/pending"
	for _, tc := range []struct {
		auth  bool
		query string
		live  bool
		want  int
	}{{false, "", true, 401}, {true, "?path=/media/SD%20Card", true, 400}, {true, "", false, 403}} {
		s.mediaImportLive = tc.live
		r := httptest.NewRequest("GET", path+tc.query, nil)
		if tc.auth {
			r.Header.Set("Authorization", "Bearer "+s.authToken)
		}
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if w.Code != tc.want {
			t.Fatalf("gating %d want %d", w.Code, tc.want)
		}
	}
}
func TestLiveBrokerRejectsActorSuppliedBindingAndUnknownFields(t *testing.T) {
	parent, c := liveBrokerFixture(t)
	if _, e := parent.queue.Claim(context.Background(), c.ID, c.Binding, time.Now()); e != nil {
		t.Fatal(e)
	}
	wrong := c.Binding
	wrong.EngineIdentity = "restarted"
	ack, _ := json.Marshal(map[string]any{"id": c.ID, "observation": usbcopy.LiveCommandObservation{Outcome: "applied", Binding: wrong}})
	for _, body := range []string{string(ack), `{"id":"` + c.ID + `","execute":"delete"}`} {
		r := httptest.NewRequest("POST", "/api/v1/media-import/native/"+c.JobID+"/observe", strings.NewReader(body))
		w := httptest.NewRecorder()
		serveNativeMediaCommand(w, r, func(context.Context, string) (mediaLiveCommandContext, error) { return parent, nil })
		if w.Code != 400 && w.Code != 409 {
			t.Fatal("unsafe actor payload accepted", w.Code)
		}
	}
	record, e := parent.queue.Read(context.Background())
	if e != nil || record.Phase != "claimed" {
		t.Fatal("bad actor result changed claim")
	}
}
func TestLiveSessionCannotRebindToAnotherEngine(t *testing.T) {
	root := t.TempDir()
	id := strings.Repeat("a", 64)
	request := mediaJobRequest{ID: id, Mode: "live", DestinationIdentity: mediaDestinationIdentity{FilesystemUUID: "6ae0-5e56", LibraryUUID: "647495f9-fd0e-4772-95b9-c69de5aac767"}, Mounts: mediaJobMounts{BootID: "boot", Destination: mediaMountIdentity{Root: "/media/Denon", MountID: "547", DeviceID: "8:1", SysfsPath: "/sata"}}}
	session, e := createMediaLiveSession(root, request, "engine-before")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = createMediaLiveSession(root, request, "engine-after"); e == nil {
		t.Fatal("worker restart silently rebound live job")
	}
	actual, e := readMediaLiveSession(root, id)
	if e != nil || actual != session {
		t.Fatalf("session changed: %+v %v", actual, e)
	}
	request.Mounts.Destination.MountID = "different"
	if mediaLiveMountBinding(request) == session.Binding.MountIdentity {
		t.Fatal("mount change lost in binding")
	}
}
func TestLiveBrokerRequiresSharedSupervisorLease(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "media-jobs")
	if e := os.Mkdir(dir, 0700); e != nil {
		t.Fatal(e)
	}
	f, e := os.OpenFile(filepath.Join(dir, "supervisor.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		t.Fatal(e)
	}
	defer f.Close()
	if e = mediaLiveSupervisorHeld(root); e == nil {
		t.Fatal("dispatched without supervisor")
	}
	if e = unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); e != nil {
		t.Fatal(e)
	}
	defer unix.Flock(int(f.Fd()), unix.LOCK_UN)
	if e = mediaLiveSupervisorHeld(root); e != nil {
		t.Fatal(e)
	}
}
