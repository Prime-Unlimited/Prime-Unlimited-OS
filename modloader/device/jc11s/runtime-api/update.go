package main

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type rollbackRecord struct {
	SchemaVersion  int             `json:"schemaVersion"`
	ModID          string          `json:"modId"`
	Payload        string          `json:"payload"`
	Previous       installedMod    `json:"previous"`
	PreviousSafety *modSafetyState `json:"previousSafety,omitempty"`
	CreatedAt      int64           `json:"createdAt"`
}

type updateResult struct {
	TransactionID string `json:"transactionId"`
	ID            string `json:"id"`
	Version       string `json:"version"`
	Revision      int    `json:"revision"`
	Updated       bool   `json:"updated"`
	RolledBack    bool   `json:"rolledBack"`
}

func installedFromCatalog(item mod, catalogID, root string, enabled bool) installedMod {
	return installedMod{ID: item.ID, Version: item.Version, Revision: item.Revision, PackageSHA256: item.SHA256,
		CatalogID: catalogID, Architecture: item.Architecture, ABI: item.ABI, Entrypoint: item.Entrypoint,
		Overlay: item.Overlay, Service: item.Service, Root: root, Enabled: enabled, InstalledAt: time.Now().Unix(),
		Dependencies: item.Dependencies, Conflicts: item.Conflicts}
}

func restoreUpdate(target, backup string) {
	_ = os.RemoveAll(target)
	if _, err := os.Lstat(backup); err == nil {
		_ = os.Rename(backup, target)
	}
}

func updateDisabledMod(item mod, catalogID, cacheRoot, stagingRoot, root string) (result updateResult, err error) {
	committed := false
	result.ID, result.Version, result.Revision = item.ID, item.Version, item.Revision
	if _, err = inspectStagedPackage(item, catalogID, cacheRoot, stagingRoot); err != nil {
		return
	}
	registryRoot := filepath.Join(root, "registry")
	registryPath := filepath.Join(registryRoot, "state.json")
	state, err := loadRegistry(registryPath)
	if err != nil {
		return result, err
	}
	previous, exists := state.Mods[item.ID]
	if !exists {
		return result, errNotInstalled
	}
	if previous.Enabled {
		return result, errors.New("enabled mod cannot be updated")
	}
	safety, err := loadSafetyState(root)
	if err != nil {
		return result, err
	}
	var previousSafety *modSafetyState
	if value, ok := safety.Mods[item.ID]; ok {
		copy := value
		previousSafety = &copy
	}
	available, _ := updateDetails(item, previous)
	if !available {
		return result, errors.New("catalog item is not newer")
	}
	oldTarget := filepath.Join(root, filepath.FromSlash(previous.Root))
	if info, statErr := os.Lstat(oldTarget); statErr != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return result, errors.New("installed payload is unsafe")
	}
	newRoot := filepath.ToSlash(filepath.Join("mods", "installed", item.ID, item.Version))
	target := filepath.Join(root, filepath.FromSlash(newRoot))
	if target != oldTarget {
		if _, statErr := os.Lstat(target); statErr == nil {
			return result, errors.New("update target already exists")
		} else if !os.IsNotExist(statErr) {
			return result, statErr
		}
	}
	versionRoot := filepath.Dir(oldTarget)
	temporary, err := os.MkdirTemp(versionRoot, ".update-*")
	if err != nil {
		return result, err
	}
	defer os.RemoveAll(temporary)
	if err = os.Chmod(temporary, 0o700); err != nil {
		return result, err
	}
	if err = copyInstalledTree(filepath.Join(stagingRoot, item.SHA256), temporary); err != nil {
		return result, err
	}
	txid, err := randomTransactionID()
	if err != nil {
		return result, err
	}
	result.TransactionID = txid
	rollbackRoot := filepath.Join(root, "mods", "rollback", item.ID)
	if err = ensurePrivateDirectory(rollbackRoot); err != nil {
		return result, err
	}
	backup := filepath.Join(rollbackRoot, txid)
	if err = os.Rename(oldTarget, backup); err != nil {
		return result, err
	}
	defer func() {
		if err != nil && !committed {
			_ = os.RemoveAll(target)
			if _, statErr := os.Lstat(backup); statErr == nil {
				_ = os.Rename(backup, oldTarget)
			}
			result.RolledBack = true
		}
	}()
	if err = os.Rename(temporary, target); err != nil {
		return result, err
	}
	next := registryState{SchemaVersion: 1, Mods: make(map[string]installedMod, len(state.Mods))}
	for id, installed := range state.Mods {
		next.Mods[id] = installed
	}
	next.Mods[item.ID] = installedFromCatalog(item, catalogID, newRoot, false)
	if err = writeAtomicJSON(registryPath, next); err != nil {
		return result, err
	}
	if err = writeActivationManifest(root, next); err != nil {
		_ = writeAtomicJSON(registryPath, state)
		return result, err
	}
	record := rollbackRecord{SchemaVersion: 1, ModID: item.ID, Payload: filepath.ToSlash(filepath.Join("mods", "rollback", item.ID, txid)), Previous: previous, PreviousSafety: previousSafety, CreatedAt: time.Now().Unix()}
	if err = writeAtomicJSON(filepath.Join(rollbackRoot, "current.json"), record); err != nil {
		_ = writeAtomicJSON(registryPath, state)
		_ = writeActivationManifest(root, state)
		return result, err
	}
	// Quarantine records failures of the exact old payload. A different signed
	// and inspected payload receives one clean retry. Global Safe Mode remains.
	delete(safety.Mods, item.ID)
	if err = writeSafetyState(root, safety); err != nil {
		_ = os.Remove(filepath.Join(rollbackRoot, "current.json"))
		_ = writeAtomicJSON(registryPath, state)
		_ = writeActivationManifest(root, state)
		return result, err
	}
	committed = true
	if entries, readErr := os.ReadDir(rollbackRoot); readErr == nil {
		for _, entry := range entries {
			if entry.Name() != txid && entry.Name() != "current.json" {
				_ = os.RemoveAll(filepath.Join(rollbackRoot, entry.Name()))
			}
		}
	}
	now := time.Now().Unix()
	transaction := transactionRecord{ID: txid, Operation: "update", ModID: item.ID, Version: item.Version, Status: "completed", Stage: "registry-committed", CreatedAt: now, CompletedAt: now}
	_ = writeAtomicJSON(filepath.Join(registryRoot, "transactions", txid+".json"), transaction)
	result.Updated = true
	return result, nil
}

