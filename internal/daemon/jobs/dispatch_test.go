package jobs_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jstamagal/bitchtea/internal/daemon"
	"github.com/jstamagal/bitchtea/internal/daemon/jobs"
	"github.com/jstamagal/bitchtea/internal/session"
)

// TestRunDispatchesCheckpointJob exercises the full daemon pipeline:
// submit a real envelope, run the daemon for one tick with the jobs.Handle
// dispatcher wired in, and assert the envelope ended up in done/ with a
// success result.
func TestRunDispatchesCheckpointJob(t *testing.T) {
	base := t.TempDir()
	mb := daemon.New(base)

	// Seed a real session JSONL the checkpoint handler can load.
	sessDir := filepath.Join(base, "sessions")
	sess, err := session.New(sessDir)
	if err != nil {
		t.Fatalf("session.New: %v", err)
	}
	if err := sess.Append(session.Entry{Role: "user", Content: "hello"}); err != nil {
		t.Fatalf("Append: %v", err)
	}

	args, _ := json.Marshal(struct {
		SessionPath string `json:"session_path"`
	}{SessionPath: sess.Path})

	// The daemon's crash-recovery scan fails any mail entry older than its
	// startTime, so we submit the envelope asynchronously after Run is
	// already polling. A short sleep is enough — Run's own poll cadence is
	// the only thing we need to overlap.
	idCh := make(chan string, 1)
	errCh := make(chan error, 1)
	go func() {
		time.Sleep(150 * time.Millisecond)
		id, err := mb.Submit(daemon.Job{
			Kind:        jobs.KindSessionCheckpoint,
			Args:        args,
			WorkDir:     base,
			SubmittedAt: time.Now().UTC(),
		})
		errCh <- err
		idCh <- id
	}()

	logger := log.New(&bytes.Buffer{}, "", 0)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := daemon.Run(ctx, daemon.RunOptions{
		BaseDir:     base,
		PollEvery:   50 * time.Millisecond,
		DrainBudget: 200 * time.Millisecond,
		Logger:      logger,
		Dispatch:    jobs.Handle,
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if err := <-errCh; err != nil {
		t.Fatalf("Submit: %v", err)
	}
	id := <-idCh

	// mail/ empty, done/ has the envelope, success=true.
	if _, err := os.Stat(filepath.Join(mb.Paths().MailDir, id+".json")); !os.IsNotExist(err) {
		t.Fatalf("mail file lingered: %v", err)
	}
	donePath := filepath.Join(mb.Paths().DoneDir, id+".json")
	data, err := os.ReadFile(donePath)
	if err != nil {
		t.Fatalf("read done: %v", err)
	}
	res, err := daemon.UnmarshalResult(data)
	if err != nil {
		t.Fatalf("UnmarshalResult: %v", err)
	}
	if !res.Success {
		t.Fatalf("expected success=true, got %+v", res)
	}
	if res.Kind != jobs.KindSessionCheckpoint {
		t.Fatalf("Result.Kind = %q, want %q", res.Kind, jobs.KindSessionCheckpoint)
	}

	// And the actual checkpoint sidecar exists in the session dir.
	if _, err := os.Stat(filepath.Join(sessDir, ".bitchtea_checkpoint.json")); err != nil {
		t.Fatalf("checkpoint sidecar missing: %v", err)
	}
}

// TestRunFailsUnknownKindWithDispatcher verifies that even with the real
// dispatcher wired in, an unknown kind ends up in failed/ — the dispatcher
// returns the "no handler" Result and run.go translates that to Fail().
func TestRunFailsUnknownKindWithDispatcher(t *testing.T) {
	base := t.TempDir()
	mb := daemon.New(base)

	// Submit after Run starts so the recovery scan doesn't preempt the
	// dispatcher (we want to assert dispatch handled the unknown kind,
	// not that recovery rescued a "crashed" envelope).
	idCh := make(chan string, 1)
	errCh := make(chan error, 1)
	go func() {
		time.Sleep(150 * time.Millisecond)
		id, err := mb.Submit(daemon.Job{
			Kind:        "definitely_not_real",
			Args:        json.RawMessage(`{}`),
			WorkDir:     base,
			SubmittedAt: time.Now().UTC(),
		})
		errCh <- err
		idCh <- id
	}()

	logger := log.New(&bytes.Buffer{}, "", 0)
	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()

	if err := daemon.Run(ctx, daemon.RunOptions{
		BaseDir:     base,
		PollEvery:   50 * time.Millisecond,
		DrainBudget: 100 * time.Millisecond,
		Logger:      logger,
		Dispatch:    jobs.Handle,
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if err := <-errCh; err != nil {
		t.Fatalf("Submit: %v", err)
	}
	id := <-idCh

	failedPath := filepath.Join(mb.Paths().FailedDir, id+".json")
	data, err := os.ReadFile(failedPath)
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	res, err := daemon.UnmarshalResult(data)
	if err != nil {
		t.Fatalf("UnmarshalResult: %v", err)
	}
	if res.Success {
		t.Fatalf("want success=false")
	}
	if res.Error == "" {
		t.Fatalf("want non-empty error")
	}
}
