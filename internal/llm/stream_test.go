package llm

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"charm.land/fantasy"

	"github.com/jstamagal/bitchtea/internal/tools"
)

// --- safeSend ---------------------------------------------------------------
//
// safeSend(ctx, ch, ev) blocks on `ch <- ev` until either the receiver picks
// up the value (returns nil) or ctx is cancelled (returns ctx.Err()). These
// tests pin both branches plus the buffered-channel and cancel-while-blocked
// behaviors. They use a 2-second hard timeout per case to ensure the test
// suite never hangs if safeSend is rewritten in a way that loses the
// cancellation guarantee.

const safeSendTestTimeout = 2 * time.Second

func TestSafeSend_NormalSendUnbuffered(t *testing.T) {
	ch := make(chan StreamEvent)
	ctx := context.Background()

	done := make(chan error, 1)
	go func() {
		done <- safeSend(ctx, ch, StreamEvent{Type: "text", Text: "hi"})
	}()

	select {
	case got := <-ch:
		if got.Type != "text" || got.Text != "hi" {
			t.Fatalf("received wrong event: %+v", got)
		}
	case <-time.After(safeSendTestTimeout):
		t.Fatal("never received event from safeSend on unbuffered channel")
	}

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("safeSend returned %v on successful send, want nil", err)
		}
	case <-time.After(safeSendTestTimeout):
		t.Fatal("safeSend did not return after value was received")
	}
}

func TestSafeSend_BufferedChannelDoesNotBlock(t *testing.T) {
	ch := make(chan StreamEvent, 1)
	ctx := context.Background()

	done := make(chan error, 1)
	go func() {
		done <- safeSend(ctx, ch, StreamEvent{Type: "thinking"})
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("safeSend returned %v on buffered send, want nil", err)
		}
	case <-time.After(safeSendTestTimeout):
		t.Fatal("safeSend did not return immediately on buffered channel with capacity")
	}

	select {
	case got := <-ch:
		if got.Type != "thinking" {
			t.Fatalf("buffered value wrong: %+v", got)
		}
	default:
		t.Fatal("expected value to be sitting in the buffered channel")
	}
}

func TestSafeSend_ContextAlreadyCancelled(t *testing.T) {
	// When ctx is already cancelled and the channel cannot accept the value
	// (no receiver, no buffer), safeSend must return ctx.Err() and the value
	// must NOT land on the channel.
	ch := make(chan StreamEvent)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	done := make(chan error, 1)
	go func() {
		done <- safeSend(ctx, ch, StreamEvent{Type: "text", Text: "should not land"})
	}()

	select {
	case err := <-done:
		if err != context.Canceled {
			t.Fatalf("safeSend returned %v, want context.Canceled", err)
		}
	case <-time.After(safeSendTestTimeout):
		t.Fatal("safeSend did not return after ctx was already cancelled")
	}

	// Drain attempt — the value must not be sitting on the channel.
	select {
	case got := <-ch:
		t.Fatalf("safeSend leaked event onto channel after cancel: %+v", got)
	default:
	}
}

func TestSafeSend_ContextCancelledWhileBlocked(t *testing.T) {
	// safeSend starts blocked (unbuffered channel, no receiver). Then we
	// cancel the context. safeSend must unblock and return ctx.Err() rather
	// than wait forever for a receiver.
	ch := make(chan StreamEvent)
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		done <- safeSend(ctx, ch, StreamEvent{Type: "text", Text: "blocked"})
	}()

	// Give the goroutine a moment to enter the select. Without a small
	// scheduling beat the cancel can race ahead of the select{} in safeSend
	// and the test still passes — but the explicit beat documents intent.
	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err != context.Canceled {
			t.Fatalf("safeSend returned %v, want context.Canceled", err)
		}
	case <-time.After(safeSendTestTimeout):
		t.Fatal("safeSend did not unblock on ctx cancel")
	}
}

// --- toolResultText ---------------------------------------------------------
//
// toolResultText extracts the text payload from a fantasy.ToolResultOutputContent.
// Today it handles two shapes — the value ToolResultOutputContentText and a
// pointer *ToolResultOutputContentText — and returns "" for everything else
// (including a nil pointer of the recognized pointer type, and any unknown
// concrete type that satisfies the interface). These tests pin that contract.

func TestToolResultText_ValueTextPart(t *testing.T) {
	got := toolResultText(fantasy.ToolResultOutputContentText{Text: "hello"})
	if got != "hello" {
		t.Fatalf("toolResultText(value) = %q, want %q", got, "hello")
	}
}

func TestToolResultText_PointerTextPart(t *testing.T) {
	got := toolResultText(&fantasy.ToolResultOutputContentText{Text: "ptr-hello"})
	if got != "ptr-hello" {
		t.Fatalf("toolResultText(pointer) = %q, want %q", got, "ptr-hello")
	}
}

