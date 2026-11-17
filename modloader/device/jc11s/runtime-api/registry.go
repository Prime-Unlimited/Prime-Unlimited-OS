package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
)

type installedMod struct {
	ID            string `json:"id"`
	Version       string `json:"version"`
	PackageSHA256 string `json:"packageSha256"`
	CatalogID     string `json:"catalogId"`
	Architecture  string `json:"architecture"`
	ABI           string `json:"abi"`
	Entrypoint    string `json:"entrypoint"`
	Root          string `json:"root"`
	Enabled       bool   `json:"enabled"`
	InstalledAt   int64  `json:"installedAt"`
}

type registryState struct {
	SchemaVersion int                     `json:"schemaVersion"`
	Mods          map[string]installedMod `json:"mods"`
}

type installJournal struct {
	SchemaVersion int           `json:"schemaVersion"`
	Operation     string        `json:"operation"`
	Phase         string        `json:"phase"`
	TransactionID string        `json:"transactionId"`
	Target        string        `json:"target"`
	Quarantine    string        `json:"quarantine,omitempty"`
	Previous      registryState `json:"previous"`
	Next          registryState `json:"next"`
}

type transactionRecord struct {
	ID          string `json:"id"`
	Operation   string `json:"operation"`
	ModID       string `json:"modId"`
	Version     string `json:"version"`
	Status      string `json:"status"`
	Stage       string `json:"stage"`
	CreatedAt   int64  `json:"createdAt"`
	CompletedAt int64  `json:"completedAt,omitempty"`
}

type installResult struct {
	TransactionID string `json:"transactionId"`
	ID            string `json:"id"`
	Version       string `json:"version"`
	Installed     bool   `json:"installed"`
	Enabled       bool   `json:"enabled"`
}

type uninstallResult struct {
	TransactionID string `json:"transactionId"`
	ID            string `json:"id"`
	Version       string `json:"version"`
	Uninstalled   bool   `json:"uninstalled"`
}

type activationResult struct {
	TransactionID string `json:"transactionId"`
	ID            string `json:"id"`
	Version       string `json:"version"`
	Enabled       bool   `json:"enabled"`
}

type activationEntry struct {
	ID         string `json:"id"`
	Version    string `json:"version"`
	Root       string `json:"root"`
	Entrypoint string `json:"entrypoint"`
}

type activationManifest struct {
	SchemaVersion int               `json:"schemaVersion"`
	Mods          []activationEntry `json:"mods"`
}

var errAlreadyInstalled = errors.New("mod is already installed")
var errNotInstalled = errors.New("mod is not installed")
var installCheckpoint = func(string) error { return nil }
var uninstallCheckpoint = func(string) error { return nil }
var activationCheckpoint = func(string) error { return nil }

func ensurePrivateDirectory(path string) error {
	if err := os.MkdirAll(path, 0o700); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0o700 {
		return errors.New("private state directory is unsafe")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != uint32(os.Geteuid()) {
		return errors.New("private state directory has wrong owner")
	}
	return nil
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	err = directory.Sync()
	closeErr := directory.Close()
	if err != nil {
		return err
	}
	return closeErr
}

func writeAtomicJSON(path string, value any) error {
	directory := filepath.Dir(path)
	if err := ensurePrivateDirectory(directory); err != nil {
		return err
	}
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	temporary, err := os.CreateTemp(directory, ".state-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return err
	}
	if _, err = temporary.Write(data); err == nil {
		err = temporary.Sync()
	}
	if closeErr := temporary.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return err
	}
	return syncDirectory(directory)
}

func emptyRegistry() registryState {
	return registryState{SchemaVersion: 1, Mods: map[string]installedMod{}}
}

func loadRegistry(path string) (registryState, error) {
	data, err := readOwnedFile(path, 4*1024*1024, "installed registry")
	if os.IsNotExist(err) {
		return emptyRegistry(), nil
	}
	if err != nil {
		return registryState{}, err
	}
	var state registryState
	if err := decodeStrictJSON(data, &state); err != nil {
		return registryState{}, errors.New("invalid installed registry")
	}
	if err := validateRegistryState(state); err != nil {
		return registryState{}, err
	}
	return state, nil
}

