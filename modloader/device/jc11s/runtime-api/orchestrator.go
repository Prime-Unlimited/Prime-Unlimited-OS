package main

import (
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"time"
)

type launchRun struct {
	ID          string         `json:"id"`
	RootModID   string         `json:"rootModId"`
	State       string         `json:"state"`
	StartedAt   int64          `json:"startedAt"`
	CompletedAt int64          `json:"completedAt"`
	Steps       []launchStatus `json:"steps"`
}

func resolveInstalledLaunchOrder(state registryState, id string) ([]string, error) {
	visiting, visited := map[string]bool{}, map[string]bool{}
	order := []string{}
	var visit func(string) error
	visit = func(current string) error {
		if visiting[current] {
			return fmt.Errorf("cyclic enabled dependency involving %s", current)
		}
		if visited[current] {
			return nil
		}
		item, exists := state.Mods[current]
		if !exists || !item.Enabled {
			return fmt.Errorf("required mod %s is not enabled", current)
		}
		visiting[current] = true
		dependencies := append([]dependency{}, item.Dependencies...)
		sort.Slice(dependencies, func(i, j int) bool { return dependencies[i].ID < dependencies[j].ID })
		for _, dep := range dependencies {
			if dep.Type != "" && dep.Type != "mod" {
				return errors.New("library process launch is unsupported")
			}
			dependencyItem, exists := state.Mods[dep.ID]
			if !exists || !dependencyItem.Enabled {
				return fmt.Errorf("required mod %s is not enabled", dep.ID)
			}
			matches, err := versionSatisfies(dependencyItem.Version, dep.Constraint)
			if err != nil || !matches {
				return fmt.Errorf("enabled mod %s does not satisfy %s", dep.ID, dep.Constraint)
			}
			if err := visit(dep.ID); err != nil {
				return err
			}
		}
		visiting[current], visited[current] = false, true
		order = append(order, current)
		return nil
	}
	if err := visit(id); err != nil {
		return nil, err
	}
	selected := map[string]bool{}
	for _, selectedID := range order {
		selected[selectedID] = true
	}
	for _, selectedID := range order {
		for _, conflict := range state.Mods[selectedID].Conflicts {
			if state.Mods[conflict].Enabled || selected[conflict] {
				return nil, fmt.Errorf("enabled %s conflicts with %s", selectedID, conflict)
			}
		}
		for otherID, other := range state.Mods {
			if !other.Enabled || otherID == selectedID {
				continue
			}
			for _, conflict := range other.Conflicts {
				if conflict == selectedID {
					return nil, fmt.Errorf("enabled %s conflicts with %s", otherID, selectedID)
				}
			}
		}
	}
	return order, nil
}

func launchEnabledSet(id, root string) (launchRun, error) {
	run := launchRun{RootModID: id, State: "failed", StartedAt: time.Now().Unix(), Steps: []launchStatus{}}
	txid, err := randomTransactionID()
	if err != nil {
		return run, err
	}
	run.ID = txid
	state, err := loadRegistry(filepath.Join(root, "registry", "state.json"))
	if err != nil {
		return run, err
	}
	order, err := resolveInstalledLaunchOrder(state, id)
	if err == nil {
		for _, modID := range order {
			status, launchErr := launchEnabledMod(modID, root)
			run.Steps = append(run.Steps, status)
			if launchErr != nil {
				err = fmt.Errorf("launch set stopped at %s", modID)
				break
			}
		}
	}
	run.CompletedAt = time.Now().Unix()
	if err == nil {
		run.State = "completed"
	}
	if writeErr := writeAtomicJSON(filepath.Join(root, "runtime", "runs", txid+".json"), run); writeErr != nil {
		return run, writeErr
	}
	if err != nil {
		return run, err
	}
	return run, nil
}

func loadLaunchRun(id, root string) (launchRun, error) {
	var run launchRun
	data, err := readOwnedFile(filepath.Join(root, "runtime", "runs", id+".json"), 1024*1024, "launch run")
	if err != nil {
		return run, err
	}
	if decodeStrictJSON(data, &run) != nil || run.ID != id {
		return run, errors.New("invalid launch run")
	}
	return run, nil
}
