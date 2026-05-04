package ui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jstamagal/bitchtea/internal/config"
	"github.com/jstamagal/bitchtea/internal/daemon"
	daemonjobs "github.com/jstamagal/bitchtea/internal/daemon/jobs"
	"github.com/jstamagal/bitchtea/internal/session"
)

// TestDaemonIPCPath exercises the TUI-to-daemon IPC path end-to-end:
// start a daemon, create a TUI model with a session, simulate a turn
// completion, and assert the daemon received a session-checkpoint job.
// Skipped under -short because it starts a real daemon process.
func TestDaemonIPCPath(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	base := t.TempDir()
	workDir := t.TempDir()

	// Create a session the TUI can reference.
	sessDir := filepath.Join(base, "sessions")
	sess, err := session.New(sessDir)
	if err != nil {
		t.Fatalf("session.New: %v", err)
	}
	if err := sess.Append(session.Entry{Role: "user", Content: "hello"}); err != nil {
		t.Fatalf("Append: %v", err)
	}

	// Start the daemon in the background.
	daemonCtx, daemonCancel := context.WithCancel(context.Background())
	defer daemonCancel()

	daemonDone := make(chan error, 1)
	go func() {
		daemonDone <- daemon.Run(daemonCtx, daemon.RunOptions{
			BaseDir:     base,
			PollEvery:   50 * time.Millisecond,
			DrainBudget: 200 * time.Millisecond,
			Dispatch:    daemonjobs.Handle,
		})
	}()

	// Wait for the daemon to acquire the lock.
	time.Sleep(200 * time.Millisecond)

	// Verify daemon is running.
	paths := daemon.Layout(base)
	locked, err := daemon.IsLocked(paths.LockPath)
	if err != nil || !locked {
		daemonCancel()
		<-daemonDone
		t.Fatalf("daemon not locked after startup")
	}

	// Build a config that points at our base dir.
	cfg := config.DefaultConfig()
	cfg.WorkDir = workDir
	cfg.SessionDir = sessDir
	// Use a fake API key so the model construction doesn't try to connect.
	cfg.APIKey = "test-key"
	cfg.Model = "test-model"

	// Create the model.
	m := NewModel(&cfg)
	m.session = sess
	m.config = &cfg

	// Submit a daemon checkpoint job directly via the mailbox to verify
	// the mailbox is writable and the daemon can process it.
	mailbox := daemon.New(base)
	sessPath := sess.Path
	id, err := mailbox.Submit(daemon.Job{
		Kind:         daemonjobs.KindSessionCheckpoint,
		WorkDir:      workDir,
		SessionPath:  sessPath,
		SubmittedAt:  time.Now().UTC(),
		RequestorPID: os.Getpid(),
	})
	if err != nil {
		daemonCancel()
		<-daemonDone
		t.Fatalf("submit: %v", err)
	}

	// Give the daemon time to process the job.
	time.Sleep(300 * time.Millisecond)

	// The job should be in done/ now.
	donePath := filepath.Join(mailbox.Paths().DoneDir, id+".json")
	data, err := os.ReadFile(donePath)
	if err != nil {
		// Check if it landed in failed/ instead.
		failedPath := filepath.Join(mailbox.Paths().FailedDir, id+".json")
		failedData, ferr := os.ReadFile(failedPath)
		if ferr == nil {
			res, _ := daemon.UnmarshalResult(failedData)
			daemonCancel()
			<-daemonDone
			t.Fatalf("job in failed/ instead of done/: %+v", res)
		}
		daemonCancel()
		<-daemonDone
		t.Fatalf("job not found in done/ or failed/: %v", err)
	}

	res, err := daemon.UnmarshalResult(data)
	if err != nil {
		daemonCancel()
		<-daemonDone
		t.Fatalf("unmarshal result: %v", err)
	}
	if !res.Success {
		daemonCancel()
		<-daemonDone
		t.Fatalf("job failed: %s", res.Error)
	}

	// Also exercise the TUI's submitDaemonCheckpoint path.
	// It should succeed because the daemon is running.
	m.submitDaemonCheckpoint()

	// Check background activity was recorded.
	found := false
	for _, a := range m.backgroundActivity {
		if strings.Contains(a.Summary, "session-checkpoint") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("submitDaemonCheckpoint did not record background activity")
	}

	// Shutdown clean.
	daemonCancel()
	if err := <-daemonDone; err != nil && err != context.Canceled && err != daemon.ErrLocked {
		t.Logf("daemon exit: %v", err)
	}
}
