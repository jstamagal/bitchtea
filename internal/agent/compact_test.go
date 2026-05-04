package agent

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"charm.land/fantasy"

	"github.com/jstamagal/bitchtea/internal/config"
	"github.com/jstamagal/bitchtea/internal/llm"
	"github.com/jstamagal/bitchtea/internal/tools"
)

// helpers ----------------------------------------------------------------

// makeMessages builds a slice of n fantasy messages with a system prompt at [0].
func makeMessages(n int) []fantasy.Message {
	msgs := make([]fantasy.Message, 0, n)
	msgs = append(msgs, fantasyTextMessage("system", "You are a helpful assistant."))
	for i := 1; i < n; i++ {
		role := "user"
		if i%2 == 0 {
			role = "assistant"
		}
		msgs = append(msgs, fantasyTextMessage(role, "message "+strings.Repeat("x", i)))
	}
	return msgs
}

// summaryStreamer is a fakeStreamer that returns a fixed summary text.
func summaryStreamer(summary string) *fakeStreamer {
	return &fakeStreamer{
		responses: []func(chan<- llm.StreamEvent){
			func(events chan<- llm.StreamEvent) {
				events <- llm.StreamEvent{Type: "text", Text: "NONE"}
				events <- llm.StreamEvent{Type: "done"}
			},
			func(events chan<- llm.StreamEvent) {
				events <- llm.StreamEvent{Type: "text", Text: summary}
				events <- llm.StreamEvent{Type: "done"}
			},
		},
	}
}

func newTestAgent(t *testing.T, streamer *fakeStreamer) *Agent {
	t.Helper()
	cfg := config.DefaultConfig()
	cfg.WorkDir = t.TempDir()
	cfg.SessionDir = t.TempDir()
	a := NewAgentWithStreamer(&cfg, streamer)
	a.bootstrapMsgCount = 1
	a.contextMsgs[a.currentContext] = a.messages[:1]
	return a
}

// tests ------------------------------------------------------------------

func TestCompactNoOpWhenFewerThanSixMessages(t *testing.T) {
	streamer := summaryStreamer("should never appear")
	agent := newTestAgent(t, streamer)

	// 5 messages: system + 4 others → fewer than 6, compact should no-op.
	agent.messages = makeMessages(5)
	origLen := len(agent.messages)

	if err := agent.Compact(context.Background()); err != nil {
		t.Fatalf("Compact returned error: %v", err)
	}

	if len(agent.messages) != origLen {
		t.Fatalf("expected %d messages unchanged, got %d", origLen, len(agent.messages))
	}

	// Streamer should NOT have been called.
	if streamer.calls != 0 {
		t.Fatalf("expected 0 streamer calls, got %d", streamer.calls)
	}
}

func TestCompactNoOpExactlySixMessages(t *testing.T) {
	// Boundary: exactly 6 messages should still compact (6 >= 6).
	streamer := summaryStreamer("summary")
	agent := newTestAgent(t, streamer)
	agent.messages = makeMessages(6)

	if err := agent.Compact(context.Background()); err != nil {
		t.Fatalf("Compact returned error: %v", err)
	}

	// With 6 messages, compaction fires.
	if streamer.calls != 2 {
		t.Fatalf("expected 2 streamer calls, got %d", streamer.calls)
	}
}

func TestCompact_fewMessagesIsNoOp(t *testing.T) {
	streamer := summaryStreamer("unused")
	agent := newTestAgent(t, streamer)
	agent.messages = makeMessages(5)
	agent.SetSavedIdx(DefaultContextKey, len(agent.messages))

	before := append([]fantasy.Message(nil), agent.messages...)

	if err := agent.Compact(context.Background()); err != nil {
		t.Fatalf("Compact returned error: %v", err)
	}

	if streamer.calls != 0 {
		t.Fatalf("expected 0 streamer calls, got %d", streamer.calls)
	}
	if len(agent.messages) != len(before) {
		t.Fatalf("expected %d messages unchanged, got %d", len(before), len(agent.messages))
	}
	for i := range before {
		if agent.messages[i].Role != before[i].Role || msgText(agent.messages[i]) != msgText(before[i]) {
			t.Fatalf("message %d changed: got {%s,%q}, want {%s,%q}",
				i, agent.messages[i].Role, msgText(agent.messages[i]), before[i].Role, msgText(before[i]))
		}
	}
	if got := agent.SavedIdx(DefaultContextKey); got != len(before) {
		t.Fatalf("expected saved idx unchanged at %d, got %d", len(before), got)
	}
}

