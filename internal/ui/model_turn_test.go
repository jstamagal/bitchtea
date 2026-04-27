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
	"github.com/jstamagal/bitchtea/internal/llm"
	"github.com/jstamagal/bitchtea/internal/session"
	"github.com/jstamagal/bitchtea/internal/tools"
)

type stubStreamer struct{}

func (stubStreamer) StreamChat(_ context.Context, _ []llm.Message, _ *tools.Registry, events chan<- llm.StreamEvent) {
	close(events)
}

type singleReplyStreamer struct {
	text string
}

func (s singleReplyStreamer) StreamChat(_ context.Context, _ []llm.Message, _ *tools.Registry, events chan<- llm.StreamEvent) {
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

func TestUpUnqueuesLastQueuedMessage(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.WorkDir = t.TempDir()
	cfg.SessionDir = t.TempDir()

	model := NewModel(&cfg)
	model.queued = []string{"first", "second"}

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyUp})
	got := updated.(Model)

	if len(got.queued) != 1 || got.queued[0] != "first" {
		t.Fatalf("expected last queued message to be removed, got %#v", got.queued)
	}
	if got.input.Value() != "second" {
		t.Fatalf("expected unqueued message in input, got %q", got.input.Value())
	}
}

func TestCtrlCCancelsTurnWithoutClearingQueue(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.WorkDir = t.TempDir()
	cfg.SessionDir = t.TempDir()

	model := NewModel(&cfg)
	model.streaming = true
	model.queued = []string{"first", "second"}
	canceled := false
	model.cancel = func() { canceled = true }

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	got := updated.(Model)

	if !canceled {
		t.Fatal("expected ctrl+c to cancel active turn")
	}
	if got.streaming {
		t.Fatal("expected ctrl+c to stop streaming")
	}
	if len(got.queued) != 2 {
		t.Fatalf("expected first ctrl+c to leave queued messages intact, got %#v", got.queued)
	}
	if got.queueClearArmed {
		t.Fatal("expected ctrl+c not to arm queue clearing")
	}
	if got.ctrlCStage != 1 {
		t.Fatalf("expected first ctrl+c to set stage 1, got %d", got.ctrlCStage)
	}
	if len(got.messages) == 0 || !strings.Contains(got.messages[len(got.messages)-1].Content, "Press Ctrl+C again to clear") {
		t.Fatalf("expected ctrl+c feedback to report queued messages, got %#v", got.messages)
	}
}

func TestSecondCtrlCClearsQueueAfterCancellingTurn(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.WorkDir = t.TempDir()
	cfg.SessionDir = t.TempDir()

	model := NewModel(&cfg)
	model.streaming = true
	model.queued = []string{"first", "second"}
	model.cancel = func() {}

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	updated, cmd := updated.(Model).Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	got := updated.(Model)

	if cmd != nil {
		t.Fatal("expected second ctrl+c to clear queue without quitting")
	}
	if len(got.queued) != 0 {
		t.Fatalf("expected second ctrl+c to clear queued messages, got %#v", got.queued)
	}
	if got.queueClearArmed {
		t.Fatal("expected ctrl+c not to arm queue clearing")
	}
	if got.ctrlCStage != 2 {
		t.Fatalf("expected second ctrl+c to set stage 2, got %d", got.ctrlCStage)
	}
}

func TestThirdCtrlCQuitsWithinWindow(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.WorkDir = t.TempDir()
	cfg.SessionDir = t.TempDir()

	model := NewModel(&cfg)
	model.streaming = true
	model.queued = []string{"first", "second"}
	model.cancel = func() {}

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	updated, _ = updated.(Model).Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	_, cmd := updated.(Model).Update(tea.KeyMsg{Type: tea.KeyCtrlC})

	if cmd == nil {
		t.Fatal("expected third ctrl+c within window to quit")
	}
}

func TestCtrlCSequenceResetsAfterTimeout(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.WorkDir = t.TempDir()
	cfg.SessionDir = t.TempDir()

	model := NewModel(&cfg)
	model.ctrlCStage = 2
	model.ctrlCLast = time.Now().Add(-2 * ctrlCGraduationWindow)

	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	got := updated.(Model)

	if cmd != nil {
		t.Fatal("expected ctrl+c after timeout not to quit")
	}
	if got.ctrlCStage != 1 {
		t.Fatalf("expected ctrl+c stage to reset to 1, got %d", got.ctrlCStage)
	}
	if len(got.messages) == 0 || !strings.Contains(got.messages[len(got.messages)-1].Content, "twice more") {
		t.Fatalf("expected reset ctrl+c feedback, got %#v", got.messages)
	}
}

