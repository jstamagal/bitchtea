package daemon_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/jstamagal/bitchtea/internal/daemon"
)

// TestDaemonE2E is the documented end-to-end smoke test. To run it locally:
//
//	go test -run TestDaemonE2E ./internal/daemon/
//
// What it does:
//
//  1. Builds cmd/daemon into a t.TempDir-located binary.
//  2. Spawns the binary with HOME pointed at a separate t.TempDir so it
//     cannot touch the developer's real ~/.bitchtea/.
//  3. Submits a session-checkpoint job to mail/.
//  4. Polls done/ at 50ms until the result envelope appears (10s budget).
//  5. SIGTERMs the daemon and asserts a graceful exit within 5s.
//
// Wall-clock target: well under 10s on the developer's laptop. The polling
// cadence (50ms) and the daemon's own poll cadence (driven by the binary's
// internal default of 5s — but the first processOnce runs immediately at
// startup) bound the latency.
//
// Skipped on non-Linux because the daemon code uses unix.Flock; the build
// would fail before we got here, but the explicit skip makes the intent
// unambiguous when this test surfaces in `go test -v` output on macOS.
func TestDaemonE2E(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skipf("daemon e2e exercises Linux flock semantics; skipping on %s", runtime.GOOS)
	}
	if testing.Short() {
		t.Skip("e2e smoke skipped in -short mode")
	}

	startWall := time.Now()

	// 1) Build the daemon binary into the test's tempdir. We do NOT rely on
	// a pre-built binary on PATH — that would couple the test to whatever
	// the developer last built and could mask regressions.
	binDir := t.TempDir()
	binPath := filepath.Join(binDir, "bitchtea-daemon")
	build := exec.Command("go", "build", "-o", binPath, "./cmd/daemon")
	// The test binary's CWD is /home/admin/bitchtea/internal/daemon — walk
	// up two levels to find the module root.
	build.Dir = repoRoot(t)
	build.Env = os.Environ()
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build ./cmd/daemon: %v\n%s", err, out)
	}

	// 2) Set up an isolated HOME so config.BaseDir() resolves into a
	// tempdir. The daemon writes daemon.{lock,pid,log} and daemon/{mail,done,
	// failed}/ here.
	home := t.TempDir()
	cmd := exec.Command(binPath)
	cmd.Env = append(os.Environ(), "HOME="+home)
	// Capture stdout/stderr into the test log so failures show daemon output.
	logBuf := &syncBuffer{}
	cmd.Stdout = logBuf
	cmd.Stderr = logBuf

	if err := cmd.Start(); err != nil {
		t.Fatalf("start daemon: %v", err)
	}
	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = cmd.Process.Signal(syscall.SIGKILL) // belt-and-braces cleanup
		}
	})

	base := filepath.Join(home, ".bitchtea")
	paths := daemon.Layout(base)

	// Wait for the daemon's "started" log line. We wait for the *log*
	// (not the pidfile) because the pidfile is written before startTime is
	// captured for crash recovery — submitting too early can land our job
	// in failed/ as a "previous daemon crashed" entry. The "started" line
	// is logged after recoverCrashedJobs returns, so seeing it guarantees
	// our subsequent Submit will have an mtime > startTime.
	if err := waitFor(3*time.Second, 50*time.Millisecond, func() bool {
		return strings.Contains(logBuf.String(), "started")
	}); err != nil {
		t.Fatalf("daemon never logged 'started': %v\nlog:\n%s", err, logBuf.String())
	}
	// Sanity: the pidfile should be present by now too.
	if _, err := daemon.ReadPid(paths.PidPath); err != nil {
		t.Fatalf("daemon pidfile missing despite 'started' log: %v", err)
	}

	// 3) Submit a real session-checkpoint job. We seed a minimal session
	// JSONL in HOME so the handler has something concrete to load.
	sessDir := filepath.Join(home, "session")
	if err := os.MkdirAll(sessDir, 0o700); err != nil {
		t.Fatalf("mkdir sessdir: %v", err)
	}
	sessPath := filepath.Join(sessDir, "smoke.jsonl")
	// The session JSONL format is "one Entry per line". An empty file is
	// fine — session.Load tolerates zero entries — but we write one entry
	// so the checkpoint output is non-trivial.
	entry := map[string]any{"role": "user", "content": "hello"}
	entryJSON, _ := json.Marshal(entry)
	if err := os.WriteFile(sessPath, append(entryJSON, '\n'), 0o600); err != nil {
		t.Fatalf("write session jsonl: %v", err)
	}

	args, _ := json.Marshal(struct {
		SessionPath string `json:"session_path"`
	}{SessionPath: sessPath})

	mb := daemon.New(base)
	id, err := mb.Submit(daemon.Job{
		Kind:        "session-checkpoint",
		Args:        args,
		WorkDir:     home,
		Scope:       daemon.Scope{Kind: "root"},
		SubmittedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}

	// 4) Poll done/ for the result. 10s wall-clock budget covers the
	// daemon's first-poll-after-recovery latency comfortably.
	donePath := filepath.Join(paths.DoneDir, id+".json")
	failedPath := filepath.Join(paths.FailedDir, id+".json")
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(donePath); err == nil {
			break
		}
		if _, err := os.Stat(failedPath); err == nil {
			data, _ := os.ReadFile(failedPath)
			t.Fatalf("job ended up in failed/: %s\nlog:\n%s", data, logBuf.String())
		}
		time.Sleep(50 * time.Millisecond)
	}
	data, err := os.ReadFile(donePath)
	if err != nil {
		t.Fatalf("done envelope never appeared (deadline exceeded): %v\nlog:\n%s", err, logBuf.String())
	}
	res, err := daemon.UnmarshalResult(data)
	if err != nil {
		t.Fatalf("UnmarshalResult: %v\nraw:%s", err, data)
	}
	if !res.Success {
		t.Fatalf("expected success=true, got %+v\nlog:\n%s", res, logBuf.String())
	}
	if res.Kind != "session-checkpoint" {
		t.Fatalf("Result.Kind = %q, want session-checkpoint", res.Kind)
	}

	// 5) SIGTERM and assert graceful exit within 5s.
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("SIGTERM: %v", err)
	}
	exitCh := make(chan error, 1)
	go func() { exitCh <- cmd.Wait() }()
	select {
	case err := <-exitCh:
		if err != nil {
			// Graceful drain returns nil per run.go. If the process exited
			// non-zero there's a bug worth surfacing — but only fail if it
			// wasn't a signal-driven exit.
			if _, ok := err.(*exec.ExitError); !ok {
				t.Fatalf("daemon exit: %v\nlog:\n%s", err, logBuf.String())
			}
		}
	case <-time.After(5 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatalf("daemon did not exit within 5s of SIGTERM\nlog:\n%s", logBuf.String())
	}

	// pidfile should be gone after a clean shutdown.
	if _, err := os.Stat(paths.PidPath); !os.IsNotExist(err) {
		t.Fatalf("pidfile lingered after graceful stop: %v", err)
	}

	t.Logf("e2e smoke wall clock: %s", time.Since(startWall))
}

// repoRoot walks up from the test's CWD until it finds go.mod. We use it
// to pin the `go build` invocation to the module root regardless of where
// the test happens to run from.
func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	dir := wd
	for i := 0; i < 8; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Fatalf("could not locate go.mod walking up from %s", wd)
	return ""
}

// waitFor polls cond every interval until it returns true or the budget
// expires. Returns context.DeadlineExceeded on timeout.
func waitFor(budget, interval time.Duration, cond func() bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), budget)
	defer cancel()
	tick := time.NewTicker(interval)
	defer tick.Stop()
	if cond() {
		return nil
	}
	for {
		select {
		case <-tick.C:
			if cond() {
				return nil
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// syncBuffer is a tiny thread-safe buffer for the daemon subprocess output.
// We can't use bytes.Buffer directly because the Wait goroutine and the
// test goroutine both touch it.
type syncBuffer struct {
	mu  sync.Mutex
	buf []byte
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.buf = append(b.buf, p...)
	return len(p), nil
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return string(b.buf)
}
