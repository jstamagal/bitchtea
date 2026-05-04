package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"charm.land/fantasy"

	"github.com/jstamagal/bitchtea/internal/mcp"
	"github.com/jstamagal/bitchtea/internal/tools"
)

// mcpNamespacePrefix mirrors the const baked into internal/mcp/manager.go and
// docs/phase-6-mcp-contract.md ("Tool naming"). It is the only string that
// can appear at the front of an MCP-routed tool name; the local Registry
// never emits a tool with this prefix, which is what makes the local-tools-
// always-win collision rule trivial — see AssembleAgentTools.
const mcpNamespacePrefix = "mcp__"

// mcpAgentTool is the fantasy.AgentTool adapter for one MCP-served tool. It
// holds a back-reference to the Manager so Run can dispatch through the
// authorize → audit → server.CallTool pipeline (the contract-mandated path —
// see docs/phase-6-mcp-contract.md "Permission boundaries"). The schema is
// passed through verbatim from the MCP server, in keeping with the contract's
// "the server's JSON schema passed through as-is" rule.
//
// Run NEVER returns a Go error: every failure path is wrapped in
// fantasy.NewTextErrorResponse so the fantasy stream stays alive — same
// convention as bitchteaTool and the typed wrappers in this package.
type mcpAgentTool struct {
	manager *mcp.Manager
	info    fantasy.ToolInfo
}

func (t *mcpAgentTool) Info() fantasy.ToolInfo { return t.info }

func (t *mcpAgentTool) Run(ctx context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
	if t.manager == nil {
		return fantasy.NewTextErrorResponse("Error: mcp manager not configured"), nil
	}
	// Honor cancellation up front — keeps the audit/authorize chain off the
	// wire when the caller already gave up. Same pattern as the typed
	// wrappers (see typed_read.go).
	if err := ctx.Err(); err != nil {
		return fantasy.NewTextErrorResponse(fmt.Sprintf("Error: %v", err)), nil
	}

	res, err := t.manager.CallTool(ctx, call.Name, json.RawMessage(call.Input))
	if err != nil {
		return fantasy.NewTextErrorResponse(fmt.Sprintf("Error: %v", err)), nil
	}
	if res.IsError {
		return fantasy.NewTextErrorResponse(res.Content), nil
	}
	return fantasy.NewTextResponse(res.Content), nil
}

func (t *mcpAgentTool) ProviderOptions() fantasy.ProviderOptions     { return nil }
func (t *mcpAgentTool) SetProviderOptions(_ fantasy.ProviderOptions) {}

// MCPTools materializes one fantasy.AgentTool per MCP tool reported by
// manager.ListAllTools. The tool names are already namespaced by the manager
// as "mcp__<server>__<tool>" — MCPTools enforces the prefix as a defense in
// depth (a manager-level bug that produced an unprefixed name would otherwise
// slip through and shadow a local tool).
//
// Failure modes:
//   - manager == nil: returns (nil, nil). Calling code should treat this as
//     "MCP not opted in" — there is nothing to assemble.
//   - manager.ListAllTools error: returned alongside whatever tools loaded
//     successfully (best-effort aggregation matches manager.ListAllTools'
//     contract — schema errors drop only the offending tool).
//   - intra-MCP duplicate names: the first one wins, the rest are dropped
//     and logged. A well-behaved server set will never hit this; it exists
//     because two servers could in principle expose names that collide
//     after namespacing if a future change loosened ServerConfig validation.
//   - missing namespace prefix: dropped and logged. A manager that produced
//     such a name is buggy — but the rest of the catalog is still useful.
func MCPTools(ctx context.Context, manager *mcp.Manager) ([]fantasy.AgentTool, error) {
	if manager == nil {
		return nil, nil
	}
	listed, listErr := manager.ListAllTools(ctx)
	if len(listed) == 0 {
		return nil, listErr
	}

	out := make([]fantasy.AgentTool, 0, len(listed))
	seen := make(map[string]bool, len(listed))
	for _, nt := range listed {
		if !strings.HasPrefix(nt.Name, mcpNamespacePrefix) {
			// Manager bug — should be unreachable given namespaceName in
			// internal/mcp/manager.go, but the prefix is the load-bearing
			// invariant for collision safety so guard it here too.
			log.Printf("mcp: dropping tool %q from server %q: missing %q prefix", nt.Name, nt.Server, mcpNamespacePrefix)
			continue
		}
		if seen[nt.Name] {
			log.Printf("mcp: dropping duplicate tool %q (first registration wins)", nt.Name)
			continue
		}
		seen[nt.Name] = true

		params, required := splitMCPSchema(nt.Tool.InputSchema)
		out = append(out, &mcpAgentTool{
			manager: manager,
			info: fantasy.ToolInfo{
				Name:        nt.Name,
				Description: nt.Tool.Description,
				Parameters:  params,
				Required:    required,
				Parallel:    false,
			},
		})
	}
	return out, listErr
}