func validateRegistryState(state registryState) error {
	if state.SchemaVersion != 1 || state.Mods == nil {
		return errors.New("invalid installed registry")
	}
	for id, item := range state.Mods {
		if id != item.ID || !modIDPattern.MatchString(id) || !versionPattern.MatchString(item.Version) || len(item.PackageSHA256) != 64 {
			return errors.New("invalid installed registry entry")
		}
		expectedRoot := filepath.ToSlash(filepath.Join("mods", "installed", item.ID, item.Version))
		if item.Root != expectedRoot {
			return errors.New("invalid installed registry root")
		}
	}
	return nil
}

func writeActivationManifest(root string, state registryState) error {
	manifest := activationManifest{SchemaVersion: 1, Mods: []activationEntry{}}
	ids := make([]string, 0, len(state.Mods))
	for id, item := range state.Mods {
		if item.Enabled {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	for _, id := range ids {
		item := state.Mods[id]
		manifest.Mods = append(manifest.Mods, activationEntry{ID: id, Version: item.Version, Root: item.Root, Entrypoint: item.Entrypoint})
	}
	return writeAtomicJSON(filepath.Join(root, "activation", "manifest.json"), manifest)
}

func setModEnabled(id string, enabled bool, root string) (activationResult, error) {
	result := activationResult{ID: id, Enabled: enabled}
	if !modIDPattern.MatchString(id) {
		return result, errors.New("invalid mod id")
	}
	if err := recoverInstall(root); err != nil {
		return result, err
	}
	registryRoot := filepath.Join(root, "registry")
	transactionsRoot := filepath.Join(registryRoot, "transactions")
	for _, directory := range []string{registryRoot, transactionsRoot} {
		if err := ensurePrivateDirectory(directory); err != nil {
			return result, err
		}
	}
	registryPath := filepath.Join(registryRoot, "state.json")
	journalPath := filepath.Join(registryRoot, "pending.json")
	state, err := loadRegistry(registryPath)
	if err != nil {
		return result, err
	}
	installed, exists := state.Mods[id]
	if !exists {
		return result, errNotInstalled
	}
	result.Version = installed.Version
	if installed.Enabled == enabled {
		return result, errors.New("mod already has requested activation state")
	}
	target := filepath.Join(root, filepath.FromSlash(installed.Root))
	entrypoint := filepath.Join(target, filepath.FromSlash(installed.Entrypoint))
	info, err := os.Lstat(entrypoint)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0o600 {
		return result, errors.New("installed entrypoint is missing or unsafe")
	}
	txid, err := randomTransactionID()
	if err != nil {
		return result, err
	}
	result.TransactionID = txid
	next := registryState{SchemaVersion: 1, Mods: make(map[string]installedMod, len(state.Mods))}
	for modID, item := range state.Mods {
		next.Mods[modID] = item
	}
	installed.Enabled = enabled
	next.Mods[id] = installed
	operation := "disable"
	if enabled {
		operation = "enable"
	}
	journal := installJournal{SchemaVersion: 1, Operation: operation, Phase: "prepared", TransactionID: txid,
		Target: installed.Root, Previous: state, Next: next}
	if err := writeAtomicJSON(journalPath, journal); err != nil {
		return result, err
	}
	if err := activationCheckpoint("journal-prepared"); err != nil {
		return result, err
	}
	if err := writeAtomicJSON(registryPath, next); err != nil {
		return result, err
	}
	if err := activationCheckpoint("registry-committed"); err != nil {
		return result, err
	}
	if err := writeActivationManifest(root, next); err != nil {
		return result, err
	}
	if err := activationCheckpoint("manifest-committed"); err != nil {
		return result, err
	}
	journal.Phase = "committed"
	if err := writeAtomicJSON(journalPath, journal); err != nil {
		return result, err
	}
	if err := activationCheckpoint("journal-committed"); err != nil {
		return result, err
	}
	now := time.Now().Unix()
	record := transactionRecord{ID: txid, Operation: operation, ModID: id, Version: installed.Version, Status: "completed",
		Stage: "manifest-committed", CreatedAt: now, CompletedAt: now}
	if err := writeAtomicJSON(filepath.Join(transactionsRoot, txid+".json"), record); err != nil {
		return result, err
	}
	if err := os.Remove(journalPath); err != nil {
		return result, err
	}
	if err := syncDirectory(registryRoot); err != nil {
		return result, err
	}
	return result, nil
}

func randomTransactionID() (string, error) {
	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	return hex.EncodeToString(random), nil
}

func copyInstalledTree(source, temporary string) error {
	directories := []string{temporary}
	err := filepath.Walk(source, func(current string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(source, current)
		if err != nil {
			return err
		}
		if relative == "." {
			return nil
		}
		if relative == stageMarker {
			return nil
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return errors.New("staged symlink refused")
		}
		target := filepath.Join(temporary, relative)
		if info.IsDir() {
			if err := os.Mkdir(target, 0o700); err != nil {
				return err
			}
			if err := os.Chmod(target, 0o700); err != nil {
				return err
			}
			directories = append(directories, target)
			return nil
		}
		if !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
			return errors.New("unsafe staged file")
		}
		input, err := os.Open(current)
		if err != nil {
			return err
		}
		output, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			input.Close()
			return err
		}
		_, copyErr := io.Copy(output, input)
		closeInErr := input.Close()
		syncErr := output.Sync()
		closeOutErr := output.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeInErr != nil {
			return closeInErr
		}
		if syncErr != nil {
			return syncErr
		}
		return closeOutErr
	})
	if err != nil {
		return err
	}
	for index := len(directories) - 1; index >= 0; index-- {
		if err := syncDirectory(directories[index]); err != nil {
			return err
		}
	}
	return nil
}

