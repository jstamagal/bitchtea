package llm

import (
	"strings"
	"testing"
)

// --- buildProvider: provider name routing ---------------------------------

func TestBuildProvider_Anthropic(t *testing.T) {
	p, err := buildProvider(providerConfig{provider: "anthropic", apiKey: "sk-test"})
	if err != nil {
		t.Fatal(err)
	}
	if p == nil {
		t.Fatal("expected non-nil provider")
	}
}

func TestBuildProvider_AnthropicStripsV1(t *testing.T) {
	_, err := buildProvider(providerConfig{
		provider: "anthropic",
		apiKey:   "sk-test",
		baseURL:  "https://api.anthropic.com/v1",
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestBuildProvider_UnsupportedProvider(t *testing.T) {
	_, err := buildProvider(providerConfig{provider: "cohere", apiKey: "sk-test"})
	if err == nil {
		t.Fatal("expected error for unsupported provider")
	}
	if !strings.Contains(err.Error(), "unsupported provider") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBuildProvider_OpenAIEmptyBase(t *testing.T) {
	p, err := buildProvider(providerConfig{provider: "openai", apiKey: "sk-test"})
	if err != nil {
		t.Fatal(err)
	}
	if p == nil {
		t.Fatal("expected non-nil provider")
	}
}

func TestBuildProvider_DefaultProviderRoutesOpenAI(t *testing.T) {
	p, err := buildProvider(providerConfig{provider: "", apiKey: "sk-test"})
	if err != nil {
		t.Fatal(err)
	}
	if p == nil {
		t.Fatal("expected non-nil provider")
	}
}

// --- routeOpenAICompatible: host-based routing ----------------------------
// All fantasy providers return the same concrete type and Name(), so we
// verify routing by testing hostOf directly (below) and only assert
// non-nil / no-error here.

func TestRouteOpenAI_EmptyBase(t *testing.T) {
	p, err := routeOpenAICompatible(providerConfig{apiKey: "sk-test"})
	if err != nil {
		t.Fatal(err)
	}
	if p == nil {
		t.Fatal("expected non-nil provider for empty base")
	}
}

func TestRouteOpenAI_OpenAIHost(t *testing.T) {
	p, err := routeOpenAICompatible(providerConfig{
		apiKey:  "sk-test",
		baseURL: "https://api.openai.com/v1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if p == nil {
		t.Fatal("expected non-nil provider")
	}
}

func TestRouteOpenAI_OpenRouterHost(t *testing.T) {
	p, err := routeOpenAICompatible(providerConfig{
		apiKey:  "sk-test",
		baseURL: "https://openrouter.ai/api/v1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if p == nil {
		t.Fatal("expected non-nil provider")
	}
}

func TestRouteOpenAI_VercelHost(t *testing.T) {
	p, err := routeOpenAICompatible(providerConfig{
		apiKey:  "sk-test",
		baseURL: "https://ai-gateway.vercel.sh/v1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if p == nil {
		t.Fatal("expected non-nil provider")
	}
}

func TestRouteOpenAI_OllamaHost(t *testing.T) {
	p, err := routeOpenAICompatible(providerConfig{
		apiKey:  "",
		baseURL: "http://localhost:11434/v1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if p == nil {
		t.Fatal("expected non-nil provider for ollama")
	}
}

func TestRouteOpenAI_CustomHost(t *testing.T) {
	p, err := routeOpenAICompatible(providerConfig{
		apiKey:  "sk-test",
		baseURL: "https://my-proxy.example.com/v1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if p == nil {
		t.Fatal("expected non-nil provider for custom host")
	}
}

func TestRouteOpenAI_InvalidURL(t *testing.T) {
	_, err := routeOpenAICompatible(providerConfig{
		apiKey:  "sk-test",
		baseURL: "://bad",
	})
	if err == nil {
		t.Fatal("expected error for invalid URL")
	}
}

// --- hostOf / hostOfMust: the routing primitives -------------------------
// These are the real regression surface — if hostOf parses correctly, the
// switch in routeOpenAICompatible routes correctly.

func TestHostOf_RoutesEachProvider(t *testing.T) {
	tests := []struct {
		url  string
		host string
	}{
		{"https://api.openai.com/v1", "api.openai.com"},
		{"https://openrouter.ai/api/v1", "openrouter.ai"},
		{"https://ai-gateway.vercel.sh/v1", "ai-gateway.vercel.sh"},
		{"http://localhost:11434/v1", "localhost:11434"},
		{"https://api.z.ai/v1", "api.z.ai"},
		{"https://my-proxy.example.com/v1", "my-proxy.example.com"},
	}
	for _, tt := range tests {
		got, err := hostOf(tt.url)
		if err != nil {
			t.Errorf("hostOf(%q): %v", tt.url, err)
			continue
		}
		if got != tt.host {
			t.Errorf("hostOf(%q) = %q, want %q", tt.url, got, tt.host)
		}
	}
}

func TestHostOf_InvalidURL(t *testing.T) {
	_, err := hostOf("://bad")
	if err == nil {
		t.Fatal("expected error for invalid URL")
	}
}

func TestHostOfMust_PanicsOnBadURL(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on bad URL")
		}
	}()
	hostOfMust("://bad")
}

func TestHostOfMust_ReturnsHost(t *testing.T) {
	h := hostOfMust("https://api.openai.com/v1")
	if h != "api.openai.com" {
		t.Fatalf("expected api.openai.com, got %q", h)
	}
}

// --- stripV1Suffix -------------------------------------------------------

func TestStripV1Suffix(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"https://api.anthropic.com/v1", "https://api.anthropic.com"},
		{"https://api.anthropic.com/v1/", "https://api.anthropic.com"},
		{"https://api.anthropic.com", "https://api.anthropic.com"},
		{"https://api.anthropic.com/", "https://api.anthropic.com/"},
		{"", ""},
	}
	for _, tt := range tests {
		got := stripV1Suffix(tt.in)
		if got != tt.want {
			t.Errorf("stripV1Suffix(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
