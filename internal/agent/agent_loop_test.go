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
