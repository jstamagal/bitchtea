package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

// AllowAllAuthorizer is the v1 default. It must permit every call;
// bt-p6-client relies on this to ship MCP without a stricter store
// landing first.
func TestAllowAllAuthorizer_AlwaysNil(t *testing.T) {
	a := AllowAllAuthorizer{}
	args := json.RawMessage(`{"path":"/tmp/x"}`)
	if err := a.Authorize(context.Background(), "fs", "read_file", args); err != nil {
		t.Fatalf("AllowAllAuthorizer should never deny; got %v", err)
	}
}

// DefaultAuthorizer is the seam future code should reach for; verify it
// hands back the same default behavior.
func TestDefaultAuthorizer_IsAllowAll(t *testing.T) {
	a := DefaultAuthorizer()
	if err := a.Authorize(context.Background(), "any", "any", nil); err != nil {
		t.Fatalf("DefaultAuthorizer should allow; got %v", err)
	}
}

// TestNoopAuditHook_DoesNotPanic verifies that the no-op audit hook tolerates
// zero-value events (bt-p6-client calls OnToolStart/OnToolEnd unconditionally)
// AND non-zero-value events without side effects.
func TestNoopAuditHook_DoesNotPanic(t *testing.T) {
	h := NoopAuditHook{}

	// Zero-value events: must not panic.
	h.OnToolStart(context.Background(), ToolCallStart{})
	h.OnToolEnd(context.Background(), ToolCallEnd{})

	// Non-zero-value events: must not panic and must not produce observable
	// side effects (the type has no fields that could change, so this is just
	// a smoke test that the method body doesn't dereference fields).
	start := ToolCallStart{
		Server: "test-server",
		Tool:   "read_file",
		Args:   json.RawMessage(`{"path":"/etc/passwd"}`),
		When:   time.Now(),
	}
	end := ToolCallEnd{
		Server:     "test-server",
		Tool:       "read_file",
		Result:     json.RawMessage(`{"content":"secret"}`),
		Err:        errors.New("denied"),
		DurationMS: 42,
		When:       time.Now(),
	}
	h.OnToolStart(context.Background(), start)
	h.OnToolEnd(context.Background(), end)

	// Verify DefaultAuditHook returns a usable NoopAuditHook.
	def := DefaultAuditHook()
	if def == nil {
		t.Fatal("DefaultAuditHook must not return nil")
	}
	def.OnToolStart(context.Background(), start)
	def.OnToolEnd(context.Background(), end)
}
