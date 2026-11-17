package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRepeatedRunFailuresQuarantineAndDisableRootMod(t *testing.T) {
	root, item := launcherFixture(t, "#!/bin/sh\nexit 9\n", true)
	for expected := 1; expected <= failureQuarantineThreshold; expected++ {
		run, err := launchEnabledSet(item.ID, root)
		if err == nil || run.FailureCount != expected || run.Quarantined != (expected == failureQuarantineThreshold) {
			t.Fatalf("failure %d: %#v %v", expected, run, err)
		}
	}
	state, err := loadRegistry(filepath.Join(root, "registry", "state.json"))
	if err != nil || state.Mods[item.ID].Enabled {
		t.Fatalf("quarantined mod remained enabled: %#v %v", state, err)
	}
	blocked, err := launchEnabledSet(item.ID, root)
	if err == nil || blocked.State != "blocked-quarantine" || len(blocked.Steps) != 0 {
		t.Fatalf("quarantine bypassed: %#v %v", blocked, err)
	}
	if _, err := recoverQuarantinedMod(root, item.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := setModEnabled(item.ID, true, root); err != nil {
		t.Fatal(err)
	}
	entrypoint := filepath.Join(root, filepath.FromSlash(item.Root), filepath.FromSlash(item.Entrypoint))
	if err := os.WriteFile(entrypoint, []byte("#!/bin/sh\nexit 0\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	success, err := launchEnabledSet(item.ID, root)
	if err != nil || success.State != "completed" || success.FailureCount != 0 || success.Quarantined {
		t.Fatalf("recovered launch: %#v %v", success, err)
	}
}

func TestSafeModeBlocksLaunchWithoutIncrementingFailures(t *testing.T) {
	root, item := launcherFixture(t, "#!/bin/sh\nexit 0\n", true)
	state, err := setSafeMode(root, true)
	if err != nil || !state.SafeMode {
		t.Fatalf("safe mode enable: %#v %v", state, err)
	}
	run, err := launchEnabledSet(item.ID, root)
	if err == nil || run.State != "blocked-safe-mode" || run.FailureCount != 0 || len(run.Steps) != 0 {
		t.Fatalf("safe mode bypassed: %#v %v", run, err)
	}
	state, err = loadSafetyState(root)
	if err != nil || len(state.Mods) != 0 {
		t.Fatalf("safe mode counted failure: %#v %v", state, err)
	}
	if _, err := setSafeMode(root, false); err != nil {
		t.Fatal(err)
	}
	run, err = launchEnabledSet(item.ID, root)
	if err != nil || run.State != "completed" {
		t.Fatalf("launch after safe mode: %#v %v", run, err)
	}
}

func TestDirectLaunchHonorsSafeModeAndQuarantine(t *testing.T) {
	root, item := launcherFixture(t, "#!/bin/sh\nexit 0\n", true)
	if _, err := setSafeMode(root, true); err != nil {
		t.Fatal(err)
	}
	if _, err := launchEnabledMod(item.ID, root); err == nil {
		t.Fatal("direct launch bypassed safe mode")
	}
	state, err := loadSafetyState(root)
	if err != nil {
		t.Fatal(err)
	}
	state.SafeMode = false
	state.Mods[item.ID] = modSafetyState{FailureCount: failureQuarantineThreshold, Quarantined: true}
	if err := writeSafetyState(root, state); err != nil {
		t.Fatal(err)
	}
	if _, err := launchEnabledMod(item.ID, root); err == nil {
		t.Fatal("direct launch bypassed quarantine")
	}
}

func TestSafetyStateRejectsInconsistentQuarantine(t *testing.T) {
	root := t.TempDir()
	state := emptySafetyState()
	state.Mods["bad"] = modSafetyState{FailureCount: 1, Quarantined: true}
	if err := writeSafetyState(root, state); err != nil {
		t.Fatal(err)
	}
	if _, err := loadSafetyState(root); err == nil {
		t.Fatal("inconsistent quarantine accepted")
	}
}
