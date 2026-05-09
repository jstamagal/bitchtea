package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"charm.land/fantasy"

	"github.com/jstamagal/bitchtea/internal/config"
	"github.com/jstamagal/bitchtea/internal/llm"
)

// TestPhase3BootstrapMessagesAreFantasyShape pins the bootstrap layout that
// NewAgentWithStreamer produces after the swap to []fantasy.Message: every
// bootstrap message carries one TextPart, the roles alternate
// system→(user/assistant)*, and bootstrapMsgCount indexes the fantasy slice
// directly.
func TestPhase3BootstrapMessagesAreFantasyShape(t *testing.T) {
	workDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(workDir, "AGENTS.md"), []byte("project rules"), 0644); err != nil {
		t.Fatalf("write AGENTS.md: %v", err)
	}

	cfg := config.DefaultConfig()
	cfg.WorkDir = workDir
	cfg.SessionDir = t.TempDir()

	a := NewAgentWithStreamer(&cfg, &fakeStreamer{})

	msgs := a.Messages()
	if len(msgs) == 0 {
		t.Fatal("expected bootstrap messages")
	}
	if got := a.BootstrapMessageCount(); got != len(msgs) {
		t.Fatalf("bootstrap count %d should equal len(messages) %d on a fresh agent", got, len(msgs))
	}

	// First message must be the system prompt with a single TextPart.
	if msgs[0].Role != fantasy.MessageRoleSystem {
		t.Fatalf("expected first message role system, got %q", msgs[0].Role)
	}
	if len(msgs[0].Content) != 1 {
		t.Fatalf("expected system message to have 1 part, got %d", len(msgs[0].Content))
	}
	if _, ok := msgs[0].Content[0].(fantasy.TextPart); !ok {
		t.Fatalf("expected system message part to be TextPart, got %T", msgs[0].Content[0])
	}

	// Every bootstrap message must have at least one TextPart with non-empty
	// text — no smuggled tool calls or empty messages in the bootstrap.
	for i, m := range msgs {
		if len(m.Content) == 0 {
			t.Fatalf("bootstrap message %d has no parts", i)
		}
		text := msgText(m)
		if text == "" {
			t.Fatalf("bootstrap message %d (role=%q) has empty text projection", i, m.Role)
		}
	}

	// Persona anchor: the last two bootstrap messages must be
	// user(personaAnchorReminder) → assistant(personaRehearsal). The reminder
	// points at the <persona> block in the system prompt instead of repeating
	// the entire personaPrompt — same anchor purpose, drops several KB of
	// duplicated bootstrap text per session.
	last := msgs[len(msgs)-1]
	prev := msgs[len(msgs)-2]
	if prev.Role != fantasy.MessageRoleUser || msgText(prev) != personaAnchorReminder {
		t.Fatalf("expected penultimate bootstrap message to be the persona-anchor reminder")
	}
	if last.Role != fantasy.MessageRoleAssistant || msgText(last) != personaRehearsal {
		t.Fatalf("expected final bootstrap message to be the persona-rehearsal assistant turn")
	}
}

// TestPhase3RestoreFromFantasySlice checks the new RestoreMessages contract:
// it accepts []fantasy.Message, refreshes the system prompt, and never drops
// non-text parts. Also verifies bootstrapMsgCount is reset (a restored
// session has no bootstrap window — every message is user-visible history).
func TestPhase3RestoreFromFantasySlice(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.WorkDir = t.TempDir()
	cfg.SessionDir = t.TempDir()

	a := NewAgentWithStreamer(&cfg, &fakeStreamer{})

	restored := []fantasy.Message{
		fantasyTextMessage("system", "stale prompt that must be replaced"),
		fantasyTextMessage("user", "what's the weather"),
		fantasyAssistantWithToolCall("checking", "call_w", "bash", `{"cmd":"weather"}`),
		fantasyToolResult("call_w", "sunny"),
		fantasyTextMessage("assistant", "It's sunny."),
	}
	a.RestoreMessages(restored)

	got := a.Messages()
	if len(got) != len(restored) {
		t.Fatalf("expected %d restored messages, got %d", len(restored), len(got))
	}

	// System prompt is always rebuilt from live tool definitions — must NOT
	// be the stale text we passed in.
	if msgText(got[0]) == "stale prompt that must be replaced" {
		t.Fatal("RestoreMessages must refresh the system prompt; stale text survived")
	}
	if !strings.Contains(msgText(got[0]), "<tool_rules>") {
		t.Fatal("restored system prompt missing <tool_rules> section")
	}

	// User text part survives.
	if got[1].Role != fantasy.MessageRoleUser || msgText(got[1]) != "what's the weather" {
		t.Fatalf("user message did not round-trip: %+v", got[1])
	}

	// Assistant tool call part survives — this is the load-bearing check
	// for the fantasy-native swap. Pre-Phase-3 the call traveled through
	// llm.ToolCall; now it must be a fantasy.ToolCallPart.
	calls := msgToolCalls(got[2])
	if len(calls) != 1 || calls[0].ID != "call_w" || calls[0].Function.Name != "bash" {
		t.Fatalf("assistant tool call not preserved: %+v", got[2])
	}

	// Tool result part survives.
	if msgToolCallID(got[3]) != "call_w" || msgText(got[3]) != "sunny" {
		t.Fatalf("tool result not preserved: %+v", got[3])
	}

	// After RestoreMessages, bootstrapMsgCount is 1 (covering the freshly
	// rebuilt system message at index 0) so prompt-cache marker placement
	// keeps working across resumed sessions. The previous value of 0
	// permanently disabled applyAnthropicCacheMarkers because
	// bootstrapPreparedIndex returned -1 for every subsequent turn.
	if a.BootstrapMessageCount() != 1 {
		t.Fatalf("expected bootstrap count 1 after restore (system message), got %d", a.BootstrapMessageCount())
	}
}

