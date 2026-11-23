package main

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDeclineSurvivesAPIRecreationOnlyForSameInsertion(t *testing.T) {
	root := t.TempDir()
	event := mediaInsertEvent{ID: strings.Repeat("a", 64), mountKey: "504:8:18", mountPath: "/media/MP3 2"}
	first := &server{root: root, mediaEvents: map[string]mediaInsertEvent{event.mountKey: event}}
	response := httptest.NewRecorder()
	request := httptest.NewRequest("POST", "/", strings.NewReader(`{"eventId":"`+event.ID+`","decision":"decline"}`))
	first.handleMediaImportDecision(response, request)
	if response.Code != 200 {
		t.Fatalf("decline failed: %s", response.Body.String())
	}
	second := &server{root: root}
	fresh := event
	fresh.ID = strings.Repeat("b", 64)
	restored, e := second.restoreMediaResolution(fresh)
	if e != nil || !restored.Resolved || restored.ID != event.ID {
		t.Fatalf("decision lost: %+v %v", restored, e)
	}
	fresh.mountKey = "551:8:18"
	restored, e = second.restoreMediaResolution(fresh)
	if e != nil || restored.Resolved {
		t.Fatal("new insertion incorrectly suppressed")
	}
	boot, e := mediaBootID()
	if e != nil {
		t.Fatal(e)
	}
	otherBoot := "11111111-1111-1111-1111-111111111111"
	if otherBoot == boot {
		otherBoot = "22222222-2222-2222-2222-222222222222"
	}
	_, found, e := readMediaResolution(root, otherBoot, event.mountKey, event.mountPath)
	if e != nil || found {
		t.Fatal("old boot decision reused")
	}
}

func TestReusedMountIDDoesNotReusePreviousDeviceDecision(t *testing.T) {
	root := t.TempDir()
	boot, e := mediaBootID()
	if e != nil {
		t.Fatal(e)
	}
	event := mediaInsertEvent{ID: strings.Repeat("a", 64), mountKey: "223:8:18", mountPath: "/media/MP3 2", deviceGeneration: "26"}
	if e = writeMediaResolution(root, boot, event); e != nil {
		t.Fatal(e)
	}
	s := &server{root: root}
	fresh := event
	fresh.ID = strings.Repeat("b", 64)
	same, e := s.restoreMediaResolution(fresh)
	if e != nil || !same.Resolved || same.ID != event.ID {
		t.Fatal("same insertion lost decline on API restart", e)
	}
	fresh.deviceGeneration = "27"
	newInsertion, e := s.restoreMediaResolution(fresh)
	if e != nil || newInsertion.Resolved || newInsertion.ID != fresh.ID {
		t.Fatal("recycled mount ID suppressed genuinely new insertion", e)
	}
	legacy := event
	legacy.deviceGeneration = ""
	if e = writeMediaResolution(root, boot, legacy); e != nil {
		t.Fatal(e)
	}
	newInsertion, e = s.restoreMediaResolution(fresh)
	if e != nil || newInsertion.Resolved {
		t.Fatal("legacy decision suppressed generation-aware insertion", e)
	}
}
func TestDiskSequenceReadFromPartitionParentAndPhysicalJobBinding(t *testing.T) {
	disk := t.TempDir()
	partition := filepath.Join(disk, "sdb2")
	if e := os.Mkdir(partition, 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(disk, "diskseq"), []byte("27\n"), 0600); e != nil {
		t.Fatal(e)
	}
	if got := blockDeviceGeneration(partition); got != "27" {
		t.Fatal("partition generation missing", got)
	}
	v := mountedBlockVolume{mountID: "223", deviceID: "8:18", mountPath: "/media/MP3 2", sysfsPath: "/sys/block/sdb/sdb2", deviceGeneration: "27"}
	identity := mediaIdentity(v)
	if _, ok := mediaJobMountMatch([]mountedBlockVolume{v}, identity); !ok {
		t.Fatal("same device generation rejected")
	}
	v.deviceGeneration = "28"
	if _, ok := mediaJobMountMatch([]mountedBlockVolume{v}, identity); ok {
		t.Fatal("running import accepted replacement with recycled mount/device IDs")
	}
}
func TestMediaResolutionRecordCannotCrossMountPath(t *testing.T) {
	root := t.TempDir()
	event := mediaInsertEvent{ID: strings.Repeat("c", 64), mountKey: "504:8:18", mountPath: "/media/MP3 2"}
	boot := "11111111-1111-1111-1111-111111111111"
	if e := writeMediaResolution(root, boot, event); e != nil {
		t.Fatal(e)
	}
	_, found, e := readMediaResolution(root, boot, event.mountKey, "/media/SD Card")
	if e != nil || found {
		t.Fatal("decision applied to another source")
	}
	if _, e := mediaResolutionPath(root, boot, "broken", event.mountPath); e == nil {
		t.Fatal("invalid mount key accepted")
	}
}
