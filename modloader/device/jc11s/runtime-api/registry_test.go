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
			journal := installJournal{SchemaVersion: 1, Phase: phase, TransactionID: "abcdef0123456789abcdef0123456789", Target: item.Root, Previous: previous, Next: next}
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

func TestRegistryRejectsEnabledOrUnsafeState(t *testing.T) {
	root := t.TempDir()
	item := installedMod{ID: "unsafe", Version: "1.0.0", PackageSHA256: strings.Repeat("a", 64), CatalogID: "test", Architecture: "armv7l", ABI: "gnueabihf", Entrypoint: "mod/start", Root: "mods/installed/unsafe/1.0.0", Enabled: true}
	path := filepath.Join(root, "registry", "state.json")
	if err := writeAtomicJSON(path, recoveryState(item)); err != nil {
		t.Fatal(err)
	}
	if _, err := loadRegistry(path); err == nil {
		t.Fatal("enabled state was accepted before activation support")
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