func TestCompact_noneResponseSkipsMemory(t *testing.T) {
	streamer := summaryStreamer("summary still happens")
	agent := newTestAgent(t, streamer)
	agent.messages = makeMessages(8)

	if err := agent.Compact(context.Background()); err != nil {
		t.Fatalf("Compact returned error: %v", err)
	}

	if streamer.calls != 2 {
		t.Fatalf("expected 2 streamer calls, got %d", streamer.calls)
	}
	path := DailyMemoryPath(agent.config.SessionDir, agent.config.WorkDir, time.Now())
	if data, err := os.ReadFile(path); !os.IsNotExist(err) {
		t.Fatalf("expected no daily memory file for NONE response, err=%v data=%q", err, string(data))
	}
	if got := msgText(agent.messages[1]); !strings.Contains(got, "summary still happens") {
		t.Fatalf("expected compact summary to be written to history, got %q", got)
	}
}

func TestCompact_streamerErrorPropagates(t *testing.T) {
	wantErr := errors.New("summary failed")
	streamer := &fakeStreamer{
		responses: []func(chan<- llm.StreamEvent){
			func(events chan<- llm.StreamEvent) {
				events <- llm.StreamEvent{Type: "text", Text: "NONE"}
				events <- llm.StreamEvent{Type: "done"}
			},
			func(events chan<- llm.StreamEvent) {
				events <- llm.StreamEvent{Type: "error", Error: wantErr}
			},
		},
	}
	agent := newTestAgent(t, streamer)
	agent.messages = makeMessages(8)
	before := append([]fantasy.Message(nil), agent.messages...)

	err := agent.Compact(context.Background())
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected %v, got %v", wantErr, err)
	}
	if len(agent.messages) != len(before) {
		t.Fatalf("expected messages unchanged after error, got %d want %d", len(agent.messages), len(before))
	}
	if streamer.calls != 2 {
		t.Fatalf("expected 2 streamer calls, got %d", streamer.calls)
	}
}

func TestCompact_bootstrapBoundaryRespected(t *testing.T) {
	streamer := summaryStreamer("middle compacted")
	agent := newTestAgent(t, streamer)
	agent.messages = []fantasy.Message{
		fantasyTextMessage("system", "system prompt"),
		fantasyTextMessage("user", "bootstrap context"),
		fantasyTextMessage("assistant", "bootstrap ack"),
		fantasyTextMessage("user", "old user detail"),
		fantasyTextMessage("assistant", "old assistant detail"),
		fantasyTextMessage("user", "recent one"),
		fantasyTextMessage("assistant", "recent two"),
		fantasyTextMessage("user", "recent three"),
		fantasyTextMessage("assistant", "recent four"),
	}
	agent.bootstrapMsgCount = 3
	bootstrap := append([]fantasy.Message(nil), agent.messages[:agent.bootstrapMsgCount]...)

	if err := agent.Compact(context.Background()); err != nil {
		t.Fatalf("Compact returned error: %v", err)
	}

	for i, want := range bootstrap {
		got := agent.messages[i]
		if got.Role != want.Role || msgText(got) != msgText(want) {
			t.Fatalf("bootstrap message %d changed: got {%s,%q}, want {%s,%q}",
				i, got.Role, msgText(got), want.Role, msgText(want))
		}
	}
	if got := msgText(agent.messages[agent.bootstrapMsgCount]); !strings.Contains(got, "middle compacted") {
		t.Fatalf("expected summary after bootstrap prefix, got %q", got)
	}
}

