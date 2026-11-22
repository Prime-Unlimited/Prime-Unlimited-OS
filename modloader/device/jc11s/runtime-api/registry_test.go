package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func stagedInstallFixture(t *testing.T) (mod, string, string, string) {
	t.Helper()
	payload := makeZIP(t, []zipEntry{{name: "mod/start.sh", data: "#!/bin/sh\n", mode: 0o777}, {name: "mod/data.txt", data: "payload"}})
	item, cacheRoot := cachedZIP(t, payload)
	item.Version = "1.0.0"
	item.Architecture = "armv7l"
	item.ABI = "gnueabihf"
	item.Entrypoint = "mod/start.sh"
	item.Approval = &approval{Maintainer: "Prime Unlimited", Devices: []string{"JC11S"}}
	stagingRoot := filepath.Join(t.TempDir(), "stage")
	if _, err := stagePackage(item, cacheRoot, stagingRoot, defaultStageLimits); err != nil {
		t.Fatal(err)
	}
	return item, cacheRoot, stagingRoot, t.TempDir()
}

func TestInstallDisabledModCommitsFilesRegistryAndTransaction(t *testing.T) {
	item, cacheRoot, stagingRoot, root := stagedInstallFixture(t)
	result, err := installDisabledMod(item, "trusted-test", cacheRoot, stagingRoot, root)
	if err != nil || !result.Installed || result.Enabled || len(result.TransactionID) != 32 {
		t.Fatalf("install failed: %#v %v", result, err)
	}
	target := filepath.Join(root, "mods", "installed", item.ID, item.Version)
	for path, mode := range map[string]os.FileMode{target: 0o700, filepath.Join(target, "mod"): 0o700, filepath.Join(target, item.Entrypoint): 0o600} {
		info, statErr := os.Lstat(path)
		if statErr != nil || info.Mode().Perm() != mode {
			t.Fatalf("unsafe installed path %s: %#v %v", path, info, statErr)
		}
	}
	if _, err := os.Lstat(filepath.Join(target, stageMarker)); !os.IsNotExist(err) {
		t.Fatal("private stage marker was installed")
	}
	state, err := loadRegistry(filepath.Join(root, "registry", "state.json"))
	installed := state.Mods[item.ID]
	if err != nil || installed.Enabled || installed.PackageSHA256 != item.SHA256 || installed.Entrypoint != item.Entrypoint {
		t.Fatalf("bad registry: %#v %v", state, err)
	}
	records, err := listTransactions(root)
	if err != nil || len(records) != 1 || records[0].Status != "completed" {
		t.Fatalf("bad transactions: %#v %v", records, err)
	}
	if _, err := installDisabledMod(item, "trusted-test", cacheRoot, stagingRoot, root); !errors.Is(err, errAlreadyInstalled) {
		t.Fatalf("duplicate install not rejected: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(root, "registry", "pending.json")); !os.IsNotExist(err) {
		t.Fatal("completed journal remains")
	}
}

func TestVerifiedUpdateAndRollbackPreservePreviousVersion(t *testing.T) {
	item, cacheRoot, stagingRoot, root := stagedInstallFixture(t)
	item.Revision = 1
	if _, err := installDisabledMod(item, "trusted-test", cacheRoot, stagingRoot, root); err != nil {
		t.Fatal(err)
	}
	payload := makeZIP(t, []zipEntry{{name: "mod/start.sh", data: "#!/bin/sh\n", mode: 0o777}, {name: "mod/data.txt", data: "updated"}})
	updated, updatedCache := cachedZIP(t, payload)
	updated.ID = item.ID
	updated.Name = item.Name
	updated.Version = "1.1.0"
	updated.Revision = 2
	updated.Models = item.Models
	updated.Firmware = item.Firmware
	updated.Architecture = item.Architecture
	updated.ABI = item.ABI
	updated.Entrypoint = item.Entrypoint
	updated.Approval = item.Approval
	updatedStage := filepath.Join(t.TempDir(), "stage")
	if _, err := stagePackage(updated, updatedCache, updatedStage, defaultStageLimits); err != nil {
		t.Fatal(err)
	}
	result, err := updateDisabledMod(updated, "trusted-test", updatedCache, updatedStage, root)
	if err != nil || !result.Updated || result.RolledBack {
		t.Fatalf("update=%#v err=%v", result, err)
	}
	state, err := loadRegistry(filepath.Join(root, "registry", "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if state.Mods[item.ID].Version != "1.1.0" || state.Mods[item.ID].Revision != 2 {
		t.Fatalf("new state=%#v", state.Mods[item.ID])
	}
	rolled, err := rollbackMod(item.ID, root)
	if err != nil || !rolled.RolledBack {
		t.Fatalf("rollback=%#v err=%v", rolled, err)
	}
	state, err = loadRegistry(filepath.Join(root, "registry", "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if state.Mods[item.ID].Version != "1.0.0" || state.Mods[item.ID].PackageSHA256 != item.SHA256 {
		t.Fatalf("restored state=%#v", state.Mods[item.ID])
	}
}

func TestInstallAPIExposesDisabledStateAndTransaction(t *testing.T) {
	item, cacheRoot, stagingRoot, root := stagedInstallFixture(t)
	token := strings.Repeat("a", 64)
	handler := &server{model: "JC11S", firmware: "5.3.0", architecture: "armv7l", abi: "gnueabihf", authToken: token,
		root: root, cacheRoot: cacheRoot, stagingRoot: stagingRoot, catalog: catalogState{Status: "valid", Trusted: true,
			Catalog: catalog{SchemaVersion: 2, CatalogID: "trusted-test", Mods: []mod{item}, Libraries: []library{}}}}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/install", strings.NewReader(`{"id":"catalog-smoke-test"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+token)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusAccepted || !strings.Contains(response.Body.String(), `"enabled":false`) {
		t.Fatalf("install response: %d %s", response.Code, response.Body.String())
	}
	for _, path := range []string{"/api/v1/installed", "/api/v1/transactions"} {
		response = httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), item.ID) {
			t.Fatalf("%s: %d %s", path, response.Code, response.Body.String())
		}
	}
}

