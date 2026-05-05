package jobs

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jstamagal/bitchtea/internal/daemon"
	"github.com/jstamagal/bitchtea/internal/llm"
	"github.com/jstamagal/bitchtea/internal/memory"
	"github.com/jstamagal/bitchtea/internal/session"
)

func TestSessionStitchScansSessionFiles(t *testing.T) {
	root := t.TempDir()
	sessionDir := filepath.Join(root, "sessions")
	workDir := filepath.Join(root, "work")
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Create two session files with tool calls.
	sess1 := &session.Session{
		Path:    filepath.Join(sessionDir, "2026-05-01_100000.jsonl"),
		Entries: []session.Entry{},
	}
	sess1.Append(session.Entry{Role: "user", Content: "read the file"})
	sess1.Append(session.Entry{
		Role:    "assistant",
		Content: "Reading.",
		ToolCalls: []llm.ToolCall{
			{ID: "c1", Type: "function", Function: llm.FunctionCall{Name: "read", Arguments: `{"path":"main.go"}`}},
		},
	})

	sess2 := &session.Session{
		Path:    filepath.Join(sessionDir, "2026-05-02_100000.jsonl"),
		Entries: []session.Entry{},
	}
	sess2.Append(session.Entry{Role: "user", Content: "edit the config"})
	sess2.Append(session.Entry{
		Role:    "assistant",
		Content: "Editing.",
		ToolCalls: []llm.ToolCall{
			{ID: "c2", Type: "function", Function: llm.FunctionCall{Name: "edit", Arguments: `{"path":"config.yaml"}`}},
		},
	})

	args := sessionStitchArgs{
		SessionDir: sessionDir,
		WorkDir:    workDir,
	}
	argsJSON, _ := json.Marshal(args)

	job := daemon.Job{
		Kind:    KindSessionStitch,
		Args:    argsJSON,
		WorkDir: workDir,
	}

	res := Handle(context.Background(), job)
	if !res.Success {
		t.Fatalf("session-stitch failed: %s", res.Error)
	}

	var out sessionStitchOutput
	if err := json.Unmarshal(res.Output, &out); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	if out.SessionsScanned != 2 {
		t.Fatalf("expected 2 sessions scanned, got %d", out.SessionsScanned)
	}
	if len(out.TopTools) == 0 {
		t.Fatal("expected top tools in output")
	}

	// Verify that memory was written.
	hotPath := memory.HotPath(sessionDir, workDir, memory.RootScope())
	data, err := os.ReadFile(hotPath)
	if err != nil {
		t.Fatalf("read hot memory: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "Cross-session patterns") {
		t.Fatalf("expected stitch content in hot memory, got: %q", content)
	}
	if !strings.Contains(content, "read") || !strings.Contains(content, "edit") {
		t.Fatalf("expected tool names in stitch output, got: %q", content)
	}
}

func TestSessionStitchRequiresSessionDir(t *testing.T) {
	job := daemon.Job{
		Kind: KindSessionStitch,
		WorkDir: t.TempDir(),
	}
	res := Handle(context.Background(), job)
	if res.Success {
		t.Fatal("expected failure without session_dir")
	}
	if !strings.Contains(res.Error, "session_dir is required") {
		t.Fatalf("unexpected error: %q", res.Error)
	}
}

func TestSessionStitchRequiresWorkDir(t *testing.T) {
	args := sessionStitchArgs{SessionDir: t.TempDir()}
	argsJSON, _ := json.Marshal(args)
	job := daemon.Job{
		Kind: KindSessionStitch,
		Args: argsJSON,
	}
	res := Handle(context.Background(), job)
	if res.Success {
		t.Fatal("expected failure without work_dir")
	}
	if !strings.Contains(res.Error, "work_dir is required") {
		t.Fatalf("unexpected error: %q", res.Error)
	}
}

func TestSessionStitchEmptyDirIsNoOp(t *testing.T) {
	root := t.TempDir()
	sessionDir := filepath.Join(root, "sessions")
	workDir := filepath.Join(root, "work")
	os.MkdirAll(sessionDir, 0o755)
	os.MkdirAll(workDir, 0o755)

	args := sessionStitchArgs{SessionDir: sessionDir, WorkDir: workDir}
	argsJSON, _ := json.Marshal(args)

	job := daemon.Job{
		Kind:   KindSessionStitch,
		Args:   argsJSON,
		WorkDir: workDir,
	}

	res := Handle(context.Background(), job)
	if !res.Success {
		t.Fatalf("empty dir should succeed: %s", res.Error)
	}
	var out sessionStitchOutput
	if err := json.Unmarshal(res.Output, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.SessionsScanned != 0 {
		t.Fatalf("expected 0 sessions in empty dir, got %d", out.SessionsScanned)
	}
}

func TestSessionStitchCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	args := sessionStitchArgs{SessionDir: t.TempDir(), WorkDir: t.TempDir()}
	argsJSON, _ := json.Marshal(args)

	job := daemon.Job{
		Kind:   KindSessionStitch,
		Args:   argsJSON,
		WorkDir: t.TempDir(),
	}

	res := Handle(ctx, job)
	if res.Success {
		t.Fatal("expected failure on cancelled context")
	}
	if !strings.Contains(res.Error, "session-stitch:") {
		t.Fatalf("unexpected error: %q", res.Error)
	}
}

func TestSessionStitchMaxSessionsCapsScan(t *testing.T) {
	root := t.TempDir()
	sessionDir := filepath.Join(root, "sessions")
	workDir := filepath.Join(root, "work")
	os.MkdirAll(sessionDir, 0o755)
	os.MkdirAll(workDir, 0o755)

	// Create 3 session files.
	for i := 0; i < 3; i++ {
		sess, _ := session.New(sessionDir)
		sess.Append(session.Entry{Role: "user", Content: "msg"})
	}

	args := sessionStitchArgs{
		SessionDir:  sessionDir,
		WorkDir:     workDir,
		MaxSessions: 2,
	}
	argsJSON, _ := json.Marshal(args)

	job := daemon.Job{
		Kind:   KindSessionStitch,
		Args:   argsJSON,
		WorkDir: workDir,
	}

	res := Handle(context.Background(), job)
	if !res.Success {
		t.Fatalf("max_sessions cap: %s", res.Error)
	}
	var out sessionStitchOutput
	if err := json.Unmarshal(res.Output, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.SessionsScanned > 2 {
		t.Fatalf("max_sessions=2 should cap scan, got %d", out.SessionsScanned)
	}
}

func TestTopN(t *testing.T) {
	counts := map[string]int{"read": 10, "edit": 5, "bash": 8, "write": 3}
	top := topN(counts, 3)
	if len(top) != 3 {
		t.Fatalf("expected 3, got %d: %v", len(top), top)
	}
	// Should be sorted by count descending.
	if !strings.HasPrefix(top[0], "read") {
		t.Fatalf("highest count should be first, got %q", top[0])
	}
	if !strings.HasPrefix(top[1], "bash") {
		t.Fatalf("second should be bash, got %q", top[1])
	}
}

func TestExtractPaths(t *testing.T) {
	counts := map[string]int{}
	extractPaths(`{"path":"main.go","offset":1}`, counts)
	extractPaths(`{"path":"main.go","offset":5}`, counts)
	extractPaths(`{"path":"config.yaml"}`, counts)
	extractPaths(`not json`, counts) // should not panic

	if counts["main.go"] != 2 {
		t.Fatalf("expected main.go count=2, got %d", counts["main.go"])
	}
	if counts["config.yaml"] != 1 {
		t.Fatalf("expected config.yaml count=1, got %d", counts["config.yaml"])
	}
}