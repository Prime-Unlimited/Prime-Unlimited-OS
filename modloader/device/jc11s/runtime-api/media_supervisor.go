package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type mediaMountIdentity struct {
	MountID    string
	DeviceID   string
	Root       string
	SysfsPath  string
	Generation string
}
type mediaJobMounts struct {
	BootID      string
	Source      mediaMountIdentity
	Destination mediaMountIdentity
}
type mediaEngineController struct {
	validateDestination func(context.Context, mountedBlockVolume) error
	approved            func() bool
	mounts              mediaJobMounts
	command             func(context.Context, string, ...string) ([]byte, error)
	volumes             func() ([]mountedBlockVolume, error)
	bootID              func() (string, error)
	nativeRunning       func() (bool, error)
	pause               func(context.Context, time.Duration) error
}

func newMediaEngineController(model string, mounts mediaJobMounts) (*mediaEngineController, error) {
	if model != "JP24" || os.Geteuid() != 0 {
		return nil, errors.New("supervised imports are only verified on root JP24")
	}
	return &mediaEngineController{mounts: mounts, command: mediaEngineCommand, volumes: readMountedBlockVolumes, bootID: mediaBootID, nativeRunning: mediaNativeEngineRunning, pause: mediaEnginePause}, nil
}
func mediaEngineCommand(ctx context.Context, name string, args ...string) ([]byte, error) {
	limited, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	output, err := exec.CommandContext(limited, name, args...).CombinedOutput()
	if err != nil {
		return nil, errors.New("Engine supervision command failed")
	}
	return output, nil
}
func mediaBootID() (string, error) {
	b, e := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if e != nil {
		return "", e
	}
	id := strings.TrimSpace(string(b))
	if len(id) != 36 {
		return "", errors.New("boot identity unavailable")
	}
	return id, nil
}
func mediaNativeEngineRunning() (bool, error) {
	entries, e := os.ReadDir("/proc")
	if e != nil {
		return false, e
	}
	for _, entry := range entries {
		if _, e := strconv.Atoi(entry.Name()); e != nil || !entry.IsDir() {
			continue
		}
		target, e := os.Readlink(filepath.Join("/proc", entry.Name(), "exe"))
		if e != nil {
			if os.IsNotExist(e) {
				continue
			}
			return false, errors.New("native process state unavailable")
		}
		if target == "/usr/Engine/Engine" || target == "/usr/Engine/Engine (deleted)" {
			return true, nil
		}
	}
	return false, nil
}
func mediaEnginePause(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
func mediaJobMountMatch(volumes []mountedBlockVolume, identity mediaMountIdentity) (mountedBlockVolume, bool) {
	for _, volume := range volumes {
		if volume.mountID == identity.MountID && volume.deviceID == identity.DeviceID && volume.mountPath == identity.Root && volume.sysfsPath == identity.SysfsPath && (identity.Generation == "" || volume.deviceGeneration == identity.Generation) {
			return volume, true
		}
	}
	return mountedBlockVolume{}, false
}
func (c *mediaEngineController) ValidateVolumes(ctx context.Context) error {
	if c.approved != nil && !c.approved() {
		return errors.New("USB Copy approval was revoked")
	}
	if e := ctx.Err(); e != nil {
		return e
	}
	boot, e := c.bootID()
	if e != nil || boot != c.mounts.BootID {
		return errors.New("import mount identities belong to another boot")
	}
	volumes, e := c.volumes()
	if e != nil {
		return e
	}
	source, ok := mediaJobMountMatch(volumes, c.mounts.Source)
	if !ok || !isReadableMediaSource(source) {
		return errors.New("source mount changed or became unavailable")
	}
	target, ok := mediaJobMountMatch(volumes, c.mounts.Destination)
	if !ok || !isInternalMediaDestination(target) {
		return errors.New("destination mount changed or became unavailable")
	}
	if source.deviceID == target.deviceID || source.mountPath == target.mountPath {
		return errors.New("source and destination must be distinct")
	}
	if c.validateDestination != nil {
		return c.validateDestination(ctx, target)
	}
	return nil
}
func mediaRecoveryUnit(id string) (string, error) {
	if !validMediaEventID(id) {
		return "", errors.New("invalid recovery job id")
	}
	return "pu-usb-recovery-" + id, nil
}
func (c *mediaEngineController) ArmRecovery(ctx context.Context, id string) error {
	unit, e := mediaRecoveryUnit(id)
	if e != nil {
		return e
	}
	if _, e = c.command(ctx, "/bin/systemctl", "is-active", "--quiet", "prime4-mod-sshd.service"); e != nil {
		return errors.New("independent SSH service unavailable")
	}
	output, e := c.command(ctx, "/bin/systemctl", "show", "prime4-mod-sshd.service", "--property=ControlGroup", "--value")
	if e != nil || strings.TrimSpace(string(output)) != "/system.slice/prime4-mod-sshd.service" {
		return errors.New("SSH is not independently supervised")
	}
	_, e = c.command(ctx, "/usr/bin/systemd-run", "--quiet", "--unit="+unit, "--on-active=480s", "/bin/systemctl", "start", "engine.service")
	return e
}
func (c *mediaEngineController) StopEngine(ctx context.Context) error {
	if _, e := c.command(ctx, "/bin/systemctl", "stop", "engine.service"); e != nil {
		return e
	}
	running, e := c.nativeRunning()
	if e != nil {
		return e
	}
	if running {
		return errors.New("native Engine remains active")
	}
	return nil
}
func (c *mediaEngineController) StartEngine(ctx context.Context) error {
	if _, e := c.command(ctx, "/bin/systemctl", "start", "engine.service"); e != nil {
		return e
	}
	for i := 0; i < 80; i++ {
		running, e := c.nativeRunning()
		if e != nil {
			return e
		}
		if running {
			_, e = c.command(ctx, "/bin/systemctl", "is-active", "--quiet", "engine.service")
			return e
		}
		if e = c.pause(ctx, 250*time.Millisecond); e != nil {
			return e
		}
	}
	return errors.New("native Engine did not recover")
}
func (c *mediaEngineController) DisarmRecovery(ctx context.Context, id string) error {
	unit, e := mediaRecoveryUnit(id)
	if e != nil {
		return e
	}
	_, e = c.command(ctx, "/bin/systemctl", "stop", unit+".timer")
	return e
}
