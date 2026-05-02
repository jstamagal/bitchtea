package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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

func TestDetectProviderOpenRouter(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("OPENROUTER_API_KEY", "sk-or-test")
	t.Setenv("ZAI_API_KEY", "")
	t.Setenv("BITCHTEA_MODEL", "")

	cfg := DefaultConfig()
	DetectProvider(&cfg)

	if cfg.Provider != "openai" {
		t.Fatalf("expected openai provider, got %q", cfg.Provider)
	}
	if cfg.Profile != "openrouter" {
		t.Fatalf("expected openrouter profile, got %q", cfg.Profile)
	}
	if cfg.APIKey != "sk-or-test" {
		t.Fatalf("expected openrouter api key, got %q", cfg.APIKey)
	}
	if cfg.BaseURL != "https://openrouter.ai/api/v1" {
		t.Fatalf("unexpected base URL: %q", cfg.BaseURL)
	}
}

func TestDetectProviderZAI(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("OPENROUTER_API_KEY", "")
	t.Setenv("ZAI_API_KEY", "sk-zai-test")
	t.Setenv("BITCHTEA_MODEL", "")

	cfg := DefaultConfig()
	DetectProvider(&cfg)

	if cfg.Provider != "openai" {
		t.Fatalf("expected openai provider, got %q", cfg.Provider)
	}
	if cfg.Profile != "zai-openai" {
		t.Fatalf("expected zai-openai profile, got %q", cfg.Profile)
	}
	if cfg.APIKey != "sk-zai-test" {
		t.Fatalf("expected zai api key, got %q", cfg.APIKey)
	}
	if cfg.BaseURL != "https://api.z.ai/api/coding/paas/v4" {
		t.Fatalf("unexpected base URL: %q", cfg.BaseURL)
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
	if len(names) != 16 {
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
	if len(names) != 15 {
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
	for _, want := range []string{"aihubmix", "avian", "copilot", "cortecs", "custom", "huggingface", "ionet", "nebius", "ollama", "openrouter", "synthetic", "venice", "vercel", "xai", "zai-anthropic", "zai-openai"} {
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

func TestMigrateDataPaths(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	// Create old XDG dirs with a sentinel file
	oldSessions := home + "/.local/share/bitchtea/sessions"
	oldProfiles := home + "/.config/bitchtea/profiles"
	os.MkdirAll(oldSessions, 0755)
	os.MkdirAll(oldProfiles, 0755)
	os.WriteFile(oldSessions+"/test.jsonl", []byte("old-session"), 0644)
	os.WriteFile(oldProfiles+"/myprofile.json", []byte("{}"), 0644)

	if err := MigrateDataPaths(); err != nil {
		t.Fatalf("migration failed: %v", err)
	}

	// New paths should exist
	newSessions := home + "/.bitchtea/sessions"
	newProfiles := home + "/.bitchtea/profiles"
	if _, err := os.Stat(newSessions + "/test.jsonl"); err != nil {
		t.Fatalf("session file not migrated: %v", err)
	}
	if _, err := os.Stat(newProfiles + "/myprofile.json"); err != nil {
		t.Fatalf("profile file not migrated: %v", err)
	}

	// Old paths should be gone
	if _, err := os.Stat(oldSessions); !os.IsNotExist(err) {
		t.Fatal("old sessions dir should have been moved")
	}
	if _, err := os.Stat(oldProfiles); !os.IsNotExist(err) {
		t.Fatal("old profiles dir should have been moved")
	}
}

func TestMigrateDataPathsSkipsIfNewExists(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	oldSessions := home + "/.local/share/bitchtea/sessions"
	newSessions := home + "/.bitchtea/sessions"
	os.MkdirAll(oldSessions, 0755)
	os.MkdirAll(newSessions, 0755)
	os.WriteFile(oldSessions+"/old.jsonl", []byte("old"), 0644)
	os.WriteFile(newSessions+"/new.jsonl", []byte("new"), 0644)

	if err := MigrateDataPaths(); err != nil {
		t.Fatalf("migration failed: %v", err)
	}

	// Old should still exist (not clobbered)
	if _, err := os.Stat(oldSessions + "/old.jsonl"); err != nil {
		t.Fatal("old file should still exist when new dir already present")
	}
	// New should still have its own file
	if _, err := os.Stat(newSessions + "/new.jsonl"); err != nil {
		t.Fatal("new file should still exist")
	}
}

// TestBuiltinProfilesServiceIdentity asserts that every built-in profile
// carries Provider as the wire format and Service as its upstream identity.
// See docs/phase-9-service-identity.md for the mapping source of truth.
func TestBuiltinProfilesServiceIdentity(t *testing.T) {
	// Clear API-key env vars so builtinProfile() doesn't populate APIKey
	// from the host environment.
	for _, env := range []string{
		"OPENROUTER_API_KEY", "AIHUBMIX_API_KEY", "AVIAN_API_KEY",
		"GITHUB_TOKEN", "COPILOT_API_KEY", "CORTECS_API_KEY",
		"HUGGINGFACE_API_KEY", "IONET_API_KEY", "NEBIUS_API_KEY",
		"SYNTHETIC_API_KEY", "VENICE_API_KEY", "VERCEL_API_KEY",
		"XAI_API_KEY", "ZAI_API_KEY",
	} {
		t.Setenv(env, "")
	}

	cases := []struct {
		name            string
		wantProvider    string
		wantService     string
	}{
		{"ollama", "openai", "ollama"},
		{"openrouter", "openai", "openrouter"},
		{"aihubmix", "openai", "aihubmix"},
		{"avian", "openai", "avian"},
		{"copilot", "openai", "copilot"},
		{"cortecs", "openai", "cortecs"},
		{"huggingface", "openai", "huggingface"},
		{"ionet", "openai", "ionet"},
		{"nebius", "openai", "nebius"},
		{"synthetic", "openai", "synthetic"},
		{"venice", "openai", "venice"},
		{"vercel", "openai", "vercel"},
		{"xai", "openai", "xai"},
		{"zai-openai", "openai", "zai-openai"},
		{"zai-anthropic", "anthropic", "zai-anthropic"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, ok := builtinProfile(tc.name)
			if !ok {
				t.Fatalf("built-in profile %q not found", tc.name)
			}
			if p.Provider != tc.wantProvider {
				t.Errorf("Provider: want %q, got %q", tc.wantProvider, p.Provider)
			}
			if p.Service != tc.wantService {
				t.Errorf("Service: want %q, got %q", tc.wantService, p.Service)
			}
		})
	}
}

// TestDefaultConfigService asserts the zero-env default Config carries
// Service="openai" matching the default OpenAI BaseURL.
func TestDefaultConfigService(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Service != "openai" {
		t.Fatalf("expected Service=openai, got %q", cfg.Service)
	}
}

// TestDetectProviderSetsService verifies env-detection branches set Service
// to the upstream identity, not just Provider.
func TestDetectProviderSetsService(t *testing.T) {
	cases := []struct {
		name        string
		env         map[string]string
		wantSvc     string
		wantProv    string
	}{
		{
			name:     "anthropic",
			env:      map[string]string{"ANTHROPIC_API_KEY": "sk-ant"},
			wantSvc:  "anthropic",
			wantProv: "anthropic",
		},
		{
			name:     "openrouter",
			env:      map[string]string{"OPENROUTER_API_KEY": "sk-or"},
			wantSvc:  "openrouter",
			wantProv: "openai",
		},
		{
			name:     "zai",
			env:      map[string]string{"ZAI_API_KEY": "sk-zai"},
			wantSvc:  "zai-openai",
			wantProv: "openai",
		},
		{
			name:     "openai",
			env:      map[string]string{"OPENAI_API_KEY": "sk-oa"},
			wantSvc:  "openai",
			wantProv: "openai",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Clear all detection env vars first.
			for _, k := range []string{"ANTHROPIC_API_KEY", "OPENROUTER_API_KEY", "ZAI_API_KEY", "OPENAI_API_KEY", "BITCHTEA_MODEL"} {
				t.Setenv(k, "")
			}
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			cfg := DefaultConfig()
			cfg.APIKey = "" // DefaultConfig may have read OPENAI_API_KEY
			DetectProvider(&cfg)
			if cfg.Service != tc.wantSvc {
				t.Errorf("Service: want %q, got %q", tc.wantSvc, cfg.Service)
			}
			if cfg.Provider != tc.wantProv {
				t.Errorf("Provider: want %q, got %q", tc.wantProv, cfg.Provider)
			}
		})
	}
}

// TestProfileServiceLazyMigrationByName loads a profile JSON without a
// "service" field whose name matches a built-in; the loader should derive
// Service from the built-in mapping.
func TestProfileServiceLazyMigrationByName(t *testing.T) {
	dir := t.TempDir()
	origDir := ProfilesDir
	ProfilesDir = func() string { return dir }
	defer func() { ProfilesDir = origDir }()

	raw := `{"name":"openrouter","provider":"openai","base_url":"https://openrouter.ai/api/v1","api_key":"sk-x","model":"foo"}`
	if err := os.WriteFile(filepath.Join(dir, "openrouter.json"), []byte(raw), 0600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	p, err := LoadProfile("openrouter")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if p.Service != "openrouter" {
		t.Fatalf("expected lazy-derived Service=openrouter, got %q", p.Service)
	}
}

// TestProfileServiceLazyMigrationByHost loads a profile whose name does not
// match a built-in, but whose BaseURL host matches one. Service should be
// derived from the host match.
func TestProfileServiceLazyMigrationByHost(t *testing.T) {
	dir := t.TempDir()
	origDir := ProfilesDir
	ProfilesDir = func() string { return dir }
	defer func() { ProfilesDir = origDir }()

	raw := `{"name":"my-or","provider":"openai","base_url":"https://openrouter.ai/api/v1","api_key":"sk-x","model":"foo"}`
	if err := os.WriteFile(filepath.Join(dir, "my-or.json"), []byte(raw), 0600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	p, err := LoadProfile("my-or")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if p.Service != "openrouter" {
		t.Fatalf("expected host-derived Service=openrouter, got %q", p.Service)
	}
}

// TestProfileServiceLazyMigrationFallback loads a profile whose name and host
// do not match any built-in; Service should fall back to "custom".
func TestProfileServiceLazyMigrationFallback(t *testing.T) {
	dir := t.TempDir()
	origDir := ProfilesDir
	ProfilesDir = func() string { return dir }
	defer func() { ProfilesDir = origDir }()

	raw := `{"name":"weird","provider":"openai","base_url":"https://example.com/v1","api_key":"sk-x","model":"foo"}`
	if err := os.WriteFile(filepath.Join(dir, "weird.json"), []byte(raw), 0600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	p, err := LoadProfile("weird")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if p.Service != "custom" {
		t.Fatalf("expected fallback Service=custom, got %q", p.Service)
	}
}

// TestProfileServiceOmitemptyOnMarshal asserts that a Profile with empty
// Service marshals to JSON without a "service" key, preserving compatibility
// with old binaries.
func TestProfileServiceOmitemptyOnMarshal(t *testing.T) {
	p := Profile{
		Name:     "foo",
		Provider: "openai",
		BaseURL:  "https://example.com/v1",
		APIKey:   "sk-x",
		Model:    "m",
	}
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(data), "service") {
		t.Fatalf("expected no \"service\" key in JSON, got %s", data)
	}
}

// TestProfileServicePersistsOnMarshal asserts that a Profile with a non-empty
// Service marshals it into the JSON output.
func TestProfileServicePersistsOnMarshal(t *testing.T) {
	p := Profile{
		Name:     "openrouter",
		Provider: "openai",
		Service:  "openrouter",
		BaseURL:  "https://openrouter.ai/api/v1",
		APIKey:   "sk-x",
		Model:    "m",
	}
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(data), `"service":"openrouter"`) {
		t.Fatalf("expected \"service\":\"openrouter\" in JSON, got %s", data)
	}
}

// TestApplyProfileCopiesService verifies ApplyProfile lifts Service onto the
// target Config when the profile carries one.
func TestApplyProfileCopiesService(t *testing.T) {
	cfg := Config{Service: "openai"}
	p := &Profile{Provider: "openai", Service: "openrouter", BaseURL: "https://openrouter.ai/api/v1"}
	ApplyProfile(&cfg, p)
	if cfg.Service != "openrouter" {
		t.Fatalf("expected Service=openrouter, got %q", cfg.Service)
	}
}

// TestApplySetClobbersServiceForCustom verifies that setting provider or
// baseurl via the rc/`/set` machinery downgrades Service to "custom" so
// per-service gates don't trust a stale identity.
func TestApplySetClobbersServiceForCustom(t *testing.T) {
	cfg := Config{Provider: "openai", Service: "openrouter"}
	if !ApplySet(&cfg, "baseurl", "https://example.com/v1") {
		t.Fatal("ApplySet baseurl returned false")
	}
	if cfg.Service != "custom" {
		t.Fatalf("expected Service=custom after baseurl change, got %q", cfg.Service)
	}

	cfg = Config{Provider: "openai", Service: "openrouter"}
	if !ApplySet(&cfg, "provider", "anthropic") {
		t.Fatal("ApplySet provider returned false")
	}
	if cfg.Service != "custom" {
		t.Fatalf("expected Service=custom after provider change, got %q", cfg.Service)
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
