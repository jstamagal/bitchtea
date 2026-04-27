package llm

import (
	"context"

	"github.com/jstamagal/bitchtea/internal/tools"
)

type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function FunctionCall `json:"function"`
}

type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// ToolDef and ToolFuncDef live in internal/tools to keep the dependency edge
// llm → tools one-way (internal/llm/tools.go imports Registry, and Registry's
// Definitions() returns []tools.ToolDef). The aliases below preserve the
// existing public surface so callers that wrote llm.ToolDef keep compiling.
type ToolDef = tools.ToolDef
type ToolFuncDef = tools.ToolFuncDef

// StreamEvent is emitted on the channel passed to ChatStreamer.StreamChat.
// Type is one of: "text", "thinking", "tool_call", "tool_result", "usage",
// "error", "done". The "done" event carries the rebuilt transcript in
// Messages — the agent layer appends those to its own message log.
type StreamEvent struct {
	Type       string
	Text       string
	ToolName   string
	ToolArgs   string
	ToolCallID string
	Usage      *TokenUsage
	Error      error
	Messages   []Message
}

// TokenUsage holds the token counts reported by a provider for one response.
// Cache fields are populated when the provider reports them (Anthropic, etc.)
// and zero otherwise.
type TokenUsage struct {
	InputTokens         int
	OutputTokens        int
	CacheCreationTokens int
	CacheReadTokens     int
}

// DebugInfo is passed to a Client.DebugHook before/after each upstream HTTP
// request the provider makes. ResponseBody is "(stream)" for SSE responses.
type DebugInfo struct {
	Method          string
	URL             string
	RequestHeaders  map[string][]string
	ResponseHeaders map[string][]string
	RequestBody     string
	ResponseBody    string
	StatusCode      int
}

// ChatStreamer is the minimal streaming surface used by the agent loop.
// Implementations must close events when the turn ends (or send an "error"
// or "done" event followed by close).
//
// The reg parameter is the live tools.Registry for this turn. It may be nil
// for tool-less turns (e.g., compaction summaries). Passing the Registry
// (rather than just []ToolDef) lets the implementation bind each tool's
// Run callback to Registry.Execute without a separate handshake.
type ChatStreamer interface {
	StreamChat(ctx context.Context, messages []Message, reg *tools.Registry, events chan<- StreamEvent)
}
