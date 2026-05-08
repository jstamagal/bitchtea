package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"charm.land/fantasy"

	"github.com/jstamagal/bitchtea/internal/tools"
)

func TestTranslateToolsExtractsFantasyParameterShape(t *testing.T) {
	reg := tools.NewRegistry(t.TempDir(), t.TempDir())

	translated := translateTools(reg)
	if len(translated) == 0 {
		t.Fatal("expected translated tools")
	}

	var readInfoFound bool
	for _, tool := range translated {
		info := tool.Info()
		if info.Name != "read" {
			continue
		}
		readInfoFound = true

		if _, ok := info.Parameters["path"]; !ok {
			t.Fatalf("expected read parameters to expose path property, got %+v", info.Parameters)
		}
		for _, bogus := range []string{"type", "properties", "required"} {
			if _, ok := info.Parameters[bogus]; ok {
				t.Fatalf("fantasy parameters must not include top-level schema key %q: %+v", bogus, info.Parameters)
			}
		}
		if len(info.Required) != 1 || info.Required[0] != "path" {
			t.Fatalf("expected required [path], got %+v", info.Required)
		}
	}

	if !readInfoFound {
		t.Fatal("missing read tool")
	}
}

func TestBitchteaToolRunUnknownToolReturnsErrorResponseNotGoError(t *testing.T) {
	reg := tools.NewRegistry(t.TempDir(), t.TempDir())
	tool := &bitchteaTool{
		info: fantasy.ToolInfo{Name: "definitely_not_a_real_tool"},
		reg:  reg,
	}

	resp, err := tool.Run(context.Background(), fantasy.ToolCall{
		ID:    "call_unknown",
		Name:  "definitely_not_a_real_tool",
		Input: `{}`,
	})
	if err != nil {
		t.Fatalf("Run must never return a Go error (would abort the fantasy stream); got %v", err)
	}
	if !resp.IsError {
		t.Fatalf("expected IsError=true on tool failure, got %+v", resp)
	}
	if resp.Type != "text" {
		t.Fatalf("expected text response type, got %q", resp.Type)
	}
	if !strings.Contains(resp.Content, "definitely_not_a_real_tool") {
		t.Fatalf("error response must preserve underlying error text, got %q", resp.Content)
	}
}

