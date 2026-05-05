package daemon

// Failure-mode tests asserting exact error wraps and sentinels for the
// daemon's lock and pidfile primitives. See bd issue bt-test.16. The TUI's
// `/daemon status` and `/daemon stop` flows depend on these contracts to
// distinguish "no daemon", "stale pidfile", and "real daemon running".

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// TestErrorMessage_ReadPid_EmptyFile pins the exact "daemon: empty pidfile
// <path>" message used by ReadPid when the file exists but contains nothing
// (or just whitespace). The TUI relies on the unique prefix to distinguish
// this from a parse error.
func TestErrorMessage_ReadPid_EmptyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "daemon.pid")
	if err := os.WriteFile(path, []byte("   \n\t\n"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	_, err := ReadPid(path)
	if err == nil {
		t.Fatal("expected error for empty pidfile, got nil")
	}
	want := "daemon: empty pidfile " + path
	if err.Error() != want {
		t.Fatalf("error: got %q, want %q", err.Error(), want)
	}
}

// TestErrorMessage_ReadPid_GarbageWrapsParseError verifies that ReadPid wraps
// the underlying *strconv.NumError when the pidfile contains non-numeric
// junk, and that the human-facing message includes the file path.
func TestErrorMessage_ReadPid_GarbageWrapsParseError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "daemon.pid")
	if err := os.WriteFile(path, []byte("not-a-pid\n"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	_, err := ReadPid(path)
	if err == nil {
		t.Fatal("expected parse error, got nil")
	}
	wantPrefix := "daemon: parse pidfile " + path + ":"
	if !strings.HasPrefix(err.Error(), wantPrefix) {
		t.Fatalf("error should be prefixed with %q, got %q", wantPrefix, err.Error())
	}
	var numErr *strconv.NumError
	if !errors.As(err, &numErr) {
		t.Fatalf("expected wrapped *strconv.NumError, got %T: %v", err, err)
	}
}

// TestErrorMessage_ReadPid_NegativePid pins the "daemon: invalid pid <n> in
// <path>" message used when the pidfile parses but contains a non-positive
// integer.
func TestErrorMessage_ReadPid_NegativePid(t *testing.T) {
	path := filepath.Join(t.TempDir(), "daemon.pid")
	if err := os.WriteFile(path, []byte("-7\n"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	_, err := ReadPid(path)
	if err == nil {
		t.Fatal("expected error for negative pid, got nil")
	}
	want := fmt.Sprintf("daemon: invalid pid -7 in %s", path)
	if err.Error() != want {
		t.Fatalf("error: got %q, want %q", err.Error(), want)
	}
}

// TestErrorMessage_ReadPid_Missing verifies that ReadPid returns an error
// matching fs.ErrNotExist for callers that want to treat "no daemon" as a
// non-error condition. The TUI's `/daemon stop` flow checks errors.Is(err,
// fs.ErrNotExist) before deciding whether to send SIGTERM.
func TestErrorMessage_ReadPid_Missing(t *testing.T) {
	_, err := ReadPid(filepath.Join(t.TempDir(), "absent.pid"))
	if err == nil {
		t.Fatal("expected error for missing pidfile, got nil")
	}
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("expected wrapped fs.ErrNotExist, got %v", err)
	}
}

// TestErrorMessage_ZombieDaemon_LockReleased simulates the "daemon Stop with
// zombie process" case from the issue brief: a daemon was running, exited
// (gracefully or by SIGKILL), and left behind a stale pidfile. The contract
// is that the kernel released the flock when the holding fd closed, so
// IsLocked must return false even though the pidfile still exists. ReadPid
// continues to return the stale pid without error — it's the caller's job
// to verify the process actually exists.
func TestErrorMessage_ZombieDaemon_LockReleased(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("flock semantics differ on windows")
	}
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "daemon.lock")
	pidPath := filepath.Join(dir, "daemon.pid")

	// Spawn a child process that grabs the lock, writes its pid, then
	// exits. After Wait() returns, the child's fd is closed by the kernel
	// and the flock is released — that's the zombie scenario.
	bin, err := exec.LookPath("sh")
	if err != nil {
		t.Skipf("sh not available: %v", err)
	}
	script := fmt.Sprintf(`exec 200>%q; flock -n 200 || exit 1; echo $$ > %q; exit 0`, lockPath, pidPath)
	cmd := exec.Command(bin, "-c", script)
	if out, err := cmd.CombinedOutput(); err != nil {
		// flock(1) might not be installed; fall back to acquiring the
		// lock in-process and dropping it.
		t.Logf("sh+flock unavailable (%v, %s) — acquiring in-process instead", err, out)
		l, err := Acquire(lockPath)
		if err != nil {
			t.Fatalf("Acquire: %v", err)
		}
		if err := WritePid(pidPath, os.Getpid()); err != nil {
			t.Fatalf("WritePid: %v", err)
		}
		// Drop the lock to simulate the daemon process ending.
		if err := l.Release(); err != nil {
			t.Fatalf("Release: %v", err)
		}
	}

	// After the simulated daemon is gone, IsLocked must report false even
	// though the lock file still exists on disk.
	locked, err := IsLocked(lockPath)
	if err != nil {
		t.Fatalf("IsLocked: %v", err)
	}
	if locked {
		t.Fatal("IsLocked should return false after lock holder exits (zombie pidfile case)")
	}

	// And ReadPid must keep returning the recorded pid without error —
	// liveness is the caller's concern, not ReadPid's.
	pid, err := ReadPid(pidPath)
	if err != nil {
		t.Fatalf("ReadPid: %v", err)
	}
	if pid <= 0 {
		t.Fatalf("ReadPid returned non-positive pid %d", pid)
	}
}
