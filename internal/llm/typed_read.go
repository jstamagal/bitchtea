package llm

import (
	"context"
	"encoding/json"
	"fmt"

	"charm.land/fantasy"

	"github.com/jstamagal/bitchtea/internal/tools"
)

// This file is part of the Phase 2 fantasy migration (bt-p2-read-write). It
// provides a typed fantasy.NewAgentTool wrapper for the `read` tool. Other
// tools still flow through the generic bitchteaTool adapter in tools.go until
// their own per-tool tickets land (bt-p2-bash-memory). The wrapper preserves
// current read semantics exactly:
//
//   - path is required; offset/limit are optional (1-indexed line slicing)
//   - past-EOF offset surfaces the bt-hnh "offset N is past end of file" error
//     as a model-visible tool error
//   - missing files surface as tool errors with the path in the message
//   - oversized output is truncated by the underlying execRead, not here
//
// All failure modes are wrapped via fantasy.NewTextErrorResponse — never as Go
// errors — so a bad tool call does not abort the fantasy stream. This mirrors
// the contract documented in internal/llm/typed_tool_harness_test.go.

// readArgs mirrors the JSON shape advertised in tools.Registry.Definitions for
// the `read` tool. Offset and Limit are optional ints; zero means "use the
// whole file" (matches execRead's existing branching). Field names are
// lowercase JSON to match the historical schema and recorded sessions.
type readArgs struct {
	Path   string `json:"path"`
	Offset int    `json:"offset,omitempty"`
	Limit  int    `json:"limit,omitempty"`
}

// readTool returns a typed fantasy.AgentTool for the `read` tool. The Run
// callback re-marshals its typed input back to JSON and dispatches into
// reg.Execute, keeping the actual read logic single-sourced in
// internal/tools/tools.go (per CLAUDE.md the dep edge is llm -> tools, so this
// is fine). Any execRead bug fix (e.g. bt-hnh's past-EOF handling) automatically
// applies here.
func readTool(reg *tools.Registry) fantasy.AgentTool {
	const (
		name        = "read"
		description = "Read the contents of a file. For text files, returns content. Supports offset/limit for large files."
	)
	return fantasy.NewAgentTool(
		name,
		description,
		func(ctx context.Context, in readArgs, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			// Honor cancellation up front so a cancelled stream never touches
			// the filesystem. Surfaced as a text error response per the harness
			// contract — see TestTypedToolHarness_CancelledContextSurfaces*.
			if err := ctx.Err(); err != nil {
				return fantasy.NewTextErrorResponse(fmt.Sprintf("Error: %v", err)), nil
			}

			// Re-marshal the typed input back to JSON and hand it to the
			// existing Registry.Execute switch. The marshal cannot realistically
			// fail (in is a plain struct of string + ints), but if it ever did,
			// surface it as a tool error rather than a Go error.
			raw, err := json.Marshal(in)
			if err != nil {
				return fantasy.NewTextErrorResponse(fmt.Sprintf("Error: marshal read args: %v", err)), nil
			}

			out, err := reg.Execute(ctx, name, string(raw))
			if err != nil {
				return fantasy.NewTextErrorResponse(fmt.Sprintf("Error: %v", err)), nil
			}
			return fantasy.NewTextResponse(out), nil
		},
	)
}
