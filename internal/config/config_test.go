package config

import (
	"os"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.AgentNick != "bitchtea" {
		t.Fatalf("expected agent nick 'bitchtea', got %q", cfg.AgentNick)
	}
	if cfg.MaxTokens != 4096 {
		t.Fatalf("expected max tokens 4096, got %d", cfg.MaxTokens)
	}
	if cfg.WorkDir == "" {
		t.Fatal("WorkDir should not be empty")
	}
	if cfg.SessionDir == "" {
		t.Fatal("SessionDir should not be empty")
	}
}

func TestDetectProviderOpenAI(t *testing.T) {
	os.Setenv("OPENAI_API_KEY", "sk-test")
	defer os.Unsetenv("OPENAI_API_KEY")

	cfg := DefaultConfig()
	DetectProvider(&cfg)

	if cfg.Provider != "openai" {
		t.Fatalf("expected openai provider, got %q", cfg.Provider)
	}
	if cfg.APIKey != "sk-test" {
		t.Fatalf("expected api key 'sk-test', got %q", cfg.APIKey)
	}
}

func TestEnvOr(t *testing.T) {
	os.Setenv("BITCHTEA_TEST_VAR", "custom")
	defer os.Unsetenv("BITCHTEA_TEST_VAR")

	if v := envOr("BITCHTEA_TEST_VAR", "default"); v != "custom" {
		t.Fatalf("expected 'custom', got %q", v)
	}
	if v := envOr("BITCHTEA_NONEXISTENT", "fallback"); v != "fallback" {
		t.Fatalf("expected 'fallback', got %q", v)
	}
}
