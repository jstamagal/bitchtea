package llm

import (
	"context"
	"fmt"
	"net/http"
	"sync"

	"charm.land/fantasy"
)

// Client is bitchtea's wrapper around a fantasy.Provider + LanguageModel
// pair. Public fields are exposed for slash-command introspection
// (`/provider`, `/baseurl`, `/model`); to mutate them at runtime, callers
// MUST use the Set* methods so the cached provider rebuilds with the new
// values and the mutex protects against in-flight stream calls.
type Client struct {
	APIKey   string
	BaseURL  string
	Model    string
	Provider string

	// DebugHook is invoked for each upstream HTTP request when non-nil.
	// To set/clear it at runtime, use SetDebugHook (the cached provider
	// must rebuild because its HTTP client wraps this hook).
	DebugHook func(DebugInfo)

	mu       sync.Mutex
	provider fantasy.Provider     // cached, nil until first ensureModel
	model    fantasy.LanguageModel // cached, nil until first ensureModel
}

// NewClient builds a Client. The provider/model are not constructed until the
// first StreamChat call (lazy init).
func NewClient(apiKey, baseURL, model, provider string) *Client {
	return &Client{
		APIKey:   apiKey,
		BaseURL:  baseURL,
		Model:    model,
		Provider: provider,
	}
}

func (c *Client) SetAPIKey(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.APIKey = key
	c.invalidateLocked()
}

func (c *Client) SetBaseURL(url string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.BaseURL = url
	c.invalidateLocked()
}

func (c *Client) SetModel(model string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Model = model
	c.invalidateLocked()
}

func (c *Client) SetProvider(provider string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Provider = provider
	c.invalidateLocked()
}

// SetDebugHook installs (or clears) the DebugHook. nil → nil is a no-op so
// callers that toggle debug off when it was already off don't pay for a
// provider rebuild on the next call.
func (c *Client) SetDebugHook(hook func(DebugInfo)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if hook == nil && c.DebugHook == nil {
		return
	}
	c.DebugHook = hook
	c.invalidateLocked()
}

func (c *Client) invalidateLocked() {
	c.provider = nil
	c.model = nil
}

// ensureModel returns a cached LanguageModel or builds one from the current
// config. The mutex is held only across cache check + provider build; the
// returned model is safe to use after the mutex releases because once built
// it is immutable from the client's POV (a Set* call replaces it in the
// cache but the existing reference stays valid for the in-flight call).
func (c *Client) ensureModel(ctx context.Context) (fantasy.LanguageModel, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.model != nil {
		return c.model, nil
	}
	if c.provider == nil {
		p, err := buildProvider(c.toProviderConfigLocked())
		if err != nil {
			return nil, fmt.Errorf("build provider: %w", err)
		}
		c.provider = p
	}
	m, err := c.provider.LanguageModel(ctx, c.Model)
	if err != nil {
		return nil, fmt.Errorf("language model %q: %w", c.Model, err)
	}
	c.model = m
	return m, nil
}

// toProviderConfigLocked snapshots the public fields into a providerConfig
// for buildProvider. Caller must hold c.mu. The HTTP client wires the debug
// hook in if one is installed.
func (c *Client) toProviderConfigLocked() providerConfig {
	cfg := providerConfig{
		provider: c.Provider,
		apiKey:   c.APIKey,
		baseURL:  c.BaseURL,
	}
	if c.DebugHook != nil {
		cfg.http = &http.Client{Transport: newDebugTransport(http.DefaultTransport, c.DebugHook)}
	}
	return cfg
}