func installDisabledMod(item mod, catalogID, cacheRoot, stagingRoot, root string) (installResult, error) {
	result := installResult{ID: item.ID, Version: item.Version, Enabled: false}
	if err := recoverInstall(root); err != nil {
		return result, err
	}
	if _, err := inspectStagedPackage(item, catalogID, cacheRoot, stagingRoot); err != nil {
		return result, err
	}
	registryRoot := filepath.Join(root, "registry")
	installedRoot := filepath.Join(root, "mods", "installed")
	transactionsRoot := filepath.Join(registryRoot, "transactions")
	for _, directory := range []string{registryRoot, installedRoot, transactionsRoot} {
		if err := ensurePrivateDirectory(directory); err != nil {
			return result, err
		}
	}
	registryPath := filepath.Join(registryRoot, "state.json")
	journalPath := filepath.Join(registryRoot, "pending.json")
	state, err := loadRegistry(registryPath)
	if err != nil {
		return result, err
	}
	if _, exists := state.Mods[item.ID]; exists {
		return result, errAlreadyInstalled
	}
	target := filepath.Join(installedRoot, item.ID, item.Version)
	if _, err := os.Lstat(target); err == nil {
		return result, errors.New("install target already exists")
	} else if !os.IsNotExist(err) {
		return result, err
	}
	versionRoot := filepath.Dir(target)
	if err := ensurePrivateDirectory(versionRoot); err != nil {
		return result, err
	}
	temporary, err := os.MkdirTemp(versionRoot, ".install-*")
	if err != nil {
		return result, err
	}
	defer os.RemoveAll(temporary)
	if err := os.Chmod(temporary, 0o700); err != nil {
		return result, err
	}
	if err := copyInstalledTree(filepath.Join(stagingRoot, item.SHA256), temporary); err != nil {
		return result, err
	}
	if err := syncDirectory(temporary); err != nil {
		return result, err
	}
	txid, err := randomTransactionID()
	if err != nil {
		return result, err
	}
	result.TransactionID = txid
	next := registryState{SchemaVersion: 1, Mods: make(map[string]installedMod, len(state.Mods)+1)}
	for id, installed := range state.Mods {
		next.Mods[id] = installed
	}
	relativeTarget := filepath.ToSlash(filepath.Join("mods", "installed", item.ID, item.Version))
	next.Mods[item.ID] = installedMod{ID: item.ID, Version: item.Version, PackageSHA256: item.SHA256, CatalogID: catalogID,
		Architecture: item.Architecture, ABI: item.ABI, Entrypoint: item.Entrypoint, Root: relativeTarget, Enabled: false, InstalledAt: time.Now().Unix()}
	journal := installJournal{SchemaVersion: 1, Operation: "install", Phase: "prepared", TransactionID: txid, Target: relativeTarget, Previous: state, Next: next}
	if err := writeAtomicJSON(journalPath, journal); err != nil {
		return result, err
	}
	if err := installCheckpoint("journal-prepared"); err != nil {
		return result, err
	}
	if err := os.Rename(temporary, target); err != nil {
		return result, err
	}
	if err := syncDirectory(versionRoot); err != nil {
		return result, err
	}
	if err := installCheckpoint("files-published"); err != nil {
		return result, err
	}
	if err := writeAtomicJSON(registryPath, next); err != nil {
		return result, err
	}
	if err := installCheckpoint("registry-committed"); err != nil {
		return result, err
	}
	journal.Phase = "committed"
	if err := writeAtomicJSON(journalPath, journal); err != nil {
		return result, err
	}
	if err := installCheckpoint("journal-committed"); err != nil {
		return result, err
	}
	record := transactionRecord{ID: txid, Operation: "install", ModID: item.ID, Version: item.Version, Status: "completed", Stage: "registry-committed", CreatedAt: time.Now().Unix(), CompletedAt: time.Now().Unix()}
	if err := writeAtomicJSON(filepath.Join(transactionsRoot, txid+".json"), record); err != nil {
		return result, err
	}
	if err := os.Remove(journalPath); err != nil {
		return result, err
	}
	if err := syncDirectory(registryRoot); err != nil {
		return result, err
	}
	result.Installed = true
	return result, nil
}

