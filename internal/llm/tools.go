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

func (t *bitchteaTool) ProviderOptions() fantasy.ProviderOptions     { return nil }
func (t *bitchteaTool) SetProviderOptions(_ fantasy.ProviderOptions) {}

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

// splitSchema converts the OpenAI-style object schema from Registry.Definitions
// into fantasy.ToolInfo's shape: Parameters is only the "properties" map, while
// Required is carried separately. Passing the whole object schema would make
// fantasy nest "type", "properties", and "required" as bogus tool parameters.
//
// Defensive: tools.Registry stores required as []string, but anything coming
// in via json.Unmarshal would be []any — handle both.
func splitSchema(params map[string]any) (map[string]any, []string) {
	if params == nil {
		return nil, nil
	}

	if rawProperties, ok := params["properties"]; ok {
		out := sanitizeProperties(rawProperties)
		return out, parseRequired(params["required"], out)
	}

	if schemaType, ok := params["type"].(string); ok {
		if schemaType != "object" {
			return map[string]any{}, nil
		}
		return map[string]any{}, nil
	}

	if rawRequired := params["required"]; rawRequired != nil {
		if _, ok := rawRequired.(map[string]any); !ok {
			return map[string]any{}, nil
		}
	}

	out := make(map[string]any, len(params))
	for k, v := range params {
		if property, ok := sanitizeProperty(v); ok {
			out[k] = property
		}
	}

	return out, parseRequired(params["required"], out)
}

func sanitizeProperties(raw any) map[string]any {
	propertyMap, ok := raw.(map[string]any)
	if !ok {
		return map[string]any{}
	}
	out := make(map[string]any, len(propertyMap))
	for k, v := range propertyMap {
		if property, ok := sanitizeProperty(v); ok {
			out[k] = property
		}
	}
	return out
}

func sanitizeProperty(raw any) (map[string]any, bool) {
	property, ok := raw.(map[string]any)
	if !ok {
		return nil, false
	}
	return property, true
}

func parseRequired(raw any, properties map[string]any) []string {
	if raw == nil {
		return nil
	}
	switch r := raw.(type) {
	case []string:
		return filterRequired(r, properties)
	case []any:
		req := make([]string, 0, len(r))
		for _, x := range r {
			if s, ok := x.(string); ok {
				req = append(req, s)
			}
		}
		return filterRequired(req, properties)
	}
	return nil
}

func filterRequired(required []string, properties map[string]any) []string {
	if len(required) == 0 {
		return nil
	}
	if properties == nil {
		return required
	}
	out := make([]string, 0, len(required))
	for _, name := range required {
		if _, ok := properties[name]; ok {
			out = append(out, name)
		}
	}
	return out
}
