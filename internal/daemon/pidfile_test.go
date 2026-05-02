package daemon

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteAndReadPid(t *testing.T) {
	path := filepath.Join(t.TempDir(), "daemon.pid")
	if err := WritePid(path, 4242); err != nil {
		t.Fatalf("WritePid: %v", err)
	}
	pid, err := ReadPid(path)
	if err != nil {
		t.Fatalf("ReadPid: %v", err)
	}
	if pid != 4242 {
		t.Fatalf("ReadPid = %d, want 4242", pid)
	}
}

func TestReadPidMissing(t *testing.T) {
	_, err := ReadPid(filepath.Join(t.TempDir(), "missing.pid"))
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("ReadPid missing: want os.ErrNotExist, got %v", err)
	}
}

func TestReadPidGarbage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "daemon.pid")
	if err := os.WriteFile(path, []byte("not-a-pid\n"), 0o600); err != nil {
		t.Fatalf("seed pidfile: %v", err)
	}
	if _, err := ReadPid(path); err == nil {
		t.Fatal("ReadPid garbage: want error, got nil")
	}
}

func TestRemovePidIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "daemon.pid")
	if err := RemovePid(path); err != nil {
		t.Fatalf("RemovePid (missing): %v", err)
	}
	if err := WritePid(path, 1234); err != nil {
		t.Fatalf("WritePid: %v", err)
	}
	if err := RemovePid(path); err != nil {
		t.Fatalf("RemovePid: %v", err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("pidfile still exists after RemovePid: %v", err)
	}
}
