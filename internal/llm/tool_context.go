package llm

import (
	"context"
	"fmt"
	"sync"

	"charm.land/fantasy"
)

// ToolContextManager tracks per-tool-call contexts derived from the turn
// context. Each tool call gets its own child context so one tool can be
// cancelled without killing the entire fantasy stream.
//
// Callers use NewToolContext to derive a child context before each tool Run,
// and CancelTool to cancel a specific active tool by its call ID.
type ToolContextManager struct {
	mu      sync.Mutex
	turnCtx context.Context
	tools   map[string]context.CancelFunc // toolCallID → cancel
}

// NewToolContextManager creates a manager rooted at the given turn context.
func NewToolContextManager(turnCtx context.Context) *ToolContextManager {
	return &ToolContextManager{
		turnCtx: turnCtx,
		tools:   make(map[string]context.CancelFunc),
	}
}

// NewToolContext derives a child context for a specific tool call. The returned
// context is cancelled when either the turn context is cancelled or CancelTool
// is called with the same toolCallID. The cancel function is stored internally
// and removed when the caller invokes the returned cleanup function.
//
// If the turn context is already done, returns the turn context itself and a
// no-op cleanup.
func (m *ToolContextManager) NewToolContext(toolCallID string) (context.Context, func()) {
	if m.turnCtx.Err() != nil {
		return m.turnCtx, func() {}
	}

	ctx, cancel := context.WithCancel(m.turnCtx)

	m.mu.Lock()
	m.tools[toolCallID] = cancel
	m.mu.Unlock()

	cleanup := func() {
		cancel()
		m.mu.Lock()
		delete(m.tools, toolCallID)
		m.mu.Unlock()
	}
	return ctx, cleanup
}

// CancelTool cancels the context for a specific tool call. Returns an error if
// no active tool with that ID exists.
func (m *ToolContextManager) CancelTool(toolCallID string) error {
	m.mu.Lock()
	cancel, ok := m.tools[toolCallID]
	m.mu.Unlock()

	if !ok {
		return fmt.Errorf("no active tool with id %s", toolCallID)
	}
	cancel()
	return nil
}

// CancelAll cancels every active tool context. Used for cleanup on turn end.
func (m *ToolContextManager) CancelAll() {
	m.mu.Lock()
	for _, cancel := range m.tools {
		cancel()
	}
	m.tools = make(map[string]context.CancelFunc)
	m.mu.Unlock()
}

// ActiveToolIDs returns the IDs of currently active (not yet cleaned up) tool
// calls. Useful for UI display.
func (m *ToolContextManager) ActiveToolIDs() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	ids := make([]string, 0, len(m.tools))
	for id := range m.tools {
		ids = append(ids, id)
	}
	return ids
}

// toolContextWrapper wraps a fantasy.AgentTool to derive a per-tool-call
// context from the ToolContextManager before delegating to the inner tool's
// Run method.
type toolContextWrapper struct {
	inner fantasy.AgentTool
	mgr   *ToolContextManager
}

func (w *toolContextWrapper) Info() fantasy.ToolInfo {
	return w.inner.Info()
}

func (w *toolContextWrapper) Run(ctx context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
	toolCtx, cleanup := w.mgr.NewToolContext(call.ID)
	defer cleanup()
	return w.inner.Run(toolCtx, call)
}

func (w *toolContextWrapper) ProviderOptions() fantasy.ProviderOptions {
	return w.inner.ProviderOptions()
}

func (w *toolContextWrapper) SetProviderOptions(opts fantasy.ProviderOptions) {
	w.inner.SetProviderOptions(opts)
}

// wrapToolsWithContext wraps each tool so its Run method derives a per-call
// context from the manager. The turn context (parent) flows through fantasy's
// fa.Stream call; the wrapper creates a child context per tool call that can
// be cancelled independently via mgr.CancelTool(toolCallID).
func wrapToolsWithContext(tools []fantasy.AgentTool, mgr *ToolContextManager) []fantasy.AgentTool {
	out := make([]fantasy.AgentTool, len(tools))
	for i, t := range tools {
		out[i] = &toolContextWrapper{inner: t, mgr: mgr}
	}
	return out
}
