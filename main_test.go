package main

import (
	"testing"

	"github.com/jstamagal/bitchtea/internal/config"
)

// TestCLIModelOverridesProfile verifies that -m overrides the profile default
// even when -p is specified. This covers the regression fixed in aad273e.
func TestCLIModelOverridesProfile(t *testing.T) {
	cfg := config.DefaultConfig()

	// Simulate: bitchtea -p ollama -m gemma4:latest
	opts, err := parseCLIArgs([]string{"-p", "ollama", "-m", "gemma4:latest"}, &cfg)
	if err != nil {
		t.Fatalf("parseCLIArgs: %v", err)
	}
	if opts.profileName != "ollama" {
		t.Fatalf("expected profileName=ollama, got %q", opts.profileName)
	}
	if cfg.Model != "gemma4:latest" {
		t.Fatalf("expected model=gemma4:latest after first parse, got %q", cfg.Model)
	}

	// Simulate ApplyProfile (ollama profile sets model to llama3.2)
	ollamaProfile := &config.Profile{
		Name:     "ollama",
		Provider: "openai",
		BaseURL:  "http://localhost:11434/v1",
		Model:    "llama3.2",
	}
	config.ApplyProfile(&cfg, ollamaProfile)
	cfg.Profile = opts.profileName

	if cfg.Model != "llama3.2" {
		t.Fatalf("ApplyProfile should have set model=llama3.2, got %q", cfg.Model)
	}

	// Re-parse — the -m flag must win over profile default
	_, _ = parseCLIArgs([]string{"-p", "ollama", "-m", "gemma4:latest"}, &cfg)

	if cfg.Model != "gemma4:latest" {
		t.Fatalf("re-parse: expected model=gemma4:latest (CLI overrides profile), got %q", cfg.Model)
	}
	if cfg.Profile != "ollama" {
		t.Fatalf("re-parse must not clear cfg.Profile, got %q", cfg.Profile)
	}
}

// TestCLIModelOverridesProfileOpenRouter exercises the openrouter profile path
// and a model name containing a slash (qwen/qwen3.6-plus).
func TestCLIModelOverridesProfileOpenRouter(t *testing.T) {
	cfg := config.DefaultConfig()

	args := []string{"-p", "openrouter", "-m", "qwen/qwen3.6-plus"}
	opts, err := parseCLIArgs(args, &cfg)
	if err != nil {
		t.Fatalf("parseCLIArgs: %v", err)
	}
	if opts.profileName != "openrouter" {
		t.Fatalf("expected profileName=openrouter, got %q", opts.profileName)
	}

	// Simulate ApplyProfile for openrouter
	openrouterProfile := &config.Profile{
		Name:     "openrouter",
		Provider: "openai",
		BaseURL:  "https://openrouter.ai/api/v1",
		Model:    "anthropic/claude-sonnet-4",
		APIKey:   "sk-or-test",
	}
	config.ApplyProfile(&cfg, openrouterProfile)
	cfg.Profile = opts.profileName

	if cfg.Model != "anthropic/claude-sonnet-4" {
		t.Fatalf("ApplyProfile should have set openrouter default model, got %q", cfg.Model)
	}

	// Re-parse — qwen model must win
	_, _ = parseCLIArgs(args, &cfg)

	if cfg.Model != "qwen/qwen3.6-plus" {
		t.Fatalf("re-parse: expected model=qwen/qwen3.6-plus (CLI overrides profile), got %q", cfg.Model)
	}
	if cfg.Profile != "openrouter" {
		t.Fatalf("re-parse must not clear cfg.Profile, got %q", cfg.Profile)
	}
	if cfg.Provider != "openai" {
		t.Fatalf("provider should remain openai (openrouter uses openai-compat API), got %q", cfg.Provider)
	}
}

// TestCLIProfileWithoutModel ensures -p alone uses the profile's default model.
func TestCLIProfileWithoutModel(t *testing.T) {
	cfg := config.DefaultConfig()

	opts, err := parseCLIArgs([]string{"-p", "ollama"}, &cfg)
	if err != nil {
		t.Fatalf("parseCLIArgs: %v", err)
	}
	if opts.profileName != "ollama" {
		t.Fatalf("expected profileName=ollama, got %q", opts.profileName)
	}

	ollamaProfile := &config.Profile{
		Name:     "ollama",
		Provider: "openai",
		BaseURL:  "http://localhost:11434/v1",
		Model:    "llama3.2",
	}
	config.ApplyProfile(&cfg, ollamaProfile)
	cfg.Profile = opts.profileName
	_, _ = parseCLIArgs([]string{"-p", "ollama"}, &cfg)

	// No -m flag → profile default should stand
	if cfg.Model != "llama3.2" {
		t.Fatalf("without -m, expected profile model=llama3.2, got %q", cfg.Model)
	}
}
