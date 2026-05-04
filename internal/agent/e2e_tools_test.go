package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/jstamagal/bitchtea/internal/config"
	"github.com/jstamagal/bitchtea/internal/llm"
	"github.com/jstamagal/bitchtea/internal/tools"
)

// e2eStreamer is a fake streamer that executes tools through a real
// tools.Registry and feeds the results back as stream events. Each entry in
// turns describes one StreamChat call; the streamer auto-advances across
// calls via an internal index.
type e2eStreamer struct {
	mu    sync.Mutex
	turns []func(ctx context.Context, reg *tools.Registry, events chan<- llm.StreamEvent)
	calls int
}

func (f *e2eStreamer) StreamChat(ctx context.Context, _ []llm.Message, reg *tools.Registry, events chan<- llm.StreamEvent) {
	defer close(events)

	f.mu.Lock()
	idx := f.calls
	f.calls++
	f.mu.Unlock()

	if idx >= len(f.turns) {
		events <- llm.StreamEvent{Type: "done"}
		return
	}
	f.turns[idx](ctx, reg, events)
}

func TestAgentTurnWithBashEcho(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e agent test in short mode")
	}

	cfg := config.DefaultConfig()
	cfg.WorkDir = t.TempDir()
	cfg.SessionDir = t.TempDir()

	streamer := &e2eStreamer{
		turns: []func(ctx context.Context, reg *tools.Registry, events chan<- llm.StreamEvent){
			func(ctx context.Context, reg *tools.Registry, events chan<- llm.StreamEvent) {
				// Emit a bash tool_call, execute it for real, then emit result + text + done.
				events <- llm.StreamEvent{
					Type:       "tool_call",
					ToolCallID: "call_1",
					ToolName:   "bash",
					ToolArgs:   `{"command":"echo hello from e2e"}`,
				}
				result, err := reg.Execute(ctx, "bash", `{"command":"echo hello from e2e"}`)
				if err != nil {
					events <- llm.StreamEvent{Type: "error", Error: err}
					return
				}
				events <- llm.StreamEvent{
					Type:       "tool_result",
					ToolCallID: "call_1",
					ToolName:   "bash",
					Text:       result,
				}
				events <- llm.StreamEvent{Type: "text", Text: "bash completed successfully"}
				events <- llm.StreamEvent{Type: "done"}
			},
		},
	}

	agent := NewAgentWithStreamer(&cfg, streamer)
	eventCh := make(chan Event, 32)

	go agent.SendMessage(context.Background(), "run echo", eventCh)

	var sawToolStart, sawToolResult bool
	var toolResultText string
	var finalText string
	var gotDone bool
	for ev := range eventCh {
		switch ev.Type {
		case "tool_start":
			if ev.ToolName == "bash" && ev.ToolCallID == "call_1" {
				sawToolStart = true
			}
		case "tool_result":
			if ev.ToolName == "bash" {
				sawToolResult = true
				toolResultText = ev.ToolResult
			}
		case "text":
			finalText += ev.Text
		case "error":
			t.Fatalf("unexpected error event: %v", ev.Error)
		case "done":
			gotDone = true
		}
	}

	if !sawToolStart {
		t.Fatal("expected tool_start event for bash")
	}
	if !sawToolResult {
		t.Fatal("expected tool_result event for bash")
	}
	if !strings.Contains(toolResultText, "hello from e2e") {
		t.Fatalf("expected tool result to contain 'hello from e2e', got %q", toolResultText)
	}
	if !strings.Contains(finalText, "bash completed successfully") {
		t.Fatalf("expected final text to contain 'bash completed successfully', got %q", finalText)
	}
	if !gotDone {
		t.Fatal("expected done event")
	}
	if streamer.calls != 1 {
		t.Fatalf("expected 1 streamer call, got %d", streamer.calls)
	}
}

