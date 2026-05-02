package mcp

import (
	"context"
	"encoding/json"
	"testing"
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

// NoopAuditHook must not panic with zero-value events. bt-p6-client
// calls OnToolStart/OnToolEnd unconditionally so a panicking default
// would be a footgun.
func TestNoopAuditHook_DoesNotPanic(t *testing.T) {
	h := NoopAuditHook{}
	h.OnToolStart(context.Background(), ToolCallStart{})
	h.OnToolEnd(context.Background(), ToolCallEnd{})
}