func TestCompact_rebuildsAsSystemSummaryLast4(t *testing.T) {
	const summaryText = "compact summary"
	streamer := summaryStreamer(summaryText)
	agent := newTestAgent(t, streamer)
	msgs := makeMessages(10)
	agent.messages = msgs
	last4 := append([]fantasy.Message(nil), msgs[len(msgs)-4:]...)

	if err := agent.Compact(context.Background()); err != nil {
		t.Fatalf("Compact returned error: %v", err)
	}

	if len(agent.messages) != 6 {
		t.Fatalf("expected system + summary + last 4 = 6 messages, got %d", len(agent.messages))
	}
	if agent.messages[0].Role != fantasy.MessageRoleSystem {
		t.Fatalf("expected system at index 0, got %q", agent.messages[0].Role)
	}
	if agent.messages[1].Role != fantasy.MessageRoleUser {
		t.Fatalf("expected summary user message at index 1, got %q", agent.messages[1].Role)
	}
	if got := msgText(agent.messages[1]); !strings.Contains(got, summaryText) {
		t.Fatalf("summary missing text %q: got %q", summaryText, got)
	}
	for i, want := range last4 {
		got := agent.messages[2+i]
		if got.Role != want.Role || msgText(got) != msgText(want) {
			t.Fatalf("last4 message %d changed: got {%s,%q}, want {%s,%q}",
				i, got.Role, msgText(got), want.Role, msgText(want))
		}
	}
}

func TestCompact_savedIdxAfter(t *testing.T) {
	streamer := summaryStreamer("summary")
	agent := newTestAgent(t, streamer)
	agent.messages = makeMessages(10)
	agent.SetSavedIdx(DefaultContextKey, len(agent.messages))

	if err := agent.Compact(context.Background()); err != nil {
		t.Fatalf("Compact returned error: %v", err)
	}

	if got, want := agent.SavedIdx(DefaultContextKey), len(agent.messages); got != want {
		t.Fatalf("expected saved idx reconciled to compacted length %d, got %d", want, got)
	}
}

func TestCompactRetainsSystemPromptAndLastFour(t *testing.T) {
	const summaryText = "This is the conversation summary."
	streamer := summaryStreamer(summaryText)
	agent := newTestAgent(t, streamer)

	msgs := makeMessages(10) // system + 9 others
	agent.messages = msgs

	// Save the last 4 messages before compaction.
	last4 := make([]fantasy.Message, 4)
	copy(last4, msgs[len(msgs)-4:])
	origSystem := msgText(msgs[0])

	if err := agent.Compact(context.Background()); err != nil {
		t.Fatalf("Compact returned error: %v", err)
	}

	// Structure: system + summary_msg + last4 = 6
	if len(agent.messages) != 6 {
		t.Fatalf("expected 6 messages after compact, got %d", len(agent.messages))
	}

	// [0] must be the original system prompt.
	if msgText(agent.messages[0]) != origSystem {
		t.Fatalf("system prompt changed after compact")
	}

	// [1] must contain the summary.
	if !strings.Contains(msgText(agent.messages[1]), summaryText) {
		t.Fatalf("summary message missing summary text; got %q", msgText(agent.messages[1]))
	}

	// [2..5] must match the original last 4 messages.
	for i := 0; i < 4; i++ {
		got := agent.messages[2+i]
		want := last4[i]
		if got.Role != want.Role || msgText(got) != msgText(want) {
			t.Fatalf("message at index %d differs: got {%s,%q}, want {%s,%q}",
				2+i, got.Role, msgText(got), want.Role, msgText(want))
		}
	}
}

func TestCompactSummaryInsertedProperly(t *testing.T) {
	const summaryText = "User asked about Go testing patterns. We discussed table-driven tests."
	streamer := summaryStreamer(summaryText)
	agent := newTestAgent(t, streamer)
	agent.messages = makeMessages(8)

	if err := agent.Compact(context.Background()); err != nil {
		t.Fatalf("Compact returned error: %v", err)
	}

	// The summary message should be at index 1 with the correct prefix.
	sumMsg := agent.messages[1]
	if sumMsg.Role != fantasy.MessageRoleUser {
		t.Fatalf("expected summary message role 'user', got %q", sumMsg.Role)
	}
	sumText := msgText(sumMsg)
	if !strings.HasPrefix(sumText, "[Previous conversation summary]:") {
		t.Fatalf("summary message missing prefix; got %q", sumText)
	}
	if !strings.Contains(sumText, summaryText) {
		t.Fatalf("summary message missing streamer output; got %q", sumText)
	}
}

