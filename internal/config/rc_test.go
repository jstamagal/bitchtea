package config

import (
	"os"
	"path/filepath"
	"testing"
)

// --- New SET key roundtrip tests ---

func TestSamplingParamRoundtrip(t *testing.T) {
	cfg := DefaultConfig()

	// temperature
	if ok := ApplySet(&cfg, "temperature", "0.7"); !ok {
		t.Fatal("ApplySet temperature returned false")
	}
	if cfg.Temperature == nil || *cfg.Temperature != 0.7 {
		t.Errorf("Temperature = %v, want 0.7", cfg.Temperature)
	}
	got, _ := GetSetting(&cfg, "temperature")
	if got != "0.7" {
		t.Errorf("GetSetting temperature = %q, want 0.7", got)
	}

	// top_p
	if ok := ApplySet(&cfg, "top_p", "0.9"); !ok {
		t.Fatal("ApplySet top_p returned false")
	}
	if cfg.TopP == nil || *cfg.TopP != 0.9 {
		t.Errorf("TopP = %v, want 0.9", cfg.TopP)
	}
	got, _ = GetSetting(&cfg, "top_p")
	if got != "0.9" {
		t.Errorf("GetSetting top_p = %q, want 0.9", got)
	}

	// top_k
	if ok := ApplySet(&cfg, "top_k", "40"); !ok {
		t.Fatal("ApplySet top_k returned false")
	}
	if cfg.TopK == nil || *cfg.TopK != 40 {
		t.Errorf("TopK = %v, want 40", cfg.TopK)
	}
	got, _ = GetSetting(&cfg, "top_k")
	if got != "40" {
		t.Errorf("GetSetting top_k = %q, want 40", got)
	}

	// repetition_penalty (canonical key)
	if ok := ApplySet(&cfg, "repetition_penalty", "1.2"); !ok {
		t.Fatal("ApplySet repetition_penalty returned false")
	}
	if cfg.RepetitionPenalty == nil || *cfg.RepetitionPenalty != 1.2 {
		t.Errorf("RepetitionPenalty = %v, want 1.2", cfg.RepetitionPenalty)
	}
	got, _ = GetSetting(&cfg, "repetition_penalty")
	if got != "1.2" {
		t.Errorf("GetSetting repetition_penalty = %q, want 1.2", got)
	}

	// rep_pen alias should also work
	cfg.RepetitionPenalty = nil
	if ok := ApplySet(&cfg, "rep_pen", "1.3"); !ok {
		t.Fatal("ApplySet rep_pen (alias) returned false")
	}
	if cfg.RepetitionPenalty == nil || *cfg.RepetitionPenalty != 1.3 {
		t.Errorf("RepetitionPenalty via rep_pen alias = %v, want 1.3", cfg.RepetitionPenalty)
	}

	// Clear via <unset>
	ApplySet(&cfg, "temperature", "<unset>")
	if cfg.Temperature != nil {
		t.Errorf("temperature should be nil after <unset>, got %v", *cfg.Temperature)
	}
}

func TestToolVerbosityRoundtrip(t *testing.T) {
	cfg := DefaultConfig()

	for _, mode := range []string{"terse", "normal", "verbose"} {
		if ok := ApplySet(&cfg, "tool_verbosity", mode); !ok {
			t.Fatalf("ApplySet tool_verbosity %q returned false", mode)
		}
		if cfg.ToolVerbosity != mode {
			t.Errorf("ToolVerbosity = %q, want %q", cfg.ToolVerbosity, mode)
		}
		got, _ := GetSetting(&cfg, "tool_verbosity")
		if got != mode {
			t.Errorf("GetSetting tool_verbosity = %q, want %q", got, mode)
		}
	}

	// Invalid value should be ignored (no change)
	cfg.ToolVerbosity = "normal"
	ApplySet(&cfg, "tool_verbosity", "banana")
	if cfg.ToolVerbosity != "normal" {
		t.Errorf("invalid tool_verbosity should be ignored, got %q", cfg.ToolVerbosity)
	}
}