func TestCancelledTurnDoneDoesNotDrainQueue(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.WorkDir = t.TempDir()
	cfg.SessionDir = t.TempDir()

	model := NewModel(&cfg)
	oldCh := make(chan agent.Event)
	model.eventCh = oldCh
	model.streaming = true
	model.queued = []string{"next"}
	model.cancel = func() {}

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	got := updated.(Model)
	updated, cmd := got.Update(agentDoneMsg{ch: oldCh})
	got = updated.(Model)

	if cmd != nil {
		t.Fatal("expected stale done after ctrl+c to produce no command")
	}
	if len(got.queued) != 1 || got.queued[0] != "next" {
		t.Fatalf("expected stale done after ctrl+c to leave queue alone, got %#v", got.queued)
	}
	if got.streaming {
		t.Fatal("expected ctrl+c to leave model stopped")
	}
}

func TestFirstEscDuringStreamingPromptsForTurnCancel(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.WorkDir = t.TempDir()
	cfg.SessionDir = t.TempDir()

	model := NewModel(&cfg)
	model.streaming = true
	model.queued = []string{"first", "second"}
	canceled := false
	model.cancel = func() { canceled = true }

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	got := updated.(Model)

	if canceled {
		t.Fatal("expected first esc without active tool to leave turn running")
	}
	if !got.streaming {
		t.Fatal("expected first esc without active tool to leave streaming on")
	}
	if len(got.queued) != 2 {
		t.Fatalf("expected first esc to keep queued messages, got %#v", got.queued)
	}
	if len(got.messages) == 0 || !strings.Contains(got.messages[len(got.messages)-1].Content, "Press Esc again to cancel the turn") {
		t.Fatalf("expected first esc feedback, got %#v", got.messages)
	}
}

func TestSecondEscCancelsTurnWithoutClearingQueue(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.WorkDir = t.TempDir()
	cfg.SessionDir = t.TempDir()

	model := NewModel(&cfg)
	model.streaming = true
	model.queued = []string{"first", "second"}
	canceled := false
	model.cancel = func() { canceled = true }

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	updated, _ = updated.(Model).Update(tea.KeyMsg{Type: tea.KeyEsc})
	got := updated.(Model)

	if !canceled {
		t.Fatal("expected second esc to cancel active turn")
	}
	if got.streaming {
		t.Fatal("expected second esc to stop streaming")
	}
	if len(got.queued) != 2 {
		t.Fatalf("expected second esc to preserve queued messages, got %#v", got.queued)
	}
	if !got.queueClearArmed {
		t.Fatal("expected second esc to arm queue clearing")
	}
	if len(got.messages) == 0 || !strings.Contains(got.messages[len(got.messages)-1].Content, "press Esc again to clear them") {
		t.Fatalf("expected second esc feedback to report queued messages, got %#v", got.messages)
	}
}

func TestThirdEscClearsQueueAfterTurnCancel(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.WorkDir = t.TempDir()
	cfg.SessionDir = t.TempDir()

	model := NewModel(&cfg)
	model.streaming = true
	model.queued = []string{"first", "second"}
	model.cancel = func() {}

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	updated, _ = updated.(Model).Update(tea.KeyMsg{Type: tea.KeyEsc})
	updated, _ = updated.(Model).Update(tea.KeyMsg{Type: tea.KeyEsc})
	got := updated.(Model)

	if len(got.queued) != 0 {
		t.Fatalf("expected third esc to clear queued messages, got %#v", got.queued)
	}
	if got.queueClearArmed {
		t.Fatal("expected queue clear arm to reset after clearing")
	}
}

func TestEscSequenceResetsAfterTimeout(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.WorkDir = t.TempDir()
	cfg.SessionDir = t.TempDir()

	model := NewModel(&cfg)
	model.streaming = true
	model.queued = []string{"first"}
	canceled := false
	model.cancel = func() { canceled = true }
	model.escStage = 1
	model.escLast = time.Now().Add(-2 * escGraduationWindow)

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	got := updated.(Model)

	if canceled {
		t.Fatal("expected esc after timeout to restart at stage 1")
	}
	if !got.streaming {
		t.Fatal("expected esc after timeout to leave turn running")
	}
	if got.escStage != 1 {
		t.Fatalf("expected esc stage to reset to 1, got %d", got.escStage)
	}
}

func TestFirstEscDuringRunningToolFallsBackToTurnCancel(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.WorkDir = t.TempDir()
	cfg.SessionDir = t.TempDir()

	model := NewModel(&cfg)
	model.streaming = true
	model.activeToolName = "bash"
	model.queued = []string{"next"}
	canceled := false
	model.cancel = func() { canceled = true }

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	got := updated.(Model)

	if !canceled {
		t.Fatal("expected first esc during active tool to cancel current turn fallback")
	}
	if got.streaming {
		t.Fatal("expected active-tool fallback to stop streaming")
	}
	if len(got.queued) != 1 {
		t.Fatalf("expected active-tool fallback to preserve queue, got %#v", got.queued)
	}
	if len(got.messages) == 0 || !strings.Contains(got.messages[len(got.messages)-1].Content, "Tool-only cancel is not wired yet") {
		t.Fatalf("expected active-tool fallback feedback, got %#v", got.messages)
	}
}