func TestCompactFlushesDailyMemoryBeforeSummaryRewrite(t *testing.T) {
	workDir := t.TempDir()
	sessionDir := t.TempDir()

	cfg := config.DefaultConfig()
	cfg.WorkDir = workDir
	cfg.SessionDir = sessionDir

	streamer := &fakeStreamer{
		responses: []func(chan<- llm.StreamEvent){
			func(events chan<- llm.StreamEvent) {
				events <- llm.StreamEvent{Type: "text", Text: "- Keep IRC routing semantics\n- Persist channel summaries daily"}
				events <- llm.StreamEvent{Type: "done"}
			},
			func(events chan<- llm.StreamEvent) {
				events <- llm.StreamEvent{Type: "text", Text: "Conversation compacted cleanly."}
				events <- llm.StreamEvent{Type: "done"}
			},
		},
	}

	agent := NewAgentWithStreamer(&cfg, streamer)
	agent.bootstrapMsgCount = 1
	agent.messages = makeMessages(8)

	if err := agent.Compact(context.Background()); err != nil {
		t.Fatalf("Compact returned error: %v", err)
	}

	if streamer.calls != 2 {
		t.Fatalf("expected 2 streamer calls, got %d", streamer.calls)
	}

	path := DailyMemoryPath(sessionDir, workDir, time.Now())
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read daily memory file: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "Keep IRC routing semantics") {
		t.Fatalf("expected durable memory flush content, got %q", content)
	}
	if got := msgText(agent.messages[1]); !strings.Contains(got, "Conversation compacted cleanly.") {
		t.Fatalf("expected compacted summary in message history, got %q", got)
	}
}

func TestCompactPreservesToolMetadata(t *testing.T) {
	streamer := summaryStreamer("tool context preserved")
	agent := newTestAgent(t, streamer)

	// Build 10 messages where the last 4 include tool metadata.
	msgs := makeMessages(10)

	// Modify message at index 7 (within last 4: indices 6,7,8,9) to have tool calls.
	msgs[7] = fantasyAssistantWithToolCall("", "call_abc", "read", `{"path":"main.go"}`)
	// Modify message at index 8 to be a tool result.
	msgs[8] = fantasyToolResult("call_abc", "package main\n\nfunc main() {}")

	agent.messages = msgs

	if err := agent.Compact(context.Background()); err != nil {
		t.Fatalf("Compact returned error: %v", err)
	}

	// After compaction: system + summary + last4 = 6
	if len(agent.messages) != 6 {
		t.Fatalf("expected 6 messages, got %d", len(agent.messages))
	}

	// The tool call message should be at index 3 (2 + offset 1 within last4).
	toolMsg := agent.messages[3]
	calls := msgToolCalls(toolMsg)
	if len(calls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(calls))
	}
	if calls[0].ID != "call_abc" {
		t.Fatalf("tool call ID not preserved: got %q", calls[0].ID)
	}
	if calls[0].Function.Name != "read" {
		t.Fatalf("tool call function name not preserved: got %q", calls[0].Function.Name)
	}

	// The tool result message should be at index 4.
	resultMsg := agent.messages[4]
	if msgToolCallID(resultMsg) != "call_abc" {
		t.Fatalf("tool result ToolCallID not preserved: got %q", msgToolCallID(resultMsg))
	}
	if resultMsg.Role != fantasy.MessageRoleTool {
		t.Fatalf("tool result role not preserved: got %q", resultMsg.Role)
	}
}

