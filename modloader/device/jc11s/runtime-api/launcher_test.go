package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func launcherFixture(t *testing.T, script string, enabled bool) (string, installedMod) {
	t.Helper()
	root := t.TempDir()
	makeLauncherFixtureTraversable(t, root)
	item := installedMod{ID: "launch-test", Version: "1.0.0", PackageSHA256: strings.Repeat("a", 64), CatalogID: "test", Architecture: "armv7l", ABI: "gnueabihf", Entrypoint: "mod/start.sh", Root: "mods/installed/launch-test/1.0.0", Enabled: enabled}
	entrypoint := filepath.Join(root, filepath.FromSlash(item.Root), "mod", "start.sh")
	if err := os.MkdirAll(filepath.Dir(entrypoint), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(entrypoint, []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeAtomicJSON(filepath.Join(root, "registry", "state.json"), registryState{SchemaVersion: 1, Mods: map[string]installedMod{item.ID: item}}); err != nil {
		t.Fatal(err)
	}
	return root, item
}

func TestLaunchEnabledShellModRecordsBoundedStatus(t *testing.T) {
	root, item := launcherFixture(t, "#!/bin/sh\nset -eu\nprintf 'ok:%s:%s\\n' \"$PRIME_UNLIMITED_MOD_ID\" \"$HOME\"\n", true)
	status, err := launchEnabledMod(item.ID, root)
	if err != nil || status.State != "completed" || status.ExitCode != 0 {
		t.Fatalf("launch: %#v %v", status, err)
	}
	data, err := readOwnedFile(filepath.Join(root, filepath.FromSlash(status.Log)), maxLaunchLogBytes+1, "launch log")
	if err != nil || !strings.Contains(string(data), "ok:launch-test:") {
		t.Fatalf("log: %q %v", data, err)
	}
	loaded, err := loadLaunchStatus(item.ID, root)
	if err != nil || loaded.State != "completed" {
		t.Fatalf("status: %#v %v", loaded, err)
	}
}

func TestLaunchFailsClosedForDisabledOrUnsupportedEntrypoint(t *testing.T) {
	root, item := launcherFixture(t, "#!/bin/sh\nexit 0\n", false)
	if _, err := launchEnabledMod(item.ID, root); err == nil {
		t.Fatal("disabled mod launched")
	}
	root, item = launcherFixture(t, "not a shell script\n", true)
	if _, err := launchEnabledMod(item.ID, root); err == nil {
		t.Fatal("unsupported entrypoint launched")
	}
}

func TestLaunchTimeoutKillsProcessGroupAndRecordsFailure(t *testing.T) {
	root, item := launcherFixture(t, "#!/bin/sh\nsleep 5\n", true)
	previous := launcherTimeout
	launcherTimeout = 100 * time.Millisecond
	status, err := launchEnabledMod(item.ID, root)
	launcherTimeout = previous
	if err == nil || status.State != "timeout" {
		t.Fatalf("timeout: %#v %v", status, err)
	}
	loaded, loadErr := loadLaunchStatus(item.ID, root)
	if loadErr != nil || loaded.State != "timeout" {
		t.Fatalf("timeout status: %#v %v", loaded, loadErr)
	}
}

// Root-run device tests launch children as nobody; t.TempDir's parent is private.
func makeLauncherFixtureTraversable(t *testing.T, root string) {
	t.Helper()
	if os.Geteuid() != 0 {
		return
	}
	for _, directory := range []string{filepath.Dir(root), root} {
		if err := os.Chmod(directory, 0o711); err != nil {
			t.Fatal(err)
		}
	}
}
