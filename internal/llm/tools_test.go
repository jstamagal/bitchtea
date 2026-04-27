package llm

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
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
	client.StreamChat(context.Background(), []Message{{Role: "user", Content: "read test.txt"}}, tools.NewRegistry(workDir, t.TempDir()), events)

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
