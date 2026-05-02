package llm

import (
	"context"
	"encoding/json"
	"fmt"

	"charm.land/fantasy"

	"github.com/jstamagal/bitchtea/internal/tools"
)

// This file is part of the Phase 2 fantasy migration (bt-p2-bash-memory). It
// provides a typed fantasy.NewAgentTool wrapper for the `search_memory` tool.
// Other tools still flow through the generic bitchteaTool adapter in tools.go
// until every per-tool ticket lands. The wrapper preserves current
// search_memory semantics exactly:
//
//   - query is required; limit is optional (execSearchMemory defaults internally)
//   - the search scope is owned by the Registry (Registry.SetScope, set per-turn
//     by the agent based on /join / /query state). The tool input does NOT
//     accept a scope override today — see execSearchMemory in
//     internal/tools/tools.go. Adding one here would silently extend the
//     schema beyond what the executor honors.
//   - empty results are a normal text response (RenderSearchResults emits a
//     "no matches" banner), NOT a tool error
//
// All failure modes are wrapped via fantasy.NewTextErrorResponse — never as Go
// errors — so a bad tool call does not abort the fantasy stream. This mirrors
// the contract documented in internal/llm/typed_tool_harness_test.go.

// searchMemoryArgs mirrors the JSON shape advertised in tools.Registry.Definitions
// for the `search_memory` tool. Limit zero means "use execSearchMemory's
// default" (the underlying memorypkg.SearchInScope handles a zero limit).
type searchMemoryArgs struct {
	Query string `json:"query"`
	Limit int    `json:"limit,omitempty"`
}

// searchMemoryTool returns a typed fantasy.AgentTool for the `search_memory`
// tool. The Run callback re-marshals its typed input back to JSON and
// dispatches into reg.Execute, which keeps the actual scope-resolution and
// rendering logic single-sourced in internal/tools/tools.go (per CLAUDE.md the
// dep edge is llm -> tools, so this is fine).
func searchMemoryTool(reg *tools.Registry) fantasy.AgentTool {
	const (
		name        = "search_memory"
		description = "Search the hot MEMORY.md file and durable daily markdown memory for past decisions, notes, and context relevant to the current worktree."
	)
	return fantasy.NewAgentTool(
		name,
		description,
		func(ctx context.Context, in searchMemoryArgs, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			// Honor cancellation up front so a cancelled stream never touches
			// the memory store. Surfaced as a text error response per the
			// harness contract — see TestTypedToolHarness_CancelledContextSurfaces*.
			if err := ctx.Err(); err != nil {
				return fantasy.NewTextErrorResponse(fmt.Sprintf("Error: %v", err)), nil
			}

			// Re-marshal the typed input back to JSON and hand it to the
			// existing Registry.Execute switch. The marshal cannot realistically
			// fail (in is a plain struct of string + int), but if it ever did,
			// surface it as a tool error rather than a Go error.
			raw, err := json.Marshal(in)
			if err != nil {
				return fantasy.NewTextErrorResponse(fmt.Sprintf("Error: marshal search_memory args: %v", err)), nil
			}

			out, err := reg.Execute(ctx, name, string(raw))
			if err != nil {
				return fantasy.NewTextErrorResponse(fmt.Sprintf("Error: %v", err)), nil
			}
			return fantasy.NewTextResponse(out), nil
		},
	)
}
