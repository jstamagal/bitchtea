package llm

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
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

// --- samplingParamsSupported tests ---

func TestSamplingParamsSupported_Anthropic(t *testing.T) {
	if samplingParamsSupported("anthropic") {
		t.Error("anthropic should not support sampling params (returns 400)")
	}
	if samplingParamsSupported("zai-anthropic") {
		t.Error("zai-anthropic should not support sampling params (returns 400)")
	}
}

func TestSamplingParamsSupported_Others(t *testing.T) {
	supported := []string{"openai", "openrouter", "ollama", "custom", "vercel", "xai", ""}
	for _, svc := range supported {
		if !samplingParamsSupported(svc) {
			t.Errorf("service=%q should support sampling params", svc)
		}
	}
}

func TestApplySamplingParams_AnthropicSkipsAll(t *testing.T) {
	temp := 0.7
	topP := 0.9
	topK := 40
	repPen := 1.2

	opts := applySamplingParams("anthropic", &temp, &topP, &repPen, &topK, nil)
	if len(opts) != 0 {
		t.Errorf("anthropic: expected 0 opts applied, got %d", len(opts))
	}
}

func TestApplySamplingParams_OpenAIForwardsAll(t *testing.T) {
	temp := 0.7
	topP := 0.9
	topK := 40

	opts := applySamplingParams("openai", &temp, &topP, nil, &topK, nil)
	// temperature + top_p + top_k = 3 opts
	if len(opts) != 3 {
		t.Errorf("openai: expected 3 opts applied, got %d", len(opts))
	}
}

func TestApplySamplingParams_NilParamsNoOpts(t *testing.T) {
	opts := applySamplingParams("openai", nil, nil, nil, nil, nil)
	if len(opts) != 0 {
		t.Errorf("nil params: expected 0 opts, got %d", len(opts))
	}
}

// --- cliproxyReasoningEffort tests ---

