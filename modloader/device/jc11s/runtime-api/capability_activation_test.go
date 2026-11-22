package main

import "testing"

func TestActivationAllowsMediaAndSplashWithRevision13Policy(t *testing.T) {
	root, app, dependency := dependencyActivationFixture(t)
	dependency.Enabled = true
	dependency.Service = &serviceSpec{Capability: "splash-image"}
	app.Dependencies = nil
	app.Service = &serviceSpec{Capability: "media-import"}
	state := registryState{SchemaVersion: 1, Mods: map[string]installedMod{app.ID: app, dependency.ID: dependency}}
	if _, _, err := planActivation(state, app.ID, true, root); err != nil {
		t.Fatalf("different capabilities rejected: %v", err)
	}
	app.Service = &serviceSpec{Capability: "splash-image"}
	state.Mods[app.ID] = app
	if _, _, err := planActivation(state, app.ID, true, root); err != nil {
		t.Fatal("revision 13 parallel-service policy rejected", err)
	}
}
