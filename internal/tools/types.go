package tools

// ToolDef is the OpenAI-compatible tool descriptor that Registry.Definitions
// returns. Lives in `tools` (not `llm`) to keep the dependency edge
// `llm → tools` one-way: `internal/llm/tools.go` imports Registry, and
// Registry.Definitions returns these types.
//
// `internal/llm` re-exports ToolDef + ToolFuncDef as type aliases so callers
// that wrote `llm.ToolDef` keep compiling unchanged.
type ToolDef struct {
	Type     string      `json:"type"`
	Function ToolFuncDef `json:"function"`
}

type ToolFuncDef struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"`
}