// TestClipроxyEffortToReasoningEffort_KnownValues verifies that all documented
// valid effort strings map to the expected openai.ReasoningEffort constants.
// "max" is documented as promoted to "xhigh" because the OpenAI SDK has no
// max constant; this test pins that mapping so a future change is deliberate.
func TestCliproxyEffortToReasoningEffort_KnownValues(t *testing.T) {
	cases := []struct {
		input string
		want  string // expected ReasoningEffort string value
	}{
		{"low", "low"},
		{"medium", "medium"},
		{"high", "high"},
		{"xhigh", "xhigh"},
		{"max", "xhigh"}, // max promoted to xhigh — no SDK constant for max
		{"LOW", "low"},   // case-insensitive
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			got, ok := cliproxyEffortToReasoningEffort(tc.input)
			if !ok {
				t.Fatalf("cliproxyEffortToReasoningEffort(%q) = _, false; want ok", tc.input)
			}
			if string(got) != tc.want {
				t.Fatalf("cliproxyEffortToReasoningEffort(%q) = %q; want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestCliproxyEffortToReasoningEffort_Unknown(t *testing.T) {
	_, ok := cliproxyEffortToReasoningEffort("ultramax")
	if ok {
		t.Error("unknown effort: ultramax; should return ok=false")
	}
	_, ok = cliproxyEffortToReasoningEffort("")
	if ok {
		t.Error("empty string should return ok=false")
	}
}

// TestApplyCliproxyReasoningEffort_AddsOptionForCliproxyAPI asserts that
// applyCliproxyReasoningEffort appends exactly one AgentOption when
// service=="cliproxyapi" and effort is a known value.
func TestApplyCliproxyReasoningEffort_AddsOptionForCliproxyAPI(t *testing.T) {
	opts := applyCliproxyReasoningEffort("cliproxyapi", "high", nil)
	if len(opts) != 1 {
		t.Fatalf("expected 1 AgentOption, got %d", len(opts))
	}
}

// TestApplyCliproxyReasoningEffort_DefaultsToXHighWhenEmpty verifies that an
// empty effort string becomes "xhigh" for cliproxyapi (Opus 4.7 sweet spot).
func TestApplyCliproxyReasoningEffort_DefaultsToXHighWhenEmpty(t *testing.T) {
	opts := applyCliproxyReasoningEffort("cliproxyapi", "", nil)
	if len(opts) != 1 {
		t.Fatalf("expected 1 AgentOption (default xhigh), got %d", len(opts))
	}
}

// TestApplyCliproxyReasoningEffort_NoOpForOtherServices asserts that services
// other than "cliproxyapi" are not given the reasoning_effort option.
func TestApplyCliproxyReasoningEffort_NoOpForOtherServices(t *testing.T) {
	for _, svc := range []string{"openai", "anthropic", "ollama", "openrouter", ""} {
		opts := applyCliproxyReasoningEffort(svc, "high", nil)
		if len(opts) != 0 {
			t.Errorf("service=%q: expected 0 opts, got %d", svc, len(opts))
		}
	}
}

// --- isRetryable ------------------------------------------------------------
//
// The classifier was upgraded from substring matching against a flat keyword
// list to a tiered approach: explicit sentinel checks (errors.Is), typed
// network errors (errors.As), word-boundary regex for HTTP codes and
// timeout/eof, then substring matching for distinctive multi-word phrases.
// These tests pin the new contract and the false-positives the old version
// produced.

func TestIsRetryable_NilIsFalse(t *testing.T) {
	if isRetryable(nil) {
		t.Fatal("isRetryable(nil) must be false")
	}
}

func TestIsRetryable_ContextCanceledIsFalse(t *testing.T) {
	// User cancellation must NOT trigger retries — that would defeat the
	// cancel intent.
	if isRetryable(context.Canceled) {
		t.Fatal("context.Canceled must not be retryable")
	}
	wrapped := fmt.Errorf("upstream call: %w", context.Canceled)
	if isRetryable(wrapped) {
		t.Fatal("wrapped context.Canceled must not be retryable")
	}
}

func TestIsRetryable_SentinelErrorsAreRetryable(t *testing.T) {
	cases := []error{
		context.DeadlineExceeded,
		io.EOF,
		io.ErrUnexpectedEOF,
	}
	for _, e := range cases {
		if !isRetryable(e) {
			t.Errorf("expected %v to be retryable", e)
		}
		if !isRetryable(fmt.Errorf("wrapped: %w", e)) {
			t.Errorf("expected wrapped %v to be retryable", e)
		}
	}
}

func TestIsRetryable_NetOpErrorIsRetryable(t *testing.T) {
	opErr := &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connection refused")}
	if !isRetryable(opErr) {
		t.Fatal("expected *net.OpError to be retryable")
	}
	if !isRetryable(fmt.Errorf("upstream: %w", opErr)) {
		t.Fatal("expected wrapped *net.OpError to be retryable")
	}
}

func TestIsRetryable_DNSErrorTemporaryYes_NotFoundNo(t *testing.T) {
	// Temporary DNS failure: retry.
	tmp := &net.DNSError{Err: "server misbehaving", Name: "api.example.com", IsTemporary: true}
	if !isRetryable(tmp) {
		t.Fatal("temporary DNS error must be retryable")
	}
	// NXDOMAIN: not retryable — the name doesn't exist.
	nxdomain := &net.DNSError{Err: "no such host", Name: "noexist.example.com", IsNotFound: true}
	if isRetryable(nxdomain) {
		t.Fatal("NXDOMAIN must NOT be retryable")
	}
}

func TestIsRetryable_HTTPCodes(t *testing.T) {
	// True positives — exact code as a word.
	for _, msg := range []string{
		"upstream returned 429",
		"got HTTP 502 Bad Gateway",
		"503 service unavailable",
		"server returned status 504",
	} {
		if !isRetryable(errors.New(msg)) {
			t.Errorf("expected %q to be retryable", msg)
		}
	}
	// False positives the old substring matcher would catch — must NOT
	// match now.
	for _, msg := range []string{
		"5029 widgets failed",
		"x4290 overflow detected",
		"counter at 502345",
	} {
		if isRetryable(errors.New(msg)) {
			t.Errorf("regression: %q must NOT be classified retryable", msg)
		}
	}
}

func TestIsRetryable_TimeoutAndEOFWordBoundary(t *testing.T) {
	// True positives — actual word.
	for _, msg := range []string{
		"i/o timeout",
		"request timeout exceeded",
		"timed out reading body",
		"unexpected eof",
		"got eof from server",
	} {
		if !isRetryable(errors.New(msg)) {
			t.Errorf("expected %q to be retryable", msg)
		}
	}
	// False positives the old substring matcher would catch — must NOT
	// match now.
	for _, msg := range []string{
		"timeoutHandler invoked",
		"set timeoutMs=5000",
		"calling timeoutMiddleware",
		"function fooEofBar undefined",
		"variable named beofcake declared",
	} {
		if isRetryable(errors.New(msg)) {
			t.Errorf("regression: %q must NOT be classified retryable", msg)
		}
	}
}

func TestIsRetryable_DistinctivePhrases(t *testing.T) {
	for _, msg := range []string{
		"rate limit exceeded",
		"too many requests, slow down",
		"server overloaded, try later",
		"internal server error",
		"service unavailable right now",
		"connection refused by peer",
		"connection reset by peer",
		"broken pipe",
		"tls handshake timeout",
		"tls: handshake failure",
		"no such host: api.example.com",
		"dial tcp 127.0.0.1:443: refused",
		"i/o timeout",
		"temporary failure in resolver",
		"server says try again",
	} {
		if !isRetryable(errors.New(msg)) {
			t.Errorf("expected phrase %q to be retryable", msg)
		}
	}
}

func TestIsRetryable_RandomErrorIsNotRetryable(t *testing.T) {
	for _, msg := range []string{
		"json: invalid character at position 12",
		"missing required field",
		"unauthorized",
		"forbidden",
		"validation failed: name too long",
		"",
	} {
		if isRetryable(errors.New(msg)) {
			t.Errorf("non-transient error %q must NOT be retryable", msg)
		}
	}
}