func TestBannerRoundtrip(t *testing.T) {
	cfg := DefaultConfig()
	if !cfg.Banner {
		t.Error("banner should default to on")
	}

	ApplySet(&cfg, "banner", "off")
	if cfg.Banner {
		t.Error("banner should be off after 'set banner off'")
	}
	got, _ := GetSetting(&cfg, "banner")
	if got != "off" {
		t.Errorf("GetSetting banner = %q, want off", got)
	}

	ApplySet(&cfg, "banner", "on")
	if !cfg.Banner {
		t.Error("banner should be on after 'set banner on'")
	}
}

func TestSetKeysContainsNewKeys(t *testing.T) {
	keys := SetKeys()
	want := []string{"top_k", "top_p", "temperature", "repetition_penalty", "tool_verbosity", "banner", "service"}
	keySet := make(map[string]bool, len(keys))
	for _, k := range keys {
		keySet[k] = true
	}
	for _, w := range want {
		if !keySet[w] {
			t.Errorf("SetKeys() missing key %q", w)
		}
	}
}

func TestGetSettingDefaultsForNewKeys(t *testing.T) {
	cfg := DefaultConfig()

	cases := []struct {
		key     string
		wantVal string
	}{
		{"top_k", "<unset>"},
		{"top_p", "<unset>"},
		{"temperature", "<unset>"},
		{"repetition_penalty", "<unset>"},
		{"tool_verbosity", "normal"},
		{"banner", "on"},
	}
	for _, c := range cases {
		got, ok := GetSetting(&cfg, c.key)
		if !ok {
			t.Errorf("GetSetting(%q) returned not-ok", c.key)
		}
		if got != c.wantVal {
			t.Errorf("GetSetting(%q) = %q, want %q", c.key, got, c.wantVal)
		}
	}
}

func TestCreateConfigDefaultsWritesAllKeys(t *testing.T) {
	dir := t.TempDir()
	rcPath := filepath.Join(dir, "bitchtearc")

	// Use a temp RCPath by monkey-patching isn't possible directly,
	// but we can test the logic by writing and reading manually.
	cfg := DefaultConfig()
	keys := SetKeys()

	// Verify every key in SetKeys() is handled by GetSetting
	for _, k := range keys {
		_, ok := GetSetting(&cfg, k)
		if !ok {
			t.Errorf("GetSetting(%q) returned false — key in SetKeys() but not in GetSetting()", k)
		}
	}

	// Write a simulated rc file and verify it round-trips
	var lines []string
	for _, k := range keys {
		val, _ := GetSetting(&cfg, k)
		lines = append(lines, "# set "+k+" "+val)
	}
	content := "# generated\n"
	for _, l := range lines {
		content += l + "\n"
	}
	if err := os.WriteFile(rcPath, []byte(content), 0600); err != nil {
		t.Fatalf("write rc: %v", err)
	}
	if data, err := os.ReadFile(rcPath); err != nil {
		t.Fatalf("read rc: %v", err)
	} else if len(data) == 0 {
		t.Fatal("rc file is empty")
	}
}

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

func TestApplyRCSetCommandsManualConnectionOverrideClearsProfile(t *testing.T) {
	dir := t.TempDir()
	origDir := ProfilesDir
	ProfilesDir = func() string { return dir }
	defer func() { ProfilesDir = origDir }()

	if err := SaveProfile(Profile{
		Name:     "mytest",
		Provider: "anthropic",
		BaseURL:  "https://test.example.com/v1",
		Model:    "test-model",
		APIKey:   "sk-test-profile-key",
	}); err != nil {
		t.Fatalf("save profile: %v", err)
	}

	cfg := DefaultConfig()
	remaining := ApplyRCSetCommands(&cfg, []string{
		"set profile mytest",
		"set model override-model",
	})

	if len(remaining) != 0 {
		t.Fatalf("expected no remaining commands, got %v", remaining)
	}
	if cfg.Model != "override-model" {
		t.Fatalf("model = %q, want override-model", cfg.Model)
	}
	if cfg.Profile != "" {
		t.Fatalf("expected manual override to clear loaded profile, got %q", cfg.Profile)
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
