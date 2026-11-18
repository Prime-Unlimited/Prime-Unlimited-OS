package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProvisionAuthorizedUICreatesPrivateRuntimeCopy(t *testing.T) {
	root := t.TempDir()
	uiRoot := filepath.Join(root, "ui")
	if err := os.MkdirAll(uiRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	template := []byte("property string authToken: \"__PRIME_UNLIMITED_AUTH_TOKEN__\"\n")
	for _, name := range []string{"ModLoader", "OverlayHost"} {
		if err := os.WriteFile(filepath.Join(uiRoot, name+".template.qml"), template, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	token := strings.Repeat("a", 64)
	if err := provisionAuthorizedUI(root, token); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"ModLoader", "OverlayHost"} {
		path := filepath.Join(uiRoot, name+".qml")
		data, err := os.ReadFile(path)
		if err != nil || !strings.Contains(string(data), token) || strings.Contains(string(data), string(uiAuthorizationMarker)) {
			t.Fatalf("authorized UI content invalid for %s: %v", name, err)
		}
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("authorized UI mode for %s: %v %v", name, info.Mode().Perm(), err)
		}
	}
}

func TestProvisionAuthorizedUIRejectsMissingOrDuplicateMarker(t *testing.T) {
	for _, content := range []string{"no marker", string(uiAuthorizationMarker) + string(uiAuthorizationMarker)} {
		root := t.TempDir()
		if err := os.MkdirAll(filepath.Join(root, "ui"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "ui", "ModLoader.template.qml"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := provisionAuthorizedUI(root, strings.Repeat("a", 64)); err == nil {
			t.Fatal("invalid marker accepted")
		}
	}
}
