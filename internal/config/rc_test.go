package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseRCFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bitchtearc")
	content := `# bitchtearc — startup commands
set provider anthropic
set model claude-opus-4-6

# blank lines and comments are ignored
set apikey sk-ant-test-key-1234567890

join #code
`
	os.WriteFile(path, []byte(content), 0644)

	lines := parseRCFile(path)
	want := []string{
		"set provider anthropic",
		"set model claude-opus-4-6",
		"set apikey sk-ant-test-key-1234567890",
		"join #code",
	}
	if len(lines) != len(want) {
		t.Fatalf("got %d lines, want %d: %v", len(lines), len(want), lines)
	}
	for i, got := range lines {
		if got != want[i] {
			t.Errorf("line %d: got %q, want %q", i, got, want[i])
		}
	}
}

func TestParseRCFileMissing(t *testing.T) {
	lines := parseRCFile("/nonexistent/bitchtearc")
	if lines != nil {
		t.Fatalf("expected nil for missing file, got %v", lines)
	}
}

func TestApplyRCSetCommands(t *testing.T) {
	cfg := DefaultConfig()
	lines := []string{
		"set provider anthropic",
		"set model claude-opus-4-6",
		"set nick testuser",
		"set auto-next on",
		"join #code",
		"query buddy",
	}

	remaining := ApplyRCSetCommands(&cfg, lines)

	if cfg.Provider != "anthropic" {
		t.Errorf("provider = %q, want anthropic", cfg.Provider)
	}
	if cfg.Model != "claude-opus-4-6" {
		t.Errorf("model = %q, want claude-opus-4-6", cfg.Model)
	}
	if cfg.UserNick != "testuser" {
		t.Errorf("nick = %q, want testuser", cfg.UserNick)
	}
	if !cfg.AutoNextSteps {
		t.Error("auto-next should be on")
	}
	if len(remaining) != 2 {
		t.Fatalf("expected 2 remaining lines, got %d: %v", len(remaining), remaining)
	}
	if remaining[0] != "join #code" || remaining[1] != "query buddy" {
		t.Errorf("unexpected remaining: %v", remaining)
	}
}

func TestApplyRCSetCommandsProfile(t *testing.T) {
	dir := t.TempDir()
	origDir := ProfilesDir
	ProfilesDir = func() string { return dir }
	defer func() { ProfilesDir = origDir }()

	// Save a test profile.
	SaveProfile(Profile{
		Name:     "mytest",
		Provider: "anthropic",
		BaseURL:  "https://test.example.com/v1",
		Model:    "test-model",
		APIKey:   "sk-test-profile-key",
	})

	cfg := DefaultConfig()
	remaining := ApplyRCSetCommands(&cfg, []string{"set profile mytest"})

	if len(remaining) != 0 {
		t.Fatalf("expected no remaining, got %v", remaining)
	}
	if cfg.Provider != "anthropic" {
		t.Errorf("provider = %q, want anthropic", cfg.Provider)
	}
	if cfg.Model != "test-model" {
		t.Errorf("model = %q, want test-model", cfg.Model)
	}
	if cfg.Profile != "mytest" {
		t.Errorf("profile = %q, want mytest", cfg.Profile)
	}
}

func TestApplyRCSetCommandsInvalidProviderIgnored(t *testing.T) {
	cfg := DefaultConfig()
	original := cfg.Provider
	ApplyRCSetCommands(&cfg, []string{"set provider badprov"})
	if cfg.Provider != original {
		t.Errorf("invalid provider should not change config, got %q", cfg.Provider)
	}
}

func TestParseBoolSetting(t *testing.T) {
	for _, v := range []string{"on", "true", "1", "yes", "ON", "True"} {
		if !parseBoolSetting(v) {
			t.Errorf("parseBoolSetting(%q) = false, want true", v)
		}
	}
	for _, v := range []string{"off", "false", "0", "no", ""} {
		if parseBoolSetting(v) {
			t.Errorf("parseBoolSetting(%q) = true, want false", v)
		}
	}
}
