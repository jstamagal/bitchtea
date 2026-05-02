package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestRunRejectsJobsWithoutHandler verifies the scaffold behavior: any job
// that lands in mail/ during a Run should be moved to failed/ with the
// "no handler" reason. When bt-p7-session-jobs lands real handlers, this
// test will need to grow a registry assertion.
func TestRunRejectsJobsWithoutHandler(t *testing.T) {
	base := t.TempDir()
	mb := New(base)
	job := Job{
		Kind:        "compact",
		Args:        json.RawMessage(`{}`),
		WorkDir:     "/tmp/work",
		Scope:       Scope{Kind: "root"},
		SubmittedAt: time.Now().UTC(),
	}
	id, err := mb.Submit(job)
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}

	var logBuf bytes.Buffer
	logger := log.New(&logBuf, "", 0)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	// Use a short poll so the loop still iterates at least once before ctx ends.
	if err := Run(ctx, RunOptions{
		BaseDir:     base,
		PollEvery:   50 * time.Millisecond,
		DrainBudget: 100 * time.Millisecond,
		Logger:      logger,
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Mail should be empty; failed/ should hold the rejected job.
	if _, err := os.Stat(filepath.Join(mb.Paths().MailDir, id+".json")); !os.IsNotExist(err) {
		t.Fatalf("mail file lingered: %v", err)
	}
	failedPath := filepath.Join(mb.Paths().FailedDir, id+".json")
	data, err := os.ReadFile(failedPath)
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	res, err := UnmarshalResult(data)
	if err != nil {
		t.Fatalf("UnmarshalResult: %v", err)
	}
	if res.Success {
		t.Fatal("expected success=false")
	}
	if res.Error == "" {
		t.Fatal("expected non-empty error reason")
	}
}

// TestRunFailsWhenLockHeld verifies the single-instance guard: if some other
// process holds daemon.lock, Run returns ErrLocked immediately.
func TestRunFailsWhenLockHeld(t *testing.T) {
	base := t.TempDir()
	paths := Layout(base)
	if err := os.MkdirAll(base, 0o700); err != nil {
		t.Fatalf("mkdir base: %v", err)
	}
	holder, err := Acquire(paths.LockPath)
	if err != nil {
		t.Fatalf("seed lock: %v", err)
	}
	defer holder.Release()

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	err = Run(ctx, RunOptions{BaseDir: base, PollEvery: 50 * time.Millisecond, DrainBudget: 50 * time.Millisecond})
	if !errors.Is(err, ErrLocked) {
		t.Fatalf("Run with held lock: want ErrLocked, got %v", err)
	}
}

// TestRunWritesPidFile checks the informational pid file appears for the
// duration of the run and is removed on graceful shutdown.
func TestRunWritesPidFile(t *testing.T) {
	base := t.TempDir()
	paths := Layout(base)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, RunOptions{
			BaseDir:     base,
			PollEvery:   25 * time.Millisecond,
			DrainBudget: 100 * time.Millisecond,
			Logger:      log.New(&bytes.Buffer{}, "", 0),
		})
	}()

	// Wait for pid file to appear.
	deadline := time.Now().Add(2 * time.Second)
	var pid int
	var err error
	for time.Now().Before(deadline) {
		pid, err = ReadPid(paths.PidPath)
		if err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil {
		cancel()
		<-done
		t.Fatalf("pidfile never appeared: %v", err)
	}
	if pid != os.Getpid() {
		t.Fatalf("pidfile = %d, want %d", pid, os.Getpid())
	}

	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	if _, err := os.Stat(paths.PidPath); !os.IsNotExist(err) {
		t.Fatalf("pidfile not cleaned up: %v", err)
	}
}