// TestPhase3ToolTranscriptSurvivesTurn drives one turn through the agent
// where the streamer emits text + a tool call + a tool result, then asserts
// all three survive in agent.Messages() as fantasy parts in order. This is
// the "tool transcript preservation" acceptance criterion.
func TestPhase3ToolTranscriptSurvivesTurn(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.WorkDir = t.TempDir()
	cfg.SessionDir = t.TempDir()

	streamer := &fakeStreamer{
		responses: []func(chan<- llm.StreamEvent){
			func(events chan<- llm.StreamEvent) {
				events <- llm.StreamEvent{Type: "text", Text: "looking"}
				events <- llm.StreamEvent{Type: "tool_call", ToolCallID: "c_a", ToolName: "read", ToolArgs: `{"path":"x"}`}
				events <- llm.StreamEvent{Type: "tool_result", ToolCallID: "c_a", ToolName: "read", Text: "file body"}
				events <- llm.StreamEvent{Type: "text", Text: " done."}
				events <- llm.StreamEvent{
					Type: "done",
					Messages: []llm.Message{
						{Role: "assistant", Content: "looking", ToolCalls: []llm.ToolCall{{ID: "c_a", Type: "function", Function: llm.FunctionCall{Name: "read", Arguments: `{"path":"x"}`}}}},
						{Role: "tool", ToolCallID: "c_a", Content: "file body"},
						{Role: "assistant", Content: " done."},
					},
				}
			},
		},
	}

	a := NewAgentWithStreamer(&cfg, streamer)
	startLen := a.MessageCount()

	eventCh := make(chan Event, 32)
	go a.SendMessage(context.Background(), "read x", eventCh)
	for range eventCh {
	}

	msgs := a.Messages()
	tail := msgs[startLen:]
	// user prompt + 3 spliced ev.Messages = 4 trailing messages
	if len(tail) != 4 {
		t.Fatalf("expected 4 trailing messages (user, assistant+toolcall, tool, assistant), got %d", len(tail))
	}

	if tail[0].Role != fantasy.MessageRoleUser || !strings.Contains(msgText(tail[0]), "read x") {
		t.Fatalf("first trailing message must be the user prompt: %+v", tail[0])
	}

	asst := tail[1]
	if asst.Role != fantasy.MessageRoleAssistant {
		t.Fatalf("expected assistant message at tail[1], got role %q", asst.Role)
	}
	if !strings.Contains(msgText(asst), "looking") {
		t.Fatalf("assistant text part missing, got %q", msgText(asst))
	}
	calls := msgToolCalls(asst)
	if len(calls) != 1 || calls[0].ID != "c_a" || calls[0].Function.Name != "read" {
		t.Fatalf("assistant tool call part missing or wrong: %+v", asst)
	}

	tool := tail[2]
	if tool.Role != fantasy.MessageRoleTool || msgToolCallID(tool) != "c_a" || msgText(tool) != "file body" {
		t.Fatalf("tool result message not preserved: %+v", tool)
	}

	final := tail[3]
	if final.Role != fantasy.MessageRoleAssistant || !strings.Contains(msgText(final), "done") {
		t.Fatalf("final assistant text not preserved: %+v", final)
	}
}

