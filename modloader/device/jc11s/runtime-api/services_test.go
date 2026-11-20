package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func serviceFixture(t *testing.T) (string, installedMod) {
	t.Helper()
	root, item := launcherFixture(t, "#!/bin/sh\nexit 0\n", true)
	binary := make([]byte, 64)
	copy(binary, []byte{0x7f, 'E', 'L', 'F', 1, 1})
	binary[18] = 40
	digest := sha256.Sum256(binary)
	item.Service = &serviceSpec{Binary: "mod/server", SHA256: hex.EncodeToString(digest[:]), Capability: "splash-image"}
	if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(item.Root), "mod/server"), binary, 0600); err != nil {
		t.Fatal(err)
	}
	saveServiceFixture(t, root, item)
	return root, item
}

func saveServiceFixture(t *testing.T, root string, item installedMod) {
	t.Helper()
	if err := writeAtomicJSON(filepath.Join(root, "registry/state.json"), registryState{SchemaVersion: 1, Mods: map[string]installedMod{item.ID: item}}); err != nil {
		t.Fatal(err)
	}
}

func mockServices(t *testing.T) *[]string {
	t.Helper()
	oldCommand, oldBase, oldRoot, oldStopped := serviceCommand, serviceRunBase, serviceProvisioningAllowed, serviceStopped
	t.Cleanup(func() {
		serviceCommand, serviceRunBase, serviceProvisioningAllowed, serviceStopped = oldCommand, oldBase, oldRoot, oldStopped
	})
	serviceRunBase = filepath.Join(t.TempDir(), "run")
	serviceProvisioningAllowed = func() bool { return true }
	serviceStopped = func(string) bool { return true }
	calls := []string{}
	serviceCommand = func(name string, args ...string) error {
		calls = append(calls, name+" "+strings.Join(args, " "))
		return nil
	}
	return &calls
}

func TestServiceSandboxLifecycle(t *testing.T) {
	calls := mockServices(t)
	root, item := serviceFixture(t)
	if err := reconcileServices(root); err != nil {
		t.Fatal(err)
	}
	command := strings.Join(*calls, "\n")
	for _, required := range []string{"DynamicUser=yes", "ProtectSystem=strict", "NoNewPrivileges=yes", "CapabilityBoundingSet=", "KillMode=control-group", "StandardInput=file:", "--managed"} {
		if !strings.Contains(command, required) {
			t.Fatalf("missing sandbox property %s", required)
		}
	}
	record, err := loadServiceRecord(root, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(record.Credentials.WebToken) != 64 || len(record.Credentials.BrokerToken) != 64 || record.Credentials.WebToken == record.Credentials.BrokerToken {
		t.Fatal("tokens are not distinct 256-bit secrets")
	}
	if strings.Contains(command, record.Credentials.BrokerToken) || strings.Contains(command, record.Credentials.WebToken) {
		t.Fatal("secrets leaked into process arguments")
	}
	if _, err := setModEnabled(item.ID, false, root); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(*calls, "\n"), "systemctl stop "+serviceName(item.ID)+".service") {
		t.Fatal("disable did not stop service")
	}
	if _, err := loadServiceRecord(root, item.ID); !os.IsNotExist(err) {
		t.Fatal("disable retained broker credentials")
	}
	if _, err := uninstallDisabledMod(item.ID, root); err != nil {
		t.Fatal(err)
	}
}

func TestServiceStopsInSafeModeAndQuarantine(t *testing.T) {
	for _, mode := range []string{"safe", "quarantine"} {
		t.Run(mode, func(t *testing.T) {
			mockServices(t)
			root, item := serviceFixture(t)
			if err := reconcileServices(root); err != nil {
				t.Fatal(err)
			}
			if mode == "safe" {
				if _, err := setSafeMode(root, true); err != nil {
					t.Fatal(err)
				}
			} else {
				for i := 0; i < failureQuarantineThreshold; i++ {
					if _, err := recordRunOutcome(root, item.ID, false); err != nil {
						t.Fatal(err)
					}
				}
			}
			if _, err := loadServiceRecord(root, item.ID); !os.IsNotExist(err) {
				t.Fatal("blocked service kept credentials")
			}
		})
	}
}

func TestServiceRejectsTamperedBinaryAndSymlink(t *testing.T) {
	for _, attack := range []string{"contents", "symlink", "architecture"} {
		t.Run(attack, func(t *testing.T) {
			root, item := serviceFixture(t)
			path := filepath.Join(root, item.Root, "mod/server")
			data, _ := os.ReadFile(path)
			switch attack {
			case "contents":
				data[63] = 1
				_ = os.WriteFile(path, data, 0600)
			case "symlink":
				_ = os.Rename(path, path+".real")
				_ = os.Symlink(path+".real", path)
			case "architecture":
				item.Architecture = "aarch64"
			}
			if _, err := verifiedServiceBinary(root, item); err == nil {
				t.Fatal("unsafe binary accepted")
			}
		})
	}
}

func TestServiceLaunchFailureQuarantines(t *testing.T) {
	mockServices(t)
	root, item := serviceFixture(t)
	serviceCommand = func(name string, args ...string) error {
		if name == "systemd-run" {
			return errors.New("unsupported sandbox property")
		}
		return nil
	}
	for i := 0; i < failureQuarantineThreshold; i++ {
		if err := reconcileServices(root); err == nil {
			t.Fatal("failed provisioning reported success")
		}
	}
	safety, err := loadSafetyState(root)
	if err != nil || !safety.Mods[item.ID].Quarantined {
		t.Fatal("failed service not quarantined")
	}
}

