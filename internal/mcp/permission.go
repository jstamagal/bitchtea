package mcp

import (
	"context"
	"encoding/json"
)

// Authorizer is consulted by the MCP client manager (bt-p6-client) BEFORE
// every MCP tool dispatch, including reconnect retries. Built-in tools
// never go through it.
//
// The interface is intentionally tiny so a future per-server / per-tool
// allow/deny store and a prompt-on-first-use UX can both implement it
// without taking on extra surface area.
//
// Returning a non-nil error denies the call. The returned error is
// surfaced to the model verbatim, so implementations should produce a
// short, model-readable string (e.g. "mcp: fs:write_file denied by
// policy"). Returning nil authorizes the call.
//
// The args parameter is the resolved JSON object the model produced for
// this tool call, after schema validation but before it reaches the
// wire. Implementations may inspect args to enforce shape gates.
type Authorizer interface {
	Authorize(ctx context.Context, server string, tool string, args json.RawMessage) error
}

// AllowAllAuthorizer is the v1 default: it permits every MCP tool call
// the model requests, provided MCP is enabled in config. A stricter
// policy lands in a follow-up task; we ship the interface now so
// bt-p6-client can wire to it without later churn.
//
// Default-allow is documented in docs/phase-6-mcp-contract.md as the v1
// stance: opting in by enabling MCP at all is the trust gate.
type AllowAllAuthorizer struct{}

// Authorize always returns nil.
func (AllowAllAuthorizer) Authorize(_ context.Context, _ string, _ string, _ json.RawMessage) error {
	return nil
}

// DefaultAuthorizer returns the Authorizer used when the caller has not
// supplied one. It exists so tests and bt-p6-client share a single
// "v1 default" reference rather than each constructing AllowAllAuthorizer{}
// inline (which would make tightening the default a multi-site change).
func DefaultAuthorizer() Authorizer {
	return AllowAllAuthorizer{}
}
