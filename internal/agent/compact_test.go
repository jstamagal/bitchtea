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

	// With 6 messages, compaction fires: system + summary + ack + last 4 = 7.
	if streamer.calls != 2 {
		t.Fatalf("expected 2 streamer calls, got %d", streamer.calls)
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

	// Structure: system + summary_msg + ack + last4 = 7
	if len(agent.messages) != 7 {
		t.Fatalf("expected 7 messages after compact, got %d", len(agent.messages))
	}

	// [0] must be the original system prompt.
	if msgText(agent.messages[0]) != origSystem {
		t.Fatalf("system prompt changed after compact")
	}

	// [1] must contain the summary.
	if !strings.Contains(msgText(agent.messages[1]), summaryText) {
		t.Fatalf("summary message missing summary text; got %q", msgText(agent.messages[1]))
	}

	// [2] must be the assistant acknowledgement.
	if agent.messages[2].Role != fantasy.MessageRoleAssistant {
		t.Fatalf("expected assistant ack at index 2, got role %q", agent.messages[2].Role)
	}

	// [3..6] must match the original last 4 messages.
	for i := 0; i < 4; i++ {
		got := agent.messages[3+i]
		want := last4[i]
		if got.Role != want.Role || msgText(got) != msgText(want) {
			t.Fatalf("message at index %d differs: got {%s,%q}, want {%s,%q}",
				3+i, got.Role, msgText(got), want.Role, msgText(want))
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

	// After compaction: system + summary + ack + last4 = 7
	if len(agent.messages) != 7 {
		t.Fatalf("expected 7 messages, got %d", len(agent.messages))
	}

	// The tool call message should be at index 4 (3 + offset 1 within last4).
	toolMsg := agent.messages[4]
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

	// The tool result message should be at index 5.
	resultMsg := agent.messages[5]
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
			d, _ := os.ReadFile(rootDailyPath); return d
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
