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

type fakeStreamer struct {
	mu        sync.Mutex
	responses []func(chan<- llm.StreamEvent)
	calls     int
}

func (f *fakeStreamer) StreamChat(_ context.Context, _ []llm.Message, _ *tools.Registry, events chan<- llm.StreamEvent) {
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
				events <- llm.StreamEvent{
					Type:       "tool_result",
					ToolCallID: "call_1",
					ToolName:   "read",
					Text:       "hello from tool",
				}
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
	if streamer.calls != 1 {
		t.Fatalf("expected 1 streamer call (fantasy owns the loop), got %d", streamer.calls)
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

func TestSendMessageForwardsThinkingEvents(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.WorkDir = t.TempDir()
	cfg.SessionDir = t.TempDir()

	streamer := &fakeStreamer{
		responses: []func(chan<- llm.StreamEvent){
			func(events chan<- llm.StreamEvent) {
				events <- llm.StreamEvent{Type: "thinking", Text: "plan: "}
				events <- llm.StreamEvent{Type: "thinking", Text: "inspect file"}
				events <- llm.StreamEvent{Type: "text", Text: "done"}
				events <- llm.StreamEvent{Type: "done"}
			},
		},
	}

	agent := NewAgentWithStreamer(&cfg, streamer)
	eventCh := make(chan Event, 16)

	go agent.SendMessage(context.Background(), "hello", eventCh)

	var thinking string
	var text string
	for ev := range eventCh {
		switch ev.Type {
		case "thinking":
			thinking += ev.Text
		case "text":
			text += ev.Text
		case "error":
			t.Fatalf("unexpected error event: %v", ev.Error)
		}
	}

	if thinking != "plan: inspect file" {
		t.Fatalf("expected forwarded thinking text, got %q", thinking)
	}
	if text != "done" {
		t.Fatalf("expected final text, got %q", text)
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

	if got := agent.BootstrapMessageCount(); got != 7 {
		t.Fatalf("expected 7 bootstrap messages, got %d", got)
	}
	if got := agent.MessageCount(); got != 7 {
		t.Fatalf("expected bootstrap messages to be in history, got %d", got)
	}
}

func TestBuildSystemPromptMentionsSearchMemory(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.WorkDir = t.TempDir()

	reg := tools.NewRegistry(cfg.WorkDir, t.TempDir())
	prompt := buildSystemPrompt(&cfg, reg.Definitions())
	if !strings.Contains(prompt, "search_memory") {
		t.Fatalf("expected search_memory in system prompt, got %q", prompt)
	}
	if !strings.Contains(prompt, "prior decision") {
		t.Fatalf("expected recall guidance in system prompt, got %q", prompt)
	}
}

func TestSystemPromptIncludesLiveToolDefinitions(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.WorkDir = t.TempDir()
	cfg.SessionDir = t.TempDir()

	reg := tools.NewRegistry(cfg.WorkDir, cfg.SessionDir)
	prompt := buildSystemPrompt(&cfg, reg.Definitions())

	for _, def := range reg.Definitions() {
		if !strings.Contains(prompt, "- "+def.Function.Name+"(") {
			t.Fatalf("system prompt missing tool %q:\n%s", def.Function.Name, prompt)
		}
	}
	for _, want := range []string{
		"Tool schemas are attached to the provider request",
		"terminal_start, terminal_keys, terminal_wait",
		"Quit safely with keys",
		"preview_image(",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("system prompt missing %q:\n%s", want, prompt)
		}
	}
}

func TestRestoreMessagesRefreshesSystemPromptToolDefinitions(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.WorkDir = t.TempDir()
	cfg.SessionDir = t.TempDir()

	agent := NewAgentWithStreamer(&cfg, &fakeStreamer{})
	agent.RestoreMessages([]llm.Message{
		{Role: "system", Content: "old stale prompt"},
		{Role: "user", Content: "hello"},
	})

	prompt := agent.SystemPrompt()
	if strings.Contains(prompt, "old stale prompt") {
		t.Fatalf("expected restore to refresh stale system prompt, got %q", prompt)
	}
	if !strings.Contains(prompt, "terminal_start(") || !strings.Contains(prompt, "preview_image(") {
		t.Fatalf("restored system prompt missing live tools:\n%s", prompt)
	}
}

func TestMaybeQueueFollowUpStartsWithAutoNextSteps(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.WorkDir = t.TempDir()
	cfg.SessionDir = t.TempDir()
	cfg.AutoNextSteps = true
	cfg.AutoNextIdea = true

	agent := NewAgentWithStreamer(&cfg, &fakeStreamer{})
	agent.lastTurnState = turnStateCompleted

	followUp := agent.MaybeQueueFollowUp()
	if followUp == nil {
		t.Fatal("expected follow-up request")
	}
	if followUp.Label != "auto-next-steps" {
		t.Fatalf("expected auto-next-steps label, got %q", followUp.Label)
	}
	if followUp.Kind != followUpKindAutoNextSteps {
		t.Fatalf("expected auto-next-steps kind, got %v", followUp.Kind)
	}
	if !strings.Contains(followUp.Prompt, autoNextDoneToken) {
		t.Fatalf("expected auto-next done token in prompt, got %q", followUp.Prompt)
	}
}

func TestMaybeQueueFollowUpContinuesStepsUntilDone(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.WorkDir = t.TempDir()
	cfg.SessionDir = t.TempDir()
	cfg.AutoNextSteps = true

	agent := NewAgentWithStreamer(&cfg, &fakeStreamer{})
	agent.lastTurnState = turnStateCompleted
	agent.lastCompletedFollowUp = followUpKindAutoNextSteps
	agent.lastAssistantRaw = "Implemented the fix and still need to run go test."

	followUp := agent.MaybeQueueFollowUp()
	if followUp == nil {
		t.Fatal("expected follow-up request")
	}
	if followUp.Label != "auto-next-steps" {
		t.Fatalf("expected auto-next-steps label, got %q", followUp.Label)
	}
}

func TestMaybeQueueFollowUpSwitchesFromStepsToIdeaWhenDone(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.WorkDir = t.TempDir()
	cfg.SessionDir = t.TempDir()
	cfg.AutoNextSteps = true
	cfg.AutoNextIdea = true

	agent := NewAgentWithStreamer(&cfg, &fakeStreamer{})
	agent.lastTurnState = turnStateCompleted
	agent.lastCompletedFollowUp = followUpKindAutoNextSteps
	agent.lastAssistantRaw = autoNextDoneToken + ": tests are green and the task is complete."

	followUp := agent.MaybeQueueFollowUp()
	if followUp == nil {
		t.Fatal("expected follow-up request")
	}
	if followUp.Label != "auto-next-idea" {
		t.Fatalf("expected auto-next-idea label, got %q", followUp.Label)
	}
	if followUp.Kind != followUpKindAutoNextIdea {
		t.Fatalf("expected auto-next-idea kind, got %v", followUp.Kind)
	}
	if !strings.Contains(followUp.Prompt, autoIdeaDoneToken) {
		t.Fatalf("expected auto-idea done token in prompt, got %q", followUp.Prompt)
	}
}

func TestMaybeQueueFollowUpStopsWhenIdeaLoopIsDone(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.WorkDir = t.TempDir()
	cfg.SessionDir = t.TempDir()
	cfg.AutoNextIdea = true

	agent := NewAgentWithStreamer(&cfg, &fakeStreamer{})
	agent.lastTurnState = turnStateCompleted
	agent.lastCompletedFollowUp = followUpKindAutoNextIdea
	agent.lastAssistantRaw = autoIdeaDoneToken + ": nothing worthwhile remains."

	if followUp := agent.MaybeQueueFollowUp(); followUp != nil {
		t.Fatalf("expected no follow-up after completed idea loop, got %#v", followUp)
	}
}

func TestMaybeQueueFollowUpSkipsCanceledTurn(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.WorkDir = t.TempDir()
	cfg.SessionDir = t.TempDir()
	cfg.AutoNextSteps = true

	agent := NewAgentWithStreamer(&cfg, &fakeStreamer{})
	agent.lastTurnState = turnStateCanceled

	if followUp := agent.MaybeQueueFollowUp(); followUp != nil {
		t.Fatalf("expected no follow-up after canceled turn, got %#v", followUp)
	}
}

func TestSendMessageSplicesRealMessagesAndSanitizesAssistant(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.WorkDir = t.TempDir()
	cfg.SessionDir = t.TempDir()

	streamer := &fakeStreamer{
		responses: []func(chan<- llm.StreamEvent){
			func(events chan<- llm.StreamEvent) {
				events <- llm.StreamEvent{Type: "text", Text: autoNextDoneToken}
				events <- llm.StreamEvent{Type: "text", Text: ": shipped."}
				events <- llm.StreamEvent{
					Type: "done",
					Messages: []llm.Message{
						{Role: "assistant", Content: autoNextDoneToken + ": shipped.", ToolCalls: []llm.ToolCall{{ID: "c1", Type: "function", Function: llm.FunctionCall{Name: "read", Arguments: `{"path":"x"}`}}}},
						{Role: "tool", ToolCallID: "c1", Content: "file body"},
					},
				}
			},
		},
	}

	ag := NewAgentWithStreamer(&cfg, streamer)
	startLen := ag.MessageCount()

	eventCh := make(chan Event, 16)
	go ag.SendFollowUp(context.Background(), &FollowUpRequest{
		Label:  "auto-next-steps",
		Prompt: AutoNextPrompt(),
		Kind:   followUpKindAutoNextSteps,
	}, eventCh)
	for range eventCh {
	}

	// startLen + 1 (user prompt) + 2 (assistant + tool from ev.Messages) = startLen + 3
	if got := ag.MessageCount(); got != startLen+3 {
		t.Fatalf("expected %d messages, got %d", startLen+3, got)
	}
	msgs := ag.Messages()
	asst := msgs[len(msgs)-2]
	if asst.Role != "assistant" {
		t.Fatalf("expected assistant role at -2, got %q", asst.Role)
	}
	if strings.Contains(asst.Content, autoNextDoneToken) {
		t.Fatalf("assistant content not sanitized, got %q", asst.Content)
	}
	if asst.Content != "shipped." {
		t.Fatalf("expected sanitized 'shipped.', got %q", asst.Content)
	}
	if len(asst.ToolCalls) != 1 {
		t.Fatalf("expected ToolCalls preserved on assistant message, got %+v", asst.ToolCalls)
	}
	tool := msgs[len(msgs)-1]
	if tool.Role != "tool" || tool.ToolCallID != "c1" || tool.Content != "file body" {
		t.Fatalf("tool message not preserved: %+v", tool)
	}
	if got := ag.LastAssistantDisplayContent(); got != "shipped." {
		t.Fatalf("LastAssistantDisplayContent = %q", got)
	}
}

func TestSendMessageSanitizesAutonomyDoneTokens(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.WorkDir = t.TempDir()
	cfg.SessionDir = t.TempDir()

	streamer := &fakeStreamer{
		responses: []func(chan<- llm.StreamEvent){
			func(events chan<- llm.StreamEvent) {
				events <- llm.StreamEvent{Type: "text", Text: autoNextDoneToken}
				events <- llm.StreamEvent{Type: "text", Text: ": all tests passed."}
				events <- llm.StreamEvent{Type: "done"}
			},
		},
	}

	agent := NewAgentWithStreamer(&cfg, streamer)
	eventCh := make(chan Event, 16)

	go agent.SendFollowUp(context.Background(), &FollowUpRequest{
		Label:  "auto-next-steps",
		Prompt: AutoNextPrompt(),
		Kind:   followUpKindAutoNextSteps,
	}, eventCh)

	var gotText strings.Builder
	for ev := range eventCh {
		if ev.Type == "text" {
			gotText.WriteString(ev.Text)
		}
	}

	if strings.Contains(gotText.String(), autoNextDoneToken) {
		t.Fatalf("expected streamed text to hide done token, got %q", gotText.String())
	}
	if got := agent.LastAssistantDisplayContent(); got != "all tests passed." {
		t.Fatalf("expected sanitized assistant content, got %q", got)
	}
}
