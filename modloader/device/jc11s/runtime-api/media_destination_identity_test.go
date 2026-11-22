package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testDestinationIdentity() mediaDestinationIdentity {
	return mediaDestinationIdentity{"6ae0-5e56", "9b7a80aa-4f11-4118-a42f-d631c2dddd34"}
}
func TestFilesystemUUIDUsesOnlyMatchingDeviceAlias(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "sda1")
	other := filepath.Join(root, "sdb1")
	aliases := filepath.Join(root, "aliases")
	if err := os.Mkdir(aliases, 0700); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{source, other} {
		if err := os.WriteFile(p, nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	for alias, target := range map[string]string{"6AE0-5E56": source, "1111-2222": other, "not-a-uuid": source} {
		if err := os.Symlink(target, filepath.Join(aliases, alias)); err != nil {
			t.Fatal(err)
		}
	}
	uuid, err := filesystemUUIDForDevice(aliases, source)
	if err != nil || uuid != "6ae0-5e56" {
		t.Fatalf("uuid=%s err=%v", uuid, err)
	}
	if err = os.Symlink(source, filepath.Join(aliases, "3333-4444")); err != nil {
		t.Fatal(err)
	}
	if _, err = filesystemUUIDForDevice(aliases, source); err == nil {
		t.Fatal("ambiguous identity accepted")
	}
}
func TestReviewResolvesPersistentIdentityAcrossBootAndMountChanges(t *testing.T) {
	wanted := testDestinationIdentity()
	request := mediaJobRequest{DestinationIdentity: wanted, Mounts: mediaJobMounts{BootID: "old", Destination: mediaMountIdentity{MountID: "1", DeviceID: "8:1", Root: "/media/Old", SysfsPath: "old"}}}
	volume := mountedBlockVolume{mountID: "20", deviceID: "8:17", mountPath: "/media/New", sysfsPath: "sata"}
	eligible := func(v mountedBlockVolume) bool { return !v.usb }
	reader := func(context.Context, mountedBlockVolume) (mediaDestinationIdentity, error) { return wanted, nil }
	result, ok := resolveMediaReviewFromVolumes(context.Background(), request, "new", []mountedBlockVolume{volume}, reader, eligible)
	if !ok || result.mountPath != "/media/New" {
		t.Fatal("same identities not resolved after reboot")
	}
	mismatch := func(context.Context, mountedBlockVolume) (mediaDestinationIdentity, error) {
		i := wanted
		i.LibraryUUID = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
		return i, nil
	}
	if _, ok = resolveMediaReviewFromVolumes(context.Background(), request, "new", []mountedBlockVolume{volume}, mismatch, eligible); ok {
		t.Fatal("different library accepted")
	}
	if _, ok = resolveMediaReviewFromVolumes(context.Background(), request, "new", []mountedBlockVolume{volume, volume}, reader, eligible); ok {
		t.Fatal("duplicate identities accepted")
	}
	volume.usb = true
	if _, ok = resolveMediaReviewFromVolumes(context.Background(), request, "new", []mountedBlockVolume{volume}, reader, eligible); ok {
		t.Fatal("removable destination accepted")
	}
	request.DestinationIdentity = mediaDestinationIdentity{}
	volume.usb = false
	volume.mountID = "1"
	volume.deviceID = "8:1"
	volume.mountPath = "/media/Old"
	volume.sysfsPath = "old"
	if _, ok = resolveMediaReviewFromVolumes(context.Background(), request, "new", []mountedBlockVolume{volume}, reader, eligible); ok {
		t.Fatal("legacy request attributed across reboot")
	}
	if _, ok = resolveMediaReviewFromVolumes(context.Background(), request, "old", []mountedBlockVolume{volume}, reader, eligible); !ok {
		t.Fatal("legacy same-boot matching rejected")
	}
	unavailable := func(context.Context, mountedBlockVolume) (mediaDestinationIdentity, error) {
		return wanted, errors.New("unavailable")
	}
	request.DestinationIdentity = wanted
	if _, ok = resolveMediaReviewFromVolumes(context.Background(), request, "new", []mountedBlockVolume{volume}, unavailable, eligible); ok {
		t.Fatal("unreadable identity accepted")
	}
}
func TestJobRequestsPersistIdentityAndRejectPartialIdentity(t *testing.T) {
	root := t.TempDir()
	request := mediaJobRequest{ID: strings.Repeat("a", 64), EventID: strings.Repeat("b", 64), SourceDigest: strings.Repeat("c", 64), DestinationIdentity: testDestinationIdentity()}
	request.CreatedAt = time.Now().UTC()
	if err := writeMediaJobRequest(root, request); err != nil {
		t.Fatal(err)
	}
	saved, err := readMediaJobRequest(root, request.ID)
	if err != nil || saved.DestinationIdentity != request.DestinationIdentity {
		t.Fatal("identity lost", err)
	}
	request.ID = strings.Repeat("d", 64)
	request.DestinationIdentity.LibraryUUID = ""
	if err = writeMediaJobRequest(root, request); err == nil {
		t.Fatal("partial identity accepted")
	}
}
