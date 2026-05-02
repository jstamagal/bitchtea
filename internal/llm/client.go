package llm

import (
	"context"
	"fmt"
	"net/http"
	"sync"

	"charm.land/fantasy"

	"github.com/jstamagal/bitchtea/internal/mcp"
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

	// Service is the upstream service identity ("anthropic", "openai",
	// "ollama", "openrouter", "zai-anthropic", ...). Used as a per-service
	// behavior gate (e.g. Anthropic prompt-cache markers). Empty string
	// means "no per-service gating", which is treated as off for every
	// gated feature. Mutating Service does NOT invalidate the cached
	// provider — the wire format is determined by Provider.
	Service string

	// BootstrapMsgCount mirrors agent.Agent.bootstrapMsgCount and is the
	// count of session-start messages (system prompt + AGENTS.md/CLAUDE.md
	// + persona anchor) that form the longest stable prefix in the
	// conversation. The Anthropic cache marker rides on the last surviving
	// bootstrap message in fantasy's `prior` slice. Zero means "no
	// bootstrap known" and disables marker placement.
	BootstrapMsgCount int

	// DebugHook is invoked for each upstream HTTP request when non-nil.
	// To set/clear it at runtime, use SetDebugHook (the cached provider
	// must rebuild because its HTTP client wraps this hook).
	DebugHook func(DebugInfo)

	mu       sync.Mutex
	provider fantasy.Provider      // cached, nil until first ensureModel
	model    fantasy.LanguageModel // cached, nil until first ensureModel

	// mcpManager, when non-nil, supplies MCP tools that are appended after
	// the local Registry tools on every Stream call. nil means "MCP not
	// opted in" — behavior matches pre-Phase-6. Wiring is owned by the
	// agent bootstrap (Agent.SetMCPManager), not by this package.
	mcpManager *mcp.Manager

	// toolCtx holds the per-turn ToolContextManager. Set at the start of
	// each streamOnce call, cleared when the turn ends. The agent reads
	// it to expose CancelTool to the UI.
	toolCtx *ToolContextManager

	// promptDrain is an optional hook that PrepareStep calls on every step
	// to drain queued user prompts mid-turn. Set by the agent via
	// SetPromptDrain before the first StreamChat call.
	promptDrain func() []string
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

// SetService updates the upstream service identity used for per-service
// behavior gates (e.g. Anthropic prompt-cache markers). Does not invalidate
// the cached provider because Service is consumed inside PrepareStep, not
// during provider construction.
func (c *Client) SetService(service string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Service = service
}

// SetBootstrapMsgCount mirrors the agent's bootstrap-message count into the
// client so PrepareStep can place the Anthropic cache marker on the last
// surviving bootstrap message. Cheap to call before every StreamChat — there
// is no provider invalidation.
func (c *Client) SetBootstrapMsgCount(n int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.BootstrapMsgCount = n
}

// InjectLanguageModelForTesting replaces the cached LanguageModel with a
// caller-supplied one. This is the public seam used by cross-package smoke
// tests (e.g. internal/agent) that need a fake fantasy.LanguageModel without
// reaching into the unexported model field. Production code MUST NOT call
// this; the contract is "set it before any StreamChat call, never mid-flight".
// It does not invalidate the provider — by design, since the only caller is
// test code that wants the fake to outlive subsequent Set* calls.
func (c *Client) InjectLanguageModelForTesting(model fantasy.LanguageModel) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.model = model
}

// SetPromptDrain installs a queue-drain hook that PrepareStep calls on
// every step. The function drains all pending user prompts and returns
// them; the returned strings will be appended to prepared.Messages as
// synthetic user messages. Pass nil to disable mid-turn drain.
func (c *Client) SetPromptDrain(fn func() []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.promptDrain = fn
}

// SetMCPManager installs (or clears) the MCP manager whose tools will be
// merged into every subsequent StreamChat call. Pass nil to disable MCP
// dispatch (and revert to the local-only tool surface). Cheap to call —
// no provider invalidation is needed because the manager is consumed
// inside streamOnce, not during provider construction.
func (c *Client) SetMCPManager(m *mcp.Manager) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.mcpManager = m
}

// MCPManager returns the currently installed MCP manager, or nil if none.
// Used by streamOnce when assembling the per-turn tool list and by tests
// that want to assert the wiring without reaching into private state.
func (c *Client) MCPManager() *mcp.Manager {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.mcpManager
}

// ToolContextManager returns the per-turn tool context manager for the
// currently active stream, or nil if no stream is active. The agent uses
// this to expose CancelTool to the UI.
func (c *Client) ToolContextManager() *ToolContextManager {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.toolCtx
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
