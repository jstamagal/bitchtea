package llm

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"charm.land/fantasy"

	"github.com/jstamagal/bitchtea/internal/tools"
)

// fakeProvider is just enough to populate the cache without going on the
// network; the Set* tests only check that the cache slot is cleared.
type fakeProvider struct{}

func (fakeProvider) Name() string                                                  { return "fake" }
func (fakeProvider) LanguageModel(_ any, _ string) (fantasy.LanguageModel, error)  { return nil, nil }
func (fakeProvider) Models() []string                                              { return nil }

// primeCache forces the client into a state where both cached fields are
// non-nil, so an invalidation actually has something to clear.
func primeCache(c *Client) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.provider = struct{ fantasy.Provider }{} // any non-nil interface value
	c.model = struct{ fantasy.LanguageModel }{}
}

func cachedNonNil(c *Client) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.provider != nil || c.model != nil
}

func TestNewClientCopiesFields(t *testing.T) {
	c := NewClient("k", "https://x", "m", "openai")
	if c.APIKey != "k" || c.BaseURL != "https://x" || c.Model != "m" || c.Provider != "openai" {
		t.Fatalf("NewClient did not copy fields: %+v", c)
	}
	if c.provider != nil || c.model != nil {
		t.Fatalf("NewClient must not eagerly build provider/model: %+v", c)
	}
}

func TestSetAPIKeyInvalidatesCache(t *testing.T) {
	c := NewClient("a", "u", "m", "p")
	primeCache(c)
	c.SetAPIKey("b")
	if c.APIKey != "b" {
		t.Fatalf("APIKey not updated, got %q", c.APIKey)
	}
	if cachedNonNil(c) {
		t.Fatal("SetAPIKey did not invalidate cached provider/model")
	}
}

func TestSetBaseURLInvalidatesCache(t *testing.T) {
	c := NewClient("a", "u", "m", "p")
	primeCache(c)
	c.SetBaseURL("https://other")
	if c.BaseURL != "https://other" {
		t.Fatalf("BaseURL not updated, got %q", c.BaseURL)
	}
	if cachedNonNil(c) {
		t.Fatal("SetBaseURL did not invalidate cached provider/model")
	}
}

func TestSetModelInvalidatesCache(t *testing.T) {
	c := NewClient("a", "u", "m", "p")
	primeCache(c)
	c.SetModel("m2")
	if c.Model != "m2" {
		t.Fatalf("Model not updated, got %q", c.Model)
	}
	if cachedNonNil(c) {
		t.Fatal("SetModel did not invalidate cached provider/model")
	}
}

func TestSetProviderInvalidatesCache(t *testing.T) {
	c := NewClient("a", "u", "m", "openai")
	primeCache(c)
	c.SetProvider("anthropic")
	if c.Provider != "anthropic" {
		t.Fatalf("Provider not updated, got %q", c.Provider)
	}
	if cachedNonNil(c) {
		t.Fatal("SetProvider did not invalidate cached provider/model")
	}
}

func TestSetDebugHookInstallInvalidatesCache(t *testing.T) {
	c := NewClient("a", "u", "m", "p")
	primeCache(c)
	c.SetDebugHook(func(DebugInfo) {})
	if c.DebugHook == nil {
		t.Fatal("SetDebugHook did not install hook")
	}
	if cachedNonNil(c) {
		t.Fatal("SetDebugHook(install) did not invalidate cache — provider must rebuild with new HTTP client")
	}
}

func TestSetDebugHookClearInvalidatesCache(t *testing.T) {
	c := NewClient("a", "u", "m", "p")
	c.SetDebugHook(func(DebugInfo) {})
	primeCache(c)
	c.SetDebugHook(nil)
	if c.DebugHook != nil {
		t.Fatal("SetDebugHook(nil) did not clear hook")
	}
	if cachedNonNil(c) {
		t.Fatal("SetDebugHook(nil) over installed hook must invalidate cache")
	}
}

func TestSetDebugHookNilNoOpKeepsCache(t *testing.T) {
	c := NewClient("a", "u", "m", "p")
	primeCache(c)
	// Hook was already nil — calling Set with nil must NOT pay rebuild cost.
	c.SetDebugHook(nil)
	if !cachedNonNil(c) {
		t.Fatal("SetDebugHook(nil) over nil should be a no-op and preserve cache")
	}
}

// blockingLanguageModel is a fantasy.LanguageModel whose Stream blocks until
// release is closed, then emits a minimal finish part. It exists so the
// concurrency test can keep multiple StreamChat calls in flight while
// hammering Set* from other goroutines.
type blockingLanguageModel struct {
	release chan struct{}
	calls   atomic.Int32
}

func (m *blockingLanguageModel) Generate(context.Context, fantasy.Call) (*fantasy.Response, error) {
	return nil, fmt.Errorf("Generate not implemented")
}