func uninstallDisabledMod(id, root string) (uninstallResult, error) {
	result := uninstallResult{ID: id}
	if !modIDPattern.MatchString(id) {
		return result, errors.New("invalid mod id")
	}
	if err := recoverInstall(root); err != nil {
		return result, err
	}
	registryRoot := filepath.Join(root, "registry")
	transactionsRoot := filepath.Join(registryRoot, "transactions")
	for _, directory := range []string{registryRoot, transactionsRoot} {
		if err := ensurePrivateDirectory(directory); err != nil {
			return result, err
		}
	}
	registryPath := filepath.Join(registryRoot, "state.json")
	journalPath := filepath.Join(registryRoot, "pending.json")
	state, err := loadRegistry(registryPath)
	if err != nil {
		return result, err
	}
	installed, exists := state.Mods[id]
	if !exists {
		return result, errNotInstalled
	}
	if installed.Enabled {
		return result, errors.New("enabled mod cannot be uninstalled")
	}
	result.Version = installed.Version
	target := filepath.Join(root, filepath.FromSlash(installed.Root))
	info, err := os.Lstat(target)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0o700 {
		return result, errors.New("installed mod payload is missing or unsafe")
	}
	txid, err := randomTransactionID()
	if err != nil {
		return result, err
	}
	result.TransactionID = txid
	versionRoot := filepath.Dir(target)
	quarantine := filepath.Join(versionRoot, ".uninstall-"+txid)
	relativeQuarantine, err := filepath.Rel(root, quarantine)
	if err != nil {
		return result, err
	}
	next := registryState{SchemaVersion: 1, Mods: make(map[string]installedMod, len(state.Mods)-1)}
	for modID, item := range state.Mods {
		if modID != id {
			next.Mods[modID] = item
		}
	}
	journal := installJournal{SchemaVersion: 1, Operation: "uninstall", Phase: "prepared", TransactionID: txid,
		Target: installed.Root, Quarantine: filepath.ToSlash(relativeQuarantine), Previous: state, Next: next}
	if err := writeAtomicJSON(journalPath, journal); err != nil {
		return result, err
	}
	if err := uninstallCheckpoint("journal-prepared"); err != nil {
		return result, err
	}
	if err := os.Rename(target, quarantine); err != nil {
		return result, err
	}
	if err := syncDirectory(versionRoot); err != nil {
		return result, err
	}
	if err := uninstallCheckpoint("payload-quarantined"); err != nil {
		return result, err
	}
	if err := writeAtomicJSON(registryPath, next); err != nil {
		return result, err
	}
	if err := uninstallCheckpoint("registry-committed"); err != nil {
		return result, err
	}
	journal.Phase = "committed"
	if err := writeAtomicJSON(journalPath, journal); err != nil {
		return result, err
	}
	if err := uninstallCheckpoint("journal-committed"); err != nil {
		return result, err
	}
	if err := os.RemoveAll(quarantine); err != nil {
		return result, err
	}
	if err := syncDirectory(versionRoot); err != nil {
		return result, err
	}
	now := time.Now().Unix()
	record := transactionRecord{ID: txid, Operation: "uninstall", ModID: id, Version: installed.Version, Status: "completed",
		Stage: "registry-committed", CreatedAt: now, CompletedAt: now}
	if err := writeAtomicJSON(filepath.Join(transactionsRoot, txid+".json"), record); err != nil {
		return result, err
	}
	if err := os.Remove(journalPath); err != nil {
		return result, err
	}
	if err := syncDirectory(registryRoot); err != nil {
		return result, err
	}
	_ = os.Remove(versionRoot)
	result.Uninstalled = true
	return result, nil
}

