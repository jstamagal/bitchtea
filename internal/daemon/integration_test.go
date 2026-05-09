package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Integration coverage for the daemon. The end-to-end subprocess smoke
// lives in e2e_test.go; this file holds the in-process failure-mode tests:
//
//   - stale lock recovery (kernel-released lock + stale pid file)
//   - lock contention (two daemons compete for the same base dir)
//   - crash recovery (pre-existing mail entries are quarantined)
//   - dispatcher hook (job.IDs flow through Run → Dispatch → done/)
//
// These are cheap (sub-second each) so we run them as plain `go test`. They
// never spawn a subprocess; the only fork-style activity is goroutines.

// TestStaleLockRecovered simulates the post-SIGKILL scenario described in
// docs/phase-7-process-model.md §"Single-instance behavior":
//
//   - A previous daemon left a pid file behind with a pid that no longer
//     exists in the process table.
//   - The kernel released the previous flock when the process died.
//
// A fresh Run() should acquire the lock cleanly and overwrite the pid file
// with its own pid. We verify both: the run completes without error, and
// the pid file ends up with os.Getpid().
func TestStaleLockRecovered(t *testing.T) {
	base := t.TempDir()
	paths := Layout(base)

	if err := os.MkdirAll(base, 0o700); err != nil {
		t.Fatalf("mkdir base: %v", err)
	}
	// Plant a stale pid file pointing at a pid that almost certainly does
	// not exist. 99999 is well above the typical default pid_max (32k) on
	// most Linux installs and even when it isn't, the chance of collision
	// with an unrelated process during the test window is negligible.
	if err := WritePid(paths.PidPath, 99999); err != nil {
		t.Fatalf("seed stale pidfile: %v", err)
	}
	// Simulate the prior daemon: take the lock, release it (mimicking the
	// kernel cleanup that happens on process exit).
	holder, err := Acquire(paths.LockPath)
	if err != nil {
		t.Fatalf("seed lock: %v", err)
	}
	if err := holder.Release(); err != nil {
		t.Fatalf("release seed lock: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, RunOptions{
			BaseDir:     base,
			PollEvery:   25 * time.Millisecond,
			DrainBudget: 100 * time.Millisecond,
			Logger:      log.New(&bytes.Buffer{}, "", 0),
		})
	}()

	// Wait until the new daemon has overwritten the pid file with its own pid.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		pid, err := ReadPid(paths.PidPath)
		if err == nil && pid == os.Getpid() {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	pid, err := ReadPid(paths.PidPath)
	if err != nil {
		cancel()
		<-done
		t.Fatalf("pidfile after stale recovery: %v", err)
	}
	if pid != os.Getpid() {
		cancel()
		<-done
		t.Fatalf("pidfile = %d, want %d (our pid); stale value not overwritten", pid, os.Getpid())
	}

	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Run after stale recovery: %v", err)
	}
}