func TestAgentTurnWithMultipleToolCalls(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e agent test in short mode")
	}

	workDir := t.TempDir()
	// Create a file for the read tool to consume.
	testFile := filepath.Join(workDir, "notes.txt")
	if err := os.WriteFile(testFile, []byte("multi-tool test content"), 0644); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	cfg := config.DefaultConfig()
	cfg.WorkDir = workDir
	cfg.SessionDir = t.TempDir()

	streamer := &e2eStreamer{
		turns: []func(ctx context.Context, reg *tools.Registry, events chan<- llm.StreamEvent){
			func(ctx context.Context, reg *tools.Registry, events chan<- llm.StreamEvent) {
				// First tool: bash listing the file.
				events <- llm.StreamEvent{
					Type:       "tool_call",
					ToolCallID: "call_bash",
					ToolName:   "bash",
					ToolArgs:   `{"command":"ls notes.txt"}`,
				}
				bashResult, err := reg.Execute(ctx, "bash", `{"command":"ls notes.txt"}`)
				if err != nil {
					events <- llm.StreamEvent{Type: "error", Error: err}
					return
				}
				events <- llm.StreamEvent{
					Type:       "tool_result",
					ToolCallID: "call_bash",
					ToolName:   "bash",
					Text:       bashResult,
				}

				// Second tool: read the file.
				events <- llm.StreamEvent{
					Type:       "tool_call",
					ToolCallID: "call_read",
					ToolName:   "read",
					ToolArgs:   `{"path":"notes.txt"}`,
				}
				readResult, err := reg.Execute(ctx, "read", `{"path":"notes.txt"}`)
				if err != nil {
					events <- llm.StreamEvent{Type: "error", Error: err}
					return
				}
				events <- llm.StreamEvent{
					Type:       "tool_result",
					ToolCallID: "call_read",
					ToolName:   "read",
					Text:       readResult,
				}

				events <- llm.StreamEvent{Type: "text", Text: "both tools ran"}
				events <- llm.StreamEvent{Type: "done"}
			},
		},
	}

	agent := NewAgentWithStreamer(&cfg, streamer)
	eventCh := make(chan Event, 32)

	go agent.SendMessage(context.Background(), "list and read the file", eventCh)

	var bashStart, bashResult bool
	var readStart, readResult bool
	var bashText, readText string
	var finalText string
	for ev := range eventCh {
		switch ev.Type {
		case "tool_start":
			switch ev.ToolCallID {
			case "call_bash":
				bashStart = true
			case "call_read":
				readStart = true
			}
		case "tool_result":
			switch ev.ToolCallID {
			case "call_bash":
				bashResult = true
				bashText = ev.ToolResult
			case "call_read":
				readResult = true
				readText = ev.ToolResult
			}
		case "text":
			finalText += ev.Text
		case "error":
			t.Fatalf("unexpected error event: %v", ev.Error)
		}
	}

	if !bashStart || !readStart {
		t.Fatalf("expected both tool_start events: bash=%v read=%v", bashStart, readStart)
	}
	if !bashResult || !readResult {
		t.Fatalf("expected both tool_result events: bash=%v read=%v", bashResult, readResult)
	}
	if !strings.Contains(bashText, "notes.txt") {
		t.Fatalf("expected bash output to contain 'notes.txt', got %q", bashText)
	}
	if !strings.Contains(readText, "multi-tool test content") {
		t.Fatalf("expected read output to contain file content, got %q", readText)
	}
	if finalText == "" {
		t.Fatal("expected text event after multi-tool execution")
	}
}

func TestAgentTurnWithToolError(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e agent test in short mode")
	}

	cfg := config.DefaultConfig()
	cfg.WorkDir = t.TempDir()
	cfg.SessionDir = t.TempDir()

	streamer := &e2eStreamer{
		turns: []func(ctx context.Context, reg *tools.Registry, events chan<- llm.StreamEvent){
			func(ctx context.Context, reg *tools.Registry, events chan<- llm.StreamEvent) {
				// Run `false` — exit code 1. The tool returns output + "Exit code: 1"
				// with nil error (see execBash).
				events <- llm.StreamEvent{
					Type:       "tool_call",
					ToolCallID: "call_err",
					ToolName:   "bash",
					ToolArgs:   `{"command":"false"}`,
				}
				result, execErr := reg.Execute(ctx, "bash", `{"command":"false"}`)
				if execErr != nil {
					events <- llm.StreamEvent{Type: "error", Error: execErr}
					return
				}
				events <- llm.StreamEvent{
					Type:       "tool_result",
					ToolCallID: "call_err",
					ToolName:   "bash",
					Text:       result,
				}
				events <- llm.StreamEvent{Type: "text", Text: "command failed but we saw the exit code"}
				events <- llm.StreamEvent{Type: "done"}
			},
		},
	}

	agent := NewAgentWithStreamer(&cfg, streamer)
	eventCh := make(chan Event, 32)

	go agent.SendMessage(context.Background(), "run false", eventCh)

	var sawToolStart, sawToolResult bool
	var toolResultText string
	var gotDone bool
	for ev := range eventCh {
		switch ev.Type {
		case "tool_start":
			if ev.ToolName == "bash" {
				sawToolStart = true
			}
		case "tool_result":
			if ev.ToolName == "bash" {
				sawToolResult = true
				toolResultText = ev.ToolResult
			}
		case "error":
			t.Fatalf("unexpected error event: %v", ev.Error)
		case "done":
			gotDone = true
		}
	}

	if !sawToolStart {
		t.Fatal("expected tool_start event")
	}
	if !sawToolResult {
		t.Fatal("expected tool_result event")
	}
	if !strings.Contains(toolResultText, "Exit code: 1") {
		t.Fatalf("expected tool result to contain 'Exit code: 1' (error surfaced in result), got %q", toolResultText)
	}
	if !gotDone {
		t.Fatal("expected done event")
	}
}
