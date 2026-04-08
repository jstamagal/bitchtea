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
)

type fakeStreamer struct {
	mu        sync.Mutex
	responses []func(chan<- llm.StreamEvent)
	calls     int
}

func (f *fakeStreamer) StreamChat(_ context.Context, _ []llm.Message, _ []llm.ToolDef, events chan<- llm.StreamEvent) {
	defer close(events)

	f.mu.Lock()
	idx := f.calls
	f.calls++
	f.mu.Unlock()

	if idx >= len(f.responses) {
		events <- llm.StreamEvent{Type: "done"}
		return
	}

	f.responses[idx](events)
}

func TestSendMessageWithInjectedStreamer(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.WorkDir = t.TempDir()
	cfg.SessionDir = t.TempDir()

	streamer := &fakeStreamer{
		responses: []func(chan<- llm.StreamEvent){
			func(events chan<- llm.StreamEvent) {
				events <- llm.StreamEvent{Type: "text", Text: "offline reply"}
				events <- llm.StreamEvent{Type: "done"}
			},
		},
	}

	agent := NewAgentWithStreamer(&cfg, streamer)
	eventCh := make(chan Event, 16)

	go agent.SendMessage(context.Background(), "hello", eventCh)

	var text string
	var gotDone bool
	for ev := range eventCh {
		switch ev.Type {
		case "text":
			text += ev.Text
		case "done":
			gotDone = true
		case "error":
			t.Fatalf("unexpected error event: %v", ev.Error)
		}
	}

	if text != "offline reply" {
		t.Fatalf("expected offline reply, got %q", text)
	}
	if !gotDone {
		t.Fatal("expected done event")
	}
	if streamer.calls != 1 {
		t.Fatalf("expected 1 streamer call, got %d", streamer.calls)
	}
	if agent.CostTracker.TotalTokens() == 0 {
		t.Fatal("expected fallback token estimation to record usage")
	}
}

func TestSendMessageExecutesToolCallWithoutNetwork(t *testing.T) {
	workDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(workDir, "test.txt"), []byte("hello from tool"), 0644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	cfg := config.DefaultConfig()
	cfg.WorkDir = workDir
	cfg.SessionDir = t.TempDir()

	streamer := &fakeStreamer{
		responses: []func(chan<- llm.StreamEvent){
			func(events chan<- llm.StreamEvent) {
				events <- llm.StreamEvent{
					Type:       "tool_call",
					ToolCallID: "call_1",
					ToolName:   "read",
					ToolArgs:   `{"path":"test.txt"}`,
				}
				events <- llm.StreamEvent{Type: "done"}
			},
			func(events chan<- llm.StreamEvent) {
				events <- llm.StreamEvent{Type: "text", Text: "done after tool"}
				events <- llm.StreamEvent{Type: "done"}
			},
		},
	}

	agent := NewAgentWithStreamer(&cfg, streamer)
	eventCh := make(chan Event, 32)

	go agent.SendMessage(context.Background(), "read the file", eventCh)

	var sawToolStart bool
	var sawToolResult bool
	var sawFinalText bool
	for ev := range eventCh {
		switch ev.Type {
		case "tool_start":
			if ev.ToolName == "read" {
				sawToolStart = true
			}
		case "tool_result":
			if strings.Contains(ev.ToolResult, "hello from tool") {
				sawToolResult = true
			}
		case "text":
			if strings.Contains(ev.Text, "done after tool") {
				sawFinalText = true
			}
		case "error":
			t.Fatalf("unexpected error event: %v", ev.Error)
		}
	}

	if !sawToolStart {
		t.Fatal("expected tool_start event")
	}
	if !sawToolResult {
		t.Fatal("expected tool_result event with file contents")
	}
	if !sawFinalText {
		t.Fatal("expected final text event after tool execution")
	}
	if streamer.calls != 2 {
		t.Fatalf("expected 2 streamer calls, got %d", streamer.calls)
	}
}

func TestSendMessageUsesReportedUsageWhenAvailable(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.WorkDir = t.TempDir()
	cfg.SessionDir = t.TempDir()

	streamer := &fakeStreamer{
		responses: []func(chan<- llm.StreamEvent){
			func(events chan<- llm.StreamEvent) {
				events <- llm.StreamEvent{Type: "text", Text: "ok"}
				events <- llm.StreamEvent{
					Type:  "usage",
					Usage: &llm.TokenUsage{InputTokens: 321, OutputTokens: 54},
				}
				events <- llm.StreamEvent{Type: "done"}
			},
		},
	}

	agent := NewAgentWithStreamer(&cfg, streamer)
	eventCh := make(chan Event, 16)

	go agent.SendMessage(context.Background(), "hello", eventCh)

	for ev := range eventCh {
		if ev.Type == "error" {
			t.Fatalf("unexpected error event: %v", ev.Error)
		}
	}

	if agent.CostTracker.InputTokens != 321 {
		t.Fatalf("expected 321 input tokens, got %d", agent.CostTracker.InputTokens)
	}
	if agent.CostTracker.OutputTokens != 54 {
		t.Fatalf("expected 54 output tokens, got %d", agent.CostTracker.OutputTokens)
	}
}

func TestSendMessageEmitsDoneAfterStreamError(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.WorkDir = t.TempDir()
	cfg.SessionDir = t.TempDir()

	streamer := &fakeStreamer{
		responses: []func(chan<- llm.StreamEvent){
			func(events chan<- llm.StreamEvent) {
				events <- llm.StreamEvent{Type: "error", Error: context.Canceled}
			},
		},
	}

	agent := NewAgentWithStreamer(&cfg, streamer)
	eventCh := make(chan Event, 16)

	go agent.SendMessage(context.Background(), "hello", eventCh)

	var gotError bool
	var gotDone bool
	var gotIdle bool
	for ev := range eventCh {
		switch ev.Type {
		case "error":
			gotError = true
			if ev.Error != context.Canceled {
				t.Fatalf("expected context.Canceled error, got %v", ev.Error)
			}
		case "done":
			gotDone = true
		case "state":
			if ev.State == StateIdle {
				gotIdle = true
			}
		}
	}

	if !gotError {
		t.Fatal("expected error event")
	}
	if !gotDone {
		t.Fatal("expected done event after stream error")
	}
	if !gotIdle {
		t.Fatal("expected idle state before termination")
	}
}

func TestNewAgentTracksBootstrapMessageCount(t *testing.T) {
	workDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(workDir, "AGENTS.md"), []byte("project rules"), 0644); err != nil {
		t.Fatalf("write AGENTS.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workDir, "MEMORY.md"), []byte("previous notes"), 0644); err != nil {
		t.Fatalf("write MEMORY.md: %v", err)
	}

	cfg := config.DefaultConfig()
	cfg.WorkDir = workDir
	cfg.SessionDir = t.TempDir()

	agent := NewAgentWithStreamer(&cfg, &fakeStreamer{})

	if got := agent.BootstrapMessageCount(); got != 5 {
		t.Fatalf("expected 5 bootstrap messages, got %d", got)
	}
	if got := agent.MessageCount(); got != 5 {
		t.Fatalf("expected bootstrap messages to be in history, got %d", got)
	}
}
