package llm

import (
	"context"
	"encoding/json"
	"fmt"

	"charm.land/fantasy"

	"github.com/jstamagal/bitchtea/internal/tools"
)

// This file is part of the Phase 2 fantasy migration (bt-p2-switch). It
// provides a typed fantasy.NewAgentTool wrapper for the `write_memory` tool
// (the sibling of search_memory; added to the Registry surface by bt-vhs).
// Other tools that have not yet been ported still flow through the generic
// bitchteaTool compatibility adapter in tools.go. The wrapper preserves
// current write_memory semantics exactly:
//
//   - content is required; everything else is optional
//   - title is an optional markdown heading prepended to the entry
//   - scope override accepts "" / "current" (use Registry.Scope), "root",
//     "channel", or "query"; "channel" and "query" require name=#chan / nick
//   - daily=true appends to the durable per-day archive instead of the hot
//     MEMORY.md (matches execWriteMemory's branching)
//   - the Registry's per-turn scope (Registry.SetScope) is the default scope
//     when no override is provided
//
// All failure modes are wrapped via fantasy.NewTextErrorResponse — never as Go
// errors — so a bad tool call does not abort the fantasy stream. This mirrors
// the contract documented in internal/llm/typed_tool_harness_test.go.

// writeMemoryArgs mirrors the JSON shape advertised in tools.Registry.Definitions
// for the `write_memory` tool. Only Content is required; the others are
// optional and pass through to execWriteMemory's switch on Scope.
type writeMemoryArgs struct {
	Content string `json:"content"`
	Title   string `json:"title,omitempty"`
	Scope   string `json:"scope,omitempty"`
	Name    string `json:"name,omitempty"`
	Daily   bool   `json:"daily,omitempty"`
}

// writeMemoryTool returns a typed fantasy.AgentTool for the `write_memory`
// tool. The Run callback re-marshals its typed input back to JSON and
// dispatches into reg.Execute, keeping the actual scope-resolution and
// memory-append logic single-sourced in internal/tools/tools.go (per CLAUDE.md
// the dep edge is llm -> tools, so this is fine).
func writeMemoryTool(reg *tools.Registry) fantasy.AgentTool {
	const (
		name        = "write_memory"
		description = "Persist a memory entry (decision, preference, work-state note) into hot memory for the current scope, or override with scope='root' or a specific channel/query. Use 'daily' to append to the durable daily archive instead. Appended as a dated markdown section so search_memory can recall it later."
	)
	return fantasy.NewAgentTool(
		name,
		description,
		func(ctx context.Context, in writeMemoryArgs, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			// Honor cancellation up front so a cancelled stream never touches
			// the memory store. Surfaced as a text error response per the
			// harness contract — see TestTypedToolHarness_CancelledContextSurfaces*.
			if err := ctx.Err(); err != nil {
				return fantasy.NewTextErrorResponse(fmt.Sprintf("Error: %v", err)), nil
			}

			// Re-marshal the typed input back to JSON and hand it to the
			// existing Registry.Execute switch. The marshal cannot realistically
			// fail (in is plain strings + a bool), but if it ever did, surface
			// it as a tool error rather than a Go error.
			raw, err := json.Marshal(in)
			if err != nil {
				return fantasy.NewTextErrorResponse(fmt.Sprintf("Error: marshal write_memory args: %v", err)), nil
			}

			out, err := reg.Execute(ctx, name, string(raw))
			if err != nil {
				return fantasy.NewTextErrorResponse(fmt.Sprintf("Error: %v", err)), nil
			}
			if isStructuredToolError(out) {
				return fantasy.NewTextErrorResponse(out), nil
			}
			return fantasy.NewTextResponse(out), nil
		},
	)
}
