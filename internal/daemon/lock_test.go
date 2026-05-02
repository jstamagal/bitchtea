package daemon

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestAcquireSucceedsOnFreshLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "daemon.lock")
	lock, err := Acquire(path)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if err := lock.Release(); err != nil {
		t.Fatalf("Release: %v", err)
	}
}

func TestAcquireFailsWhenAlreadyHeld(t *testing.T) {
	path := filepath.Join(t.TempDir(), "daemon.lock")
	first, err := Acquire(path)
	if err != nil {
		t.Fatalf("first Acquire: %v", err)
	}
	defer first.Release()

	_, err = Acquire(path)
	if !errors.Is(err, ErrLocked) {
		t.Fatalf("second Acquire: want ErrLocked, got %v", err)
	}
}

func TestIsLockedReportsHeld(t *testing.T) {
	path := filepath.Join(t.TempDir(), "daemon.lock")
	// Missing file → not locked.
	locked, err := IsLocked(path)
	if err != nil {
		t.Fatalf("IsLocked (missing): %v", err)
	}
	if locked {
		t.Fatal("missing lock file reported as locked")
	}

	held, err := Acquire(path)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	defer held.Release()

	locked, err = IsLocked(path)
	if err != nil {
		t.Fatalf("IsLocked (held): %v", err)
	}
	if !locked {
		t.Fatal("held lock reported as not locked")
	}
}

func TestIsLockedAfterReleaseReturnsFalse(t *testing.T) {
	path := filepath.Join(t.TempDir(), "daemon.lock")
	held, err := Acquire(path)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if err := held.Release(); err != nil {
		t.Fatalf("Release: %v", err)
	}
	locked, err := IsLocked(path)
	if err != nil {
		t.Fatalf("IsLocked: %v", err)
	}
	if locked {
		t.Fatal("released lock still reported as locked")
	}
}
