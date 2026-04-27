package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/jstamagal/bitchtea/internal/config"
	"github.com/jstamagal/bitchtea/internal/llm"
	"github.com/jstamagal/bitchtea/internal/tools"
)

type replayTurn []llm.StreamEvent

type fixtureStreamer struct {
	mu    sync.Mutex
	turns []replayTurn
	calls int
}

func loadReplayFixture(t *testing.T, name string) []replayTurn {
	t.Helper()

	path := filepath.Join("testdata", name)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}

	var turns []replayTurn
	if err := json.Unmarshal(data, &turns); err != nil {
		t.Fatalf("unmarshal fixture %s: %v", name, err)
	}

	return turns
}

func newFixtureStreamer(t *testing.T, name string) *fixtureStreamer {
	t.Helper()
	return &fixtureStreamer{turns: loadReplayFixture(t, name)}
}

func (f *fixtureStreamer) StreamChat(_ context.Context, _ []llm.Message, _ *tools.Registry, events chan<- llm.StreamEvent) {
	defer close(events)

	f.mu.Lock()
	idx := f.calls
	f.calls++
	var turn replayTurn
	if idx < len(f.turns) {
		turn = f.turns[idx]
	}
	f.mu.Unlock()

	if len(turn) == 0 {
		events <- llm.StreamEvent{Type: "done"}
		return
	}

	for _, event := range turn {
		events <- event
	}
}

func TestReplaySimpleReply(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.WorkDir = t.TempDir()
	cfg.SessionDir = t.TempDir()

	streamer := newFixtureStreamer(t, "simple_reply.json")
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

func TestReplayToolLoop(t *testing.T) {
	workDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(workDir, "test.txt"), []byte("hello from tool"), 0644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	cfg := config.DefaultConfig()
	cfg.WorkDir = workDir
	cfg.SessionDir = t.TempDir()

	streamer := newFixtureStreamer(t, "tool_loop.json")
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

func TestReplayExhaustedFixtureDoesNotPanic(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.WorkDir = t.TempDir()
	cfg.SessionDir = t.TempDir()

	streamer := newFixtureStreamer(t, "simple_reply.json")
	agent := NewAgentWithStreamer(&cfg, streamer)

	for i := 0; i < 2; i++ {
		eventCh := make(chan Event, 16)
		go agent.SendMessage(context.Background(), "hello", eventCh)

		var gotDone bool
		for ev := range eventCh {
			switch ev.Type {
			case "done":
				gotDone = true
			case "error":
				t.Fatalf("unexpected error event on call %d: %v", i+1, ev.Error)
			}
		}
		if !gotDone {
			t.Fatalf("expected done event on call %d", i+1)
		}
	}

	if streamer.calls != 2 {
		t.Fatalf("expected 2 streamer calls, got %d", streamer.calls)
	}
}
