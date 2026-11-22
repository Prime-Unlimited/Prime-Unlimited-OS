package main

import (
	"context"
	"os"
	"testing"
)

// Explicit opt-in: read internal filesystem aliases and Engine identity only.
func TestMediaDestinationIdentityDeviceProbe(t *testing.T) {
	if os.Getenv("PRIME_USB_IDENTITY_PROBE") != "1" {
		t.Skip("device-only read probe")
	}
	volumes, err := readMountedBlockVolumes()
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, volume := range volumes {
		if !isInternalMediaDestination(volume) {
			continue
		}
		identity, e := readMediaDestinationIdentity(context.Background(), volume)
		if e != nil || !identity.valid() {
			t.Fatalf("internal identity unavailable: %v", e)
		}
		count++
	}
	if count != 1 {
		t.Fatalf("expected one internal music volume, found %d", count)
	}
	t.Log("internal filesystem and Engine library identities verified read-only")
}
