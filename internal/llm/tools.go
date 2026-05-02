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

// translateTools builds one fantasy.AgentTool per Registry definition. Called
// per stream-call (cheap — definitions are static, only the Required slice
// varies).
//
// Phase 2 migration status (bt-p2-switch): every Registry tool is dispatched
// through typedToolFor first; only tools that have not yet had their per-tool
// typed-wrapper ticket land fall through to the generic bitchteaTool adapter
// below. The fallback path is now compatibility code — it remains because the
// terminal_* family and preview_image still need careful per-tool ports
// (PTY/image handling, separate tickets). Do NOT add new tools through the
// fallback path; add a typed wrapper and wire it into typedToolFor instead.
func translateTools(reg *tools.Registry) []fantasy.AgentTool {
	defs := reg.Definitions()
	out := make([]fantasy.AgentTool, 0, len(defs))
	for _, d := range defs {
		// Preferred path: typed fantasy wrapper. If a wrapper exists for this
		// tool name it owns the schema, JSON deserialization, and Run dispatch.
		if typed := typedToolFor(d.Function.Name, reg); typed != nil {
			out = append(out, typed)
			continue
		}
		// Compatibility path: generic bitchteaTool adapter using
		// Registry.Execute(name, argsJSON). This branch only runs for the
		// unported tools listed in typedToolFor's doc comment. Once those
		// tools have typed wrappers this whole branch can be deleted.
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

// typedToolFor returns the typed fantasy wrapper for name, or nil if no typed
// wrapper exists yet (in which case translateTools falls back to the generic
// bitchteaTool adapter — see the "compatibility path" comment in translateTools).
//
// Tools currently routed through typed wrappers (no longer touch
// Registry.Execute via the generic adapter):
//
//   - read           (typed_read.go         · bt-p2-read-write)
//   - write          (typed_write.go        · bt-p2-read-write)
//   - edit           (typed_edit.go         · bt-p2-edit)
//   - bash           (typed_bash.go         · bt-p2-bash-memory)
//   - search_memory  (typed_search_memory.go · bt-p2-bash-memory)
//   - write_memory   (typed_write_memory.go · bt-p2-switch)
//
// Tools still on the generic bitchteaTool compatibility path (need typed
// wrappers in follow-up tickets, NOT to be expanded with new tools):
//
//   - terminal_start, terminal_send, terminal_keys, terminal_snapshot,
//     terminal_wait, terminal_resize, terminal_close
//     (PTY lifecycle — careful args + state handling, see internal/tools/terminal.go)
//   - preview_image
//     (image decode + render — see internal/tools/image.go)
func typedToolFor(name string, reg *tools.Registry) fantasy.AgentTool {
	switch name {
	case "edit":
		return editTool(reg)
	case "read":
		return readTool(reg)
	case "write":
		return writeTool(reg)
	case "bash":
		return bashTool(reg)
	case "search_memory":
		return searchMemoryTool(reg)
	case "write_memory":
		return writeMemoryTool(reg)
	}
	return nil
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
	// Return nil (not an empty slice) when every required name was filtered
	// out — some providers reject "required": [] / null. Callers and downstream
	// fantasy code only emit the field when this is non-empty.
	if len(out) == 0 {
		return nil
	}
	return out
}
