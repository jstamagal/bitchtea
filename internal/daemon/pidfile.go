package daemon

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// WritePid writes pid as a single decimal line to path, atomically via tmp+rename.
// The file is informational (the flock is the source of truth for liveness),
// but the TUI reads it for `/daemon status` output.
func WritePid(path string, pid int) error {
	if err := os.MkdirAll(parentDir(path), 0o700); err != nil {
		return fmt.Errorf("daemon: create pidfile parent: %w", err)
	}
	tmp := path + ".tmp"
	data := []byte(strconv.Itoa(pid) + "\n")
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("daemon: write pidfile tmp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("daemon: rename pidfile: %w", err)
	}
	return nil
}

// ReadPid returns the pid stored in path, or (0, os.ErrNotExist) if missing.
// Callers (status / stop) treat ErrNotExist as "no daemon"; other errors
// indicate a corrupted file and bubble up.
func ReadPid(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err // includes os.ErrNotExist for the missing case
	}
	s := strings.TrimSpace(string(data))
	if s == "" {
		return 0, fmt.Errorf("daemon: empty pidfile %s", path)
	}
	pid, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("daemon: parse pidfile %s: %w", path, err)
	}
	if pid <= 0 {
		return 0, fmt.Errorf("daemon: invalid pid %d in %s", pid, path)
	}
	return pid, nil
}

// RemovePid deletes the pidfile if present. Missing-file is not an error —
// graceful shutdown should be idempotent.
func RemovePid(path string) error {
	err := os.Remove(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
