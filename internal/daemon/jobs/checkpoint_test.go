package jobs

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jstamagal/bitchtea/internal/daemon"
	"github.com/jstamagal/bitchtea/internal/llm"
	"github.com/jstamagal/bitchtea/internal/session"
)

// seedSession creates a session JSONL in dir with a small mix of user,
// assistant (with tool_calls), and tool entries so checkpoint summarisation
// has something to count.
func seedSession(t *testing.T, dir string) string {
	t.Helper()
	sess, err := session.New(dir)
	if err != nil {
		t.Fatalf("session.New: %v", err)
	}
	if err := sess.Append(session.Entry{Role: "user", Content: "hi"}); err != nil {
		t.Fatalf("Append user: %v", err)
	}
	if err := sess.Append(session.Entry{
		Role:    "assistant",
		Content: "calling read",
		ToolCalls: []llm.ToolCall{{
			ID:       "call-1",
			Type:     "function",
			Function: llm.FunctionCall{Name: "read", Arguments: `{"path":"/etc/hosts"}`},
		}},
	}); err != nil {
		t.Fatalf("Append assistant: %v", err)
	}
	if err := sess.Append(session.Entry{
		Role: "tool", ToolName: "read", ToolCallID: "call-1", Content: "ok",
	}); err != nil {
		t.Fatalf("Append tool: %v", err)
	}
	if err := sess.Append(session.Entry{Role: "user", Content: "thanks"}); err != nil {
		t.Fatalf("Append user2: %v", err)
	}
	return sess.Path
}

func TestCheckpointWritesSiblingFile(t *testing.T) {
	dir := t.TempDir()
	sessionPath := seedSession(t, dir)

	args, _ := json.Marshal(checkpointArgs{SessionPath: sessionPath, Model: "test-model"})
	job := daemon.Job{
		Kind: KindSessionCheckpoint,
		Args: args,
	}
	res := Handle(context.Background(), job)
	if !res.Success {
		t.Fatalf("checkpoint: want success, got %+v", res)
	}

	cpPath := filepath.Join(dir, ".bitchtea_checkpoint.json")
	data, err := os.ReadFile(cpPath)
	if err != nil {
		t.Fatalf("read checkpoint: %v", err)
	}
	var cp session.Checkpoint
	if err := json.Unmarshal(data, &cp); err != nil {
		t.Fatalf("unmarshal checkpoint: %v", err)
	}
	if cp.TurnCount != 2 {
		t.Fatalf("TurnCount = %d, want 2", cp.TurnCount)
	}
	if cp.ToolCalls["read"] != 1 {
		t.Fatalf("ToolCalls[read] = %d, want 1", cp.ToolCalls["read"])
	}
	if cp.Model != "test-model" {
		t.Fatalf("Model = %q, want test-model", cp.Model)
	}

	// Result.Output should embed the structured checkpoint summary.
	var out checkpointOutput
	if err := json.Unmarshal(res.Output, &out); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	if out.CheckpointPath != cpPath {
		t.Fatalf("output.CheckpointPath = %q, want %q", out.CheckpointPath, cpPath)
	}
	if out.TurnCount != 2 || out.ToolCallCount != 1 {
		t.Fatalf("output counts wrong: %+v", out)
	}
}

func TestCheckpointAcceptsEnvelopeSessionPath(t *testing.T) {
	// Caller drops args entirely; daemon envelope's SessionPath is used.
	dir := t.TempDir()
	sessionPath := seedSession(t, dir)
	job := daemon.Job{Kind: KindSessionCheckpoint, SessionPath: sessionPath}
	res := Handle(context.Background(), job)
	if !res.Success {
		t.Fatalf("envelope-only checkpoint: %+v", res)
	}
}

func TestCheckpointMissingSessionPath(t *testing.T) {
	res := Handle(context.Background(), daemon.Job{Kind: KindSessionCheckpoint})
	if res.Success {
		t.Fatalf("missing session_path: want failure, got success")
	}
	if !strings.Contains(res.Error, "session_path is required") {
		t.Fatalf("error = %q", res.Error)
	}
}

func TestCheckpointIdempotent(t *testing.T) {
	dir := t.TempDir()
	sessionPath := seedSession(t, dir)

	args, _ := json.Marshal(checkpointArgs{SessionPath: sessionPath})
	job := daemon.Job{Kind: KindSessionCheckpoint, Args: args}

	res1 := Handle(context.Background(), job)
	if !res1.Success {
		t.Fatalf("first run: %+v", res1)
	}
	cpPath := filepath.Join(dir, ".bitchtea_checkpoint.json")
	first, err := os.ReadFile(cpPath)
	if err != nil {
		t.Fatalf("read after first: %v", err)
	}
	var cp1 session.Checkpoint
	if err := json.Unmarshal(first, &cp1); err != nil {
		t.Fatalf("unmarshal first: %v", err)
	}

	// Sleep enough to guarantee SaveCheckpoint's embedded Timestamp
	// changes between runs — the *meaningful* fields should still match.
	time.Sleep(10 * time.Millisecond)

	res2 := Handle(context.Background(), job)
	if !res2.Success {
		t.Fatalf("second run: %+v", res2)
	}
	second, err := os.ReadFile(cpPath)
	if err != nil {
		t.Fatalf("read after second: %v", err)
	}
	var cp2 session.Checkpoint
	if err := json.Unmarshal(second, &cp2); err != nil {
		t.Fatalf("unmarshal second: %v", err)
	}

	if cp1.TurnCount != cp2.TurnCount {
		t.Fatalf("TurnCount drifted: %d -> %d", cp1.TurnCount, cp2.TurnCount)
	}
	if len(cp1.ToolCalls) != len(cp2.ToolCalls) {
		t.Fatalf("ToolCalls map size drifted: %d -> %d", len(cp1.ToolCalls), len(cp2.ToolCalls))
	}
	for name, n := range cp1.ToolCalls {
		if cp2.ToolCalls[name] != n {
			t.Fatalf("ToolCalls[%s] drifted: %d -> %d", name, n, cp2.ToolCalls[name])
		}
	}
}

func TestCheckpointHonorsCancellation(t *testing.T) {
	dir := t.TempDir()
	sessionPath := seedSession(t, dir)
	args, _ := json.Marshal(checkpointArgs{SessionPath: sessionPath})

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancel

	res := Handle(ctx, daemon.Job{Kind: KindSessionCheckpoint, Args: args})
	if res.Success {
		t.Fatalf("pre-cancelled ctx: want failure, got success")
	}
	if !strings.Contains(res.Error, "context canceled") && !strings.Contains(res.Error, "canceled") {
		t.Fatalf("error = %q (want canceled)", res.Error)
	}
	// The checkpoint file must NOT exist — handler aborted before write.
	if _, err := os.Stat(filepath.Join(dir, ".bitchtea_checkpoint.json")); !os.IsNotExist(err) {
		t.Fatalf("checkpoint should not exist after cancellation: %v", err)
	}
}
