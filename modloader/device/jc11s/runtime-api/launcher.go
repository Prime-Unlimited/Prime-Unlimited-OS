package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
)

const maxLaunchLogBytes = 256 * 1024
const maxLaunchScriptBytes = 1024 * 1024

var launcherTimeout = 5 * time.Second

type launchStatus struct {
	ID          string `json:"id"`
	Version     string `json:"version"`
	State       string `json:"state"`
	ExitCode    int    `json:"exitCode"`
	StartedAt   int64  `json:"startedAt"`
	CompletedAt int64  `json:"completedAt"`
	Log         string `json:"log"`
}

type boundedLogWriter struct {
	writer io.Writer
	left   int
}

func (value *boundedLogWriter) Write(data []byte) (int, error) {
	original := len(data)
	if value.left > 0 {
		chunk := data
		if len(chunk) > value.left {
			chunk = chunk[:value.left]
		}
		written, err := value.writer.Write(chunk)
		value.left -= written
		if err != nil {
			return written, err
		}
	}
	return original, nil
}

func readLaunchScript(path string) (*os.File, error) {
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, errors.New("entrypoint is missing or unsafe")
	}
	file := os.NewFile(uintptr(fd), path)
	info, err := file.Stat()
	if err != nil || validateOwnedRegularFile(info, 0o600) != nil || info.Size() <= 0 || info.Size() > maxLaunchScriptBytes {
		file.Close()
		return nil, errors.New("entrypoint is missing or unsafe")
	}
	header := make([]byte, 10)
	count, readErr := io.ReadFull(file, header)
	if readErr != nil && readErr != io.ErrUnexpectedEOF {
		file.Close()
		return nil, readErr
	}
	if !bytes.HasPrefix(header[:count], []byte("#!/bin/sh\n")) {
		file.Close()
		return nil, errors.New("only #!/bin/sh entrypoints are supported")
	}
	if _, err := file.Seek(0, 0); err != nil {
		file.Close()
		return nil, err
	}
	return file, nil
}

func ensureTraverseDirectory(path string) error {
	if err := os.MkdirAll(path, 0o711); err != nil {
		return err
	}
	if err := os.Chmod(path, 0o711); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0o711 {
		return errors.New("unsafe runtime traversal directory")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != uint32(os.Geteuid()) {
		return errors.New("runtime traversal directory has wrong owner")
	}
	return nil
}

func launchEnabledMod(id, root string) (launchStatus, error) {
	status := launchStatus{ID: id, State: "failed", ExitCode: -1, StartedAt: time.Now().Unix()}
	state, err := loadRegistry(filepath.Join(root, "registry", "state.json"))
	if err != nil {
		return status, err
	}
	item, exists := state.Mods[id]
	if !exists {
		return status, errNotInstalled
	}
	status.Version = item.Version
	if !item.Enabled {
		return status, errors.New("mod is not enabled")
	}
	entrypoint := filepath.Join(root, filepath.FromSlash(item.Root), filepath.FromSlash(item.Entrypoint))
	script, err := readLaunchScript(entrypoint)
	if err != nil {
		return status, err
	}
	defer script.Close()
	runtimeBase := filepath.Join(root, "runtime")
	workRoot := filepath.Join(runtimeBase, "work")
	runtimeRoot := filepath.Join(workRoot, id)
	logRoot := filepath.Join(root, "runtime", "logs")
	statusRoot := filepath.Join(root, "runtime", "status")
	for _, directory := range []string{runtimeBase, workRoot} {
		if err := ensureTraverseDirectory(directory); err != nil {
			return status, err
		}
	}
	for _, directory := range []string{logRoot, statusRoot} {
		if err := ensurePrivateDirectory(directory); err != nil {
			return status, err
		}
	}
	if err := os.MkdirAll(runtimeRoot, 0o700); err != nil {
		return status, err
	}
	if info, statErr := os.Lstat(runtimeRoot); statErr != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return status, errors.New("unsafe mod runtime directory")
	}
	if err := os.Chmod(runtimeRoot, 0o700); err != nil {
		return status, err
	}
	if os.Geteuid() == 0 {
		if err := os.Chown(runtimeRoot, 65534, 65534); err != nil {
			return status, err
		}
		if err := os.Chmod(runtimeRoot, 0o700); err != nil {
			return status, err
		}
	}
	logRelative := filepath.ToSlash(filepath.Join("runtime", "logs", id+".log"))
	status.Log = logRelative
	logFile, err := os.OpenFile(filepath.Join(root, filepath.FromSlash(logRelative)), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return status, err
	}
	writer := &boundedLogWriter{writer: logFile, left: maxLaunchLogBytes}
	ctx, cancel := context.WithTimeout(context.Background(), launcherTimeout)
	defer cancel()
	command := exec.CommandContext(ctx, "/bin/sh", "-s")
	command.Stdin, command.Stdout, command.Stderr = script, writer, writer
	command.Dir = runtimeRoot
	command.Env = []string{"PATH=/usr/bin:/bin", "HOME=" + runtimeRoot, "TMPDIR=" + runtimeRoot, "PRIME_UNLIMITED_MOD_ID=" + id, "PRIME_UNLIMITED_MOD_VERSION=" + item.Version}
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGKILL}
	if os.Geteuid() == 0 {
		command.SysProcAttr.Credential = &syscall.Credential{Uid: 65534, Gid: 65534, NoSetGroups: true}
	}
	err = command.Start()
	if err == nil {
		done := make(chan error, 1)
		go func() { done <- command.Wait() }()
		select {
		case err = <-done:
		case <-ctx.Done():
			_ = syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
			err = <-done
			status.State = "timeout"
		}
	}
	if closeErr := logFile.Sync(); err == nil {
		err = closeErr
	}
	if closeErr := logFile.Close(); err == nil {
		err = closeErr
	}
	status.CompletedAt = time.Now().Unix()
	if command.ProcessState != nil {
		status.ExitCode = command.ProcessState.ExitCode()
	}
	if status.State != "timeout" {
		if err == nil {
			status.State, status.ExitCode = "completed", 0
		} else {
			status.State = "failed"
		}
	}
	if writeErr := writeAtomicJSON(filepath.Join(statusRoot, id+".json"), status); writeErr != nil {
		return status, writeErr
	}
	if err != nil {
		return status, fmt.Errorf("mod launch %s: %w", status.State, err)
	}
	return status, nil
}

func loadLaunchStatus(id, root string) (launchStatus, error) {
	var status launchStatus
	data, err := readOwnedFile(filepath.Join(root, "runtime", "status", id+".json"), 64*1024, "launch status")
	if err != nil {
		return status, err
	}
	if decodeStrictJSON(data, &status) != nil || status.ID != id || !modIDPattern.MatchString(status.ID) {
		return status, errors.New("invalid launch status")
	}
	return status, nil
}
