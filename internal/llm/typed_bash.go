package llm

import (
	"context"
	"encoding/json"
	"fmt"

	"charm.land/fantasy"

	"github.com/jstamagal/bitchtea/internal/tools"
)

// This file is part of the Phase 2 fantasy migration (bt-p2-bash-memory). It
// provides a typed fantasy.NewAgentTool wrapper for the `bash` tool. Other
// tools still flow through the generic bitchteaTool adapter in tools.go until
// every per-tool ticket lands. The wrapper preserves current bash semantics
// exactly:
//
//   - command is required; timeout is optional (default 30s, applied by execBash)
//   - non-zero exit codes return as a normal text response that includes the
//     "Exit code: N" suffix the model relies on (NOT a tool error)
//   - parent-context cancellation surfaces the bt-91b "command cancelled"
//     wording as a tool error (distinct from timeout)
//   - tool-level timeout surfaces the bt-91b "command timed out after Ns"
//     wording as a tool error (distinct from cancel)
//   - oversized stdout/stderr is truncated at a UTF-8 rune boundary by execBash
//     (bt-71q) before reaching this wrapper — we don't re-truncate
//   - bash is invoked with `bash -c` (bt-h1u), not `-lc`; we don't override that
//
// All failure modes are wrapped via fantasy.NewTextErrorResponse — never as Go
// errors — so a bad tool call does not abort the fantasy stream. This mirrors
// the contract documented in internal/llm/typed_tool_harness_test.go.

// bashArgs mirrors the JSON shape advertised in tools.Registry.Definitions for
// the `bash` tool. Timeout is in seconds; zero means "use the execBash default
// (30s)" and matches the existing branching in execBash. There is intentionally
// no working_dir field — the registry's WorkDir is the only directory bash
// runs in, and exposing an override would break the current tool surface.
type bashArgs struct {
	Command string `json:"command"`
	Timeout int    `json:"timeout,omitempty"`
}

// bashTool returns a typed fantasy.AgentTool for the `bash` tool. The Run
// callback re-marshals its typed input back to JSON and dispatches into
// reg.Execute, which keeps the actual subprocess logic single-sourced in
// internal/tools/tools.go (per CLAUDE.md the dep edge is llm -> tools, so this
// is fine). Bug fixes to execBash (bt-91b cancel/timeout split, bt-71q UTF-8
// truncation, bt-h1u `-c` not `-lc`) automatically apply here.
func bashTool(reg *tools.Registry) fantasy.AgentTool {
	const (
		name        = "bash"
		description = "Execute a bash command. Returns stdout and stderr."
	)
	return fantasy.NewAgentTool(
		name,
		description,
		func(ctx context.Context, in bashArgs, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			// Honor cancellation up front so a cancelled stream never spawns a
			// subprocess. Surfaced as a text error response per the harness
			// contract — see TestTypedToolHarness_CancelledContextSurfaces*.
			if err := ctx.Err(); err != nil {
				return fantasy.NewTextErrorResponse(fmt.Sprintf("Error: %v", err)), nil
			}

			// Re-marshal the typed input back to JSON and hand it to the
			// existing Registry.Execute switch. The marshal cannot realistically
			// fail (in is a plain struct of string + int), but if it ever did,
			// surface it as a tool error instead of a Go error.
			raw, err := json.Marshal(in)
			if err != nil {
				return fantasy.NewTextErrorResponse(fmt.Sprintf("Error: marshal bash args: %v", err)), nil
			}

			out, err := reg.Execute(ctx, name, string(raw))
			if err != nil {
				// execBash returns a Go error for cancel/timeout/start-failure
				// paths; non-zero inner exit is NOT an error (it embeds an
				// "Exit code: N" suffix in out). Surface every Go error as a
				// tool error so the fantasy stream stays alive.
				return fantasy.NewTextErrorResponse(fmt.Sprintf("Error: %v", err)), nil
			}
			return fantasy.NewTextResponse(out), nil
		},
	)
}
