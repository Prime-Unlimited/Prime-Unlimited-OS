package main

import (
	"errors"
	"os"
	"path/filepath"
	"time"
)

const failureQuarantineThreshold = 3

type modSafetyState struct {
	FailureCount int   `json:"failureCount"`
	Quarantined  bool  `json:"quarantined"`
	LastFailure  int64 `json:"lastFailure,omitempty"`
}

type safetyState struct {
	SchemaVersion int                       `json:"schemaVersion"`
	SafeMode      bool                      `json:"safeMode"`
	Mods          map[string]modSafetyState `json:"mods"`
}

func emptySafetyState() safetyState {
	return safetyState{SchemaVersion: 1, Mods: map[string]modSafetyState{}}
}

func loadSafetyState(root string) (safetyState, error) {
	path := filepath.Join(root, "safety", "state.json")
	data, err := readOwnedFile(path, 1024*1024, "safety state")
	if os.IsNotExist(err) {
		return emptySafetyState(), nil
	}
	if err != nil {
		return safetyState{}, err
	}
	var state safetyState
	if decodeStrictJSON(data, &state) != nil || state.SchemaVersion != 1 || state.Mods == nil {
		return safetyState{}, errors.New("invalid safety state")
	}
	for id, item := range state.Mods {
		if !modIDPattern.MatchString(id) || item.FailureCount < 0 || item.FailureCount > failureQuarantineThreshold || item.Quarantined != (item.FailureCount >= failureQuarantineThreshold) {
			return safetyState{}, errors.New("invalid mod safety state")
		}
	}
	return state, nil
}

func writeSafetyState(root string, state safetyState) error {
	return writeAtomicJSON(filepath.Join(root, "safety", "state.json"), state)
}

func setSafeMode(root string, enabled bool) (safetyState, error) {
	state, err := loadSafetyState(root)
	if err != nil {
		return state, err
	}
	state.SafeMode = enabled
	if err := writeSafetyState(root, state); err != nil {
		return state, err
	}
	return state, reconcileServices(root)
}

func recoverQuarantinedMod(root, id string) (safetyState, error) {
	state, err := loadSafetyState(root)
	if err != nil {
		return state, err
	}
	if !modIDPattern.MatchString(id) {
		return state, errors.New("invalid mod id")
	}
	delete(state.Mods, id)
	if err := writeSafetyState(root, state); err != nil {
		return state, err
	}
	return state, reconcileServices(root)
}

func recordRunOutcome(root, id string, succeeded bool) (modSafetyState, error) {
	state, err := loadSafetyState(root)
	if err != nil {
		return modSafetyState{}, err
	}
	if succeeded {
		delete(state.Mods, id)
	} else {
		item := state.Mods[id]
		if item.FailureCount < failureQuarantineThreshold {
			item.FailureCount++
		}
		item.Quarantined = item.FailureCount >= failureQuarantineThreshold
		item.LastFailure = time.Now().Unix()
		state.Mods[id] = item
	}
	item := state.Mods[id]
	if err := writeSafetyState(root, state); err != nil {
		return item, err
	}
	return item, reconcileServices(root)
}
