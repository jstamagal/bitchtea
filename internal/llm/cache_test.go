package llm

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"charm.land/fantasy"
	"charm.land/fantasy/providers/anthropic"

	"github.com/jstamagal/bitchtea/internal/tools"
)

// capturingLanguageModel is a fantasy.LanguageModel that records the messages
// (with their ProviderOptions) sent to Stream so the test can assert the
// presence or absence of Anthropic prompt-cache markers on specific positions
// in the prepared prompt.
type capturingLanguageModel struct {
	mu       sync.Mutex
	captured []fantasy.Message
}

func (m *capturingLanguageModel) Generate(context.Context, fantasy.Call) (*fantasy.Response, error) {
	return nil, fmt.Errorf("Generate not used in cache test")
}

func (m *capturingLanguageModel) Stream(_ context.Context, call fantasy.Call) (fantasy.StreamResponse, error) {
	m.mu.Lock()
	// Copy the slice so later mutations by the caller don't race the test.
	m.captured = append(m.captured[:0], call.Prompt...)
	m.mu.Unlock()
	return func(yield func(fantasy.StreamPart) bool) {
		yield(fantasy.StreamPart{
			Type:         fantasy.StreamPartTypeFinish,
			FinishReason: fantasy.FinishReasonStop,
			Usage:        fantasy.Usage{InputTokens: 1, OutputTokens: 1, TotalTokens: 2},
		})
	}, nil
}

func (m *capturingLanguageModel) GenerateObject(context.Context, fantasy.ObjectCall) (*fantasy.ObjectResponse, error) {
	return nil, fmt.Errorf("GenerateObject not used in cache test")
}

func (m *capturingLanguageModel) StreamObject(context.Context, fantasy.ObjectCall) (fantasy.ObjectStreamResponse, error) {
	return nil, fmt.Errorf("StreamObject not used in cache test")
}

func (m *capturingLanguageModel) Provider() string { return "capture" }
func (m *capturingLanguageModel) Model() string    { return "capture-model" }

// snapshot returns a copy of the recorded messages safe for assertion.
func (m *capturingLanguageModel) snapshot() []fantasy.Message {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]fantasy.Message, len(m.captured))
	copy(out, m.captured)
	return out
}

// hasAnthropicCacheControl reports whether msg carries the Anthropic ephemeral
// cache_control marker via ProviderOptions.
func hasAnthropicCacheControl(msg fantasy.Message) bool {
	if msg.ProviderOptions == nil {
		return false
	}
	opt, ok := msg.ProviderOptions[anthropic.Name]
	if !ok || opt == nil {
		return false
	}
	cc, ok := opt.(*anthropic.ProviderCacheControlOptions)
	if !ok || cc == nil {
		return false
	}
	return cc.CacheControl.Type == "ephemeral"
}

// runCaptureTurn primes the client cache with a capturing LanguageModel,
// runs one StreamChat with a synthetic transcript that has 4 bootstrap
// messages followed by a single user prompt, and returns the captured
// fantasy.Messages as fantasy/PrepareStep saw them post-marker placement.
func runCaptureTurn(t *testing.T, service string, bootstrapMsgCount int) []fantasy.Message {
	t.Helper()

	model := &capturingLanguageModel{}
	client := NewClient("test-key", "https://example.invalid", "test-model", "anthropic")
	client.SetService(service)
	client.SetBootstrapMsgCount(bootstrapMsgCount)

	// Pre-seed the cached LanguageModel so ensureModel doesn't try to dial
	// a network endpoint for the synthetic provider.
	client.mu.Lock()
	client.model = model
	client.mu.Unlock()

	// Bootstrap = system + (user, assistant) context pair + (user, assistant)
	// persona anchor = 5 messages. Last bootstrap message is the persona
	// rehearsal at index 4. After splitForFantasy + createPrompt, fantasy's
	// opts.Messages reads:
	//   [0] system (rebuilt from systemPrompt)
	//   [1] user "context"
	//   [2] assistant "got it"
	//   [3] user "persona"
	//   [4] assistant "rehearsal"  <-- bootstrap boundary, expect marker here
	//   [5] user "first real prompt" (the live prompt)
	msgs := []Message{
		{Role: "system", Content: "system prompt"},
		{Role: "user", Content: "context files"},
		{Role: "assistant", Content: "got it"},
		{Role: "user", Content: "persona prompt"},
		{Role: "assistant", Content: "rehearsal"},
		{Role: "user", Content: "first real prompt"},
	}
	if bootstrapMsgCount != 5 && bootstrapMsgCount != 0 {
		t.Fatalf("test fixture only models bootstrapMsgCount in {0, 5}; got %d", bootstrapMsgCount)
	}

	reg := tools.NewRegistry(t.TempDir(), t.TempDir())

	events := make(chan StreamEvent, 32)
	done := make(chan struct{})
	go func() {
		for range events {
		}
		close(done)
	}()
	client.StreamChat(context.Background(), msgs, reg, events)
	<-done

	captured := model.snapshot()
	if len(captured) == 0 {
		t.Fatal("capturing model never received a Stream call")
	}
	return captured
}

