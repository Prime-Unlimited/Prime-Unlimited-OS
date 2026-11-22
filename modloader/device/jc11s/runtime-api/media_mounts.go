package main

import (
	"bufio"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// mountedBlockVolume is an internal runtime record. Its mount path and kernel
// device number must never be returned to a mod or copied from API input.
type mountedBlockVolume struct {
	mountID   string
	deviceID  string
	mountPath string
	sysfsPath string
	source    string
	fsType    string
	options   map[string]bool
	usb       bool
	sdCard    bool
}

var sysfsUSBPort = regexp.MustCompile(`^usb[0-9]+$`)
var sysfsATAController = regexp.MustCompile(`^ata[0-9]+$`)
var sysfsNVMeController = regexp.MustCompile(`^nvme[0-9]+$`)
var sysfsSATAPlatform = regexp.MustCompile(`^[[:xdigit:]]+\.sata$`)
var sysfsMMCHost = regexp.MustCompile(`^mmc([0-9]+)$`)

func readMountedBlockVolumes() ([]mountedBlockVolume, error) {
	file, err := os.Open("/proc/self/mountinfo")
	if err != nil {
		return nil, errors.New("mount table unavailable")
	}
	defer file.Close()
	return parseMountedBlockVolumes(file, "/sys/dev/block")
}

func parseMountedBlockVolumes(file *os.File, sysDevBlock string) ([]mountedBlockVolume, error) {
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 4096), 1<<20)
	volumes := make([]mountedBlockVolume, 0)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		separator := -1
		for i, field := range fields {
			if field == "-" {
				separator = i
				break
			}
		}
		if separator < 6 || len(fields) < separator+4 {
			continue
		}
		deviceID := fields[2]
		if !validDeviceNumber(deviceID) {
			continue
		}
		mountPath := unescapeMountField(fields[4])
		if !filepath.IsAbs(mountPath) || filepath.Clean(mountPath) != mountPath {
			continue
		}
		volume := mountedBlockVolume{
			mountID: fields[0], deviceID: deviceID, mountPath: mountPath,
			fsType: fields[separator+1], source: unescapeMountField(fields[separator+2]),
			options: optionSet(fields[5] + "," + fields[separator+3]),
		}
		if target, err := filepath.EvalSymlinks(filepath.Join(sysDevBlock, deviceID)); err == nil {
			volume.sysfsPath = target
			volume.usb = hasUSBAncestor(target)
			volume.sdCard = hasSDCardAncestor(target)
		} else {
			// A mount with no block-device sysfs record is not treated as a USB
			// event. This fails closed for network, FUSE, and synthetic mounts.
			continue
		}
		volumes = append(volumes, volume)
	}
	if err := scanner.Err(); err != nil {
		return nil, errors.New("mount table could not be parsed")
	}
	return volumes, nil
}

func validDeviceNumber(value string) bool {
	major, minor, ok := strings.Cut(value, ":")
	if !ok || major == "" || minor == "" || strings.Contains(minor, ":") {
		return false
	}
	for _, part := range []string{major, minor} {
		for _, char := range part {
			if char < '0' || char > '9' {
				return false
			}
		}
	}
	return true
}

func unescapeMountField(value string) string {
	return strings.NewReplacer(`\040`, " ", `\011`, "\t", `\012`, "\n", `\134`, `\`).Replace(value)
}

func optionSet(value string) map[string]bool {
	result := make(map[string]bool)
	for _, option := range strings.Split(value, ",") {
		if option != "" {
			result[option] = true
		}
	}
	return result
}

func hasUSBAncestor(sysfsPath string) bool {
	for _, component := range strings.Split(filepath.Clean(sysfsPath), string(filepath.Separator)) {
		if sysfsUSBPort.MatchString(component) {
			return true
		}
	}
	return false
}

func hasSDCardAncestor(sysfsPath string) bool {
	components := strings.Split(filepath.Clean(sysfsPath), string(filepath.Separator))
	for i, component := range components {
		match := sysfsMMCHost.FindStringSubmatch(component)
		if len(match) != 2 || match[1] == "0" {
			continue
		}
		if i > 0 && components[i-1] == "mmc_host" {
			return true
		}
	}
	return false
}

func isInternalDrivePath(sysfsPath string) bool {
	hasInternalBus := false
	cleanPath := strings.TrimPrefix(filepath.Clean(sysfsPath), string(filepath.Separator))
	components := strings.Split(cleanPath, string(filepath.Separator))
	for i, component := range components {
		if sysfsUSBPort.MatchString(component) || strings.HasPrefix(component, "mmc") || strings.Contains(component, "mmc_host") {
			return false
		}
		if sysfsATAController.MatchString(component) || sysfsNVMeController.MatchString(component) ||
			(i > 0 && components[i-1] == "platform" && sysfsSATAPlatform.MatchString(component)) {
			hasInternalBus = true
		}
	}
	return hasInternalBus
}