// AssembleAgentTools returns the merged tool surface for one fantasy turn:
// every local Registry tool first, then every MCP tool. Local tools always
// win on a name collision because (1) the local list is appended first and
// (2) any MCP name collides only on the "mcp__"-prefixed shape, which the
// local Registry never produces.
//
// The dedup pass below catches the impossible-but-defended case of a local
// tool whose name happens to start with "mcp__": that local tool is kept
// (the local-wins rule still applies), and any MCP tool with the same name
// is dropped and logged. If a future refactor adds an "mcp__"-prefixed
// local tool by mistake, this guard surfaces the conflict deterministically
// rather than silently double-registering.
//
// When mcpTools is nil/empty (the MCP-disabled default), the result is the
// same slice translateTools would have produced on its own — i.e. the
// behavior with no manager configured is identical to today.
func AssembleAgentTools(reg *tools.Registry, mcpTools []fantasy.AgentTool) []fantasy.AgentTool {
	local := translateTools(reg)
	if len(mcpTools) == 0 {
		return local
	}

	out := make([]fantasy.AgentTool, 0, len(local)+len(mcpTools))
	seen := make(map[string]bool, len(local)+len(mcpTools))
	for _, t := range local {
		name := t.Info().Name
		if seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, t)
	}
	for _, t := range mcpTools {
		name := t.Info().Name
		if !strings.HasPrefix(name, mcpNamespacePrefix) {
			log.Printf("mcp: dropping tool %q: missing %q prefix", name, mcpNamespacePrefix)
			continue
		}
		if seen[name] {
			// Local-wins. With the contract's "mcp__" prefix this is only
			// reachable if a local tool somehow grew an "mcp__" name, or
			// if MCPTools' own dedup let a duplicate slip; either way,
			// dropping the MCP entry preserves the local surface.
			log.Printf("mcp: dropping tool %q: name collides with local registry", name)
			continue
		}
		seen[name] = true
		out = append(out, t)
	}
	return out
}

// splitMCPSchema is the MCP-side analogue of splitSchema in tools.go. It
// accepts a raw JSON Schema document (per the MCP spec the `inputSchema`
// field is always an object schema) and returns the (properties, required)
// pair fantasy.ToolInfo wants. Anything that doesn't parse as a JSON object
// is treated as "no parameters" rather than failing — the contract's
// failure rule for an unparseable schema is "drop that one tool", but at
// this layer the tool name is already in flight, so a tolerant fallback
// keeps the call surface available rather than nuking it.
func splitMCPSchema(raw json.RawMessage) (map[string]any, []string) {
	if len(raw) == 0 {
		return map[string]any{}, nil
	}
	var schema map[string]any
	if err := json.Unmarshal(raw, &schema); err != nil {
		return map[string]any{}, nil
	}
	return splitSchema(schema)
}
