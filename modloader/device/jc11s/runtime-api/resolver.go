package main

import (
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type semanticVersion struct {
	major, minor, patch int
	prerelease          string
}

type resolutionNode struct {
	Type    string `json:"type"`
	ID      string `json:"id"`
	Version string `json:"version"`
}

type resolutionResult struct {
	ID    string           `json:"id"`
	Order []resolutionNode `json:"order"`
}

func parseSemanticVersion(value string) (semanticVersion, error) {
	var result semanticVersion
	parts := strings.SplitN(value, "-", 2)
	numbers := strings.Split(parts[0], ".")
	if len(numbers) < 1 || len(numbers) > 3 {
		return result, errors.New("unsupported version")
	}
	parsed := make([]int, 3)
	for index, number := range numbers {
		if number == "" || (len(number) > 1 && number[0] == '0') {
			return result, errors.New("unsupported version")
		}
		value, err := strconv.Atoi(number)
		if err != nil || value < 0 {
			return result, errors.New("unsupported version")
		}
		parsed[index] = value
	}
	result.major, result.minor, result.patch = parsed[0], parsed[1], parsed[2]
	if len(parts) == 2 {
		if parts[1] == "" {
			return result, errors.New("unsupported version")
		}
		for _, char := range parts[1] {
			if !(char >= '0' && char <= '9') && !(char >= 'A' && char <= 'Z') && !(char >= 'a' && char <= 'z') && char != '.' && char != '-' {
				return result, errors.New("unsupported version")
			}
		}
		result.prerelease = parts[1]
	}
	return result, nil
}

func compareVersions(left, right semanticVersion) int {
	for _, pair := range [][2]int{{left.major, right.major}, {left.minor, right.minor}, {left.patch, right.patch}} {
		if pair[0] < pair[1] {
			return -1
		}
		if pair[0] > pair[1] {
			return 1
		}
	}
	if left.prerelease == right.prerelease {
		return 0
	}
	if left.prerelease == "" {
		return 1
	}
	if right.prerelease == "" {
		return -1
	}
	if left.prerelease < right.prerelease {
		return -1
	}
	return 1
}

func versionSatisfies(version, constraint string) (bool, error) {
	if constraint == "" || constraint == "*" {
		return true, nil
	}
	actual, err := parseSemanticVersion(version)
	if err != nil {
		return false, err
	}
	for _, raw := range strings.Split(constraint, ",") {
		term := strings.TrimSpace(raw)
		if term == "" {
			continue
		}
		if strings.HasSuffix(term, ".*") {
			prefix := strings.Split(strings.TrimSuffix(term, ".*"), ".")
			if len(prefix) > 3 || len(prefix) == 0 {
				return false, errors.New("invalid wildcard constraint")
			}
			values := []int{actual.major, actual.minor, actual.patch}
			for index, part := range prefix {
				number, parseErr := strconv.Atoi(part)
				if parseErr != nil || number != values[index] {
					return false, parseErr
				}
			}
			continue
		}
		if term[0] == '^' || term[0] == '~' {
			base, parseErr := parseSemanticVersion(term[1:])
			if parseErr != nil {
				return false, parseErr
			}
			if compareVersions(actual, base) < 0 {
				return false, nil
			}
			boundary := semanticVersion{major: base.major, minor: base.minor + 1}
			if term[0] == '^' {
				if base.major > 0 {
					boundary = semanticVersion{major: base.major + 1}
				} else {
					boundary = semanticVersion{minor: base.minor + 1}
				}
			}
			if compareVersions(actual, boundary) >= 0 {
				return false, nil
			}
			continue
		}
		op := "="
		wanted := term
		for _, candidate := range []string{"<=", ">=", "==", "<", ">", "="} {
			if strings.HasPrefix(term, candidate) {
				op, wanted = candidate, strings.TrimSpace(strings.TrimPrefix(term, candidate))
				break
			}
		}
		expected, parseErr := parseSemanticVersion(wanted)
		if parseErr != nil {
			return false, parseErr
		}
		comparison := compareVersions(actual, expected)
		valid := (op == "=" || op == "==") && comparison == 0 || op == "<" && comparison < 0 || op == ">" && comparison > 0 || op == "<=" && comparison <= 0 || op == ">=" && comparison >= 0
		if !valid {
			return false, nil
		}
	}
	return true, nil
}

func (s *server) resolveCatalogMod(id string) (resolutionResult, error) {
	result := resolutionResult{ID: id, Order: []resolutionNode{}}
	if s.catalog.Status != "valid" || !s.catalog.Trusted || s.catalog.Catalog.SchemaVersion != 2 {
		return result, errors.New("trusted v2 catalog required")
	}
	mods := make(map[string]mod, len(s.catalog.Catalog.Mods))
	for _, item := range s.catalog.Catalog.Mods {
		mods[item.ID] = item
	}
	libraries := make(map[string]library, len(s.catalog.Catalog.Libraries))
	for _, item := range s.catalog.Catalog.Libraries {
		libraries[item.ID] = item
	}
	registry, err := loadRegistry(filepath.Join(s.root, "registry", "state.json"))
	if err != nil {
		return result, err
	}
	visiting, visited := map[string]bool{}, map[string]bool{}
	selectedMods := map[string]bool{}
	var visit func(string, string, string) error
	visit = func(kind, nodeID, constraint string) error {
		key := kind + ":" + nodeID
		if visiting[key] {
			return fmt.Errorf("cyclic dependency involving %s", key)
		}
		if visited[key] {
			for _, node := range result.Order {
				if node.Type == kind && node.ID == nodeID {
					ok, checkErr := versionSatisfies(node.Version, constraint)
					if checkErr != nil || !ok {
						return errors.New("incompatible combined version constraints")
					}
				}
			}
			return nil
		}
		var version string
		var dependencies []dependency
		var conflicts []string
		if kind == "mod" {
			item, exists := mods[nodeID]
			if !exists || !s.compatible(item) {
				return fmt.Errorf("compatible mod %s not found", nodeID)
			}
			version, dependencies, conflicts = item.Version, append([]dependency{}, item.Dependencies...), item.Conflicts
			for _, dep := range item.Libraries {
				dep.Type = "library"
				dependencies = append(dependencies, dep)
			}
			selectedMods[nodeID] = true
		} else {
			item, exists := libraries[nodeID]
			if !exists || item.Revoked || !contains(item.Models, s.model) || !contains(item.Firmware, s.firmware) || item.Architecture != s.architecture || item.ABI != s.abi || item.Approval == nil || !contains(item.Approval.Devices, s.model) {
				return fmt.Errorf("compatible library %s not found", nodeID)
			}
			version, dependencies, conflicts = item.Version, item.Dependencies, item.Conflicts
		}
		ok, checkErr := versionSatisfies(version, constraint)
		if checkErr != nil || !ok {
			return fmt.Errorf("%s %s does not satisfy %s", kind, nodeID, constraint)
		}
		for _, conflict := range conflicts {
			if selectedMods[conflict] || registry.Mods[conflict].Enabled {
				return fmt.Errorf("%s conflicts with %s", nodeID, conflict)
			}
		}
		visiting[key] = true
		sort.Slice(dependencies, func(i, j int) bool {
			left, right := dependencies[i].Type+":"+dependencies[i].ID, dependencies[j].Type+":"+dependencies[j].ID
			return left < right
		})
		for _, dep := range dependencies {
			depKind := dep.Type
			if depKind == "" {
				depKind = "mod"
			}
			if err := visit(depKind, dep.ID, dep.Constraint); err != nil {
				return err
			}
		}
		visiting[key], visited[key] = false, true
		result.Order = append(result.Order, resolutionNode{Type: kind, ID: nodeID, Version: version})
		return nil
	}
	if err := visit("mod", id, "*"); err != nil {
		return result, err
	}
	for selected := range selectedMods {
		for _, item := range mods {
			if selectedMods[item.ID] {
				for _, conflict := range item.Conflicts {
					if conflict == selected {
						return result, fmt.Errorf("%s conflicts with %s", item.ID, selected)
					}
				}
			}
		}
	}
	for installedID, installed := range registry.Mods {
		if !installed.Enabled {
			continue
		}
		if item, exists := mods[installedID]; exists {
			for _, conflict := range item.Conflicts {
				if selectedMods[conflict] {
					return result, fmt.Errorf("enabled %s conflicts with %s", installedID, conflict)
				}
			}
		}
	}
	return result, nil
}
