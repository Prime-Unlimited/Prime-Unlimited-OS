package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeInstalledScript(t *testing.T, root string, item installedMod, body string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(item.Root), filepath.FromSlash(item.Entrypoint))
	if err := os.WriteFile(path, []byte("#!/bin/sh\nset -eu\n"+body+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestLaunchSetRunsDependenciesFirstAndPersistsRun(t *testing.T) {
	root, app, dep := dependencyActivationFixture(t)
	state, err := loadRegistry(filepath.Join(root, "registry", "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	appItem, depItem := state.Mods[app.ID], state.Mods[dep.ID]
	appItem.Enabled, depItem.Enabled = true, true
	state.Mods[app.ID], state.Mods[dep.ID] = appItem, depItem
	if err := writeAtomicJSON(filepath.Join(root, "registry", "state.json"), state); err != nil {
		t.Fatal(err)
	}
	writeInstalledScript(t, root, depItem, "printf dependency")
	writeInstalledScript(t, root, appItem, "printf app")
	run, err := launchEnabledSet(app.ID, root)
	if err != nil || run.State != "completed" || len(run.Steps) != 2 || run.Steps[0].ID != dep.ID || run.Steps[1].ID != app.ID {
		t.Fatalf("run: %#v %v", run, err)
	}
	loaded, err := loadLaunchRun(run.ID, root)
	if err != nil || loaded.State != "completed" {
		t.Fatalf("loaded run: %#v %v", loaded, err)
	}
}

func TestLaunchSetStopsAtFirstFailure(t *testing.T) {
	root, app, dep := dependencyActivationFixture(t)
	state, err := loadRegistry(filepath.Join(root, "registry", "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	appItem, depItem := state.Mods[app.ID], state.Mods[dep.ID]
	appItem.Enabled, depItem.Enabled = true, true
	state.Mods[app.ID], state.Mods[dep.ID] = appItem, depItem
	if err := writeAtomicJSON(filepath.Join(root, "registry", "state.json"), state); err != nil {
		t.Fatal(err)
	}
	writeInstalledScript(t, root, depItem, "exit 7")
	writeInstalledScript(t, root, appItem, "printf must-not-run")
	run, err := launchEnabledSet(app.ID, root)
	if err == nil || run.State != "failed" || len(run.Steps) != 1 || run.Steps[0].ID != dep.ID || run.Steps[0].ExitCode != 7 {
		t.Fatalf("failed run: %#v %v", run, err)
	}
	if _, statErr := os.Lstat(filepath.Join(root, "runtime", "status", app.ID+".json")); !os.IsNotExist(statErr) {
		t.Fatal("dependent ran after dependency failure")
	}
	data, readErr := readOwnedFile(filepath.Join(root, "runtime", "runs", run.ID+".json"), 1024*1024, "launch run")
	if readErr != nil || !strings.Contains(string(data), `"state":"failed"`) {
		t.Fatalf("failed run missing: %s %v", data, readErr)
	}
}

func TestLaunchOrderRejectsDisabledDependency(t *testing.T) {
	root, app, dep := dependencyActivationFixture(t)
	state, err := loadRegistry(filepath.Join(root, "registry", "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	appItem := state.Mods[app.ID]
	appItem.Enabled = true
	state.Mods[app.ID] = appItem
	if _, err := resolveInstalledLaunchOrder(state, app.ID); err == nil || !strings.Contains(err.Error(), dep.ID) {
		t.Fatalf("disabled dependency accepted: %v", err)
	}
}