func recoveryState(item installedMod) registryState {
	return registryState{SchemaVersion: 1, Mods: map[string]installedMod{item.ID: item}}
}

func TestInstallJournalRecoveryPreparedAndCommitted(t *testing.T) {
	for _, phase := range []string{"prepared", "committed"} {
		t.Run(phase, func(t *testing.T) {
			root := t.TempDir()
			registryRoot := filepath.Join(root, "registry")
			item := installedMod{ID: "recover-test", Version: "1.0.0", PackageSHA256: strings.Repeat("a", 64), CatalogID: "test", Architecture: "armv7l", ABI: "gnueabihf", Entrypoint: "mod/start", Root: "mods/installed/recover-test/1.0.0", Enabled: false}
			previous := emptyRegistry()
			next := recoveryState(item)
			target := filepath.Join(root, filepath.FromSlash(item.Root))
			if err := os.MkdirAll(target, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(filepath.Join(root, "mods"), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(filepath.Join(root, "mods", "installed"), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(filepath.Join(root, "mods", "installed", item.ID), 0o700); err != nil {
				t.Fatal(err)
			}
			initial := next
			if phase == "committed" {
				initial = previous
			}
			if err := writeAtomicJSON(filepath.Join(registryRoot, "state.json"), initial); err != nil {
				t.Fatal(err)
			}
			journal := installJournal{SchemaVersion: 1, Operation: "install", Phase: phase, TransactionID: "abcdef0123456789abcdef0123456789", Target: item.Root, Previous: previous, Next: next}
			if err := writeAtomicJSON(filepath.Join(registryRoot, "pending.json"), journal); err != nil {
				t.Fatal(err)
			}
			if err := recoverInstall(root); err != nil {
				t.Fatal(err)
			}
			state, err := loadRegistry(filepath.Join(registryRoot, "state.json"))
			if err != nil {
				t.Fatal(err)
			}
			_, installed := state.Mods[item.ID]
			if installed != (phase == "committed") {
				t.Fatalf("phase=%s installed=%v", phase, installed)
			}
			_, statErr := os.Lstat(target)
			if phase == "prepared" && !os.IsNotExist(statErr) {
				t.Fatal("prepared target not rolled back")
			}
			if phase == "committed" && statErr != nil {
				t.Fatal("committed target removed")
			}
			if _, err := os.Lstat(filepath.Join(registryRoot, "pending.json")); !os.IsNotExist(err) {
				t.Fatal("journal not cleared")
			}
		})
	}
}

func TestUninstallDisabledModCommitsRemovalAndTransaction(t *testing.T) {
	item, cacheRoot, stagingRoot, root := stagedInstallFixture(t)
	if _, err := installDisabledMod(item, "trusted-test", cacheRoot, stagingRoot, root); err != nil {
		t.Fatal(err)
	}
	result, err := uninstallDisabledMod(item.ID, root)
	if err != nil || !result.Uninstalled || result.Version != item.Version || len(result.TransactionID) != 32 {
		t.Fatalf("uninstall failed: %#v %v", result, err)
	}
	state, err := loadRegistry(filepath.Join(root, "registry", "state.json"))
	if err != nil || len(state.Mods) != 0 {
		t.Fatalf("registry not cleared: %#v %v", state, err)
	}
	if _, err := os.Lstat(filepath.Join(root, "mods", "installed", item.ID, item.Version)); !os.IsNotExist(err) {
		t.Fatal("uninstalled payload remains")
	}
	records, err := listTransactions(root)
	foundUninstall := false
	for _, record := range records {
		foundUninstall = foundUninstall || (record.Operation == "uninstall" && record.Status == "completed")
	}
	if err != nil || len(records) != 2 || !foundUninstall {
		t.Fatalf("bad transactions: %#v %v", records, err)
	}
	if _, err := uninstallDisabledMod(item.ID, root); !errors.Is(err, errNotInstalled) {
		t.Fatalf("missing uninstall not rejected: %v", err)
	}
}

func TestUninstallAPIIsAuthenticatedAndCatalogIndependent(t *testing.T) {
	item, cacheRoot, stagingRoot, root := stagedInstallFixture(t)
	if _, err := installDisabledMod(item, "trusted-test", cacheRoot, stagingRoot, root); err != nil {
		t.Fatal(err)
	}
	token := strings.Repeat("a", 64)
	handler := &server{authToken: token, root: root}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/uninstall", strings.NewReader(`{"id":"catalog-smoke-test"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+token)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusAccepted || !strings.Contains(response.Body.String(), `"uninstalled":true`) {
		t.Fatalf("uninstall response: %d %s", response.Code, response.Body.String())
	}
	request = httptest.NewRequest(http.MethodPost, "/api/v1/uninstall", strings.NewReader(`{"id":"catalog-smoke-test"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+token)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("missing uninstall response: %d %s", response.Code, response.Body.String())
	}
}

func TestUninstallFailureCheckpointsRecoverDeterministically(t *testing.T) {
	for _, checkpoint := range []string{"journal-prepared", "payload-quarantined", "registry-committed", "journal-committed"} {
		t.Run(checkpoint, func(t *testing.T) {
			item, cacheRoot, stagingRoot, root := stagedInstallFixture(t)
			if _, err := installDisabledMod(item, "trusted-test", cacheRoot, stagingRoot, root); err != nil {
				t.Fatal(err)
			}
			uninstallCheckpoint = func(stage string) error {
				if stage == checkpoint {
					return errors.New("injected failure")
				}
				return nil
			}
			_, err := uninstallDisabledMod(item.ID, root)
			uninstallCheckpoint = func(string) error { return nil }
			if err == nil {
				t.Fatal("injected uninstall failure did not fire")
			}
			if err := recoverInstall(root); err != nil {
				t.Fatal(err)
			}
			state, err := loadRegistry(filepath.Join(root, "registry", "state.json"))
			if err != nil {
				t.Fatal(err)
			}
			_, installed := state.Mods[item.ID]
			wantInstalled := checkpoint != "journal-committed"
			if installed != wantInstalled {
				t.Fatalf("installed=%v want=%v", installed, wantInstalled)
			}
			target := filepath.Join(root, "mods", "installed", item.ID, item.Version)
			_, statErr := os.Lstat(target)
			if wantInstalled && statErr != nil {
				t.Fatal("rolled-back payload missing")
			}
			if !wantInstalled && !os.IsNotExist(statErr) {
				t.Fatal("committed payload remains")
			}
		})
	}
}

func TestInstallFailureCheckpointsRecoverDeterministically(t *testing.T) {
	for _, checkpoint := range []string{"journal-prepared", "files-published", "registry-committed", "journal-committed"} {
		t.Run(checkpoint, func(t *testing.T) {
			item, cacheRoot, stagingRoot, root := stagedInstallFixture(t)
			installCheckpoint = func(stage string) error {
				if stage == checkpoint {
					return errors.New("injected failure")
				}
				return nil
			}
			_, err := installDisabledMod(item, "trusted-test", cacheRoot, stagingRoot, root)
			installCheckpoint = func(string) error { return nil }
			if err == nil {
				t.Fatal("injected install failure did not fire")
			}
			if err := recoverInstall(root); err != nil {
				t.Fatal(err)
			}
			state, err := loadRegistry(filepath.Join(root, "registry", "state.json"))
			if err != nil {
				t.Fatal(err)
			}
			_, installed := state.Mods[item.ID]
			wantInstalled := checkpoint == "journal-committed"
			if installed != wantInstalled {
				t.Fatalf("installed=%v want=%v", installed, wantInstalled)
			}
			target := filepath.Join(root, "mods", "installed", item.ID, item.Version)
			_, statErr := os.Lstat(target)
			if wantInstalled && statErr != nil {
				t.Fatal("committed target missing")
			}
			if !wantInstalled && !os.IsNotExist(statErr) {
				t.Fatal("prepared target not rolled back")
			}
		})
	}
}

func TestRegistryAcceptsEnabledStateAndRejectsUnsafeFile(t *testing.T) {
	root := t.TempDir()
	item := installedMod{ID: "unsafe", Version: "1.0.0", PackageSHA256: strings.Repeat("a", 64), CatalogID: "test", Architecture: "armv7l", ABI: "gnueabihf", Entrypoint: "mod/start", Root: "mods/installed/unsafe/1.0.0", Enabled: true}
	path := filepath.Join(root, "registry", "state.json")
	if err := writeAtomicJSON(path, recoveryState(item)); err != nil {
		t.Fatal(err)
	}
	if _, err := loadRegistry(path); err != nil {
		t.Fatalf("enabled state rejected: %v", err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "target.json")
	if err := os.WriteFile(target, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	if _, err := loadRegistry(path); err == nil {
		t.Fatal("registry symlink was accepted")
	}
}

func TestEnableDisableWritesManifestWithoutExecuting(t *testing.T) {
	item, cacheRoot, stagingRoot, root := stagedInstallFixture(t)
	if _, err := installDisabledMod(item, "trusted-test", cacheRoot, stagingRoot, root); err != nil {
		t.Fatal(err)
	}
	enabled, err := setModEnabled(item.ID, true, root)
	if err != nil || !enabled.Enabled {
		t.Fatalf("enable failed: %#v %v", enabled, err)
	}
	manifestData, err := readOwnedFile(filepath.Join(root, "activation", "manifest.json"), 1024*1024, "activation manifest")
	if err != nil || !strings.Contains(string(manifestData), `"id":"catalog-smoke-test"`) {
		t.Fatalf("enabled manifest missing: %s %v", manifestData, err)
	}
	entrypoint := filepath.Join(root, "mods", "installed", item.ID, item.Version, filepath.FromSlash(item.Entrypoint))
	if info, err := os.Lstat(entrypoint); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("entrypoint became executable: %#v %v", info, err)
	}
	if _, err := uninstallDisabledMod(item.ID, root); err == nil {
		t.Fatal("enabled mod was uninstalled")
	}
	disabled, err := setModEnabled(item.ID, false, root)
	if err != nil || disabled.Enabled {
		t.Fatalf("disable failed: %#v %v", disabled, err)
	}
	manifestData, err = readOwnedFile(filepath.Join(root, "activation", "manifest.json"), 1024*1024, "activation manifest")
	if err != nil || string(manifestData) != "{\"schemaVersion\":1,\"mods\":[]}\n" {
		t.Fatalf("disabled manifest not empty: %s %v", manifestData, err)
	}
}

func TestEnableFailureCheckpointsRecoverDeterministically(t *testing.T) {
	for _, checkpoint := range []string{"journal-prepared", "registry-committed", "manifest-committed", "journal-committed"} {
		t.Run(checkpoint, func(t *testing.T) {
			item, cacheRoot, stagingRoot, root := stagedInstallFixture(t)
			if _, err := installDisabledMod(item, "trusted-test", cacheRoot, stagingRoot, root); err != nil {
				t.Fatal(err)
			}
			activationCheckpoint = func(stage string) error {
				if stage == checkpoint {
					return errors.New("injected failure")
				}
				return nil
			}
			_, err := setModEnabled(item.ID, true, root)
			activationCheckpoint = func(string) error { return nil }
			if err == nil {
				t.Fatal("injected activation failure did not fire")
			}
			if err := recoverInstall(root); err != nil {
				t.Fatal(err)
			}
			state, err := loadRegistry(filepath.Join(root, "registry", "state.json"))
			if err != nil {
				t.Fatal(err)
			}
			wantEnabled := checkpoint == "journal-committed"
			if state.Mods[item.ID].Enabled != wantEnabled {
				t.Fatalf("enabled=%v want=%v", state.Mods[item.ID].Enabled, wantEnabled)
			}
			manifest, err := readOwnedFile(filepath.Join(root, "activation", "manifest.json"), 1024*1024, "activation manifest")
			if err != nil || strings.Contains(string(manifest), item.ID) != wantEnabled {
				t.Fatalf("manifest mismatch: %s %v", manifest, err)
			}
		})
	}
}

func dependencyActivationFixture(t *testing.T) (string, installedMod, installedMod) {
	t.Helper()
	root := t.TempDir()
	makeLauncherFixtureTraversable(t, root)
	makeItem := func(id, version string) installedMod {
		item := installedMod{ID: id, Version: version, PackageSHA256: strings.Repeat("a", 64), CatalogID: "test", Architecture: "armv7l", ABI: "gnueabihf", Entrypoint: "mod/start", Root: filepath.ToSlash(filepath.Join("mods", "installed", id, version))}
		entrypoint := filepath.Join(root, filepath.FromSlash(item.Root), "mod", "start")
		if err := os.MkdirAll(filepath.Dir(entrypoint), 0o700); err != nil {
			t.Fatal(err)
		}
		for _, directory := range []string{filepath.Join(root, "mods"), filepath.Join(root, "mods", "installed"), filepath.Join(root, "mods", "installed", id), filepath.Dir(entrypoint)} {
			if err := os.Chmod(directory, 0o700); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.WriteFile(entrypoint, []byte("not executed"), 0o600); err != nil {
			t.Fatal(err)
		}
		return item
	}
	depItem := makeItem("dependency", "2.1.0")
	app := makeItem("app", "1.0.0")
	app.Dependencies = []dependency{{ID: "dependency", Constraint: "^2.0"}}
	state := registryState{SchemaVersion: 1, Mods: map[string]installedMod{"app": app, "dependency": depItem}}
	if err := writeAtomicJSON(filepath.Join(root, "registry", "state.json"), state); err != nil {
		t.Fatal(err)
	}
	return root, app, depItem
}

func TestEnableAtomicallyActivatesInstalledDependencyClosure(t *testing.T) {
	root, app, depItem := dependencyActivationFixture(t)
	result, err := setModEnabled(app.ID, true, root)
	if err != nil || strings.Join(result.Affected, ",") != "dependency,app" {
		t.Fatalf("enable closure: %#v %v", result, err)
	}
	state, err := loadRegistry(filepath.Join(root, "registry", "state.json"))
	if err != nil || !state.Mods[app.ID].Enabled || !state.Mods[depItem.ID].Enabled {
		t.Fatalf("closure not enabled: %#v %v", state, err)
	}
	if _, err := setModEnabled(depItem.ID, false, root); err == nil {
		t.Fatal("required dependency was disabled")
	}
	manifest, err := readOwnedFile(filepath.Join(root, "activation", "manifest.json"), 1024*1024, "activation manifest")
	if err != nil || !strings.Contains(string(manifest), `"id":"app"`) || !strings.Contains(string(manifest), `"id":"dependency"`) {
		t.Fatalf("manifest missing closure: %s %v", manifest, err)
	}
}

func TestEnableDependencyClosureFailsClosed(t *testing.T) {
	root, app, depItem := dependencyActivationFixture(t)
	state, err := loadRegistry(filepath.Join(root, "registry", "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	delete(state.Mods, depItem.ID)
	if err := writeAtomicJSON(filepath.Join(root, "registry", "state.json"), state); err != nil {
		t.Fatal(err)
	}
	if _, err := setModEnabled(app.ID, true, root); err == nil {
		t.Fatal("missing dependency accepted")
	}
	state.Mods[depItem.ID] = depItem
	app.Dependencies[0].Constraint = "^3.0"
	state.Mods[app.ID] = app
	if err := writeAtomicJSON(filepath.Join(root, "registry", "state.json"), state); err != nil {
		t.Fatal(err)
	}
	if _, err := setModEnabled(app.ID, true, root); err == nil {
		t.Fatal("incompatible dependency accepted")
	}
	app.Dependencies[0].Constraint = "^2.0"
	state.Mods[app.ID] = app
	depItem.Conflicts = []string{"app"}
	state.Mods[depItem.ID] = depItem
	if err := writeAtomicJSON(filepath.Join(root, "registry", "state.json"), state); err != nil {
		t.Fatal(err)
	}
	if _, err := setModEnabled(app.ID, true, root); err == nil {
		t.Fatal("reverse conflict accepted")
	}
	final, err := loadRegistry(filepath.Join(root, "registry", "state.json"))
	if err != nil || final.Mods[app.ID].Enabled || final.Mods[depItem.ID].Enabled {
		t.Fatalf("failed plan changed state: %#v %v", final, err)
	}
}

func TestReconcileActivationManifestKeepsFilesWhenItCannotRewrite(t *testing.T) {
	root := t.TempDir()
	registryDir := filepath.Join(root, "registry")
	if err := os.MkdirAll(registryDir, 0o700); err != nil {
		t.Fatal(err)
	}
	registryPath := filepath.Join(registryDir, "state.json")
	original := []byte("{}\n")
	if err := os.WriteFile(registryPath, original, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := reconcileActivationManifest(root); err == nil {
		t.Fatal("invalid registry was accepted")
	}
	got, err := os.ReadFile(registryPath)
	if err != nil || string(got) != string(original) {
		t.Fatalf("registry changed: %q %v", got, err)
	}
	if _, err := os.Lstat(filepath.Join(root, "activation", "manifest.json")); !os.IsNotExist(err) {
		t.Fatalf("manifest written after load failure: %v", err)
	}

	item := installedMod{ID: "recover-test", Version: "1.0.0", PackageSHA256: strings.Repeat("a", 64), CatalogID: "test", Architecture: "armv7l", ABI: "gnueabihf", Entrypoint: "mod/start", Root: "mods/installed/recover-test/1.0.0", Enabled: true}
	if err := writeAtomicJSON(registryPath, recoveryState(item)); err != nil {
		t.Fatal(err)
	}
	if err := reconcileActivationManifest(root); err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(root, "activation", "manifest.json")
	written, err := readOwnedFile(manifestPath, 1024*1024, "activation manifest")
	if err != nil || !strings.Contains(string(written), `"id":"recover-test"`) {
		t.Fatalf("enabled manifest missing: %s %v", written, err)
	}
	if err := os.Chmod(filepath.Join(root, "activation"), 0o755); err != nil {
		t.Fatal(err)
	}
	registryBefore, err := os.ReadFile(registryPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := reconcileActivationManifest(root); err == nil {
		t.Fatal("unsafe activation directory was accepted")
	}
	registryAfter, err := os.ReadFile(registryPath)
	if err != nil || string(registryAfter) != string(registryBefore) {
		t.Fatalf("registry changed after manifest failure: %q %v", registryAfter, err)
	}
	manifestAfter, err := os.ReadFile(manifestPath)
	if err != nil || string(manifestAfter) != string(written) {
		t.Fatalf("manifest changed after rewrite failure: %q %v", manifestAfter, err)
	}
}
