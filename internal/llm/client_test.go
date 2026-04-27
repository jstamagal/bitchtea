package llm

import (
	"testing"

	"charm.land/fantasy"
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
