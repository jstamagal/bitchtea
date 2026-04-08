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
	if len(names) != 5 {
		t.Fatalf("expected built-ins plus saved profile, got %v", names)
	}
	found := false
	for _, name := range names {
		if name == "test-zai" {
			found = true
			break
		}
	}
	if !found {
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
	if len(names) != 4 {
		t.Fatalf("expected only built-in profiles after delete, got %v", names)
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

func TestResolveBuiltinProfile(t *testing.T) {
	t.Setenv("OPENROUTER_API_KEY", "openrouter-key")

	p, err := ResolveProfile("openrouter")
	if err != nil {
		t.Fatalf("resolve builtin profile: %v", err)
	}
	if p.Provider != "openai" {
		t.Fatalf("expected openai provider, got %q", p.Provider)
	}
	if p.BaseURL != "https://openrouter.ai/api/v1" {
		t.Fatalf("unexpected base URL: %q", p.BaseURL)
	}
	if p.APIKey != "openrouter-key" {
		t.Fatalf("expected env-backed API key, got %q", p.APIKey)
	}
	if p.Model == "" {
		t.Fatal("builtin profile should set a default model")
	}
}

func TestListProfilesIncludesBuiltins(t *testing.T) {
	dir := t.TempDir()
	origDir := ProfilesDir
	ProfilesDir = func() string { return dir }
	defer func() { ProfilesDir = origDir }()

	if err := SaveProfile(Profile{Name: "custom"}); err != nil {
		t.Fatalf("save custom profile: %v", err)
	}

	names := ListProfiles()
	for _, want := range []string{"custom", "ollama", "openrouter", "zai-anthropic", "zai-openai"} {
		found := false
		for _, got := range names {
			if got == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("expected profile list to contain %q, got %v", want, names)
		}
	}
}

func TestProfileAllowsEmptyAPIKey(t *testing.T) {
	if !ProfileAllowsEmptyAPIKey(Config{Provider: "openai", BaseURL: "http://localhost:11434/v1"}) {
		t.Fatal("ollama-compatible localhost endpoint should allow empty API key")
	}
	if ProfileAllowsEmptyAPIKey(Config{Provider: "anthropic", BaseURL: "http://localhost:11434/v1"}) {
		t.Fatal("anthropic transport should still require an API key")
	}
	if ProfileAllowsEmptyAPIKey(Config{Provider: "openai", BaseURL: "https://api.openai.com/v1"}) {
		t.Fatal("hosted openai endpoints should still require an API key")
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