func TestBitchteaToolRunCancelledContextReturnsErrorResponseNotGoError(t *testing.T) {
	reg := tools.NewRegistry(t.TempDir(), t.TempDir())
	tool := &bitchteaTool{
		info: fantasy.ToolInfo{Name: "bash"},
		reg:  reg,
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before invocation so bash exits immediately

	resp, err := tool.Run(ctx, fantasy.ToolCall{
		ID:    "call_bash",
		Name:  "bash",
		Input: `{"command":"sleep 30","timeout":30}`,
	})
	if err != nil {
		t.Fatalf("Run must never return a Go error on ctx cancel (would abort the fantasy stream); got %v", err)
	}
	// Current behavior (pre-bt-p8): cancellation surfaces as the bash tool's
	// error string wrapped in NewTextErrorResponse. There is no synthetic
	// "user cancelled this tool call" message yet.
	if !resp.IsError {
		t.Fatalf("expected IsError=true on cancelled ctx, got %+v", resp)
	}
	if resp.Type != "text" {
		t.Fatalf("expected text response type, got %q", resp.Type)
	}
	if resp.Content == "" {
		t.Fatalf("expected non-empty error content on cancelled ctx, got %+v", resp)
	}
}

func TestSplitSchemaAcceptsAlreadySplitPropertyMap(t *testing.T) {
	params, required := splitSchema(map[string]any{
		"path": map[string]any{"type": "string"},
	})

	if len(required) != 0 {
		t.Fatalf("expected no required fields, got %+v", required)
	}
	if _, ok := params["path"]; !ok {
		t.Fatalf("expected path property, got %+v", params)
	}
}

func TestSplitSchemaEdgeCases(t *testing.T) {
	tests := []struct {
		name         string
		input        map[string]any
		wantParams   map[string]any
		wantRequired []string
	}{
		{
			name: "object schema with missing required",
			input: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path": map[string]any{"type": "string"},
				},
			},
			wantParams: map[string]any{
				"path": map[string]any{"type": "string"},
			},
		},
		{
			name: "object schema with required but no properties",
			input: map[string]any{
				"type":     "object",
				"required": []string{"path"},
			},
			wantParams: map[string]any{},
		},
		{
			name: "object schema with invalid required",
			input: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path": map[string]any{"type": "string"},
				},
				"required": "path",
			},
			wantParams: map[string]any{
				"path": map[string]any{"type": "string"},
			},
		},
		{
			name: "object schema with mixed required list",
			input: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path":  map[string]any{"type": "string"},
					"limit": map[string]any{"type": "integer"},
				},
				"required": []any{"path", 42, nil},
			},
			wantParams: map[string]any{
				"path":  map[string]any{"type": "string"},
				"limit": map[string]any{"type": "integer"},
			},
			wantRequired: []string{"path"},
		},
		{
			name: "object schema ignores required names without properties",
			input: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path": map[string]any{"type": "string"},
				},
				"required": []string{"path", "missing"},
			},
			wantParams: map[string]any{
				"path": map[string]any{"type": "string"},
			},
			wantRequired: []string{"path"},
		},
		{
			// Regression for bt-qna: when every required name names a
			// property that doesn't exist, filterRequired must return nil
			// (not []string{}) so providers don't see an empty array.
			name: "object schema with all required names filtered yields nil required",
			input: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path": map[string]any{"type": "string"},
				},
				"required": []string{"missing_one", "missing_two"},
			},
			wantParams: map[string]any{
				"path": map[string]any{"type": "string"},
			},
			wantRequired: nil,
		},
		{
			// Same as above but via the []any path (e.g. JSON-decoded input).
			name: "object schema []any required all filtered yields nil required",
			input: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path": map[string]any{"type": "string"},
				},
				"required": []any{"nope", 42, nil},
			},
			wantParams: map[string]any{
				"path": map[string]any{"type": "string"},
			},
			wantRequired: nil,
		},
		{
			name: "object schema drops malformed property schemas and required names",
			input: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path":   map[string]any{"type": "string"},
					"broken": "not a schema object",
				},
				"required": []string{"path", "broken"},
			},
			wantParams: map[string]any{
				"path": map[string]any{"type": "string"},
			},
			wantRequired: []string{"path"},
		},
		{
			name: "malformed object schema with non-map properties",
			input: map[string]any{
				"type":       "object",
				"properties": "not an object",
				"required":   []string{"path"},
			},
			wantParams: map[string]any{},
		},
		{
			name: "non-object schema",
			input: map[string]any{
				"type": "string",
			},
			wantParams: map[string]any{},
		},
		{
			name: "already split property map preserves type property",
			input: map[string]any{
				"type": map[string]any{"type": "string"},
				"path": map[string]any{"type": "string"},
			},
			wantParams: map[string]any{
				"type": map[string]any{"type": "string"},
				"path": map[string]any{"type": "string"},
			},
		},
		{
			name: "already split property map preserves required property",
			input: map[string]any{
				"required": map[string]any{"type": "string"},
				"path":     map[string]any{"type": "string"},
			},
			wantParams: map[string]any{
				"required": map[string]any{"type": "string"},
				"path":     map[string]any{"type": "string"},
			},
		},
		{
			name: "already split property map drops malformed property schema",
			input: map[string]any{
				"path":   map[string]any{"type": "string"},
				"broken": []string{"not", "a", "schema"},
			},
			wantParams: map[string]any{
				"path": map[string]any{"type": "string"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			params, required := splitSchema(tt.input)
			assertStringSlicesEqual(t, required, tt.wantRequired)
			assertMapsEqual(t, params, tt.wantParams)
		})
	}
}

// TestFilterRequiredEmptyAfterFilterMarshalsWithoutRequiredKey is the
// behavioral guardrail for bt-qna. It mirrors what fantasy's prepareTools
// builds (type/properties/required map) using a schema whose required names
// all resolve to nothing, then JSON-marshals the wire payload and asserts
// that "required" is absent — not present as [] or null.
func TestFilterRequiredEmptyAfterFilterMarshalsWithoutRequiredKey(t *testing.T) {
	params, required := splitSchema(map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{"type": "string"},
		},
		"required": []string{"missing"},
	})

	if required != nil {
		t.Fatalf("expected nil required after all names filtered, got %#v", required)
	}

	// Mirror fantasy/agent.go prepareTools wire schema construction, except
	// only emit "required" when it's non-empty — the assertion proves the
	// caller can rely on filterRequired's nil to know to omit the key.
	wire := map[string]any{
		"type":       "object",
		"properties": params,
	}
	if len(required) > 0 {
		wire["required"] = required
	}

	raw, err := json.Marshal(wire)
	if err != nil {
		t.Fatalf("marshal wire schema: %v", err)
	}
	got := string(raw)
	if strings.Contains(got, `"required"`) {
		t.Fatalf("wire schema must not include \"required\" when empty after filter; got %s", got)
	}
}

