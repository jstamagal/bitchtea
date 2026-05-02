package llm

import (
	"context"
	"encoding/json"
	"fmt"

	"charm.land/fantasy"

	"github.com/jstamagal/bitchtea/internal/tools"
)

// This file is part of the Phase 2 fantasy migration (bt-p2-read-write). It
// provides a typed fantasy.NewAgentTool wrapper for the `write` tool. Other
// tools still flow through the generic bitchteaTool adapter in tools.go until
// their own per-tool tickets land (bt-p2-bash-memory). The wrapper preserves
// current write semantics exactly:
//
//   - path and content are both required
//   - parent directories are created automatically by execWrite
//   - existing files are overwritten without warning
//   - filesystem errors (mkdir, write) surface as model-visible tool errors
//
// All failure modes are wrapped via fantasy.NewTextErrorResponse — never as Go
// errors — so a bad tool call does not abort the fantasy stream. This mirrors
// the contract documented in internal/llm/typed_tool_harness_test.go.

// writeArgs mirrors the JSON shape advertised in tools.Registry.Definitions
// for the `write` tool. Both fields are required by the schema; the typed
// wrapper does not enforce that here — fantasy's reflective schema generator
// publishes Required, and missing fields just deserialize as their zero value.
type writeArgs struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// writeTool returns a typed fantasy.AgentTool for the `write` tool. The Run
// callback re-marshals its typed input back to JSON and dispatches into
// reg.Execute, keeping the actual write logic single-sourced in
// internal/tools/tools.go (per CLAUDE.md the dep edge is llm -> tools, so this
// is fine).
func writeTool(reg *tools.Registry) fantasy.AgentTool {
	const (
		name        = "write"
		description = "Write content to a file. Creates the file if it doesn't exist, overwrites if it does. Automatically creates parent directories."
	)
	return fantasy.NewAgentTool(
		name,
		description,
		func(ctx context.Context, in writeArgs, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			// Honor cancellation up front so a cancelled stream never touches
			// the filesystem. Surfaced as a text error response per the harness
			// contract — see TestTypedToolHarness_CancelledContextSurfaces*.
			if err := ctx.Err(); err != nil {
				return fantasy.NewTextErrorResponse(fmt.Sprintf("Error: %v", err)), nil
			}

			// Re-marshal the typed input back to JSON and hand it to the
			// existing Registry.Execute switch. The marshal cannot realistically
			// fail (in is a plain struct of strings), but if it ever did,
			// surface it as a tool error rather than a Go error.
			raw, err := json.Marshal(in)
			if err != nil {
				return fantasy.NewTextErrorResponse(fmt.Sprintf("Error: marshal write args: %v", err)), nil
			}

			out, err := reg.Execute(ctx, name, string(raw))
			if err != nil {
				return fantasy.NewTextErrorResponse(fmt.Sprintf("Error: %v", err)), nil
			}
			return fantasy.NewTextResponse(out), nil
		},
	)
}