func recoverInstall(root string) error {
	registryRoot := filepath.Join(root, "registry")
	journalPath := filepath.Join(registryRoot, "pending.json")
	data, err := readOwnedFile(journalPath, 4*1024*1024, "install journal")
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var journal installJournal
	if decodeStrictJSON(data, &journal) != nil || journal.SchemaVersion != 1 || (journal.Operation != "install" && journal.Operation != "uninstall" && journal.Operation != "enable" && journal.Operation != "disable") || (journal.Phase != "prepared" && journal.Phase != "committed") || !strings.HasPrefix(journal.Target, "mods/installed/") {
		return errors.New("invalid install journal")
	}
	parts := strings.Split(journal.Target, "/")
	if len(parts) != 4 || !modIDPattern.MatchString(parts[2]) || !versionPattern.MatchString(parts[3]) {
		return errors.New("unsafe install journal target")
	}
	if !modIDPattern.MatchString(journal.TransactionID) || validateRegistryState(journal.Previous) != nil || validateRegistryState(journal.Next) != nil {
		return errors.New("invalid install journal state")
	}
	nextItem, nextExists := journal.Next.Mods[parts[2]]
	previousItem, previousExists := journal.Previous.Mods[parts[2]]
	target := filepath.Join(root, filepath.FromSlash(journal.Target))
	recoveredStage := journal.Phase
	if journal.Operation == "install" {
		if previousExists || !nextExists || len(journal.Next.Mods) != len(journal.Previous.Mods)+1 || nextItem.Version != parts[3] || nextItem.Root != journal.Target || journal.Quarantine != "" {
			return errors.New("install journal transition mismatch")
		}
	} else if journal.Operation == "uninstall" {
		expectedQuarantine := filepath.ToSlash(filepath.Join("mods", "installed", parts[2], ".uninstall-"+journal.TransactionID))
		if !previousExists || nextExists || len(journal.Previous.Mods) != len(journal.Next.Mods)+1 || previousItem.Version != parts[3] || previousItem.Root != journal.Target || journal.Quarantine != expectedQuarantine {
			return errors.New("uninstall journal transition mismatch")
		}
	} else {
		if !previousExists || !nextExists || len(journal.Previous.Mods) != len(journal.Next.Mods) || previousItem.Root != journal.Target || nextItem.Root != journal.Target || previousItem.Version != parts[3] || nextItem.Version != parts[3] || journal.Quarantine != "" || previousItem.Enabled == nextItem.Enabled || nextItem.Enabled != (journal.Operation == "enable") {
			return errors.New("activation journal transition mismatch")
		}
	}
	for id, item := range journal.Previous.Mods {
		if id != parts[2] && journal.Next.Mods[id] != item {
			return errors.New("journal changed unrelated registry entry")
		}
	}
	quarantine := filepath.Join(root, filepath.FromSlash(journal.Quarantine))
	if (journal.Operation == "enable" || journal.Operation == "disable") && journal.Phase == "prepared" {
		if err := writeAtomicJSON(filepath.Join(registryRoot, "state.json"), journal.Previous); err != nil {
			return err
		}
		if err := writeActivationManifest(root, journal.Previous); err != nil {
			return err
		}
	} else if journal.Operation == "enable" || journal.Operation == "disable" {
		if err := writeAtomicJSON(filepath.Join(registryRoot, "state.json"), journal.Next); err != nil {
			return err
		}
		if err := writeActivationManifest(root, journal.Next); err != nil {
			return err
		}
	} else if journal.Operation == "uninstall" && journal.Phase == "prepared" {
		if _, err := os.Lstat(quarantine); err == nil {
			if _, targetErr := os.Lstat(target); !os.IsNotExist(targetErr) {
				return errors.New("unsafe uninstall recovery collision")
			}
			if err := os.Rename(quarantine, target); err != nil {
				return err
			}
		} else if !os.IsNotExist(err) {
			return err
		}
		if err := writeAtomicJSON(filepath.Join(registryRoot, "state.json"), journal.Previous); err != nil {
			return err
		}
		if err := syncDirectory(filepath.Dir(target)); err != nil {
			return err
		}
	} else if journal.Operation == "uninstall" {
		if err := writeAtomicJSON(filepath.Join(registryRoot, "state.json"), journal.Next); err != nil {
			return err
		}
		if err := os.RemoveAll(quarantine); err != nil {
			return err
		}
		if err := os.RemoveAll(target); err != nil {
			return err
		}
		if err := syncDirectory(filepath.Dir(target)); err != nil {
			return err
		}
	} else if journal.Phase == "prepared" {
		if err := writeAtomicJSON(filepath.Join(registryRoot, "state.json"), journal.Previous); err != nil {
			return err
		}
		if err := os.RemoveAll(target); err != nil {
			return err
		}
		if err := syncDirectory(filepath.Dir(target)); err != nil {
			return err
		}
	} else {
		info, statErr := os.Lstat(target)
		if statErr != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0o700 {
			if err := writeAtomicJSON(filepath.Join(registryRoot, "state.json"), journal.Previous); err != nil {
				return err
			}
			recoveredStage = "rolled-back-missing-target"
		} else if err := writeAtomicJSON(filepath.Join(registryRoot, "state.json"), journal.Next); err != nil {
			return err
		}
	}
	record := transactionRecord{ID: journal.TransactionID, Operation: journal.Operation, ModID: parts[2], Version: parts[3], Status: "recovered", Stage: recoveredStage, CompletedAt: time.Now().Unix()}
	if err := writeAtomicJSON(filepath.Join(registryRoot, "transactions", journal.TransactionID+".json"), record); err != nil {
		return err
	}
	if err := os.Remove(journalPath); err != nil {
		return err
	}
	return syncDirectory(registryRoot)
}

func listTransactions(root string) ([]transactionRecord, error) {
	directory := filepath.Join(root, "registry", "transactions")
	if _, err := os.Lstat(directory); os.IsNotExist(err) {
		return []transactionRecord{}, nil
	} else if err != nil {
		return nil, err
	}
	if err := ensurePrivateDirectory(directory); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil, err
	}
	result := make([]transactionRecord, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			return nil, fmt.Errorf("unsafe transaction entry")
		}
		data, err := readOwnedFile(filepath.Join(directory, entry.Name()), 64*1024, "transaction record")
		var record transactionRecord
		if err != nil || decodeStrictJSON(data, &record) != nil {
			return nil, errors.New("invalid transaction record")
		}
		result = append(result, record)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].CreatedAt < result[j].CreatedAt })
	return result, nil
}