func rollbackMod(id, root string) (updateResult, error) {
	result := updateResult{ID: id}
	if !modIDPattern.MatchString(id) {
		return result, errors.New("invalid mod id")
	}
	registryPath := filepath.Join(root, "registry", "state.json")
	state, err := loadRegistry(registryPath)
	if err != nil {
		return result, err
	}
	current, exists := state.Mods[id]
	if !exists {
		return result, errNotInstalled
	}
	if current.Enabled {
		return result, errors.New("enabled mod cannot be rolled back")
	}
	rollbackRoot := filepath.Join(root, "mods", "rollback", id)
	data, err := readOwnedFile(filepath.Join(rollbackRoot, "current.json"), 1024*1024, "rollback record")
	if err != nil {
		return result, err
	}
	var record rollbackRecord
	expectedPrefix := filepath.ToSlash(filepath.Join("mods", "rollback", id)) + "/"
	if decodeStrictJSON(data, &record) != nil || record.SchemaVersion != 1 || record.ModID != id ||
		!strings.HasPrefix(record.Payload, expectedPrefix) || filepath.ToSlash(filepath.Clean(record.Payload)) != record.Payload {
		return result, errors.New("invalid rollback record")
	}
	backup := filepath.Join(root, filepath.FromSlash(record.Payload))
	target := filepath.Join(root, filepath.FromSlash(current.Root))
	previousTarget := filepath.Join(root, filepath.FromSlash(record.Previous.Root))
	if info, statErr := os.Lstat(backup); statErr != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return result, errors.New("rollback payload unavailable")
	}
	txid, err := randomTransactionID()
	if err != nil {
		return result, err
	}
	result.TransactionID = txid
	failed := filepath.Join(rollbackRoot, "replaced-"+txid)
	if err = os.Rename(target, failed); err != nil {
		return result, err
	}
	if err = os.Rename(backup, previousTarget); err != nil {
		_ = os.Rename(failed, target)
		return result, err
	}
	next := registryState{SchemaVersion: 1, Mods: make(map[string]installedMod, len(state.Mods))}
	for key, value := range state.Mods {
		next.Mods[key] = value
	}
	next.Mods[id] = record.Previous
	if err = writeAtomicJSON(registryPath, next); err != nil {
		_ = os.Rename(previousTarget, backup)
		_ = os.Rename(failed, target)
		return result, err
	}
	safety, err := loadSafetyState(root)
	if err != nil {
		_ = writeAtomicJSON(registryPath, state)
		_ = os.Rename(previousTarget, backup)
		_ = os.Rename(failed, target)
		return result, err
	}
	if record.PreviousSafety == nil {
		delete(safety.Mods, id)
	} else {
		safety.Mods[id] = *record.PreviousSafety
	}
	if err = writeSafetyState(root, safety); err != nil {
		_ = writeAtomicJSON(registryPath, state)
		_ = os.Rename(previousTarget, backup)
		_ = os.Rename(failed, target)
		return result, err
	}
	_ = os.RemoveAll(failed)
	_ = os.Remove(filepath.Join(rollbackRoot, "current.json"))
	now := time.Now().Unix()
	_ = writeAtomicJSON(filepath.Join(root, "registry", "transactions", txid+".json"), transactionRecord{ID: txid, Operation: "rollback", ModID: id, Version: record.Previous.Version, Status: "completed", Stage: "registry-committed", CreatedAt: now, CompletedAt: now})
	result.Version, result.Revision, result.RolledBack = record.Previous.Version, record.Previous.Revision, true
	return result, nil
}

func (s *server) handleUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	request, err := decodeCacheRequest(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	item, ok := s.requireInstallable(w, request.ID)
	if !ok {
		return
	}
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	s.catalogMu.RLock()
	catalogID := s.catalog.Catalog.CatalogID
	s.catalogMu.RUnlock()
	result, err := updateDisabledMod(item, catalogID, s.cacheRoot, s.stagingRoot, s.root)
	if err != nil {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "verified update failed; previous version retained", "rolledBack": result.RolledBack})
		return
	}
	writeJSON(w, http.StatusAccepted, result)
}

func (s *server) handleRollback(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	request, err := decodeCacheRequest(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	result, err := rollbackMod(request.ID, s.root)
	if err != nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "rollback unavailable"})
		return
	}
	writeJSON(w, http.StatusAccepted, result)
}
