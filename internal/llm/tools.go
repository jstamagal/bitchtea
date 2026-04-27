package llm

import (
	"context"
	"fmt"

	"charm.land/fantasy"

	"github.com/jstamagal/bitchtea/internal/tools"
)

// bitchteaTool wraps an entry from internal/tools.Registry as a fantasy.AgentTool.
// One instance per tool definition; Run dispatches into Registry.Execute by name.
// ProviderOptions/SetProviderOptions are no-ops because we don't pipe provider-
// specific options through tools (yet).
type bitchteaTool struct {
	info fantasy.ToolInfo
	reg  *tools.Registry
}

func (t *bitchteaTool) Info() fantasy.ToolInfo { return t.info }

// Run executes the underlying tool. A Go error returned here aborts the entire
// fantasy stream — for "this tool failed but keep the conversation alive" we
// wrap the error in NewTextErrorResponse and return nil.
func (t *bitchteaTool) Run(ctx context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
	out, err := t.reg.Execute(ctx, call.Name, call.Input)
	if err != nil {
		return fantasy.NewTextErrorResponse(fmt.Sprintf("Error: %v", err)), nil
	}
	return fantasy.NewTextResponse(out), nil
}

func (t *bitchteaTool) ProviderOptions() fantasy.ProviderOptions        { return nil }
func (t *bitchteaTool) SetProviderOptions(_ fantasy.ProviderOptions)    {}

// translateTools builds one *bitchteaTool per Registry definition. Called per
// stream-call (cheap — definitions are static, only the Required slice varies).
func translateTools(reg *tools.Registry) []fantasy.AgentTool {
	defs := reg.Definitions()
	out := make([]fantasy.AgentTool, 0, len(defs))
	for _, d := range defs {
		params, required := splitSchema(d.Function.Parameters)
		out = append(out, &bitchteaTool{
			info: fantasy.ToolInfo{
				Name:        d.Function.Name,
				Description: d.Function.Description,
				Parameters:  params,
				Required:    required,
				Parallel:    false,
			},
			reg: reg,
		})
	}
	return out
}

// splitSchema separates the "required" field out of a JSON-schema parameters
// map. Returns (paramsCopy, required) where paramsCopy is the original map
// minus the "required" key, and required is the []string list (or nil).
//
// Defensive: tools.Registry stores required as []string, but anything coming
// in via json.Unmarshal would be []any — handle both.
func splitSchema(params map[string]any) (map[string]any, []string) {
	if params == nil {
		return nil, nil
	}
	out := make(map[string]any, len(params))
	for k, v := range params {
		if k == "required" {
			continue
		}
		out[k] = v
	}
	raw, ok := params["required"]
	if !ok {
		return out, nil
	}
	switch r := raw.(type) {
	case []string:
		return out, r
	case []any:
		req := make([]string, 0, len(r))
		for _, x := range r {
			if s, ok := x.(string); ok {
				req = append(req, s)
			}
		}
		return out, req
	}
	return out, nil
}
