package daemon

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

// ErrLocked is returned by Acquire when another process holds the lock.
// Callers (especially the CLI) check for this with errors.Is so they can
// print "daemon already running" instead of a generic error.
var ErrLocked = errors.New("daemon: lock already held")

// Lock represents a held flock on the daemon lock file. The kernel releases
// the lock automatically when the file descriptor closes (which happens on
// process exit, including SIGKILL), so callers need not panic-unwind to
// guarantee release. Release() is for clean shutdown only.
type Lock struct {
	path string
	f    *os.File
}

// Acquire tries to take an exclusive, non-blocking flock on path. It creates
// the file (and any missing parent directories) with mode 0600 if needed.
// Returns ErrLocked if another process already holds the lock.
//
// The lock file is intentionally separate from the pid file (see design doc):
// the lock is authoritative; the pid file is informational and may go stale.
func Acquire(path string) (*Lock, error) {
	if err := os.MkdirAll(parentDir(path), 0o700); err != nil {
		return nil, fmt.Errorf("daemon: create lock parent: %w", err)
	}
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, fmt.Errorf("daemon: open lock file: %w", err)
	}
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		_ = f.Close()
		if errors.Is(err, unix.EWOULDBLOCK) {
			return nil, ErrLocked
		}
		return nil, fmt.Errorf("daemon: flock: %w", err)
	}
	return &Lock{path: path, f: f}, nil
}

// Release unlocks and closes the file. Idempotent.
func (l *Lock) Release() error {
	if l == nil || l.f == nil {
		return nil
	}
	// Best-effort unlock; closing the fd would also do it.
	_ = unix.Flock(int(l.f.Fd()), unix.LOCK_UN)
	err := l.f.Close()
	l.f = nil
	return err
}

// IsLocked reports whether path is currently flocked by another process. It
// works by attempting (and immediately releasing) a non-blocking exclusive
// lock. Used by `bitchtea daemon status` to render running/not-running.
func IsLocked(path string) (bool, error) {
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	f, err := os.OpenFile(path, os.O_RDWR, 0o600)
	if err != nil {
		return false, fmt.Errorf("daemon: open lock for probe: %w", err)
	}
	defer f.Close()
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		if errors.Is(err, unix.EWOULDBLOCK) {
			return true, nil
		}
		return false, fmt.Errorf("daemon: probe flock: %w", err)
	}
	_ = unix.Flock(int(f.Fd()), unix.LOCK_UN)
	return false, nil
}

func parentDir(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' {
			return path[:i]
		}
	}
	return "."
}