// TestApplyAnthropicCacheMarkers_AnthropicSetsBoundary asserts that with
// service == "anthropic" the marker rides on the last bootstrap message
// (and only on that message).
func TestApplyAnthropicCacheMarkers_AnthropicSetsBoundary(t *testing.T) {
	captured := runCaptureTurn(t, "anthropic", 5)

	// Boundary expected at opts.Messages[4] — see fixture comment above.
	const boundary = 4
	if !hasAnthropicCacheControl(captured[boundary]) {
		t.Fatalf("expected cache_control on boundary message %d; ProviderOptions=%+v",
			boundary, captured[boundary].ProviderOptions)
	}

	// No other position should carry the marker — Anthropic caps cache_control
	// markers at 4 per request and we deliberately spend exactly one.
	for i, msg := range captured {
		if i == boundary {
			continue
		}
		if hasAnthropicCacheControl(msg) {
			t.Fatalf("unexpected cache_control on message %d (role=%s)", i, msg.Role)
		}
	}
}

// TestApplyAnthropicCacheMarkers_OpenAINoMarker asserts that for an OpenAI
// service no message carries cache_control. Provider gating must be exact:
// even though the wire format here happens to be Anthropic (so the request
// would be syntactically legal), Service is the gate per Phase 9.
func TestApplyAnthropicCacheMarkers_OpenAINoMarker(t *testing.T) {
	captured := runCaptureTurn(t, "openai", 5)
	for i, msg := range captured {
		if hasAnthropicCacheControl(msg) {
			t.Fatalf("openai service must not stamp cache_control; found on message %d (role=%s)", i, msg.Role)
		}
	}
}

// TestApplyAnthropicCacheMarkers_ZaiAnthropicExcluded covers the design's
// open question: zai-anthropic speaks Anthropic wire format but is excluded
// from cache markers until a captured-payload test confirms upstream behavior.
// Until then, gating is by service name and zai-anthropic must produce a
// cache-marker-free request.
func TestApplyAnthropicCacheMarkers_ZaiAnthropicExcluded(t *testing.T) {
	captured := runCaptureTurn(t, "zai-anthropic", 5)
	for i, msg := range captured {
		if hasAnthropicCacheControl(msg) {
			t.Fatalf("zai-anthropic must be excluded from cache markers; found on message %d (role=%s)", i, msg.Role)
		}
	}
}

// TestApplyAnthropicCacheMarkers_NoBootstrapNoMarker covers the resume /
// fresh-restore path where bootstrapMsgCount has been reset to 0 — the
// helper must short-circuit and produce a marker-free request.
func TestApplyAnthropicCacheMarkers_NoBootstrapNoMarker(t *testing.T) {
	captured := runCaptureTurn(t, "anthropic", 0)
	for i, msg := range captured {
		if hasAnthropicCacheControl(msg) {
			t.Fatalf("zero bootstrap must skip marker; found on message %d (role=%s)", i, msg.Role)
		}
	}
}

// TestBootstrapPreparedIndex pins the index math the cache helper relies on.
// The fixture mirrors the layout NewAgentWithStreamer produces today: one
// system message at index 0 plus user/assistant pairs.
func TestBootstrapPreparedIndex(t *testing.T) {
	cases := []struct {
		name      string
		msgs      []Message
		bootstrap int
		want      int
	}{
		{
			name:      "no bootstrap recorded",
			msgs:      []Message{{Role: "user", Content: "x"}},
			bootstrap: 0,
			want:      -1,
		},
		{
			name: "system plus persona pair",
			msgs: []Message{
				{Role: "system", Content: "sp"},
				{Role: "user", Content: "anchor"},
				{Role: "assistant", Content: "rehearse"},
				{Role: "user", Content: "live prompt"},
			},
			bootstrap: 3,
			// opts.Messages = [system, user, assistant, live]
			// last bootstrap is opts.Messages[2]
			want: 2,
		},
		{
			name: "bootstrap larger than slice clamps to -1",
			msgs: []Message{
				{Role: "system", Content: "sp"},
			},
			bootstrap: 99,
			want:      -1,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := bootstrapPreparedIndex(tc.msgs, tc.bootstrap)
			if got != tc.want {
				t.Fatalf("bootstrapPreparedIndex(%d) = %d, want %d", tc.bootstrap, got, tc.want)
			}
		})
	}
}