// TestLockContentionTwoDaemons starts daemon-A, then tries to start daemon-B
// against the same base dir. B must return ErrLocked promptly. After A
// stops, a third Run() against the same base dir must succeed — the lock
// got released cleanly.
func TestLockContentionTwoDaemons(t *testing.T) {
	base := t.TempDir()

	ctxA, cancelA := context.WithCancel(context.Background())
	defer cancelA()

	doneA := make(chan error, 1)
	go func() {
		doneA <- Run(ctxA, RunOptions{
			BaseDir:     base,
			PollEvery:   25 * time.Millisecond,
			DrainBudget: 100 * time.Millisecond,
			Logger:      log.New(&bytes.Buffer{}, "A: ", 0),
		})
	}()

	// Wait for A's pid file to appear (proxy for "lock held").
	paths := Layout(base)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(paths.PidPath); err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if _, err := os.Stat(paths.PidPath); err != nil {
		cancelA()
		<-doneA
		t.Fatalf("daemon A never wrote pid file: %v", err)
	}

	// Daemon B against the same base must return ErrLocked.
	ctxB, cancelB := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancelB()
	errB := Run(ctxB, RunOptions{
		BaseDir:     base,
		PollEvery:   25 * time.Millisecond,
		DrainBudget: 50 * time.Millisecond,
		Logger:      log.New(&bytes.Buffer{}, "B: ", 0),
	})
	if !errors.Is(errB, ErrLocked) {
		cancelA()
		<-doneA
		t.Fatalf("daemon B with A holding lock: want ErrLocked, got %v", errB)
	}

	// Stop A; pidfile should disappear.
	cancelA()
	if err := <-doneA; err != nil {
		t.Fatalf("daemon A: %v", err)
	}
	if _, err := os.Stat(paths.PidPath); !os.IsNotExist(err) {
		t.Fatalf("pidfile lingered after A stopped: %v", err)
	}

	// Daemon C against the freed lock must succeed.
	ctxC, cancelC := context.WithCancel(context.Background())
	defer cancelC()
	doneC := make(chan error, 1)
	go func() {
		doneC <- Run(ctxC, RunOptions{
			BaseDir:     base,
			PollEvery:   25 * time.Millisecond,
			DrainBudget: 100 * time.Millisecond,
			Logger:      log.New(&bytes.Buffer{}, "C: ", 0),
		})
	}()
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(paths.PidPath); err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if _, err := os.Stat(paths.PidPath); err != nil {
		cancelC()
		<-doneC
		t.Fatalf("daemon C never claimed lock after A released: %v", err)
	}
	cancelC()
	if err := <-doneC; err != nil {
		t.Fatalf("daemon C: %v", err)
	}
}