func TestCompactFlushesDailyMemoryToScopedPath(t *testing.T) {
	workDir := t.TempDir()
	sessionDir := t.TempDir()

	cfg := config.DefaultConfig()
	cfg.WorkDir = workDir
	cfg.SessionDir = sessionDir

	streamer := &fakeStreamer{
		responses: []func(chan<- llm.StreamEvent){
			func(events chan<- llm.StreamEvent) {
				events <- llm.StreamEvent{Type: "text", Text: "- Channel-specific decision A"}
				events <- llm.StreamEvent{Type: "done"}
			},
			func(events chan<- llm.StreamEvent) {
				events <- llm.StreamEvent{Type: "text", Text: "Scoped compaction done."}
				events <- llm.StreamEvent{Type: "done"}
			},
		},
	}

	ag := NewAgentWithStreamer(&cfg, streamer)

	// Set a channel scope before compacting.
	ag.SetScope(ChannelMemoryScope("engineering", nil))
	ag.bootstrapMsgCount = 1
	ag.messages = makeMessages(8)

	if err := ag.Compact(context.Background()); err != nil {
		t.Fatalf("Compact returned error: %v", err)
	}

	// The daily memory should be written to the scoped path, not the root path.
	rootDailyPath := DailyMemoryPath(sessionDir, workDir, time.Now())
	_, rootErr := os.ReadFile(rootDailyPath)

	scopedDailyPath := ScopedDailyMemoryPath(sessionDir, workDir, ChannelMemoryScope("engineering", nil), time.Now())
	scopedData, scopedErr := os.ReadFile(scopedDailyPath)

	if !os.IsNotExist(rootErr) {
		t.Fatalf("expected root daily path to not exist, got: %v (rootErr=%v)", string(func() []byte {
			d, _ := os.ReadFile(rootDailyPath)
			return d
		}()), rootErr)
	}
	if scopedErr != nil {
		t.Fatalf("expected scoped daily file to exist: %v", scopedErr)
	}
	if !strings.Contains(string(scopedData), "Channel-specific decision A") {
		t.Fatalf("expected scoped daily content, got %q", string(scopedData))
	}
}

func TestSetScopeInjectsHotMemoryOnce(t *testing.T) {
	workDir := t.TempDir()
	sessionDir := t.TempDir()

	cfg := config.DefaultConfig()
	cfg.WorkDir = workDir
	cfg.SessionDir = sessionDir

	scope := ChannelMemoryScope("devops", nil)

	// Pre-populate scoped HOT.md.
	if err := SaveScopedMemory(sessionDir, workDir, scope, "# DevOps\n- Use terraform for infra\n"); err != nil {
		t.Fatalf("save scoped memory: %v", err)
	}

	ag := NewAgentWithStreamer(&cfg, &fakeStreamer{})
	baseCount := len(ag.messages)

	// First SetScope call should inject HOT.md.
	ag.SetScope(scope)
	if len(ag.messages) != baseCount+2 {
		t.Fatalf("expected 2 injected messages after SetScope, got %d (base=%d)", len(ag.messages)-baseCount, baseCount)
	}
	if got := msgText(ag.messages[baseCount]); !strings.Contains(got, "terraform for infra") {
		t.Fatalf("expected HOT.md content injected, got %q", got)
	}

	// Second SetScope call with same scope should NOT reinject.
	ag.SetScope(scope)
	if len(ag.messages) != baseCount+2 {
		t.Fatalf("expected no re-injection on second SetScope, got %d messages", len(ag.messages))
	}
}

type blockingStreamer struct {
	calls int
}

func (b *blockingStreamer) StreamChat(ctx context.Context, _ []llm.Message, _ *tools.Registry, events chan<- llm.StreamEvent) {
	b.calls++
	defer close(events)
	<-ctx.Done()
}

func TestCompactReturnsCanceledContextWithoutStartingStream(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.WorkDir = t.TempDir()
	cfg.SessionDir = t.TempDir()

	streamer := &blockingStreamer{}
	agent := NewAgentWithStreamer(&cfg, streamer)
	agent.bootstrapMsgCount = 1
	agent.messages = makeMessages(8)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := agent.Compact(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if streamer.calls != 0 {
		t.Fatalf("expected compact to skip streamer on canceled context, got %d calls", streamer.calls)
	}
}
