package main

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

const captureLauncher = "#!/bin/sh\n" +
	"if ! pidof live_pitch >/dev/null 2>&1; then\n" +
	"\tsystemd-run --unit=prime-dvs-decode --collect /data/prime-unlimited/dvs-stable/live_pitch >/dev/null 2>&1 || true\n" +
	"fi\n" +
	"QML_DISABLE_DISK_CACHE=1 LD_PRELOAD=/data/prime-unlimited/lib/libprime-pcm-live.so:/data/prime-unlimited/lib/libprime-ui-hook.so /usr/Engine/Engine -d0\n"

func captureELF(shared bool) []byte {
	blob := make([]byte, 64)
	copy(blob, []byte{0x7f, 'E', 'L', 'F', 1, 1})
	blob[18] = 40
	if shared {
		blob[16] = 3
	}
	return blob
}

func linkLauncher(t *testing.T, root, script string) (string, uint64) {
	t.Helper()
	dataPath := filepath.Join(root, "bin", "engine-hooked.sh")
	if err := os.MkdirAll(filepath.Dir(dataPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dataPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	enginePath := filepath.Join(t.TempDir(), "engine")
	if err := os.Link(dataPath, enginePath); err != nil {
		t.Fatal(err)
	}
	previous := engineScriptPath
	engineScriptPath = enginePath
	t.Cleanup(func() { engineScriptPath = previous })
	info, err := os.Stat(dataPath)
	if err != nil {
		t.Fatal(err)
	}
	return dataPath, info.Sys().(*syscall.Stat_t).Ino
}

func TestCaptureMeterArmsPreloadAndKeepsDVS(t *testing.T) {
	calls := mockServices(t)
	root, item := serviceFixture(t)
	server := captureELF(false)
	tap := captureELF(true)
	digest := sha256.Sum256(server)
	base := filepath.Join(root, filepath.FromSlash(item.Root), "mod")
	if err := os.WriteFile(filepath.Join(base, "server"), server, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(base, captureTapFile), tap, 0o600); err != nil {
		t.Fatal(err)
	}
	item.Service = &serviceSpec{Binary: "mod/server", SHA256: hex.EncodeToString(digest[:]), Capability: captureMeterCapability}
	saveServiceFixture(t, root, item)
	launcher, inode := linkLauncher(t, root, captureLauncher)
	if err := reconcileServices(root); err != nil {
		t.Fatal(err)
	}
	text, err := os.ReadFile(launcher)
	if err != nil {
		t.Fatal(err)
	}
	tapPath := filepath.Join(root, "lib", captureTapFile)
	want := "/data/prime-unlimited/lib/libprime-pcm-live.so:" + tapPath + ":/data/prime-unlimited/lib/libprime-ui-hook.so"
	if !strings.Contains(string(text), "LD_PRELOAD="+want+" ") {
		t.Fatalf("preload = %s", text)
	}
	if !strings.Contains(string(text), "--unit=prime-dvs-decode") || strings.Count(string(text), "LD_PRELOAD=") != 1 {
		t.Fatal("DVS launcher block changed")
	}
	after, err := os.Stat(launcher)
	if err != nil || after.Sys().(*syscall.Stat_t).Ino != inode {
		t.Fatal("launcher inode changed")
	}
	engineText, err := os.ReadFile(engineScriptPath)
	if err != nil || string(engineText) != string(text) {
		t.Fatal("engine script is not the same inode")
	}
	command := strings.Join(*calls, "\n")
	if !strings.Contains(command, "PrivateDevices=yes") || !strings.Contains(command, "StandardInput=null") || strings.Contains(command, "--managed") {
		t.Fatalf("capture sandbox = %s", command)
	}
	if _, err := setModEnabled(item.ID, false, root); err != nil {
		t.Fatal(err)
	}
	disabled, err := os.ReadFile(launcher)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(disabled), captureTapFile) || !strings.Contains(string(disabled), "libprime-pcm-live.so:/data/prime-unlimited/lib/libprime-ui-hook.so") {
		t.Fatalf("disable changed DVS preload: %s", disabled)
	}
	if !strings.Contains(string(disabled), "--unit=prime-dvs-decode") {
		t.Fatal("disable removed live_pitch")
	}
}

func TestCaptureMeterInsertsWithoutDVS(t *testing.T) {
	script := "#!/bin/sh\nLD_PRELOAD=/data/prime-unlimited/lib/libprime-ui-hook.so /usr/Engine/Engine -d0\n"
	root := tTempCapture(t, script)
	item := installedCapture(t, root)
	if err := armCaptureMeter(root, item); err != nil {
		t.Fatal(err)
	}
	text, err := os.ReadFile(filepath.Join(root, "bin", "engine-hooked.sh"))
	if err != nil {
		t.Fatal(err)
	}
	tapPath := filepath.Join(root, "lib", captureTapFile)
	if !strings.Contains(string(text), "LD_PRELOAD="+tapPath+":/data/prime-unlimited/lib/libprime-ui-hook.so ") {
		t.Fatalf("preload = %s", text)
	}
}

func TestCaptureMeterRefusesForeignLauncher(t *testing.T) {
	script := "#!/bin/sh\nLD_PRELOAD=/tmp/liba.so /bin/true\nLD_PRELOAD=/tmp/libb.so /bin/true\n"
	root := tTempCapture(t, script)
	item := installedCapture(t, root)
	before, _ := os.ReadFile(filepath.Join(root, "bin", "engine-hooked.sh"))
	if err := armCaptureMeter(root, item); err == nil {
		t.Fatal("foreign launcher accepted")
	}
	after, _ := os.ReadFile(filepath.Join(root, "bin", "engine-hooked.sh"))
	if string(before) != string(after) {
		t.Fatal("foreign launcher was modified")
	}
}

func TestCaptureMeterRejectsExecutableTap(t *testing.T) {
	root, item := serviceFixture(t)
	base := filepath.Join(root, filepath.FromSlash(item.Root), "mod")
	if err := os.WriteFile(filepath.Join(base, captureTapFile), captureELF(false), 0o600); err != nil {
		t.Fatal(err)
	}
	item.Service.Capability = captureMeterCapability
	if _, err := verifiedCaptureTap(root, item); err == nil {
		t.Fatal("ET_EXEC tap accepted")
	}
}

func tTempCapture(t *testing.T, script string) string {
	t.Helper()
	root := t.TempDir()
	makeLauncherFixtureTraversable(t, root)
	if err := os.MkdirAll(filepath.Join(root, "registry"), 0o700); err != nil {
		t.Fatal(err)
	}
	linkLauncher(t, root, script)
	return root
}

func installedCapture(t *testing.T, root string) installedMod {
	t.Helper()
	server := captureELF(false)
	tap := captureELF(true)
	digest := sha256.Sum256(server)
	item := installedMod{
		ID: "capture-test", Version: "1.0.0", PackageSHA256: strings.Repeat("b", 64),
		Architecture: "armv7l", ABI: "gnueabihf", Root: "mods/installed/capture-test/1.0.0", Enabled: true,
		Service: &serviceSpec{Binary: "mod/server", SHA256: hex.EncodeToString(digest[:]), Capability: captureMeterCapability},
	}
	base := filepath.Join(root, filepath.FromSlash(item.Root), "mod")
	if err := os.MkdirAll(base, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(base, "server"), server, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(base, captureTapFile), tap, 0o600); err != nil {
		t.Fatal(err)
	}
	return item
}
