package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const captureMeterCapability = "capture-meter"
const captureTapFile = "libprime-analyzer-tap.so"

// engineScriptPath is the Engine start script. On JC11S it is the same inode
// as /data/prime-unlimited/bin/engine-hooked.sh. Tests point it at a hard link.
var engineScriptPath = "/usr/Engine/Scripts/engine"

func knownServiceCapability(capability string) bool {
	switch capability {
	case "splash-image", "media-import", captureMeterCapability:
		return true
	default:
		return false
	}
}

func verifiedCaptureTap(root string, item installedMod) ([]byte, error) {
	if item.Service == nil || item.Service.Capability != captureMeterCapability {
		return nil, errors.New("invalid capture meter")
	}
	binaryPath := filepath.Join(root, filepath.FromSlash(item.Root), filepath.FromSlash(item.Service.Binary))
	tapPath := filepath.Join(filepath.Dir(binaryPath), captureTapFile)
	relative, err := filepath.Rel(root, tapPath)
	if err != nil || strings.HasPrefix(relative, "..") {
		return nil, errors.New("unsafe capture tap")
	}
	data, err := readOwnedFile(tapPath, 16<<20, "capture tap")
	if err != nil {
		return nil, err
	}
	if err := captureTapELF(data, item.Architecture); err != nil {
		return nil, err
	}
	return data, nil
}

func captureTapELF(data []byte, architecture string) error {
	class, machine := byte(1), byte(40)
	if architecture == "aarch64" {
		class, machine = 2, 183
	} else if architecture != "armv7l" {
		return errors.New("unsupported capture tap architecture")
	}
	if len(data) < 64 || !bytes.Equal(data[:4], []byte{0x7f, 'E', 'L', 'F'}) || data[4] != class || data[5] != 1 || data[18] != machine || data[19] != 0 {
		return errors.New("capture tap ELF target mismatch")
	}
	if binary.LittleEndian.Uint16(data[16:18]) != 3 {
		return errors.New("capture tap is not a shared library")
	}
	return nil
}

func captureLauncherFile(root string) (string, error) {
	dataPath := filepath.Join(root, "bin", "engine-hooked.sh")
	dataInfo, err := os.Lstat(dataPath)
	if err != nil || !dataInfo.Mode().IsRegular() || dataInfo.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("engine launcher is not the hooked script")
	}
	engineInfo, err := os.Lstat(engineScriptPath)
	if err != nil || !engineInfo.Mode().IsRegular() || engineInfo.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("engine launcher is not the hooked script")
	}
	if !os.SameFile(dataInfo, engineInfo) {
		return "", errors.New("engine launcher inode changed")
	}
	return dataPath, nil
}

func installCaptureLibrary(root string, payload []byte) (string, error) {
	dir := filepath.Join(root, "lib")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o022 != 0 {
		return "", errors.New("unsafe capture library directory")
	}
	dest := filepath.Join(dir, captureTapFile)
	sum := sha256.Sum256(payload)
	if existing, readErr := os.ReadFile(dest); readErr == nil && sha256.Sum256(existing) == sum {
		if err := os.Chmod(dest, 0o755); err != nil {
			return "", err
		}
		return dest, nil
	}
	temporary, err := os.CreateTemp(dir, ".capture-tap-*")
	if err != nil {
		return "", err
	}
	temporaryPath := temporary.Name()
	success := false
	defer func() {
		if !success {
			_ = temporary.Close()
			_ = os.Remove(temporaryPath)
		}
	}()
	if _, err := temporary.Write(payload); err != nil {
		return "", err
	}
	if err := temporary.Chmod(0o755); err != nil {
		return "", err
	}
	if err := temporary.Close(); err != nil {
		return "", err
	}
	if err := os.Rename(temporaryPath, dest); err != nil {
		return "", err
	}
	success = true
	return dest, nil
}

