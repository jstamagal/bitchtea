package daemon

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// newTestDaemon creates a Daemon wired to a temp dataDir with no real API keys.
func newTestDaemon(t *testing.T) *Daemon {
	t.Helper()
	cfg := Config{
		DataDir:        t.TempDir(),
		HeartbeatModel: "test-cheap-model",
		Logger:         log.New(os.Stderr, "[test] ", 0),
	}
	return New(cfg)
}

// TestDaemonStartStopsOnContextCancel verifies that canceling the context causes
// Start to return context.Canceled without hanging.
func TestDaemonStartStopsOnContextCancel(t *testing.T) {
	d := newTestDaemon(t)
	ctx, cancel := context.WithCancel(context.Background())

	runErr := make(chan error, 1)
	go func() {
		runErr <- d.Start(ctx)
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-runErr:
		if err != context.Canceled {
			t.Fatalf("expected context.Canceled, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("daemon did not stop within 2 seconds")
	}
}

// TestDaemonStopSignal verifies that Stop() terminates Start cleanly (nil error).
func TestDaemonStopSignal(t *testing.T) {
	d := newTestDaemon(t)
	ctx := context.Background()

	runErr := make(chan error, 1)
	go func() {
		runErr <- d.Start(ctx)
	}()

	time.Sleep(50 * time.Millisecond)
	d.Stop()

	select {
	case err := <-runErr:
		if err != nil {
			t.Fatalf("expected nil from Stop(), got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("daemon did not stop within 2 seconds")
	}
}

// TestCompactModelIsAlwaysOpus verifies the daemon always uses Opus for
// compaction — never the cheap heartbeat model.
func TestCompactModelIsAlwaysOpus(t *testing.T) {
	cfg := Config{
		DataDir:        t.TempDir(),
		HeartbeatModel: "gpt-4o-mini",
	}
	d := New(cfg)

	if d.compact.Model != compactModel {
		t.Fatalf("compact model must be %q, got %q", compactModel, d.compact.Model)
	}
	if d.compact.Provider != compactProvider {
		t.Fatalf("compact provider must be %q, got %q", compactProvider, d.compact.Provider)
	}
	if d.heartbeat.Model == d.compact.Model {
		t.Fatalf("heartbeat and compact models must differ; both are %q", d.heartbeat.Model)
	}
}

// TestDefaultConfigCompactModel verifies DefaultConfig sets the Opus model for
// compaction regardless of env overrides.
func TestDefaultConfigCompactModel(t *testing.T) {
	cfg := DefaultConfig()
	d := New(cfg)
	if d.compact.Model != compactModel {
		t.Fatalf("DefaultConfig compact model must be %q, got %q", compactModel, d.compact.Model)
	}
}

// TestJanitorPrunesToolBlocks verifies tool fenced blocks are removed without
// touching other content.
func TestJanitorPrunesToolBlocks(t *testing.T) {
	memDir := t.TempDir()
	hotPath := filepath.Join(memDir, "HOT.md")

	content := "# Memory\n\n" +
		"- important decision: use postgres\n\n" +
		"```tool_call\n{\"name\":\"bash\",\"args\":\"ls\"}\n```\n\n" +
		"```tool_result\nmain.go\ngo.mod\n```\n\n" +
		"- another note\n"

	if err := os.WriteFile(hotPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	// No compact APIKey → compaction skipped; prune-only.
	compact := clientParams{Model: compactModel, Provider: compactProvider}
	logger := log.New(os.Stderr, "[test] ", 0)

	if err := runJanitorInDir(context.Background(), memDir, compact, logger); err != nil {
		t.Fatalf("runJanitorInDir: %v", err)
	}

	data, _ := os.ReadFile(hotPath)
	result := string(data)

	for _, bad := range []string{"```tool_call", "```tool_result"} {
		if strings.Contains(result, bad) {
			t.Errorf("expected %q pruned, got:\n%s", bad, result)
		}
	}
	for _, good := range []string{"important decision: use postgres", "another note"} {
		if !strings.Contains(result, good) {
			t.Errorf("expected %q preserved, got:\n%s", good, result)
		}
	}
}

// TestJanitorSkipsCleanSmallFile verifies a small, tool-free HOT.md is not
// rewritten.
func TestJanitorSkipsCleanSmallFile(t *testing.T) {
	memDir := t.TempDir()
	hotPath := filepath.Join(memDir, "HOT.md")
	if err := os.WriteFile(hotPath, []byte("# Memory\n\n- small note\n"), 0644); err != nil {
		t.Fatal(err)
	}

	fi0, _ := os.Stat(hotPath)
	origMod := fi0.ModTime()
	time.Sleep(5 * time.Millisecond)

	compact := clientParams{Model: compactModel, Provider: compactProvider}
	logger := log.New(os.Stderr, "[test] ", 0)
	if err := runJanitorInDir(context.Background(), memDir, compact, logger); err != nil {
		t.Fatalf("runJanitorInDir: %v", err)
	}

	fi1, _ := os.Stat(hotPath)
	if !fi1.ModTime().Equal(origMod) {
		t.Errorf("expected HOT.md unmodified, but mod time changed")
	}
}

// TestJanitorWalksNestedContexts verifies that HOT.md files in nested channel/
// query directories are all processed.
func TestJanitorWalksNestedContexts(t *testing.T) {
	memDir := t.TempDir()
	paths := []string{
		filepath.Join(memDir, "HOT.md"),
		filepath.Join(memDir, "contexts", "channels", "engineering", "HOT.md"),
		filepath.Join(memDir, "contexts", "queries", "alice", "HOT.md"),
	}

	toolBlock := "```tool_call\n{\"name\":\"read\"}\n```\n"
	for _, p := range paths {
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("# M\n\n"+toolBlock+"\n- fact\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	compact := clientParams{Model: compactModel, Provider: compactProvider}
	logger := log.New(os.Stderr, "[test] ", 0)
	if err := runJanitorInDir(context.Background(), memDir, compact, logger); err != nil {
		t.Fatalf("runJanitorInDir: %v", err)
	}

	for _, p := range paths {
		data, _ := os.ReadFile(p)
		if strings.Contains(string(data), "```tool_call") {
			t.Errorf("%s: tool_call block not pruned", p)
		}
	}
}

// TestFlushToDailyFile verifies a dated daily file is created with the right
// header and content.
func TestFlushToDailyFile(t *testing.T) {
	dir := t.TempDir()
	hotPath := filepath.Join(dir, "HOT.md")

	if err := flushToDailyFile(hotPath, "## Old memory\n- detail A\n"); err != nil {
		t.Fatalf("flushToDailyFile: %v", err)
	}

	dailyDir := filepath.Join(dir, "daily")
	entries, err := os.ReadDir(dailyDir)
	if err != nil {
		t.Fatalf("read daily dir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 daily file, got %d", len(entries))
	}

	data, _ := os.ReadFile(filepath.Join(dailyDir, entries[0].Name()))
	body := string(data)
	if !strings.Contains(body, "daemon compaction flush") {
		t.Errorf("expected header in daily file, got:\n%s", body)
	}
	if !strings.Contains(body, "detail A") {
		t.Errorf("expected original content in daily file, got:\n%s", body)
	}
}

// TestWritePID verifies that WritePID creates the file and the cleanup removes it.
func TestWritePID(t *testing.T) {
	dir := t.TempDir()
	cleanup, err := WritePID(dir)
	if err != nil {
		t.Fatalf("WritePID: %v", err)
	}

	pidPath := filepath.Join(dir, "daemon.pid")
	data, err := os.ReadFile(pidPath)
	if err != nil {
		t.Fatalf("read pid file: %v", err)
	}
	if strings.TrimSpace(string(data)) == "" {
		t.Fatal("expected non-empty PID file")
	}

	cleanup()
	if _, err := os.Stat(pidPath); !os.IsNotExist(err) {
		t.Errorf("expected PID file removed after cleanup, got err=%v", err)
	}
}
