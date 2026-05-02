package llm

import (
	"context"
	"fmt"
	"runtime"
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
//
// entered is signalled (non-blocking send) on every Stream entry so the test
// can synchronize "all streams are inside blocking" before starting setters.
type blockingLanguageModel struct {
	release chan struct{}
	entered chan struct{}
	calls   atomic.Int32
}

func (m *blockingLanguageModel) Generate(context.Context, fantasy.Call) (*fantasy.Response, error) {
	return nil, fmt.Errorf("Generate not implemented")
}

func (m *blockingLanguageModel) Stream(ctx context.Context, _ fantasy.Call) (fantasy.StreamResponse, error) {
	m.calls.Add(1)
	if m.entered != nil {
		select {
		case m.entered <- struct{}{}:
		default:
		}
	}
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
//   - Spawn N stream goroutines and wait until each one has actually
//     entered the blocking model. (If we don't synchronize here, a setter
//     may invalidate the cached model before a slow stream goroutine
//     reaches ensureModel; that goroutine would then try to build a real
//     provider with the test's bogus URL and DNS-fail. Not a real bug —
//     just a test-setup race.)
//   - Spawn M setter goroutines that hammer the Set* surface.
//   - Release the blocking model, wait for all goroutines, assert no panic
//     and that final field state matches some setter assignment.
//
// A deadline guard (5s) calls t.Fatal if wg.Wait() doesn't return. This
// catches real deadlocks fast instead of letting them eat the full Go test
// framework timeout. If you hit the deadline guard, suspect a real
// concurrency bug in the Set*/ensureModel mutex contract.
//
// Sizes are deliberately modest (8 streams, 16 setters, 50 iter): the mutex
// surface is tiny, more goroutines do not buy more coverage but do amplify
// CI flake from goroutine churn.
//
// Pass = clean run under `go test -race`. Failure here means there is a real
// race on Client cache fields and a follow-up bd ticket is required.
func TestClientConcurrentSetAndStream(t *testing.T) {
	const (
		streamGoroutines = 8
		setterGoroutines = 16
		setterIterations = 50
		deadlineGuard    = 5 * time.Second
	)

	model := &blockingLanguageModel{
		release: make(chan struct{}),
		entered: make(chan struct{}, streamGoroutines),
	}
	client := NewClient("seed-key", "https://seed.example", "seed-model", "openai")
	// Pre-seed the cache so ensureModel returns immediately on the first
	// stream call without hitting buildProvider (which would try real
	// network setup).
	client.mu.Lock()
	client.model = model
	client.mu.Unlock()

	reg := tools.NewRegistry(t.TempDir(), t.TempDir())

	// Use a non-cancelling ctx for the streams themselves — we want them to
	// complete normally, not get cancelled mid-flight (which would push
	// them into StreamChat's retry/backoff path and make the test slow).
	// The deadlineGuard below is what actually catches a deadlock.
	ctx, cancel := context.WithCancel(context.Background())
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

	// Wait until every stream goroutine has reached blockingLanguageModel.Stream.
	// At this point each one holds an in-flight reference to the cached model
	// and is parked on the release channel; subsequent Set* invalidations no
	// longer affect them.
	for i := 0; i < streamGoroutines; i++ {
		select {
		case <-model.entered:
		case <-time.After(deadlineGuard):
			t.Fatalf("only %d/%d stream goroutines reached blocking model — possible mutex deadlock in ensureModel", i, streamGoroutines)
		}
	}

	// Setter goroutines: every iteration mutates a Client field. Streams
	// are now safely parked, so setter cache invalidation just exercises
	// the mutex without breaking the in-flight streams.
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

	// Release the blocking model so streams can complete.
	close(model.release)

	// Deadline guard: if wg.Wait doesn't return within deadlineGuard,
	// something is genuinely deadlocked. Fail fast with a useful message
	// instead of waiting for the Go test framework's much larger timeout.
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(deadlineGuard):
		buf := make([]byte, 1<<20)
		n := runtime.Stack(buf, true)
		t.Logf("goroutine dump on deadlock:\n%s", buf[:n])
		t.Fatal("test deadlock — wg.Wait() did not return within deadline; concurrent Set*/Stream path may have a real bug")
	}

	// Sanity: every stream goroutine actually entered fantasy.Stream
	// (we already gated on this via model.entered).
	if model.calls.Load() < int32(streamGoroutines) {
		t.Fatalf("expected %d stream entries, got %d", streamGoroutines, model.calls.Load())
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