func armCaptureMeter(root string, item installedMod) error {
	payload, err := verifiedCaptureTap(root, item)
	if err != nil {
		return err
	}
	launcher, err := captureLauncherFile(root)
	if err != nil {
		return err
	}
	tapPath, err := installCaptureLibrary(root, payload)
	if err != nil {
		return err
	}
	return editLauncherPreload(launcher, tapPath, true)
}

func disarmCaptureMeter(root string) error {
	launcher, err := captureLauncherFile(root)
	if err != nil {
		return err
	}
	return editLauncherPreload(launcher, filepath.Join(root, "lib", captureTapFile), false)
}

func editLauncherPreload(path, tapPath string, install bool) error {
	file, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer file.Close()
	current, err := io.ReadAll(file)
	if err != nil {
		return err
	}
	next, err := rewriteLauncherPreload(current, tapPath, install)
	if err != nil {
		return err
	}
	if bytes.Equal(current, next) {
		return nil
	}
	wrote, err := file.WriteAt(next, 0)
	if err != nil || wrote != len(next) {
		return errors.New("launcher write failed")
	}
	if err := file.Truncate(int64(len(next))); err != nil {
		return err
	}
	return file.Sync()
}

func rewriteLauncherPreload(current []byte, tapPath string, install bool) ([]byte, error) {
	text := string(current)
	lines := strings.Split(text, "\n")
	hits := make([]int, 0, 1)
	for i, line := range lines {
		if strings.Contains(line, "LD_PRELOAD=") {
			hits = append(hits, i)
		}
	}
	if len(hits) != 1 {
		return nil, errors.New("launcher preload is not recognizable")
	}
	rewritten, err := rewritePreloadLine(lines[hits[0]], tapPath, install)
	if err != nil {
		return nil, err
	}
	lines[hits[0]] = rewritten
	return []byte(strings.Join(lines, "\n")), nil
}

func rewritePreloadLine(line, tapPath string, install bool) (string, error) {
	if strings.Count(line, "LD_PRELOAD=") != 1 {
		return "", errors.New("launcher preload is not recognizable")
	}
	valueStart := strings.Index(line, "LD_PRELOAD=") + len("LD_PRELOAD=")
	rest := line[valueStart:]
	value := rest
	suffix := ""
	if end := strings.IndexAny(rest, " \t"); end >= 0 {
		value = rest[:end]
		suffix = rest[end:]
	}
	if value == "" || strings.ContainsAny(value, "\"'") {
		return "", errors.New("launcher preload is not recognizable")
	}
	parts := strings.Split(value, ":")
	kept := make([]string, 0, len(parts)+1)
	for _, part := range parts {
		if part == tapPath || strings.HasSuffix(part, "/"+captureTapFile) {
			continue
		}
		if !strings.HasPrefix(part, "/") || !strings.HasSuffix(part, ".so") || strings.Contains(part, "..") {
			return "", errors.New("launcher preload is not recognizable")
		}
		kept = append(kept, part)
	}
	if install {
		if tapPath == "" || !strings.HasPrefix(tapPath, "/") || strings.Contains(tapPath, "..") || !strings.HasSuffix(tapPath, "/"+captureTapFile) {
			return "", errors.New("capture tap path is invalid")
		}
		insertAt := len(kept)
		for i, part := range kept {
			if strings.HasSuffix(part, "/libprime-ui-hook.so") {
				insertAt = i
				break
			}
		}
		if insertAt == len(kept) {
			for i, part := range kept {
				if strings.HasSuffix(part, "/libprime-pcm-live.so") {
					insertAt = i + 1
					break
				}
			}
		}
		next := append(append([]string{}, kept[:insertAt]...), tapPath)
		kept = append(next, kept[insertAt:]...)
	}
	if len(kept) == 0 {
		return "", errors.New("launcher preload is not recognizable")
	}
	return line[:valueStart] + strings.Join(kept, ":") + suffix, nil
}