func (m *blockingLanguageModel) Stream(ctx context.Context, _ fantasy.Call) (fantasy.StreamResponse, error) {
	m.calls.Add(1)
	// Block here so the StreamChat goroutine sits inside fantasy with the
	// Client mutex released. While we sit here, the setter goroutines race
	// against ensureModel callers.
	select {
	case <-m.release:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return func(yield func(fantasy.StreamPart) bool) {
		yield(fantasy.StreamPart{
			Type:         fantasy.StreamPartTypeFinish,
			FinishReason: fantasy.FinishReasonStop,
			Usage:        fantasy.Usage{InputTokens: 1, OutputTokens: 1, TotalTokens: 2},
		})
	}, nil
}

func (m *blockingLanguageModel) GenerateObject(context.Context, fantasy.ObjectCall) (*fantasy.ObjectResponse, error) {
	return nil, fmt.Errorf("GenerateObject not implemented")
}

func (m *blockingLanguageModel) StreamObject(context.Context, fantasy.ObjectCall) (fantasy.ObjectStreamResponse, error) {
	return nil, fmt.Errorf("StreamObject not implemented")
}

func (m *blockingLanguageModel) Provider() string { return "blocking" }
func (m *blockingLanguageModel) Model() string    { return "blocking-model" }

// TestClientConcurrentSetAndStream verifies the Client mutex protects cache
// access when UI-side Set* calls run concurrently with agent-side StreamChat.
// Pre-Phase-4 prerequisite (bt-4qv): the existing mu.Lock contract in
// Set*/ensureModel must hold up under -race.
//
// Strategy:
//   - Pre-install a blocking fake LanguageModel so StreamChat returns from
//     ensureModel with a cached reference, then sits inside fantasy.Stream.
//   - Spawn N stream goroutines and M setter goroutines (~64 total).
//   - Setter goroutines hammer SetAPIKey/SetBaseURL/SetModel/SetProvider/
//     SetDebugHook in tight loops, each call invalidating the cache.
//   - Release the blocking model, wait for all goroutines, assert no panic
//     and that final field state matches the last setter assignment.
//
// Pass = clean run under `go test -race`. Failure here means there is a real
// race on Client cache fields and a follow-up bd ticket is required.
func TestClientConcurrentSetAndStream(t *testing.T) {
	const (
		streamGoroutines = 16
		setterGoroutines = 48
		setterIterations = 200
	)

	model := &blockingLanguageModel{release: make(chan struct{})}
	client := NewClient("seed-key", "https://seed.example", "seed-model", "openai")
	// Pre-seed the cache so ensureModel returns immediately on the first
	// stream call without hitting buildProvider (which would try real
	// network setup).
	client.mu.Lock()
	client.model = model
	client.mu.Unlock()

	reg := tools.NewRegistry(t.TempDir(), t.TempDir())

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var wg sync.WaitGroup

	// Stream goroutines: each one drains its own events channel until close.
	for i := 0; i < streamGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			events := make(chan StreamEvent, 8)
			go func() {
				// Drain — we don't care about contents, only that the call
				// completes without panicking under -race.
				for range events {
				}
			}()
			client.StreamChat(ctx, []Message{
				{Role: "user", Content: "ping"},
			}, reg, events)
		}()
	}

	// Setter goroutines: every iteration mutates a Client field while the
	// stream goroutines hold an in-flight reference to the cached model.
	// Each Set* takes c.mu and clears the cache; the in-flight model
	// reference stays valid per the ensureModel contract.
	for i := 0; i < setterGoroutines; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < setterIterations; j++ {
				suffix := strconv.Itoa(i) + "-" + strconv.Itoa(j)
				switch j % 5 {
				case 0:
					client.SetAPIKey("key-" + suffix)
				case 1:
					client.SetBaseURL("https://host-" + suffix + ".example")
				case 2:
					client.SetModel("model-" + suffix)
				case 3:
					// Only flip between the two valid providers so any
					// future ensureModel build wouldn't error on an
					// unsupported provider string.
					if j%2 == 0 {
						client.SetProvider("openai")
					} else {
						client.SetProvider("anthropic")
					}
				case 4:
					client.SetDebugHook(func(DebugInfo) {})
				}
			}
		}()
	}

	// Give everything a moment to interleave, then release the blocking
	// model so streams can complete.
	time.Sleep(50 * time.Millisecond)
	close(model.release)

	wg.Wait()

	// Sanity: at least one stream actually entered fantasy.Stream. (Some
	// may have errored out earlier if the test environment is pathological,
	// but we expect most to land here.)
	if model.calls.Load() == 0 {
		t.Fatal("no stream goroutines reached fantasy.Stream — test did not exercise the concurrent path")
	}

	// Final-state sanity: the Client's exported fields must hold *some*
	// value written by a setter (last-write-wins). We can't predict which
	// setter won, but every setter writes a non-empty string of the
	// expected shape, so empty/missing values would indicate a torn write.
	client.mu.Lock()
	apiKey, baseURL, modelName, provider := client.APIKey, client.BaseURL, client.Model, client.Provider
	client.mu.Unlock()
	if apiKey == "" || baseURL == "" || modelName == "" || provider == "" {
		t.Fatalf("torn final state after concurrent setters: APIKey=%q BaseURL=%q Model=%q Provider=%q", apiKey, baseURL, modelName, provider)
	}
}