// TestTranslateToolsRoutesPortedToolsThroughTypedWrappers is the bt-p2-switch
// guardrail. It locks in the dispatch order from translateTools — tools that
// have a typed wrapper in typedToolFor must NOT come back as *bitchteaTool
// (the generic Registry.Execute compatibility adapter). If a future change
// accidentally drops a tool from typedToolFor it would silently fall back to
// the generic path; this test surfaces that as a failure.
//
// Conversely, tools still on the compatibility path (terminal_*, preview_image)
// are documented in typedToolFor's comment block and are explicitly NOT
// asserted here — adding a typed wrapper for one of them is the trigger to
// extend portedToolNames below.
func TestTranslateToolsRoutesPortedToolsThroughTypedWrappers(t *testing.T) {
	reg := tools.NewRegistry(t.TempDir(), t.TempDir())
	translated := translateTools(reg)

	byName := map[string]fantasy.AgentTool{}
	for _, tool := range translated {
		byName[tool.Info().Name] = tool
	}

	portedToolNames := []string{
		"read",
		"write",
		"edit",
		"bash",
		"search_memory",
		"write_memory",
	}
	for _, name := range portedToolNames {
		tool, ok := byName[name]
		if !ok {
			t.Fatalf("translateTools did not produce a fantasy.AgentTool for ported tool %q", name)
		}
		if _, isGeneric := tool.(*bitchteaTool); isGeneric {
			t.Fatalf("ported tool %q resolved to *bitchteaTool (generic Registry.Execute path); expected typed fantasy wrapper", name)
		}
	}
}

func TestTranslateToolsProducesValidSchemasForRealRegistryDefinitions(t *testing.T) {
	for _, translatedTool := range translateTools(tools.NewRegistry(t.TempDir(), t.TempDir())) {
		info := translatedTool.Info()
		wrappedSchema := map[string]any{
			"type":       "object",
			"properties": info.Parameters,
			"required":   info.Required,
		}
		assertFantasyToolSchemaIsSafe(t, info.Name, wrappedSchema)
	}
}

func TestAssembleAgentTools_localWinsOverMCP(t *testing.T) {
	reg := tools.NewRegistry(t.TempDir(), t.TempDir())
	mcpTools := []fantasy.AgentTool{
		&staticAgentTool{info: fantasy.ToolInfo{Name: "read", Description: "from mcp"}},
	}

	assembled := AssembleAgentTools(reg, mcpTools)

	var readTool fantasy.AgentTool
	readCount := 0
	for _, tool := range assembled {
		if tool.Info().Name == "read" {
			readTool = tool
			readCount++
		}
	}
	if readCount != 1 {
		t.Fatalf("assembled read tool count = %d, want 1", readCount)
	}
	if readTool == nil {
		t.Fatal("assembled tools missing local read tool")
	}
	if readTool.Info().Description == "from mcp" {
		t.Fatalf("MCP tool replaced local read tool: %+v", readTool.Info())
	}
}

func TestAssembleAgentTools_dropsUnprefixedMCP(t *testing.T) {
	reg := tools.NewRegistry(t.TempDir(), t.TempDir())
	mcpTools := []fantasy.AgentTool{
		&staticAgentTool{info: fantasy.ToolInfo{Name: "unsafe_unprefixed", Description: "from mcp"}},
	}

	assembled := AssembleAgentTools(reg, mcpTools)
	names := toolNameSet(assembled)
	if names["unsafe_unprefixed"] {
		t.Fatalf("assembled tools kept unprefixed MCP tool: %v", names)
	}
}