func TestToolResultText_NilPointerReturnsEmpty(t *testing.T) {
	// A typed-nil *ToolResultOutputContentText must not panic; current code
	// returns "" via the `if v != nil` guard.
	var nilPtr *fantasy.ToolResultOutputContentText
	got := toolResultText(nilPtr)
	if got != "" {
		t.Fatalf("toolResultText(nil pointer) = %q, want empty string", got)
	}
}

func TestToolResultText_EmptyValueText(t *testing.T) {
	// A value-type with an empty Text field returns "" — not because of an
	// "unknown" fallthrough but because v.Text is the empty string. Pinning
	// this so a future refactor doesn't accidentally substitute a placeholder.
	got := toolResultText(fantasy.ToolResultOutputContentText{Text: ""})
	if got != "" {
		t.Fatalf("toolResultText(empty value) = %q, want empty string", got)
	}
}

func TestToolResultText_UnknownErrorTypeReturnsEmpty(t *testing.T) {
	// fantasy.ToolResultOutputContentError satisfies ToolResultOutputContent
	// but is not handled by toolResultText today. Behavior: returns "".
	got := toolResultText(fantasy.ToolResultOutputContentError{Error: errors.New("boom")})
	if got != "" {
		t.Fatalf("toolResultText(error type) = %q, want empty string", got)
	}
}

func TestToolResultText_UnknownMediaTypeReturnsEmpty(t *testing.T) {
	// fantasy.ToolResultOutputContentMedia is the other built-in alternative
	// shape. Today toolResultText does not extract anything from it.
	got := toolResultText(fantasy.ToolResultOutputContentMedia{
		Data:      "ZGF0YQ==",
		MediaType: "image/png",
	})
	if got != "" {
		t.Fatalf("toolResultText(media type) = %q, want empty string", got)
	}
}

func TestToolResultText_NilInterfaceReturnsEmpty(t *testing.T) {
	// A bare nil interface flows through the type switch with no match and
	// hits the trailing `return ""`. Pinning this so the function stays
	// crash-free at the agent boundary if a provider ever sends a tool
	// result with no Result payload.
	got := toolResultText(nil)
	if got != "" {
		t.Fatalf("toolResultText(nil interface) = %q, want empty string", got)
	}
}

// --- StreamChat -------------------------------------------------------------

type fakeStreamStep struct {
	parts []fantasy.StreamPart
	err   error
}

// fakeStreamer is a programmable fantasy.LanguageModel for StreamChat tests.
// Each Stream call consumes one fakeStreamStep, letting tests drive tool-loop
// continuation, provider errors, usage reports, and malformed parts directly.
type fakeStreamer struct {
	mu    sync.Mutex
	steps []fakeStreamStep
	calls []fantasy.Call
}

func (m *fakeStreamer) Generate(context.Context, fantasy.Call) (*fantasy.Response, error) {
	return nil, fmt.Errorf("Generate not implemented")
}

func (m *fakeStreamer) Stream(ctx context.Context, call fantasy.Call) (fantasy.StreamResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.calls = append(m.calls, call)
	if len(m.calls) > len(m.steps) {
		return nil, fmt.Errorf("unexpected stream call %d", len(m.calls))
	}
	step := m.steps[len(m.calls)-1]
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if step.err != nil {
		return nil, step.err
	}
	return func(yield func(fantasy.StreamPart) bool) {
		for _, part := range step.parts {
			if !yield(part) {
				return
			}
		}
	}, nil
}

func (m *fakeStreamer) GenerateObject(context.Context, fantasy.ObjectCall) (*fantasy.ObjectResponse, error) {
	return nil, fmt.Errorf("GenerateObject not implemented")
}

func (m *fakeStreamer) StreamObject(context.Context, fantasy.ObjectCall) (fantasy.ObjectStreamResponse, error) {
	return nil, fmt.Errorf("StreamObject not implemented")
}

func (m *fakeStreamer) Provider() string { return "fake" }
func (m *fakeStreamer) Model() string    { return "fake-model" }

func (m *fakeStreamer) callCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.calls)
}

func streamTestClient(model fantasy.LanguageModel) *Client {
	client := NewClient("", "", "fake-model", "openai")
	client.InjectLanguageModelForTesting(model)
	return client
}

func streamTestRegistry(t *testing.T) *tools.Registry {
	t.Helper()
	return tools.NewRegistry(t.TempDir(), t.TempDir())
}

func collectStreamEvents(client *Client, ctx context.Context, reg *tools.Registry) []StreamEvent {
	events := make(chan StreamEvent, 32)
	client.StreamChat(ctx, []Message{{Role: "user", Content: "test"}}, reg, events)
	return drainStreamEvents(events)
}

