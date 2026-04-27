// Package event holds the Event and State types emitted by the agent loop
// to the UI. It lives in its own package so that internal/llm can import
// these types without creating an llm -> agent dependency cycle.
package event

// State represents the agent's current state.
type State int

const (
	StateIdle     State = iota
	StateThinking       // waiting for LLM response
	StateToolCall       // executing a tool
)

// Event is emitted by the agent loop to drive the UI.
//
// Type values: "text", "tool_start", "tool_result", "thinking", "done",
// "error", "state".
type Event struct {
	Type string

	Text string // for text events (streamed tokens)

	ToolName   string // for tool events
	ToolArgs   string
	ToolResult string
	ToolError  error

	State State // for state events
	Error error // for error events
}