func TestAssembleAgentTools_dedupsDuplicateMCP(t *testing.T) {
	reg := tools.NewRegistry(t.TempDir(), t.TempDir())
	mcpTools := []fantasy.AgentTool{
		&staticAgentTool{info: fantasy.ToolInfo{Name: "mcp__fs__read", Description: "first"}},
		&staticAgentTool{info: fantasy.ToolInfo{Name: "mcp__fs__read", Description: "second"}},
	}

	assembled := AssembleAgentTools(reg, mcpTools)

	count := 0
	var kept fantasy.AgentTool
	for _, tool := range assembled {
		if tool.Info().Name == "mcp__fs__read" {
			count++
			kept = tool
		}
	}
	if count != 1 {
		t.Fatalf("assembled duplicate MCP tool count = %d, want 1", count)
	}
	if kept.Info().Description != "first" {
		t.Fatalf("kept duplicate MCP description = %q, want first", kept.Info().Description)
	}
}

func TestAssembleAgentTools_nilManager(t *testing.T) {
	mcpTools, err := MCPTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("MCPTools(nil) err: %v", err)
	}
	if mcpTools != nil {
		t.Fatalf("MCPTools(nil) tools = %v, want nil", mcpTools)
	}

	reg := tools.NewRegistry(t.TempDir(), t.TempDir())
	wantNames := toolNameSet(translateTools(reg))
	gotNames := toolNameSet(AssembleAgentTools(reg, mcpTools))
	if !sameSet(gotNames, wantNames) {
		t.Fatalf("AssembleAgentTools(reg, nil manager tools) names = %v, want %v", gotNames, wantNames)
	}
}

func assertStringSlicesEqual(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("slice len = %d, want %d: got=%+v want=%+v", len(got), len(want), got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("slice[%d] = %q, want %q: got=%+v want=%+v", i, got[i], want[i], got, want)
		}
	}
}

func assertMapsEqual(t *testing.T, got, want map[string]any) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("map len = %d, want %d: got=%+v want=%+v", len(got), len(want), got, want)
	}
	for k, wantValue := range want {
		if fmt.Sprintf("%#v", got[k]) != fmt.Sprintf("%#v", wantValue) {
			t.Fatalf("map[%q] = %#v, want %#v: got=%+v want=%+v", k, got[k], wantValue, got, want)
		}
	}
}

func assertFantasyToolSchemaIsSafe(t *testing.T, toolName string, schema map[string]any) {
	t.Helper()
	if schema["type"] != "object" {
		t.Fatalf("%s schema type = %v, want object: %+v", toolName, schema["type"], schema)
	}
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("%s schema properties = %#v, want map", toolName, schema["properties"])
	}
	for propertyName, propertySchema := range properties {
		propertyMap, ok := propertySchema.(map[string]any)
		if !ok {
			t.Fatalf("%s property %q schema = %#v, want map", toolName, propertyName, propertySchema)
		}
		propertyType, ok := propertyMap["type"]
		if !ok || propertyType == "" {
			t.Fatalf("%s property %q missing non-empty type: %+v", toolName, propertyName, propertyMap)
		}
	}
	switch required := schema["required"].(type) {
	case nil:
	case []string:
		for _, name := range required {
			if _, ok := properties[name]; !ok {
				t.Fatalf("%s required property %q is not defined in properties: %+v", toolName, name, schema)
			}
		}
	default:
		t.Fatalf("%s required = %#v, want []string or nil", toolName, required)
	}
}

type captureLanguageModel struct {
	calls []fantasy.Call
}

func (m *captureLanguageModel) Generate(context.Context, fantasy.Call) (*fantasy.Response, error) {
	return nil, fmt.Errorf("Generate not implemented")
}

func (m *captureLanguageModel) Stream(_ context.Context, call fantasy.Call) (fantasy.StreamResponse, error) {
	m.calls = append(m.calls, call)
	switch len(m.calls) {
	case 1:
		return func(yield func(fantasy.StreamPart) bool) {
			if !yield(fantasy.StreamPart{
				Type:          fantasy.StreamPartTypeToolCall,
				ID:            "call_read",
				ToolCallName:  "read",
				ToolCallInput: `{"path":"test.txt"}`,
			}) {
				return
			}
			yield(fantasy.StreamPart{
				Type:         fantasy.StreamPartTypeFinish,
				FinishReason: fantasy.FinishReasonToolCalls,
				Usage:        fantasy.Usage{InputTokens: 10, OutputTokens: 2, TotalTokens: 12},
			})
		}, nil
	case 2:
		return func(yield func(fantasy.StreamPart) bool) {
			if !yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeTextStart, ID: "text_1"}) {
				return
			}
			if !yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeTextDelta, ID: "text_1", Delta: "done"}) {
				return
			}
			if !yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeTextEnd, ID: "text_1"}) {
				return
			}
			yield(fantasy.StreamPart{
				Type:         fantasy.StreamPartTypeFinish,
				FinishReason: fantasy.FinishReasonStop,
				Usage:        fantasy.Usage{InputTokens: 5, OutputTokens: 1, TotalTokens: 6},
			})
		}, nil
	default:
		return nil, fmt.Errorf("unexpected stream call %d", len(m.calls))
	}
}

