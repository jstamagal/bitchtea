package ui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jstamagal/bitchtea/internal/agent"
	"github.com/jstamagal/bitchtea/internal/config"
	"github.com/jstamagal/bitchtea/internal/llm"
	"github.com/jstamagal/bitchtea/internal/session"
)

type stubStreamer struct{}

func (stubStreamer) StreamChat(_ context.Context, _ []llm.Message, _ []llm.ToolDef, events chan<- llm.StreamEvent) {
	close(events)
}

type singleReplyStreamer struct {
	text string
}

func (s singleReplyStreamer) StreamChat(_ context.Context, _ []llm.Message, _ []llm.ToolDef, events chan<- llm.StreamEvent) {
	defer close(events)
	events <- llm.StreamEvent{Type: "text", Text: s.text}
	events <- llm.StreamEvent{Type: "done"}
}

func TestSendToAgentCancelsPreviousContext(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.WorkDir = t.TempDir()
	cfg.SessionDir = t.TempDir()

	model := NewModel(&cfg)
	model.agent = agent.NewAgentWithStreamer(&cfg, stubStreamer{})

	canceled := false
	model.cancel = func() {
		canceled = true
	}

	cmd := model.sendToAgent("keep going")
	if !canceled {
		t.Fatal("expected previous context cancel to be called")
	}
	if cmd == nil {
		t.Fatal("expected wait command")
	}
	if !model.streaming {
		t.Fatal("expected model to enter streaming state")
	}
}

func TestAgentDoneWritesCheckpointInsteadOfMemoryFile(t *testing.T) {
	workDir := t.TempDir()
	sessionDir := t.TempDir()

	cfg := config.DefaultConfig()
	cfg.WorkDir = workDir
	cfg.SessionDir = sessionDir

	model := NewModel(&cfg)
	model.session = &session.Session{Path: filepath.Join(sessionDir, "test.jsonl")}
	model.agent = agent.NewAgentWithStreamer(&cfg, nil)
	model.agent.RestoreMessages([]llm.Message{
		{Role: "system", Content: "system prompt"},
		{Role: "user", Content: "do the thing"},
		{Role: "assistant", Content: "implemented the thing"},
	})
	model.agent.TurnCount = 3
	model.agent.ToolCalls["read"] = 2
	model.streaming = true

	updated, _ := model.Update(agentDoneMsg{})
	got := updated.(Model)

	if got.streaming {
		t.Fatal("expected streaming to stop")
	}
	checkpointPath := filepath.Join(sessionDir, ".bitchtea_checkpoint.json")
	if _, err := os.Stat(checkpointPath); err != nil {
		t.Fatalf("expected checkpoint file: %v", err)
	}
	if _, err := os.Stat(filepath.Join(workDir, "MEMORY.md")); !os.IsNotExist(err) {
		t.Fatalf("expected no MEMORY.md write, got %v", err)
	}
}

func TestAgentDoneUsesAgentFollowUpPrompt(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.WorkDir = t.TempDir()
	cfg.SessionDir = t.TempDir()
	cfg.AutoNextSteps = true

	model := NewModel(&cfg)
	model.session = &session.Session{Path: filepath.Join(cfg.SessionDir, "test.jsonl")}
	model.agent = agent.NewAgentWithStreamer(&cfg, singleReplyStreamer{text: "Fixed the bug and still need to run go test."})
	eventCh := make(chan agent.Event, 16)
	go model.agent.SendMessage(context.Background(), "fix it", eventCh)
	for range eventCh {
	}
	model.streaming = true

	updated, cmd := model.Update(agentDoneMsg{})
	got := updated.(Model)

	if cmd == nil {
		t.Fatal("expected follow-up command")
	}
	if !got.streaming {
		t.Fatal("expected follow-up to restart streaming")
	}
	if len(got.messages) == 0 {
		t.Fatal("expected system status message")
	}
	last := got.messages[len(got.messages)-1]
	if !strings.Contains(last.Content, "auto-next-steps") {
		t.Fatalf("expected auto-next system message, got %q", last.Content)
	}
}
