package mcp

import (
	"context"
	"encoding/json"
	"time"
)

// ToolCallStart describes an MCP tool invocation about to be dispatched.
// It is the payload passed to AuditHook.OnToolStart.
//
// Args is the same RawMessage the Authorizer saw — already JSON, not yet
// on the wire. Auditors that persist this value are responsible for
// running it through a redactor (see redact.go) before writing to the
// transcript or session JSONL.
type ToolCallStart struct {
	Server string
	Tool   string
	Args   json.RawMessage
	When   time.Time
}

// ToolCallEnd describes the result of an MCP tool invocation. Result is
// nil on error; Err is non-nil only on failure (transport error, server
// crash, denied by Authorizer, etc.). DurationMS is wall-clock from
// dispatch to result.
type ToolCallEnd struct {
	Server     string
	Tool       string
	Result     json.RawMessage
	Err        error
	DurationMS int64
	When       time.Time
}

// AuditHook receives lifecycle events for every MCP tool call the client
// manager dispatches. Implementations must be safe to call from the
// dispatcher's goroutine and must not block — hand the event off to a
// channel or buffer if persistence is slow.
//
// AuditHook is the seam for transcript wiring, future security review
// surfaces, and session-log emission. v1 ships a no-op default so
// bt-p6-client can call OnToolStart/OnToolEnd unconditionally without a
// nil check.
type AuditHook interface {
	OnToolStart(ctx context.Context, ev ToolCallStart)
	OnToolEnd(ctx context.Context, ev ToolCallEnd)
}

// NoopAuditHook discards every event. It is the default when the host
// does not register one. Behavior is identical to "MCP audit not wired",
// which means transcript/session files contain no MCP-specific lines —
// matching the contract's "no MCP UI when MCP is off" guarantee.
type NoopAuditHook struct{}

// OnToolStart implements AuditHook. It does nothing.
func (NoopAuditHook) OnToolStart(_ context.Context, _ ToolCallStart) {}

// OnToolEnd implements AuditHook. It does nothing.
func (NoopAuditHook) OnToolEnd(_ context.Context, _ ToolCallEnd) {}

// DefaultAuditHook returns the AuditHook used when the caller has not
// supplied one. It exists for the same reason as DefaultAuthorizer:
// give bt-p6-client and tests a single, stable reference for the v1
// default behavior.
func DefaultAuditHook() AuditHook {
	return NoopAuditHook{}
}