// TestCrashRecoveryQuarantinesPreExistingMail covers the "previous daemon
// crashed mid-job" path documented in §"Crash recovery". We pre-populate
// mail/ with two job files whose mtimes are well in the past, then start
// the daemon. Both files should land in failed/ with a reason mentioning a
// crashed previous daemon — never executed by the dispatcher.
func TestCrashRecoveryQuarantinesPreExistingMail(t *testing.T) {
	base := t.TempDir()
	mb := New(base)
	if err := mb.Init(); err != nil {
		t.Fatalf("mb.Init: %v", err)
	}
	paths := mb.Paths()

	// Submit two jobs through the normal API so the on-disk shape is real.
	job1 := Job{
		Kind:        "session-checkpoint",
		Args:        json.RawMessage(`{}`),
		WorkDir:     base,
		Scope:       Scope{Kind: "root"},
		SubmittedAt: time.Now().UTC().Add(-time.Hour),
	}
	id1, err := mb.Submit(job1)
	if err != nil {
		t.Fatalf("Submit job1: %v", err)
	}
	job2 := Job{
		Kind:        "memory-consolidate",
		Args:        json.RawMessage(`{}`),
		WorkDir:     base,
		Scope:       Scope{Kind: "root"},
		SubmittedAt: time.Now().UTC().Add(-time.Hour),
	}
	id2, err := mb.Submit(job2)
	if err != nil {
		t.Fatalf("Submit job2: %v", err)
	}

	// Backdate both mail files so they predate any daemon start time we'll
	// see in the next second. recoverCrashedJobs uses mtime, not the
	// envelope's submitted_at.
	past := time.Now().Add(-time.Hour)
	for _, id := range []string{id1, id2} {
		path := filepath.Join(paths.MailDir, id+".json")
		if err := os.Chtimes(path, past, past); err != nil {
			t.Fatalf("chtimes %s: %v", path, err)
		}
	}

	// Use a dispatcher that records every call so we can assert the
	// pre-existing entries were *not* dispatched. They should be quarantined
	// before the run loop even sees them.
	var dispatchedIDs []string
	var mu sync.Mutex
	dispatch := func(_ context.Context, j Job) Result {
		mu.Lock()
		dispatchedIDs = append(dispatchedIDs, j.ID)
		mu.Unlock()
		return Result{Success: true, Kind: j.Kind}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	if err := Run(ctx, RunOptions{
		BaseDir:     base,
		PollEvery:   25 * time.Millisecond,
		DrainBudget: 100 * time.Millisecond,
		Logger:      log.New(&bytes.Buffer{}, "", 0),
		Dispatch:    dispatch,
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	mu.Lock()
	dispatchedSnapshot := append([]string(nil), dispatchedIDs...)
	mu.Unlock()
	if len(dispatchedSnapshot) != 0 {
		t.Fatalf("dispatcher saw pre-existing jobs: %v", dispatchedSnapshot)
	}

	// Both should now live in failed/ with the crash reason.
	for _, id := range []string{id1, id2} {
		failedPath := filepath.Join(paths.FailedDir, id+".json")
		data, err := os.ReadFile(failedPath)
		if err != nil {
			t.Fatalf("read failed for %s: %v", id, err)
		}
		res, err := UnmarshalResult(data)
		if err != nil {
			t.Fatalf("UnmarshalResult %s: %v", id, err)
		}
		if res.Success {
			t.Fatalf("expected success=false for quarantined %s", id)
		}
		if !contains(res.Error, "previous daemon crashed") {
			t.Fatalf("failed/%s reason = %q, want it to mention crash recovery", id, res.Error)
		}
		if _, err := os.Stat(filepath.Join(paths.MailDir, id+".json")); !os.IsNotExist(err) {
			t.Fatalf("mail/%s lingered after recovery: %v", id, err)
		}
	}
}

// TestRunDispatchesViaHook is the in-process variant of the e2e smoke: a
// dispatcher hook records job IDs and we verify both that Dispatch was
// called and the result envelope landed in done/. The e2e smoke covers the
// same path through a built binary; this version stays in-process so it
// runs in milliseconds and is easy to debug.
func TestRunDispatchesViaHook(t *testing.T) {
	base := t.TempDir()
	mb := New(base)

	var dispatchedIDs atomic.Int32
	var dispatchedKind atomic.Value
	dispatch := func(_ context.Context, j Job) Result {
		dispatchedIDs.Add(1)
		dispatchedKind.Store(j.Kind)
		return Result{Success: true, Kind: j.Kind}
	}

	// Submit after Run has already advanced startTime so the recovery scan
	// doesn't preempt the dispatcher.
	idCh := make(chan string, 1)
	errCh := make(chan error, 1)
	go func() {
		time.Sleep(120 * time.Millisecond)
		id, err := mb.Submit(Job{
			Kind:        "session-checkpoint",
			Args:        json.RawMessage(`{}`),
			WorkDir:     base,
			Scope:       Scope{Kind: "root"},
			SubmittedAt: time.Now().UTC(),
		})
		errCh <- err
		idCh <- id
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()
	if err := Run(ctx, RunOptions{
		BaseDir:     base,
		PollEvery:   25 * time.Millisecond,
		DrainBudget: 100 * time.Millisecond,
		Logger:      log.New(&bytes.Buffer{}, "", 0),
		Dispatch:    dispatch,
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if err := <-errCh; err != nil {
		t.Fatalf("Submit: %v", err)
	}
	id := <-idCh

	if got := dispatchedIDs.Load(); got != 1 {
		t.Fatalf("dispatcher called %d times, want 1", got)
	}
	if got, _ := dispatchedKind.Load().(string); got != "session-checkpoint" {
		t.Fatalf("dispatched kind = %q, want session-checkpoint", got)
	}
	donePath := filepath.Join(mb.Paths().DoneDir, id+".json")
	data, err := os.ReadFile(donePath)
	if err != nil {
		t.Fatalf("read done: %v", err)
	}
	res, err := UnmarshalResult(data)
	if err != nil {
		t.Fatalf("UnmarshalResult: %v", err)
	}
	if !res.Success {
		t.Fatalf("want success=true, got %+v", res)
	}
}

// contains is a tiny helper so we don't import strings just for a single
// substring check inside an assertion.
func contains(haystack, needle string) bool {
	if needle == "" {
		return true
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
