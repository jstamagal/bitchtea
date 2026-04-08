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
	if cfg.SoundType != "bell" {
		t.Fatalf("expected sound type 'bell', got %q", cfg.SoundType)
	}
	if cfg.WorkDir == "" {
		t.Fatal("WorkDir should not be empty")
	}
	if cfg.SessionDir == "" {
		t.Fatal("SessionDir should not be empty")
	}
	if cfg.LogDir == "" {
		t.Fatal("LogDir should not be empty")
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

func TestProfileSaveLoadDelete(t *testing.T) {
	// Override profiles dir for test
	dir := t.TempDir()
	origDir := ProfilesDir
	ProfilesDir = func() string { return dir }
	defer func() { ProfilesDir = origDir }()

	p := Profile{
		Name:     "test-zai",
		Provider: "anthropic",
		BaseURL:  "https://api.z.ai/api/anthropic",
		APIKey:   "sk-test-12345",
		Model:    "glm-5.1",
	}

	// Save
	if err := SaveProfile(p); err != nil {
		t.Fatalf("save: %v", err)
	}

	// List
	names := ListProfiles()
	if len(names) != 1 || names[0] != "test-zai" {
		t.Fatalf("list: %v", names)
	}

	// Load
	loaded, err := LoadProfile("test-zai")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded.Provider != "anthropic" || loaded.BaseURL != "https://api.z.ai/api/anthropic" || loaded.Model != "glm-5.1" {
		t.Fatalf("loaded profile mismatch: %+v", loaded)
	}

	// Apply to config
	cfg := DefaultConfig()
	ApplyProfile(&cfg, loaded)
	if cfg.Provider != "anthropic" || cfg.Model != "glm-5.1" {
		t.Fatalf("apply profile mismatch: provider=%s model=%s", cfg.Provider, cfg.Model)
	}

	// Delete
	if err := DeleteProfile("test-zai"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	names = ListProfiles()
	if len(names) != 0 {
		t.Fatalf("expected 0 profiles after delete, got %v", names)
	}
}

func TestLoadProfileNotFound(t *testing.T) {
	dir := t.TempDir()
	origDir := ProfilesDir
	ProfilesDir = func() string { return dir }
	defer func() { ProfilesDir = origDir }()

	_, err := LoadProfile("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent profile")
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