func drainStreamEvents(events <-chan StreamEvent) []StreamEvent {
	var got []StreamEvent
	for ev := range events {
		got = append(got, ev)
	}
	return got
}

func textStep(text string, usage fantasy.Usage) fakeStreamStep {
	return fakeStreamStep{parts: []fantasy.StreamPart{
		{Type: fantasy.StreamPartTypeTextStart, ID: "text_1"},
		{Type: fantasy.StreamPartTypeTextDelta, ID: "text_1", Delta: text},
		{Type: fantasy.StreamPartTypeTextEnd, ID: "text_1"},
		{Type: fantasy.StreamPartTypeFinish, FinishReason: fantasy.FinishReasonStop, Usage: usage},
	}}
}

func singleToolCallStep(id, name, input string, usage fantasy.Usage) fakeStreamStep {
	return fakeStreamStep{parts: []fantasy.StreamPart{
		{
			Type:          fantasy.StreamPartTypeToolCall,
			ID:            id,
			ToolCallName:  name,
			ToolCallInput: input,
		},
		{Type: fantasy.StreamPartTypeFinish, FinishReason: fantasy.FinishReasonToolCalls, Usage: usage},
	}}
}

func requireEvent(t *testing.T, events []StreamEvent, eventType string) StreamEvent {
	t.Helper()
	for _, ev := range events {
		if ev.Type == eventType {
			return ev
		}
	}
	t.Fatalf("missing %q event in %+v", eventType, events)
	return StreamEvent{}
}

func requireNoEvent(t *testing.T, events []StreamEvent, eventType string) {
	t.Helper()
	for _, ev := range events {
		if ev.Type == eventType {
			t.Fatalf("unexpected %q event in %+v", eventType, events)
		}
	}
}

func TestStreamChat_midStreamError(t *testing.T) {
	workDir := t.TempDir()
	if err := os.WriteFile(workDir+"/test.txt", []byte("tool body\n"), 0644); err != nil {
		t.Fatalf("write test file: %v", err)
	}
	wantErr := errors.New("provider failed during second stream step")
	model := &fakeStreamer{steps: []fakeStreamStep{
		singleToolCallStep("call_read", "read", `{"path":"test.txt"}`, fantasy.Usage{InputTokens: 10, OutputTokens: 2, TotalTokens: 12}),
		{parts: []fantasy.StreamPart{{Type: fantasy.StreamPartTypeError, Error: wantErr}}},
	}}
	client := streamTestClient(model)
	reg := tools.NewRegistry(workDir, t.TempDir())

	events := collectStreamEvents(client, context.Background(), reg)

	toolCall := requireEvent(t, events, "tool_call")
	if toolCall.ToolName != "read" || toolCall.ToolCallID != "call_read" {
		t.Fatalf("tool_call = %+v, want read/call_read", toolCall)
	}
	toolResult := requireEvent(t, events, "tool_result")
	if toolResult.Text != "tool body\n" {
		t.Fatalf("tool_result text = %q, want file body", toolResult.Text)
	}
	errEvent := requireEvent(t, events, "error")
	if !errors.Is(errEvent.Error, wantErr) {
		t.Fatalf("error event = %v, want %v", errEvent.Error, wantErr)
	}
	requireNoEvent(t, events, "done")
	if got := model.callCount(); got != 2 {
		t.Fatalf("stream calls = %d, want 2", got)
	}
}

func TestStreamChat_contextCancelledDuringTool(t *testing.T) {
	model := &fakeStreamer{steps: []fakeStreamStep{
		singleToolCallStep("call_bash", "bash", `{"command":"sleep 5"}`, fantasy.Usage{InputTokens: 1, OutputTokens: 1, TotalTokens: 2}),
		textStep("should not happen", fantasy.Usage{InputTokens: 1, OutputTokens: 1, TotalTokens: 2}),
	}}
	client := streamTestClient(model)
	reg := streamTestRegistry(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	events := make(chan StreamEvent, 32)
	done := make(chan struct{})
	go func() {
		client.StreamChat(ctx, []Message{{Role: "user", Content: "run slow command"}}, reg, events)
		close(done)
	}()

	select {
	case ev := <-events:
		if ev.Type != "thinking" {
			t.Fatalf("first event = %+v, want thinking", ev)
		}
	case <-time.After(safeSendTestTimeout):
		t.Fatal("timed out waiting for first stream event")
	}
	call := requireNextEvent(t, events, "tool_call")
	if call.ToolName != "bash" || call.ToolCallID != "call_bash" {
		t.Fatalf("tool_call = %+v, want bash/call_bash", call)
	}
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(safeSendTestTimeout):
		t.Fatal("StreamChat did not return after context cancellation during tool execution")
	}
	got := drainStreamEvents(events)
	got = append([]StreamEvent{call}, got...)
	if ctx.Err() != context.Canceled {
		t.Fatalf("ctx.Err() = %v, want context.Canceled", ctx.Err())
	}
	requireNoEvent(t, got, "done")
	if got := model.callCount(); got > 2 {
		t.Fatalf("stream calls = %d, want cancellation to stop without extra provider retries", got)
	}
}

