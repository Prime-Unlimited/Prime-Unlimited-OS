package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func validV2Mod() mod {
	item := validMod()
	item.Architecture = "armv7l"
	item.ABI = "gnueabihf"
	item.Entrypoint = "mod/start.sh"
	item.Approval = &approval{Maintainer: "Prime Unlimited", Devices: []string{"JC11S"}}
	return item
}

func TestReleasedPrimeClockCatalogSignatureAndCompatibility(t *testing.T) {
	catalogPath := filepath.Join("..", "..", "..", "catalog", "prime-clock-v0.1.1-jc11s.json")
	state := loadVerifiedCatalog(catalogPath, strings.TrimSuffix(catalogPath, ".json")+".sig.json", filepath.Join("..", "..", "..", "catalog", "trust", "prime-unlimited-2026-1.json"))
	if state.Status != "valid" || !state.Trusted || len(state.Catalog.Mods) != 1 {
		t.Fatalf("released Prime Clock catalog invalid: %#v", state)
	}
	handler := &server{model: "JC11S", firmware: "5.3.0", architecture: "armv7l", abi: "gnueabihf"}
	if !handler.compatible(state.Catalog.Mods[0]) || state.Catalog.Mods[0].Overlay != "prime-clock/ui/PrimeClock.qml" {
		t.Fatal("released Prime Clock catalog is not JC11S overlay compatible")
	}
}

func TestCanonicalDeviceCatalogSignaturesAndIsolation(t *testing.T) {
	for _, test := range []struct{ name, file, model, architecture, abi string }{{"jc11s", "jc11s.json", "JC11S", "armv7l", "gnueabihf"}, {"jp24", "jp24.json", "JP24", "aarch64", "gnu"}} {
		t.Run(test.name, func(t *testing.T) {
			catalogPath := filepath.Join("..", "..", "..", "catalog", test.file)
			state := loadVerifiedCatalog(catalogPath, strings.TrimSuffix(catalogPath, ".json")+".sig.json", filepath.Join("..", "..", "..", "catalog", "trust", "prime-unlimited-2026-1.json"))
			if state.Status != "valid" || !state.Trusted || len(state.Catalog.Mods) != 1 {
				t.Fatalf("canonical catalog invalid: %#v", state)
			}
			runtime := server{model: test.model, firmware: "5.3.0", architecture: test.architecture, abi: test.abi, catalog: state}
			if !runtime.compatible(state.Catalog.Mods[0]) {
				t.Fatal("device catalog entry is incompatible")
			}
			wrong := runtime
			if test.model == "JC11S" {
				wrong.model = "JP24"
			} else {
				wrong.model = "JC11S"
			}
			if wrong.compatible(state.Catalog.Mods[0]) {
				t.Fatal("catalog crossed device boundary")
			}
		})
	}
}

func validV2CatalogJSON(t *testing.T, item mod) string {
	t.Helper()
	return `{"schemaVersion":2,"catalogId":"prime-unlimited-test","mods":[` + mustJSON(t, item) + `],"libraries":[]}`
}

func TestCatalogV2ValidationAndCompatibility(t *testing.T) {
	item := validV2Mod()
	state := parseCatalog([]byte(validV2CatalogJSON(t, item)))
	if state.Status != "valid" || state.Catalog.SchemaVersion != 2 || state.Catalog.CatalogID != "prime-unlimited-test" {
		t.Fatalf("valid v2 catalog rejected: %#v", state)
	}
	handler := &server{model: "JC11S", firmware: "5.3.0", architecture: "armv7l", abi: "gnueabihf", catalog: state}
	if !handler.compatible(item) {
		t.Fatal("approved matching v2 mod is not compatible")
	}
	for name, mutate := range map[string]func(*mod){
		"architecture": func(value *mod) { value.Architecture = "aarch64" },
		"abi":          func(value *mod) { value.ABI = "musl" },
		"approval":     func(value *mod) { value.Approval.Devices = []string{"JP24"} },
		"revoked":      func(value *mod) { value.Revoked = true },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := validV2Mod()
			mutate(&candidate)
			if handler.compatible(candidate) {
				t.Fatal("ineligible v2 mod reported compatible")
			}
		})
	}
}

