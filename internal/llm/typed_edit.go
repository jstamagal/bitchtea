package llm

import (
	"context"
	"encoding/json"
	"fmt"

	"charm.land/fantasy"

	"github.com/jstamagal/bitchtea/internal/tools"
)

// This file is part of the Phase 2 fantasy migration (bt-p2-edit). It provides
// a typed fantasy.NewAgentTool wrapper for the `edit` tool. Other tools still
// flow through the generic bitchteaTool adapter in tools.go until their own
// per-tool tickets land (bt-p2-read-write, bt-p2-bash-memory). The wrapper here
// preserves the current edit semantics exactly:
//
//   - exact-match replacement, single occurrence required
//   - empty oldText is rejected (use the write tool instead)
//   - non-unique matches are rejected
//   - file-not-found / write errors surface as model-visible tool errors
//
// All failure modes are wrapped via fantasy.NewTextErrorResponse — never as Go
// errors — so a bad tool call does not abort the fantasy stream. This mirrors
// the contract documented in internal/llm/typed_tool_harness_test.go.

// editArgs mirrors the JSON shape advertised in tools.Registry.Definitions for
// the `edit` tool. There is intentionally no replace_all field today — the
// current implementation requires a unique match. Adding a flag here would
// silently extend the schema beyond what internal/tools/tools.go honors.
type editArgs struct {
	Path  string         `json:"path"`
	Edits []editArgsItem `json:"edits"`
}

// editArgsItem is one (oldText, newText) pair. Field names match the existing
// JSON schema exactly (camelCase), so historical tool calls and recorded
// sessions deserialize unchanged.
type editArgsItem struct {
	OldText string `json:"oldText"`
	NewText string `json:"newText"`
}

// editTool returns a typed fantasy.AgentTool for the `edit` tool. The Run
// callback re-marshals its typed input back to JSON and dispatches into
// reg.Execute, which keeps the actual edit logic single-sourced in
// internal/tools/tools.go (per CLAUDE.md the dep edge is llm -> tools, so this
// is fine). The cost — one extra JSON round-trip per call — is negligible
// against an LLM round-trip; the upside is that bug fixes to execEdit (e.g.
// bt-z4d's "oldText must not be empty" message) automatically apply here.
func editTool(reg *tools.Registry) fantasy.AgentTool {
	const (
		name        = "edit"
		description = "Edit a file by replacing exact text matches. Each edit replaces oldText with newText."
	)
	return fantasy.NewAgentTool(
		name,
		description,
		func(ctx context.Context, in editArgs, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			// Honor cancellation up front so a cancelled stream never touches
			// the filesystem. Surfaced as a text error response per the harness
			// contract — see TestTypedToolHarness_CancelledContextSurfaces*.
			if err := ctx.Err(); err != nil {
				return fantasy.NewTextErrorResponse(fmt.Sprintf("Error: %v", err)), nil
			}

			// Re-marshal the typed input back to JSON and hand it to the
			// existing Registry.Execute switch. This avoids duplicating the
			// edit semantics in two places. The marshal cannot realistically
			// fail (in is a plain struct of strings/slices), but if it ever
			// did, surface it as a tool error instead of a Go error.
			raw, err := json.Marshal(in)
			if err != nil {
				return fantasy.NewTextErrorResponse(fmt.Sprintf("Error: marshal edit args: %v", err)), nil
			}

			out, err := reg.Execute(ctx, name, string(raw))
			if err != nil {
				return fantasy.NewTextErrorResponse(fmt.Sprintf("Error: %v", err)), nil
			}
			return fantasy.NewTextResponse(out), nil
		},
	)
}