func TestStaleAgentEventsAreIgnoredAfterChannelReplacement(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.WorkDir = t.TempDir()
	cfg.SessionDir = t.TempDir()

	model := NewModel(&cfg)
	oldCh := make(chan agent.Event)
	newCh := make(chan agent.Event)
	model.eventCh = newCh
	model.streaming = true

	updated, _ := model.Update(agentEventMsg{
		ch:    oldCh,
		event: agent.Event{Type: "tool_start", ToolName: "bash"},
	})
	got := updated.(Model)

	if len(got.messages) != 0 {
		t.Fatalf("expected stale event to be ignored, got messages %#v", got.messages)
	}
	if !got.streaming {
		t.Fatal("expected stale event to leave current streaming state alone")
	}
}

func TestStaleAgentDoneIsIgnoredAfterChannelReplacement(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.WorkDir = t.TempDir()
	cfg.SessionDir = t.TempDir()

	model := NewModel(&cfg)
	oldCh := make(chan agent.Event)
	newCh := make(chan agent.Event)
	model.eventCh = newCh
	model.streaming = true
	model.queued = []string{"next"}

	updated, cmd := model.Update(agentDoneMsg{ch: oldCh})
	got := updated.(Model)

	if cmd != nil {
		t.Fatal("expected stale done to produce no command")
	}
	if !got.streaming {
		t.Fatal("expected stale done to leave current streaming state alone")
	}
	if len(got.queued) != 1 || got.queued[0] != "next" {
		t.Fatalf("expected stale done to leave queue alone, got %#v", got.queued)
	}
	if got.eventCh != newCh {
		t.Fatal("expected stale done to leave current event channel alone")
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

func TestNotifyBackgroundActivityKeepsViewportClean(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.WorkDir = t.TempDir()
	cfg.SessionDir = t.TempDir()

	model := NewModel(&cfg)
	model.messages = append(model.messages, ChatMessage{
		Time:    time.Date(2026, 4, 8, 9, 0, 0, 0, time.UTC),
		Type:    MsgSystem,
		Content: "foreground only",
	})

	model.NotifyBackgroundActivity(BackgroundActivity{
		Time:    time.Date(2026, 4, 8, 9, 1, 0, 0, time.UTC),
		Context: "#infra",
		Sender:  "deploy-bot",
		Summary: "build failed",
	})

	if len(model.messages) != 1 {
		t.Fatalf("expected viewport messages unchanged, got %d", len(model.messages))
	}
	if model.backgroundUnread != 1 {
		t.Fatalf("expected one unread background notice, got %d", model.backgroundUnread)
	}
	if got := model.backgroundActivityReport(); !strings.Contains(got, "[#infra] <deploy-bot> build failed") {
		t.Fatalf("unexpected background report: %q", got)
	}
}

func TestViewShowsContextAndBackgroundStatus(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.WorkDir = t.TempDir()
	cfg.SessionDir = t.TempDir()

	model := NewModel(&cfg)
	model.ready = true
	model.width = 120
	model.height = 24
	model.viewport.Width = 120
	model.viewport.Height = 10
	model.SetActiveContext("#ops")
	model.NotifyBackgroundActivity(BackgroundActivity{
		Time:    time.Date(2026, 4, 8, 9, 1, 0, 0, time.UTC),
		Context: "@coding-buddy",
		Sender:  "coding-buddy",
		Summary: "left notes in /activity",
	})

	view := model.View()
	for _, want := range []string{"[#ops]", "bg:1", "coding-buddy", "/activity"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected %q in view, got %q", want, view)
		}
	}
}

func TestHandleAgentEventUpdatesThinkingPlaceholder(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.WorkDir = t.TempDir()
	cfg.SessionDir = t.TempDir()

	model := NewModel(&cfg)
	model.handleAgentEvent(agent.Event{Type: "state", State: agent.StateThinking})
	model.handleAgentEvent(agent.Event{Type: "thinking", Text: "plan: "})
	model.handleAgentEvent(agent.Event{Type: "thinking", Text: "inspect file"})

	if len(model.messages) == 0 {
		t.Fatal("expected thinking message")
	}
	last := model.messages[len(model.messages)-1]
	if last.Type != MsgThink {
		t.Fatalf("expected thinking message, got %#v", last)
	}
	if last.Content != "plan: inspect file" {
		t.Fatalf("expected accumulated thinking content, got %q", last.Content)
	}
}