func TestCatalogV2RejectsIncompleteOrAmbiguousTrustMetadata(t *testing.T) {
	valid := validV2Mod()
	tests := map[string]string{
		"missing libraries":        strings.Replace(validV2CatalogJSON(t, valid), `,"libraries":[]`, "", 1),
		"embedded authenticity":    strings.Replace(validV2CatalogJSON(t, valid), `"mods":`, `"authenticity":{"algorithm":"ed25519"},"mods":`, 1),
		"null authenticity":        strings.Replace(validV2CatalogJSON(t, valid), `"mods":`, `"authenticity":null,"mods":`, 1),
		"missing approval":         validV2CatalogJSON(t, func() mod { value := valid; value.Approval = nil; return value }()),
		"unsafe entrypoint":        validV2CatalogJSON(t, func() mod { value := valid; value.Entrypoint = "../start"; return value }()),
		"unsupported architecture": validV2CatalogJSON(t, func() mod { value := valid; value.Architecture = ""; return value }()),
		"duplicate conflicts":      validV2CatalogJSON(t, func() mod { value := valid; value.Conflicts = []string{"other", "other"}; return value }()),
	}
	for name, data := range tests {
		t.Run(name, func(t *testing.T) {
			if state := parseCatalog([]byte(data)); state.Status != "invalid" || state.Error == "" {
				t.Fatalf("invalid v2 accepted: %#v", state)
			}
		})
	}
}

func TestCatalogV1RejectsExplicitV2FieldsEvenAtZeroValues(t *testing.T) {
	base := mustJSON(t, validMod())
	for name, data := range map[string]string{
		"mod revoked false":     `{"schemaVersion":1,"mods":[` + strings.TrimSuffix(base, "}") + `,"revoked":false}]}`,
		"root empty catalog id": `{"schemaVersion":1,"catalogId":"","mods":[` + base + `]}`,
	} {
		t.Run(name, func(t *testing.T) {
			if state := parseCatalog([]byte(data)); state.Status != "invalid" {
				t.Fatalf("cross-schema field accepted: %#v", state)
			}
		})
	}
}

func TestInspectEndpointRequiresTrustedV2MetadataAndVerifiedStage(t *testing.T) {
	payload := makeZIP(t, []zipEntry{{name: "mod/start.sh", data: "#!/bin/sh\n", mode: 0o777}})
	item, cacheRoot := cachedZIP(t, payload)
	item.Architecture = "armv7l"
	item.ABI = "gnueabihf"
	item.Entrypoint = "mod/start.sh"
	item.Approval = &approval{Maintainer: "Prime Unlimited", Devices: []string{"JC11S"}}
	stagingRoot := filepath.Join(t.TempDir(), "stage")
	if _, err := stagePackage(item, cacheRoot, stagingRoot, defaultStageLimits); err != nil {
		t.Fatal(err)
	}
	token := strings.Repeat("a", 64)
	handler := &server{model: "JC11S", firmware: "5.3.0", architecture: "armv7l", abi: "gnueabihf", authToken: token,
		cacheRoot: cacheRoot, stagingRoot: stagingRoot, catalog: catalogState{Status: "valid", Trusted: true,
			Catalog: catalog{SchemaVersion: 2, CatalogID: "prime-unlimited-test", Mods: []mod{item}, Libraries: []library{}}}}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/inspect", strings.NewReader(`{"id":"catalog-smoke-test"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+token)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"eligible":true`) {
		t.Fatalf("inspection failed: %d %s", response.Code, response.Body.String())
	}
	if info, err := os.Stat(filepath.Join(stagingRoot, item.SHA256, item.Entrypoint)); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("entrypoint unexpectedly changed: %v %v", info, err)
	}

	item.Entrypoint = "mod/missing"
	handler.catalog.Catalog.Mods[0] = item
	request = httptest.NewRequest(http.MethodPost, "/api/v1/inspect", strings.NewReader(`{"id":"catalog-smoke-test"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+token)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("missing entrypoint accepted: %d %s", response.Code, response.Body.String())
	}
}
