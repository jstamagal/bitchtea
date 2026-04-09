package llm

import (
	"testing"

	"charm.land/catwalk/pkg/catwalk"
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
