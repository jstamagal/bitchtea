package agent

import "github.com/jstamagal/bitchtea/internal/agent/event"

// Event and State are re-exported here as aliases so existing callers
// (internal/ui, tests, main) keep compiling unchanged. New code in
// internal/llm imports the event package directly to avoid an
// llm -> agent dependency cycle.
type (
	Event = event.Event
	State = event.State
)

const (
	StateIdle     = event.StateIdle
	StateThinking = event.StateThinking
	StateToolCall = event.StateToolCall
)
