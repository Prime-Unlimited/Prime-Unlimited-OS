package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func resolverMod(id, version string) mod {
	return mod{ID: id, Version: version, Models: []string{"JC11S"}, Firmware: []string{"5.3.0"}, Architecture: "armv7l", ABI: "gnueabihf", Approval: &approval{Maintainer: "test", Devices: []string{"JC11S"}}}
}

func resolverServer(t *testing.T, mods []mod, libraries []library) *server {
	t.Helper()
	return &server{model: "JC11S", firmware: "5.3.0", architecture: "armv7l", abi: "gnueabihf", root: t.TempDir(), authToken: strings.Repeat("a", 64), catalog: catalogState{Status: "valid", Trusted: true, Catalog: catalog{SchemaVersion: 2, CatalogID: "resolver-test", Mods: mods, Libraries: libraries}}}
}

func TestVersionConstraints(t *testing.T) {
	for _, test := range []struct {
		version, constraint string
		want                bool
	}{
		{"1.2.3", ">=1.2,<2.0", true}, {"1.4.0", "^1.2", true}, {"2.0.0", "^1.2", false}, {"1.2.9", "1.2.*", true}, {"1.3.0", "~1.2", false},
	} {
		got, err := versionSatisfies(test.version, test.constraint)
		if err != nil || got != test.want {
			t.Fatalf("%s %s: got=%v err=%v", test.version, test.constraint, got, err)
		}
	}
}

func TestResolverOrdersTransitiveDependenciesAndLibraries(t *testing.T) {
	app, helper := resolverMod("app", "1.0.0"), resolverMod("helper", "2.1.0")
	app.Dependencies = []dependency{{ID: "helper", Constraint: "^2.0"}}
	app.Libraries = []dependency{{ID: "base", Constraint: "1.*"}}
	base := library{ID: "base", Version: "1.4.0", Models: []string{"JC11S"}, Firmware: []string{"5.3.0"}, Architecture: "armv7l", ABI: "gnueabihf", Approval: &approval{Maintainer: "test", Devices: []string{"JC11S"}}}
	result, err := resolverServer(t, []mod{app, helper}, []library{base}).resolveCatalogMod("app")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"mod:helper", "library:base", "mod:app"}
	if len(result.Order) != len(want) {
		t.Fatalf("order: %#v", result.Order)
	}
	for index, node := range result.Order {
		if node.Type+":"+node.ID != want[index] {
			t.Fatalf("order: %#v", result.Order)
		}
	}
}

func TestResolverRejectsCycleMissingVersionAndConflict(t *testing.T) {
	a, b := resolverMod("a", "1.0.0"), resolverMod("b", "2.0.0")
	a.Dependencies = []dependency{{ID: "b", Constraint: "^2.0"}}
	b.Dependencies = []dependency{{ID: "a", Constraint: "*"}}
	if _, err := resolverServer(t, []mod{a, b}, nil).resolveCatalogMod("a"); err == nil || !strings.Contains(err.Error(), "cyclic") {
		t.Fatalf("cycle accepted: %v", err)
	}
	b.Dependencies = nil
	a.Dependencies[0].Constraint = "<2"
	if _, err := resolverServer(t, []mod{a, b}, nil).resolveCatalogMod("a"); err == nil {
		t.Fatal("bad version accepted")
	}
	a.Dependencies = nil
	a.Conflicts = []string{"b"}
	aServer := resolverServer(t, []mod{a, b}, nil)
	state := emptyRegistry()
	installed := installedMod{ID: "b", Version: "2.0.0", PackageSHA256: strings.Repeat("a", 64), Root: "mods/installed/b/2.0.0", Enabled: true}
	state.Mods["b"] = installed
	if err := writeAtomicJSON(aServer.root+"/registry/state.json", state); err != nil {
		t.Fatal(err)
	}
	if _, err := aServer.resolveCatalogMod("a"); err == nil {
		t.Fatal("installed conflict accepted")
	}
	a.Conflicts = nil
	b.Conflicts = []string{"a"}
	reverseServer := resolverServer(t, []mod{a, b}, nil)
	if err := writeAtomicJSON(reverseServer.root+"/registry/state.json", state); err != nil {
		t.Fatal(err)
	}
	if _, err := reverseServer.resolveCatalogMod("a"); err == nil {
		t.Fatal("reverse installed conflict accepted")
	}
}

func TestResolveAPIIsAuthenticated(t *testing.T) {
	handler := resolverServer(t, []mod{resolverMod("app", "1.0.0")}, []library{})
	request := httptest.NewRequest(http.MethodPost, "/api/v1/resolve", strings.NewReader(`{"id":"app"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+handler.authToken)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"id":"app"`) {
		t.Fatalf("resolve: %d %s", response.Code, response.Body.String())
	}
}