func (m *captureLanguageModel) GenerateObject(context.Context, fantasy.ObjectCall) (*fantasy.ObjectResponse, error) {
	return nil, fmt.Errorf("GenerateObject not implemented")
}

func (m *captureLanguageModel) StreamObject(context.Context, fantasy.ObjectCall) (fantasy.ObjectStreamResponse, error) {
	return nil, fmt.Errorf("StreamObject not implemented")
}

func (m *captureLanguageModel) Provider() string { return "capture" }
func (m *captureLanguageModel) Model() string    { return "capture-model" }

func TestStreamChatSendsValidToolSchemaAndExecutesToolCall(t *testing.T) {
	workDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(workDir, "test.txt"), []byte("tool body\n"), 0644); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	model := &captureLanguageModel{}
	client := NewClient("", "", "capture-model", "openai")
	client.model = model

	events := make(chan StreamEvent, 32)
	reg := tools.NewRegistry(workDir, t.TempDir())
	messages := []Message{
		{Role: "system", Content: "SYSTEM_TOOL_PROMPT_MARKER terminal_start preview_image"},
		{Role: "user", Content: "read test.txt"},
	}
	client.StreamChat(context.Background(), messages, reg, events)

	var toolCallSeen bool
	var toolResultSeen bool
	var text string
	var doneMessages []Message
	for ev := range events {
		switch ev.Type {
		case "tool_call":
			toolCallSeen = ev.ToolName == "read" && ev.ToolCallID == "call_read" && ev.ToolArgs == `{"path":"test.txt"}`
		case "tool_result":
			toolResultSeen = ev.ToolName == "read" && ev.ToolCallID == "call_read" && ev.Text == "tool body\n"
		case "text":
			text += ev.Text
		case "done":
			doneMessages = ev.Messages
		case "error":
			t.Fatalf("unexpected error event: %v", ev.Error)
		}
	}

	if len(model.calls) != 2 {
		t.Fatalf("expected fantasy to make 2 model calls around the tool result, got %d", len(model.calls))
	}

	assertReadToolSchema(t, model.calls[0].Tools)
	assertAllRegistryToolsAttached(t, reg.Definitions(), model.calls[0].Tools)
	assertPromptContainsSystemText(t, model.calls[0].Prompt, "SYSTEM_TOOL_PROMPT_MARKER")

	if !toolCallSeen {
		t.Fatal("expected tool_call event for read")
	}
	if !toolResultSeen {
		t.Fatal("expected tool_result event with file body")
	}
	if text != "done" {
		t.Fatalf("expected final text %q, got %q", "done", text)
	}
	assertRebuiltToolTranscript(t, doneMessages)
}

func assertAllRegistryToolsAttached(t *testing.T, defs []tools.ToolDef, fantasyTools []fantasy.Tool) {
	t.Helper()
	got := map[string]bool{}
	for _, tool := range fantasyTools {
		if functionTool, ok := tool.(fantasy.FunctionTool); ok {
			got[functionTool.Name] = true
		}
	}
	for _, def := range defs {
		if !got[def.Function.Name] {
			t.Fatalf("provider call missing tool %q; got tools %#v", def.Function.Name, got)
		}
	}
}

func assertPromptContainsSystemText(t *testing.T, prompt fantasy.Prompt, want string) {
	t.Helper()
	for _, msg := range prompt {
		if msg.Role != fantasy.MessageRoleSystem {
			continue
		}
		for _, part := range msg.Content {
			textPart, ok := part.(fantasy.TextPart)
			if ok && strings.Contains(textPart.Text, want) {
				return
			}
		}
	}
	t.Fatalf("provider call prompt missing system text %q: %+v", want, prompt)
}

