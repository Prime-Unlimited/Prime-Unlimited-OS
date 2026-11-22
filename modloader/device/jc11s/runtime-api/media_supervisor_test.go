package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestMediaEngineRecoveryRequiresIndependentSSH(t *testing.T) {
	calls := []string{}
	controller := &mediaEngineController{command: func(ctx context.Context, name string, args ...string) ([]byte, error) {
		calls = append(calls, name+" "+strings.Join(args, " "))
		if len(args) > 0 && args[0] == "show" {
			return []byte("/system.slice/engine.service\n"), nil
		}
		return nil, nil
	}}
	if e := controller.ArmRecovery(context.Background(), strings.Repeat("a", 64)); e == nil {
		t.Fatal("engine-owned SSH accepted")
	}
	for _, call := range calls {
		if strings.Contains(call, "systemd-run") {
			t.Fatal("timer armed despite unsafe SSH")
		}
	}
	calls = nil
	if e := controller.ArmRecovery(context.Background(), "../../bad"); e == nil || len(calls) != 0 {
		t.Fatal("invalid recovery id executed commands")
	}
}
func TestMediaEngineRecoveryTimerHasFixedCommand(t *testing.T) {
	calls := []string{}
	controller := &mediaEngineController{command: func(ctx context.Context, name string, args ...string) ([]byte, error) {
		calls = append(calls, name+" "+strings.Join(args, " "))
		if len(args) > 0 && args[0] == "show" {
			return []byte("/system.slice/prime4-mod-sshd.service\n"), nil
		}
		return nil, nil
	}}
	id := strings.Repeat("b", 64)
	if e := controller.ArmRecovery(context.Background(), id); e != nil {
		t.Fatal(e)
	}
	if len(calls) != 3 || !strings.HasSuffix(calls[2], "--on-active=480s /bin/systemctl start engine.service") {
		t.Fatal("unexpected recovery command", calls)
	}
	if e := controller.DisarmRecovery(context.Background(), id); e != nil {
		t.Fatal(e)
	}
	if !strings.HasSuffix(calls[3], "pu-usb-recovery-"+id+".timer") {
		t.Fatal("wrong recovery unit")
	}
}
func TestMediaEngineStopChecksNativeProcess(t *testing.T) {
	controller := &mediaEngineController{command: func(context.Context, string, ...string) ([]byte, error) { return nil, nil }, nativeRunning: func() (bool, error) { return true, nil }}
	if e := controller.StopEngine(context.Background()); e == nil {
		t.Fatal("native process ignored")
	}
	controller.nativeRunning = func() (bool, error) { return false, errors.New("unreadable proc") }
	if e := controller.StopEngine(context.Background()); e == nil {
		t.Fatal("unknown process state accepted")
	}
}
func TestMediaEngineStartWaitsForNativeRecovery(t *testing.T) {
	checks, pauses := 0, 0
	controller := &mediaEngineController{command: func(context.Context, string, ...string) ([]byte, error) { return nil, nil }, nativeRunning: func() (bool, error) { checks++; return checks >= 3, nil }, pause: func(context.Context, time.Duration) error { pauses++; return nil }}
	if e := controller.StartEngine(context.Background()); e != nil {
		t.Fatal(e)
	}
	if checks != 3 || pauses != 2 {
		t.Fatal("native recovery was not observed")
	}
	controller.nativeRunning = func() (bool, error) { return false, nil }
	if e := controller.StartEngine(context.Background()); e == nil {
		t.Fatal("missing native process accepted")
	}
}
func TestMediaJobMountIdentityRejectsReplacement(t *testing.T) {
	identity := mediaMountIdentity{MountID: "100", DeviceID: "8:34", Root: "/media/USB", SysfsPath: "/sys/devices/usb3/block/sdc/sdc2"}
	volume := mountedBlockVolume{mountID: "100", deviceID: "8:34", mountPath: "/media/USB", sysfsPath: identity.SysfsPath}
	if _, ok := mediaJobMountMatch([]mountedBlockVolume{volume}, identity); !ok {
		t.Fatal("original mount not matched")
	}
	volume.mountID = "101"
	if _, ok := mediaJobMountMatch([]mountedBlockVolume{volume}, identity); ok {
		t.Fatal("remounted replacement accepted")
	}
	controller := &mediaEngineController{mounts: mediaJobMounts{BootID: "before"}, bootID: func() (string, error) { return "after", nil }, volumes: func() ([]mountedBlockVolume, error) { t.Fatal("volumes read for stale boot"); return nil, nil }}
	if e := controller.ValidateVolumes(context.Background()); e == nil {
		t.Fatal("stale boot accepted")
	}
}