func serviceAPIFixture(t *testing.T) (*server, string, installedMod, serviceRecord) {
	t.Helper()
	mockServices(t)
	root, item := serviceFixture(t)
	if err := reconcileServices(root); err != nil {
		t.Fatal(err)
	}
	record, err := loadServiceRecord(root, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	s := &server{root: root, authToken: strings.Repeat("x", 64), model: "JC11S", firmware: "5.3.0", architecture: "armv7l", abi: "gnueabihf",
		catalog: catalogState{Status: "valid", Trusted: true, Catalog: catalog{SchemaVersion: 2, Mods: []mod{{ID: item.ID, Version: item.Version, SHA256: item.PackageSHA256, Service: item.Service, Models: []string{"JC11S"}, Firmware: []string{"5.3.0"}, Architecture: "armv7l", ABI: "gnueabihf", Approval: &approval{Maintainer: "test", Devices: []string{"JC11S"}}}}}}}
	return s, root, item, record
}

func TestServiceBrokerWritesOnlyValidatedPNG(t *testing.T) {
	s, root, item, record := serviceAPIFixture(t)
	var pngData bytes.Buffer
	if err := png.Encode(&pngData, image.NewRGBA(image.Rect(0, 0, 4, 3))); err != nil {
		t.Fatal(err)
	}
	request := func(data []byte, token string) int {
		r := httptest.NewRequest(http.MethodPost, "/api/v1/service-splash", bytes.NewReader(data))
		r.Header.Set("Content-Type", "image/png")
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		return w.Code
	}
	if code := request(pngData.Bytes(), s.authToken); code != 401 {
		t.Fatalf("mutation token accepted as broker token: %d", code)
	}
	if code := request(pngData.Bytes(), record.Credentials.WebToken); code != 401 {
		t.Fatalf("web token accepted as broker token: %d", code)
	}
	if code := request([]byte("not PNG"), record.Credentials.BrokerToken); code != 400 {
		t.Fatalf("invalid PNG accepted: %d", code)
	}
	if code := request(pngData.Bytes(), record.Credentials.BrokerToken); code != 200 {
		t.Fatalf("upload failed: %d", code)
	}
	actual, err := os.ReadFile(filepath.Join(root, "ui/splash/SplashScreen.png"))
	if err != nil || !bytes.Equal(actual, pngData.Bytes()) {
		t.Fatal("managed splash not saved")
	}
	item.Enabled = false
	saveServiceFixture(t, root, item)
	if code := request(pngData.Bytes(), record.Credentials.BrokerToken); code != 401 {
		t.Fatalf("disabled credential accepted: %d", code)
	}
}

func TestServiceAccessRequiresManagementAuthorization(t *testing.T) {
	s, _, item, record := serviceAPIFixture(t)
	for _, token := range []string{"", record.Credentials.BrokerToken, s.authToken} {
		r := httptest.NewRequest(http.MethodPost, "/api/v1/service-access", strings.NewReader(`{"id":"`+item.ID+`"}`))
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if token == s.authToken {
			if w.Code != 200 || !strings.Contains(w.Body.String(), record.Credentials.WebToken) || strings.Contains(w.Body.String(), record.Credentials.BrokerToken) {
				t.Fatalf("invalid management response: %d", w.Code)
			}
		} else if w.Code != 401 {
			t.Fatalf("access token disclosed: %d", w.Code)
		}
	}
}

func TestServiceCatalogRevocationStopsService(t *testing.T) {
	s, root, item, _ := serviceAPIFixture(t)
	s.catalog.Catalog.Mods[0].Revoked = true
	if err := reconcileServicesPolicy(root, s.serviceApproved); err != nil {
		t.Fatal(err)
	}
	if _, err := loadServiceRecord(root, item.ID); !os.IsNotExist(err) {
		t.Fatal("revoked service retained credentials")
	}
}

func TestServiceStopFailurePreventsDisableAndCredentialRemoval(t *testing.T) {
	mockServices(t)
	root, item := serviceFixture(t)
	if err := reconcileServices(root); err != nil {
		t.Fatal(err)
	}
	serviceCommand = func(name string, args ...string) error { return errors.New("supervisor unavailable") }
	serviceStopped = func(string) bool { return false }
	if _, err := setModEnabled(item.ID, false, root); err == nil {
		t.Fatal("failed stop reported disabled")
	}
	state, err := loadRegistry(filepath.Join(root, "registry/state.json"))
	if err != nil || !state.Mods[item.ID].Enabled {
		t.Fatal("failed stop changed activation state")
	}
	if _, err := loadServiceRecord(root, item.ID); err != nil {
		t.Fatal("failed stop lost recovery record")
	}
}

func TestServiceRejectsUnsafeCapabilityDeclarations(t *testing.T) {
	for _, spec := range []*serviceSpec{
		{Binary: "../server", SHA256: strings.Repeat("a", 64), Capability: "splash-image"},
		{Binary: "mod/server", SHA256: strings.Repeat("a", 64), Capability: "root"},
		{Binary: "mod/server", SHA256: "bad", Capability: "splash-image"},
	} {
		if validateServiceSpec(spec) == nil {
			t.Fatal("unsafe service metadata accepted")
		}
	}
}
