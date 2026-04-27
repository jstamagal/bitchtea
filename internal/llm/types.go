package llm

import "context"

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

type ToolDef struct {
	Type     string      `json:"type"`
	Function ToolFuncDef `json:"function"`
}

type ToolFuncDef struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"`
}

// StreamEvent is emitted on the channel passed to ChatStreamer.StreamChat.
// Type is one of: "text", "thinking", "tool_call", "usage", "error", "done".
type StreamEvent struct {
	Type       string
	Text       string
	ToolName   string
	ToolArgs   string
	ToolCallID string
	Usage      *TokenUsage
	Error      error
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
type ChatStreamer interface {
	StreamChat(ctx context.Context, messages []Message, tools []ToolDef, events chan<- StreamEvent)
}
