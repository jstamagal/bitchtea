package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"charm.land/fantasy"

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

func TestSendMessageExitsOnContextCancel(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.WorkDir = t.TempDir()
	cfg.SessionDir = t.TempDir()

	// hangingStreamer sends a tool_call then blocks forever on a channel
	// that is never closed — simulating a tool that hangs (e.g., a long
	// bash command or a terminal_wait that never matches). The only way
	// for sendMessage to exit is via ctx.Done() in the select loop.
	hang := make(chan struct{})
	t.Cleanup(func() { close(hang) }) // prevent goroutine leak in test

	streamer := &fakeStreamer{
		responses: []func(chan<- llm.StreamEvent){
			func(events chan<- llm.StreamEvent) {
				events <- llm.StreamEvent{
					Type:       "tool_call",
					ToolCallID: "call_1",
					ToolName:   "bash",
					ToolArgs:   `{"command":"sleep 100"}`,
				}
				// Block forever — streamEvents is never closed by us.
				<-hang
			},
		},
	}

	ag := NewAgentWithStreamer(&cfg, streamer)
	eventCh := make(chan Event, 32)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		ag.SendMessage(ctx, "run a slow command", eventCh)
		close(done)
	}()

	// Let the streamer start and send the tool_call.
	time.Sleep(50 * time.Millisecond)

	// Cancel the context — this is the ctrl+c equivalent.
	cancel()

	// sendMessage must return promptly. If it's stuck on `for ev := range
	// streamEvents` without watching ctx.Done(), this timeout fires.
	select {
	case <-done:
		// sendMessage returned — the fix works.
	case <-time.After(2 * time.Second):
		t.Fatal("sendMessage did not return after context cancellation; goroutine likely stuck on streamEvents")
	}

	// Drain events and verify the expected error+done sequence.
	var gotState, gotError, gotDone bool
	for ev := range eventCh {
		switch ev.Type {
		case "state":
			if ev.State == StateIdle {
				gotState = true
			}
		case "error":
			gotError = true
			if ev.Error != context.Canceled {
				t.Fatalf("expected context.Canceled, got %v", ev.Error)
			}
		case "done":
			gotDone = true
		}
	}

	if !gotState {
		t.Fatal("expected state:idle event")
	}
	if !gotError {
		t.Fatal("expected error event with context.Canceled")
	}
	if !gotDone {
		t.Fatal("expected done event")
	}
	if ag.lastTurnState != turnStateCanceled {
		t.Fatalf("expected turnStateCanceled, got %d", ag.lastTurnState)
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
	for _, want := range []string{
		"search_memory",
		"write_memory",
		"prior decision",
		"MEMORY WORKFLOW",
		"scope='root'",
		"daily=true",
		"Consolidate",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("expected %q in system prompt, got %q", want, prompt)
		}
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
	agent.RestoreMessages([]fantasy.Message{
		fantasyTextMessage("system", "old stale prompt"),
		fantasyTextMessage("user", "hello"),
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
	if asst.Role != fantasy.MessageRoleAssistant {
		t.Fatalf("expected assistant role at -2, got %q", asst.Role)
	}
	asstText := msgText(asst)
	if strings.Contains(asstText, autoNextDoneToken) {
		t.Fatalf("assistant content not sanitized, got %q", asstText)
	}
	if asstText != "shipped." {
		t.Fatalf("expected sanitized 'shipped.', got %q", asstText)
	}
	asstCalls := msgToolCalls(asst)
	if len(asstCalls) != 1 {
		t.Fatalf("expected ToolCalls preserved on assistant message, got %+v", asstCalls)
	}
	tool := msgs[len(msgs)-1]
	if tool.Role != fantasy.MessageRoleTool || msgToolCallID(tool) != "c1" || msgText(tool) != "file body" {
		t.Fatalf("tool message not preserved: role=%q tool_call_id=%q text=%q", tool.Role, msgToolCallID(tool), msgText(tool))
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

func TestSetScopeRootDoesNotDoubleInjectBootstrapMemory(t *testing.T) {
	workDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(workDir, "MEMORY.md"), []byte("session notes"), 0644); err != nil {
		t.Fatalf("write MEMORY.md: %v", err)
	}

	cfg := config.DefaultConfig()
	cfg.WorkDir = workDir
	cfg.SessionDir = t.TempDir()

	agent := NewAgentWithStreamer(&cfg, &fakeStreamer{})

	// Bootstrap should have injected MEMORY.md once. Count the messages now.
	beforeCount := agent.MessageCount()

	// SetScope(root) should NOT add more messages since the root hot path
	// was already tracked in injectedPaths at bootstrap.
	agent.SetScope(RootMemoryScope())

	afterCount := agent.MessageCount()
	if afterCount != beforeCount {
		t.Fatalf("SetScope(root) added messages: before=%d after=%d — double-injection not prevented", beforeCount, afterCount)
	}
}

// TestResumeOverwritesPreInjectedMemory reproduces the bt-wire.10 bug:
// SetScope marks a HOT path as injected, then RestoreMessages replaces the
// message slice — the injected messages are lost but the marker stays,
// preventing future re-injection.
//
// This test is skipped until bt-wire.10 is fixed (RestoreMessages must reset
// injectedPaths or SetScope must check whether the injected messages are still
// present rather than relying on the boolean markers).
func TestResumeOverwritesPreInjectedMemory(t *testing.T) {
	t.Skip("bt-wire.10: pre-resume scoped injection markers are not cleared on RestoreMessages, " +
		"so HOT.md content is lost after resume and never re-injected")

	workDir := t.TempDir()
	sessionDir := t.TempDir()

	// Pre-populate a scoped HOT.md with known content.
	hotContent := "# Channel X\n- Important context for tests\n"
	scope := ChannelMemoryScope("x-channel", nil)
	if err := SaveScopedMemory(sessionDir, workDir, scope, hotContent); err != nil {
		t.Fatalf("save scoped memory: %v", err)
	}

	cfg := config.DefaultConfig()
	cfg.WorkDir = workDir
	cfg.SessionDir = sessionDir

	agent := NewAgentWithStreamer(&cfg, &fakeStreamer{})

	// Step 1: SetScope injects the HOT.md content.
	agent.SetScope(scope)

	// Verify the HOT.md content is present in messages.
	found := false
	for i := len(agent.messages) - 1; i >= 0; i-- {
		if strings.Contains(msgText(agent.messages[i]), "Important context for tests") {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("step 1 failed: HOT.md content was not injected by SetScope")
	}

	// Step 2: Simulate a resume — RestoreMessages replaces the entire slice.
	minimal := []fantasy.Message{
		fantasyTextMessage("user", "hello after resume"),
	}
	agent.RestoreMessages(minimal)

	// Verify the HOT.md content is GONE from messages.
	for _, m := range agent.messages {
		if strings.Contains(msgText(m), "Important context for tests") {
			t.Fatal("step 2 failed: HOT.md content still in messages after RestoreMessages (should have been replaced)")
		}
	}

	// Step 3: After resume, SetScope should re-inject the scoped HOT.md.
	// This is what bt-wire.10 fixes — currently injectedPaths still has the
	// marker, so this call is a no-op. After the fix, this should inject.
	agent.SetScope(scope)

	// After the fix, this should pass: HOT.md content should be re-injected.
	for _, m := range agent.messages {
		if strings.Contains(msgText(m), "Important context for tests") {
			return // PASS: content was re-injected after resume
		}
	}
	t.Error("bt-wire.10 bug: HOT.md content was NOT re-injected after RestoreMessages because injectedPaths was not reset")
}

func TestCompactBelowThresholdIsNoOp(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.WorkDir = t.TempDir()
	cfg.SessionDir = t.TempDir()

	agent := NewAgentWithStreamer(&cfg, &fakeStreamer{})

	// Build a history exactly 5 messages long — below the 6-message threshold.
	// Compact should return nil and not touch the messages slice.
	for i := 0; i < 5; i++ {
		agent.messages = append(agent.messages, fantasyTextMessage("user", fmt.Sprintf("msg-%d", i)))
	}
	before := len(agent.messages)

	if err := agent.Compact(context.Background()); err != nil {
		t.Fatalf("Compact below threshold: %v", err)
	}
	if len(agent.messages) != before {
		t.Fatalf("Compact should not modify messages below threshold: before=%d after=%d", before, len(agent.messages))
	}
}

func TestCompactExactlySixMessagesNoOpWhenBootstrapOverlapsEnd(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.WorkDir = t.TempDir()
	cfg.SessionDir = t.TempDir()

	agent := NewAgentWithStreamer(&cfg, &fakeStreamer{})

	// A fresh agent has 7 bootstrap messages. With exactly 6 messages total,
	// end = 6 - 4 = 2 and bootstrapEnd = 7, so bootstrapEnd > end → no-op.
	// This tests the boundary: 6 is the hard threshold, but bootstrap
	// messages still prevent compaction.
	for len(agent.messages) < 6 {
		agent.messages = append(agent.messages, fantasyTextMessage("user", "msg"))
	}
	before := len(agent.messages)

	if err := agent.Compact(context.Background()); err != nil {
		t.Fatalf("Compact at boundary: %v", err)
	}
	if len(agent.messages) != before {
		t.Fatalf("Compact should be no-op when bootstrapEnd > end: before=%d after=%d", before, len(agent.messages))
	}
}
