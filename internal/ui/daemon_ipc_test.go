package ui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jstamagal/bitchtea/internal/agent"
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

	// submitDaemonCheckpoint() resolves the daemon base via config.BaseDir(),
	// which derives from $HOME. Pin HOME to a tempdir so the production
	// resolver and the test agree on where the daemon lives.
	home := t.TempDir()
	t.Setenv("HOME", home)
	base := config.BaseDir()
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatalf("mkdir base: %v", err)
	}
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
	// Pin the daemon base dir explicitly so the test doesn't depend on
	// HOME alone — see Model.daemonBaseDir.
	m.SetDaemonBaseDir(base)

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

// TestAgentDoneMsg_FiresDaemonCheckpointCmd_WhenDaemonRunning verifies the
// real wiring added in bt-wire.6: when the agent finishes a turn (Update
// receives an agentDoneMsg) and a daemon is running on the configured
// base dir, the returned tea.Cmd submits a session-checkpoint job to the
// mailbox. Skipped under -short because it spawns a real daemon.
func TestAgentDoneMsg_FiresDaemonCheckpointCmd_WhenDaemonRunning(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	home := t.TempDir()
	t.Setenv("HOME", home)
	base := config.BaseDir()
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatalf("mkdir base: %v", err)
	}

	// Spin up a daemon.
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
	t.Cleanup(func() {
		daemonCancel()
		<-daemonDone
	})

	// Wait for daemon to lock.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		paths := daemon.Layout(base)
		locked, err := daemon.IsLocked(paths.LockPath)
		if err == nil && locked {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	// Build a model with daemon submission enabled.
	sessDir := filepath.Join(base, "sessions")
	cfg := config.DefaultConfig()
	cfg.WorkDir = t.TempDir()
	cfg.SessionDir = sessDir
	cfg.APIKey = "test-key"
	cfg.Model = "test-model"

	sess, err := session.New(sessDir)
	if err != nil {
		t.Fatalf("session.New: %v", err)
	}
	if err := sess.Append(session.Entry{Role: "user", Content: "hi"}); err != nil {
		t.Fatalf("Append: %v", err)
	}

	m := NewModel(&cfg)
	m.session = sess
	m.config = &cfg
	m.SetDaemonBaseDir(base) // production-equivalent: enables daemon IPC

	// Hook up a streamer + open eventCh so agentDoneMsg passes its early-exit guard.
	m.agent = agent.NewAgentWithStreamer(&cfg, stubStreamer{})
	m.eventCh = make(chan agent.Event)
	m.streaming = true

	// Drive the agent-done branch and pull out the cmd.
	updated, cmd := m.Update(agentDoneMsg{})
	if cmd == nil {
		t.Fatal("agentDoneMsg returned nil cmd despite daemon enabled — daemon submit not wired")
	}
	model := updated.(Model)

	// Execute the cmd to dispatch the daemon submission. tea.Batch wraps
	// child cmds; flatten them so we observe each child's message.
	msgs := drainCmd(t, cmd)

	// Find the daemon submission result.
	var submitted *daemonCheckpointSubmittedMsg
	for i := range msgs {
		if dm, ok := msgs[i].(daemonCheckpointSubmittedMsg); ok {
			submitted = &dm
			break
		}
	}
	if submitted == nil {
		t.Fatalf("no daemonCheckpointSubmittedMsg in cmd output: %v", msgs)
	}
	if submitted.err != nil {
		t.Fatalf("submit error: %v", submitted.err)
	}
	if submitted.skipped {
		t.Fatal("daemon submission was skipped despite running daemon")
	}
	if submitted.jobID == "" {
		t.Fatal("daemon submission returned empty jobID")
	}

	// Replay the result through Update to confirm background activity is recorded.
	updated2, _ := model.Update(*submitted)
	final := updated2.(Model)
	found := false
	for _, a := range final.backgroundActivity {
		if strings.Contains(a.Summary, submitted.jobID) {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("background activity missing submitted jobID %q: %+v", submitted.jobID, final.backgroundActivity)
	}

	// And on disk: the job should land in done/ (or the mailbox queue).
	mailbox := daemon.New(base)
	deadline = time.Now().Add(1 * time.Second)
	var foundOnDisk bool
	for time.Now().Before(deadline) {
		if _, err := os.Stat(filepath.Join(mailbox.Paths().DoneDir, submitted.jobID+".json")); err == nil {
			foundOnDisk = true
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if !foundOnDisk {
		t.Fatalf("daemon never observed job %s in done/", submitted.jobID)
	}
}

// TestAgentDoneMsg_NoDaemonCmd_WhenDaemonBaseDirUnset is the unit-level
// counterpart: with daemonBaseDir empty (the test default), agentDoneMsg
// must NOT spend any I/O on daemon probing. The cmd may be nil or — if a
// queued/follow-up sends another turn — a tea.Batch that does not include
// a daemonCheckpointSubmittedMsg producer.
func TestAgentDoneMsg_NoDaemonCmd_WhenDaemonBaseDirUnset(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.WorkDir = t.TempDir()
	cfg.SessionDir = t.TempDir()
	cfg.APIKey = "test-key"
	cfg.Model = "test-model"

	m := NewModel(&cfg)
	m.agent = agent.NewAgentWithStreamer(&cfg, stubStreamer{})
	m.eventCh = make(chan agent.Event)
	m.streaming = true
	// Intentionally do NOT call SetDaemonBaseDir.

	_, cmd := m.Update(agentDoneMsg{})

	// Execute whatever cmds were emitted and assert no daemon msg is in the stream.
	msgs := drainCmd(t, cmd)
	for _, msg := range msgs {
		if _, ok := msg.(daemonCheckpointSubmittedMsg); ok {
			t.Fatalf("unexpected daemonCheckpointSubmittedMsg when daemonBaseDir is unset: %+v", msg)
		}
	}
}

// drainCmd executes a tea.Cmd (possibly a tea.Batch) and returns every
// non-nil message produced. Nil cmds and nil messages are skipped. Nested
// batches are flattened. Cmds whose closures would block (e.g. waiting on
// an event channel that the test never feeds) are skipped after a short
// timeout so the drain itself doesn't hang the test.
func drainCmd(t *testing.T, cmd tea.Cmd) []tea.Msg {
	t.Helper()
	if cmd == nil {
		return nil
	}
	var out []tea.Msg
	// Run cmd in a goroutine with a timeout so a blocking child (like
	// waitForAgentEvent on an empty chan) doesn't deadlock the test.
	resultCh := make(chan tea.Msg, 1)
	go func() {
		resultCh <- cmd()
	}()
	var msg tea.Msg
	select {
	case msg = <-resultCh:
	case <-time.After(250 * time.Millisecond):
		// Treat as no-op — a cmd that blocks past the deadline contributes
		// no observable message to this drain.
		return out
	}
	if msg == nil {
		return out
	}
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, child := range batch {
			out = append(out, drainCmd(t, child)...)
		}
		return out
	}
	out = append(out, msg)
	return out
}
