package main

import (
	"testing"
	"time"
)

func TestNativeSelectionRequiresUniqueInternalLibrary(t *testing.T) {
	state := nativeCreateState{SourceDevice: "/media/Denon", Identity: mediaDestinationIdentity{LibraryUUID: "library"}}
	view := nativeDeviceObservation{Path: state.SourceDevice, UUID: "library", Row: 1, CanSelect: true}
	o := &nativePlaylistObservation{Devices: []nativeDeviceObservation{view}, CollectionIsLibrary: true, CanRegisterFiles: true}
	if !nativeSelectionMatches(o, state, false) || nativeSelectionMatches(o, state, true) {
		t.Fatal("selection readiness confused with current device")
	}
	o.Devices[0].Current = true
	if !nativeSelectionMatches(o, state, true) {
		t.Fatal("confirmed library rejected")
	}
	o.Devices[0].UUID = "other"
	if nativeSelectionMatches(o, state, false) {
		t.Fatal("wrong database accepted")
	}
	o.Devices[0] = view
	o.Devices = append(o.Devices, view)
	if nativeSelectionMatches(o, state, false) {
		t.Fatal("ambiguous native devices accepted")
	}
	o.Devices = []nativeDeviceObservation{{Path: "/media/SD Card", UUID: "library", CanSelect: true}}
	if nativeSelectionMatches(o, state, false) {
		t.Fatal("wrong drive accepted")
	}
}

func TestAllTracksConfirmationRejectsPlaylistAndWrongCollection(t *testing.T) {
	state := nativeCreateState{SourceDevice: "/media/Denon", Identity: mediaDestinationIdentity{LibraryUUID: "library"}}
	s := &server{nativeProbeUntil: time.Now().Add(time.Minute), nativeProbeReceivedAt: time.Now(), nativeProbeObservation: &nativePlaylistObservation{Devices: []nativeDeviceObservation{{Path: "/media/Denon", UUID: "library", Current: true, CanSelect: true}}, CollectionIsLibrary: true, CanRegisterFiles: true, CollectionPath: "/media/Denon", CollectionListID: "2396"}}
	if s.nativeAllTracksConfirmed(state) {
		t.Fatal("playlist accepted as All Tracks")
	}
	s.nativeProbeObservation.CollectionListID = "0"
	if !s.nativeAllTracksConfirmed(state) {
		t.Fatal("verified root rejected")
	}
	s.nativeProbeObservation.CollectionPath = "/media/MP3 2"
	if s.nativeAllTracksConfirmed(state) {
		t.Fatal("wrong collection drive accepted")
	}
}
