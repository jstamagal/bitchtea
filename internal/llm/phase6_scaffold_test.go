package llm

import (
	"testing"

	"charm.land/catwalk/pkg/catwalk"
	fantasy "charm.land/fantasy"
)

func TestResolveService(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		provider string
		baseURL  string
		want     Service
	}{
		{
			name:     "native openai",
			provider: "openai",
			baseURL:  "https://api.openai.com/v1",
			want:     ServiceOpenAI,
		},
		{
			name:     "openrouter",
			provider: "openai",
			baseURL:  "https://openrouter.ai/api/v1",
			want:     ServiceOpenRouter,
		},
		{
			name:     "ollama",
			provider: "openai",
			baseURL:  "http://localhost:11434/v1",
			want:     ServiceOllama,
		},
		{
			name:     "zai anthropic",
			provider: "anthropic",
			baseURL:  "https://api.z.ai/api/anthropic",
			want:     ServiceZAIAnthropic,
		},
		{
			name:     "native anthropic",
			provider: "anthropic",
			baseURL:  "https://api.anthropic.com/v1",
			want:     ServiceAnthropic,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := ResolveService(tt.provider, tt.baseURL); got != tt.want {
				t.Fatalf("ResolveService(%q, %q) = %q, want %q", tt.provider, tt.baseURL, got, tt.want)
			}
		})
	}
}

func TestPhase6ScaffoldCatwalkProvider(t *testing.T) {
	t.Parallel()

	scaffold := NewPhase6Scaffold("secret", "https://openrouter.ai/api/v1", "anthropic/claude-sonnet-4", "openai")
	got := scaffold.CatwalkProvider()

	if got.ID != catwalk.InferenceProviderOpenRouter {
		t.Fatalf("CatwalkProvider().ID = %q, want %q", got.ID, catwalk.InferenceProviderOpenRouter)
	}
	if got.Type != catwalk.TypeOpenRouter {
		t.Fatalf("CatwalkProvider().Type = %q, want %q", got.Type, catwalk.TypeOpenRouter)
	}
	if got.APIEndpoint != "https://openrouter.ai/api/v1" {
		t.Fatalf("CatwalkProvider().APIEndpoint = %q", got.APIEndpoint)
	}
	if len(got.Models) != 1 || got.Models[0].ID != "anthropic/claude-sonnet-4" {
		t.Fatalf("CatwalkProvider().Models = %#v", got.Models)
	}
}

func TestPhase6ScaffoldOpenAIOptions(t *testing.T) {
	t.Parallel()

	scaffold := NewPhase6Scaffold("secret", "https://api.openai.com/v1", "gpt-4o", "openai")
	opts := scaffold.OpenAIOptions()

	if len(opts) != 2 {
		t.Fatalf("OpenAIOptions() len = %d, want 2", len(opts))
	}
}

func TestPhase6RequestAdaptersFantasyMessages(t *testing.T) {
	t.Parallel()

	scaffold := NewPhase6Scaffold("secret", "https://api.openai.com/v1", "gpt-4o", "openai")
	adapters := scaffold.RequestAdapters([]Message{
		{Role: "system", Content: "follow instructions"},
		{Role: "user", Content: "hello"},
		{
			Role:    "assistant",
			Content: "working",
			ToolCalls: []ToolCall{{
				ID:   "call_1",
				Type: "function",
				Function: FunctionCall{
					Name:      "shell",
					Arguments: `{"cmd":"pwd"}`,
				},
			}},
		},
		{Role: "tool", ToolCallID: "call_1", Content: "/tmp/project"},
	}, nil)

	got := adapters.FantasyMessages()
	if len(got) != 4 {
		t.Fatalf("FantasyMessages() len = %d, want 4", len(got))
	}

	if got[0].Role != fantasy.MessageRoleSystem {
		t.Fatalf("FantasyMessages()[0].Role = %q, want %q", got[0].Role, fantasy.MessageRoleSystem)
	}
	if len(got[2].Content) != 2 {
		t.Fatalf("FantasyMessages()[2].Content len = %d, want 2", len(got[2].Content))
	}
	if text, ok := got[2].Content[0].(fantasy.TextPart); !ok || text.Text != "working" {
		t.Fatalf("FantasyMessages()[2].Content[0] = %#v", got[2].Content[0])
	}
	if toolCall, ok := got[2].Content[1].(fantasy.ToolCallPart); !ok || toolCall.ToolName != "shell" || toolCall.Input != `{"cmd":"pwd"}` {
		t.Fatalf("FantasyMessages()[2].Content[1] = %#v", got[2].Content[1])
	}
	if len(got[3].Content) != 1 {
		t.Fatalf("FantasyMessages()[3].Content len = %d, want 1", len(got[3].Content))
	}
	toolResult, ok := got[3].Content[0].(fantasy.ToolResultPart)
	if !ok {
		t.Fatalf("FantasyMessages()[3].Content[0] = %#v", got[3].Content[0])
	}
	if toolResult.ToolCallID != "call_1" {
		t.Fatalf("FantasyMessages()[3].ToolCallID = %q, want call_1", toolResult.ToolCallID)
	}
	textOutput, ok := toolResult.Output.(fantasy.ToolResultOutputContentText)
	if !ok || textOutput.Text != "/tmp/project" {
		t.Fatalf("FantasyMessages()[3].Output = %#v", toolResult.Output)
	}
}

func TestPhase6RequestAdaptersFantasyTools(t *testing.T) {
	t.Parallel()

	scaffold := NewPhase6Scaffold("secret", "https://api.openai.com/v1", "gpt-4o", "openai")
	adapters := scaffold.RequestAdapters(nil, []ToolDef{{
		Type: "function",
		Function: ToolFuncDef{
			Name:        "shell",
			Description: "run a shell command",
			Parameters: struct {
				Type       string              `json:"type"`
				Properties map[string]struct{} `json:"properties"`
			}{
				Type:       "object",
				Properties: map[string]struct{}{"cmd": {}},
			},
		},
	}})

	got, err := adapters.FantasyTools()
	if err != nil {
		t.Fatalf("FantasyTools() error = %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("FantasyTools() len = %d, want 1", len(got))
	}

	fnTool, ok := got[0].(fantasy.FunctionTool)
	if !ok {
		t.Fatalf("FantasyTools()[0] = %#v", got[0])
	}
	if fnTool.Name != "shell" {
		t.Fatalf("FantasyTools()[0].Name = %q, want shell", fnTool.Name)
	}
	if fnTool.InputSchema["type"] != "object" {
		t.Fatalf("FantasyTools()[0].InputSchema = %#v", fnTool.InputSchema)
	}
}