// TestPhase3FollowUpSanitizationStripsDoneTokenInFantasyParts pins the
// follow-up sanitizer behavior on the new fantasy.Message slice: when the
// streamer returns an assistant message whose text begins with the
// auto-next done token, the spliced fantasy.TextPart must end up sanitized
// (not raw), and the tool-call part on the same message survives.
func TestPhase3FollowUpSanitizationStripsDoneTokenInFantasyParts(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.WorkDir = t.TempDir()
	cfg.SessionDir = t.TempDir()

	streamer := &fakeStreamer{
		responses: []func(chan<- llm.StreamEvent){
			func(events chan<- llm.StreamEvent) {
				events <- llm.StreamEvent{Type: "text", Text: autoNextDoneToken + ": shipped."}
				events <- llm.StreamEvent{
					Type: "done",
					Messages: []llm.Message{
						{Role: "assistant", Content: autoNextDoneToken + ": shipped.", ToolCalls: []llm.ToolCall{{ID: "c1", Type: "function", Function: llm.FunctionCall{Name: "read", Arguments: `{"path":"y"}`}}}},
					},
				}
			},
		},
	}

	a := NewAgentWithStreamer(&cfg, streamer)
	startLen := a.MessageCount()

	eventCh := make(chan Event, 16)
	go a.SendFollowUp(context.Background(), &FollowUpRequest{
		Label:  "auto-next-steps",
		Prompt: AutoNextPrompt(),
		Kind:   followUpKindAutoNextSteps,
	}, eventCh)
	for range eventCh {
	}

	msgs := a.Messages()
	// bt-p6i: the assistant message ends with an unanswered tool_use, so
	// the sanitizer splices a synthetic tool message right after to keep
	// the next API call valid. Expect user + assistant + synthetic tool.
	if got := len(msgs); got != startLen+3 {
		t.Fatalf("expected %d messages (user+assistant+synthetic-tool), got %d", startLen+3, got)
	}

	asst := msgs[len(msgs)-2]
	if asst.Role != fantasy.MessageRoleAssistant {
		t.Fatalf("expected penultimate message to be assistant, got %q", asst.Role)
	}
	if synth := msgs[len(msgs)-1]; synth.Role != fantasy.MessageRoleTool {
		t.Fatalf("expected last message to be synthetic tool result, got role=%q", synth.Role)
	}
	text := msgText(asst)
	if strings.Contains(text, autoNextDoneToken) {
		t.Fatalf("assistant text part not sanitized, still contains done token: %q", text)
	}
	if text != "shipped." {
		t.Fatalf("expected sanitized text 'shipped.', got %q", text)
	}
	calls := msgToolCalls(asst)
	if len(calls) != 1 || calls[0].ID != "c1" {
		t.Fatalf("tool call part lost during sanitization: %+v", asst)
	}
}

// TestPhase3CompactionFlushesFantasyMessagesToMemory exercises the
// compaction-to-memory path on the fantasy-native slice: the agent owns a
// 10-message conversation, Compact() pulls out everything except the last
// 4, projects each message's text via msgText, and writes the streamed
// "extract durable memory" output to the daily memory file.
func TestPhase3CompactionFlushesFantasyMessagesToMemory(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.WorkDir = t.TempDir()
	cfg.SessionDir = t.TempDir()

	streamer := &fakeStreamer{
		responses: []func(chan<- llm.StreamEvent){
			// First call: extract durable memory.
			func(events chan<- llm.StreamEvent) {
				events <- llm.StreamEvent{Type: "text", Text: "- decision: phase 3 swapped agent.messages to fantasy.Message"}
				events <- llm.StreamEvent{Type: "done"}
			},
			// Second call: write the compacted summary.
			func(events chan<- llm.StreamEvent) {
				events <- llm.StreamEvent{Type: "text", Text: "Summary written."}
				events <- llm.StreamEvent{Type: "done"}
			},
		},
	}

	a := NewAgentWithStreamer(&cfg, streamer)
	a.bootstrapMsgCount = 1
	a.messages = makeMessages(10)

	if err := a.Compact(context.Background()); err != nil {
		t.Fatalf("Compact returned error: %v", err)
	}

	// Compaction shape: system + summary + last 4 = 6 fantasy messages.
	if len(a.messages) != 6 {
		t.Fatalf("expected 6 messages after compact, got %d", len(a.messages))
	}
	for i, m := range a.messages {
		if len(m.Content) == 0 {
			t.Fatalf("post-compact message %d has zero parts", i)
		}
	}
	if !strings.Contains(msgText(a.messages[1]), "Summary written.") {
		t.Fatalf("post-compact summary message missing streamer text: %q", msgText(a.messages[1]))
	}
}