func requireNextEvent(t *testing.T, events <-chan StreamEvent, eventType string) StreamEvent {
	t.Helper()
	deadline := time.After(safeSendTestTimeout)
	for {
		select {
		case ev, ok := <-events:
			if !ok {
				t.Fatalf("events closed before %q event", eventType)
			}
			if ev.Type == eventType {
				return ev
			}
		case <-deadline:
			t.Fatalf("timed out waiting for %q event", eventType)
		}
	}
}

func TestStreamChat_usageAccumulation(t *testing.T) {
	model := &fakeStreamer{steps: []fakeStreamStep{
		singleToolCallStep("call_read", "read", `{"path":"empty.txt"}`, fantasy.Usage{InputTokens: 10, OutputTokens: 2, TotalTokens: 12}),
		textStep("done", fantasy.Usage{InputTokens: 5, OutputTokens: 1, TotalTokens: 6, CacheCreationTokens: 3, CacheReadTokens: 4}),
	}}
	client := streamTestClient(model)
	workDir := t.TempDir()
	if err := os.WriteFile(workDir+"/empty.txt", nil, 0644); err != nil {
		t.Fatalf("write empty test file: %v", err)
	}
	events := collectStreamEvents(client, context.Background(), tools.NewRegistry(workDir, t.TempDir()))

	var total TokenUsage
	var usageEvents int
	for _, ev := range events {
		if ev.Type != "usage" {
			continue
		}
		usageEvents++
		total.InputTokens += ev.Usage.InputTokens
		total.OutputTokens += ev.Usage.OutputTokens
		total.CacheCreationTokens += ev.Usage.CacheCreationTokens
		total.CacheReadTokens += ev.Usage.CacheReadTokens
	}
	if usageEvents != 2 {
		t.Fatalf("usage events = %d, want 2 in %+v", usageEvents, events)
	}
	if total.InputTokens != 15 || total.OutputTokens != 3 || total.CacheCreationTokens != 3 || total.CacheReadTokens != 4 {
		t.Fatalf("accumulated usage = %+v, want input=15 output=3 cache_create=3 cache_read=4", total)
	}
	requireEvent(t, events, "done")
}

func TestStreamChat_emptyToolResult(t *testing.T) {
	model := &fakeStreamer{steps: []fakeStreamStep{
		singleToolCallStep("call_bash", "bash", `{"Command":"true"}`, fantasy.Usage{InputTokens: 3, OutputTokens: 1, TotalTokens: 4}),
		textStep("done", fantasy.Usage{InputTokens: 2, OutputTokens: 1, TotalTokens: 3}),
	}}
	client := streamTestClient(model)

	events := collectStreamEvents(client, context.Background(), streamTestRegistry(t))

	toolResult := requireEvent(t, events, "tool_result")
	if toolResult.ToolName != "bash" || toolResult.ToolCallID != "call_bash" {
		t.Fatalf("tool_result = %+v, want bash/call_bash", toolResult)
	}
	if toolResult.Text != "" {
		t.Fatalf("empty tool result text = %q, want empty string", toolResult.Text)
	}
	requireEvent(t, events, "done")
}

func TestStreamChat_malformedEvent(t *testing.T) {
	model := &fakeStreamer{steps: []fakeStreamStep{{
		parts: []fantasy.StreamPart{
			{Type: fantasy.StreamPartType("not_a_real_stream_part")},
			{Type: fantasy.StreamPartTypeTextStart, ID: "text_1"},
			{Type: fantasy.StreamPartTypeTextDelta, ID: "text_1", Delta: "still works"},
			{Type: fantasy.StreamPartTypeTextEnd, ID: "text_1"},
			{Type: fantasy.StreamPartTypeFinish, FinishReason: fantasy.FinishReasonStop, Usage: fantasy.Usage{InputTokens: 1, OutputTokens: 2, TotalTokens: 3}},
		},
	}}}
	client := streamTestClient(model)

	events := collectStreamEvents(client, context.Background(), nil)

	var text string
	for _, ev := range events {
		if ev.Type == "text" {
			text += ev.Text
		}
		if ev.Type == "error" {
			t.Fatalf("unexpected error after malformed provider event: %v", ev.Error)
		}
	}
	if text != "still works" {
		t.Fatalf("text after malformed event = %q, want %q", text, "still works")
	}
	requireEvent(t, events, "done")
}