func assertReadToolSchema(t *testing.T, fantasyTools []fantasy.Tool) {
	t.Helper()
	for _, tool := range fantasyTools {
		functionTool, ok := tool.(fantasy.FunctionTool)
		if !ok || functionTool.Name != "read" {
			continue
		}
		if functionTool.InputSchema["type"] != "object" {
			t.Fatalf("read tool schema type = %v, want object: %+v", functionTool.InputSchema["type"], functionTool.InputSchema)
		}
		properties, ok := functionTool.InputSchema["properties"].(map[string]any)
		if !ok {
			t.Fatalf("read tool schema missing properties object: %+v", functionTool.InputSchema)
		}
		if _, ok := properties["path"]; !ok {
			t.Fatalf("read tool properties missing path: %+v", properties)
		}
		for _, bogus := range []string{"type", "properties", "required"} {
			if _, ok := properties[bogus]; ok {
				t.Fatalf("read tool properties contain nested schema key %q: %+v", bogus, properties)
			}
		}
		required, ok := functionTool.InputSchema["required"].([]string)
		if !ok || len(required) != 1 || required[0] != "path" {
			t.Fatalf("read tool required = %#v, want [path]", functionTool.InputSchema["required"])
		}
		return
	}
	t.Fatal("missing read function tool")
}

func assertRebuiltToolTranscript(t *testing.T, messages []Message) {
	t.Helper()
	var assistantWithCall bool
	var toolResult bool
	for _, msg := range messages {
		if msg.Role == "assistant" {
			for _, call := range msg.ToolCalls {
				if call.ID == "call_read" && call.Function.Name == "read" && call.Function.Arguments == `{"path":"test.txt"}` {
					assistantWithCall = true
				}
			}
		}
		if msg.Role == "tool" && msg.ToolCallID == "call_read" && msg.Content == "tool body\n" {
			toolResult = true
		}
	}
	if !assistantWithCall {
		t.Fatalf("rebuilt transcript missing assistant tool call: %+v", messages)
	}
	if !toolResult {
		t.Fatalf("rebuilt transcript missing tool result: %+v", messages)
	}
}

// --- translateTools memoization (LOW #15 / bt-zhm) -------------------------

// TestTranslateToolsMemoizesPerRegistry asserts that two consecutive calls
// with the same Registry pointer return the same backing slice (memoization
// hit). Prior to LOW #15 every call rebuilt the wrapper slice from scratch.
func TestTranslateToolsMemoizesPerRegistry(t *testing.T) {
	reg := tools.NewRegistry(t.TempDir(), t.TempDir())
	first := translateTools(reg)
	second := translateTools(reg)
	if len(first) == 0 {
		t.Fatal("expected non-empty translated tools slice")
	}
	if &first[0] != &second[0] {
		t.Fatal("expected memoized slice (same backing array), got fresh slice")
	}
}

// TestTranslateToolsRebuildsForDifferentRegistry asserts that distinct
// Registry pointers don't share cache entries — each gets its own slice.
func TestTranslateToolsRebuildsForDifferentRegistry(t *testing.T) {
	regA := tools.NewRegistry(t.TempDir(), t.TempDir())
	regB := tools.NewRegistry(t.TempDir(), t.TempDir())
	a := translateTools(regA)
	b := translateTools(regB)
	if len(a) == 0 || len(b) == 0 {
		t.Fatal("expected non-empty translated tool slices for both registries")
	}
	if &a[0] == &b[0] {
		t.Fatal("expected distinct slices for distinct Registry pointers")
	}
}

// TestInvalidateTranslateCacheForcesRebuild verifies the invalidation hook
// drops the memoized entry so the next call rebuilds. Currently unused in
// production (Registry definitions are immutable) but exposed for future
// mutation paths.
func TestInvalidateTranslateCacheForcesRebuild(t *testing.T) {
	reg := tools.NewRegistry(t.TempDir(), t.TempDir())
	first := translateTools(reg)
	invalidateTranslateCache(reg)
	second := translateTools(reg)
	if len(first) == 0 || len(second) == 0 {
		t.Fatal("expected non-empty slices")
	}
	if &first[0] == &second[0] {
		t.Fatal("expected fresh slice after invalidate, got memoized")
	}
}
