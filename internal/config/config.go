package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"sort"
	"strings"
)

// BaseDir returns the root data directory: ~/.bitchtea/
func BaseDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".bitchtea")
}

// Config holds all runtime configuration
type Config struct {
	// API
	APIKey   string
	BaseURL  string
	Model    string
	Provider string
	Profile  string // The name of the loaded profile, if any

	// Agent behavior
	AutoNextSteps bool
	AutoNextIdea  bool
	MaxTokens     int

	// UI
	UserNick          string
	AgentNick         string
	NotificationSound bool
	SoundType         string

	// Paths
	WorkDir    string
	SessionDir string
	LogDir     string
}

// DefaultConfig returns a config with sane defaults
func DefaultConfig() Config {
	wd, _ := os.Getwd()
	home, _ := os.UserHomeDir()
	username := "anon"
	if u, err := user.Current(); err == nil {
		username = u.Username
	}

	return Config{
		APIKey:   os.Getenv("OPENAI_API_KEY"),
		BaseURL:  envOr("OPENAI_BASE_URL", "https://api.openai.com/v1"),
		Model:    envOr("BITCHTEA_MODEL", "gpt-4o"),
		Provider: envOr("BITCHTEA_PROVIDER", "openai"),

		AutoNextSteps: false,
		AutoNextIdea:  false,
		MaxTokens:     4096,

		UserNick:          username,
		AgentNick:         "bitchtea",
		NotificationSound: false,
		SoundType:         "bell",

		WorkDir:    wd,
		SessionDir: filepath.Join(home, ".bitchtea", "sessions"),
		LogDir:     filepath.Join(home, ".bitchtea", "logs"),
	}
}

// DetectProvider figures out which API to use based on env vars
func DetectProvider(cfg *Config) {
	if key := os.Getenv("ANTHROPIC_API_KEY"); key != "" && cfg.APIKey == "" {
		cfg.APIKey = key
		cfg.BaseURL = envOr("ANTHROPIC_BASE_URL", "https://api.anthropic.com/v1")
		cfg.Provider = "anthropic"
		if cfg.Model == "gpt-4o" {
			cfg.Model = "claude-sonnet-4-20250514"
		}
	} else if key := os.Getenv("OPENAI_API_KEY"); key != "" && cfg.APIKey == "" {
		cfg.APIKey = key
		cfg.Provider = "openai"
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// Profile is a saved connection configuration
type Profile struct {
	Name     string `json:"name"`
	Provider string `json:"provider"`
	BaseURL  string `json:"base_url"`
	APIKey   string `json:"api_key"`
	Model    string `json:"model"`
}

// TODO(Phase6): The Provider field in Profile (and Config) currently encodes
// the wire format ("openai" or "anthropic"), not the actual upstream service.
// As a result every Ollama/OpenRouter/aihubmix/etc. profile carries
// Provider:"openai" even though they are distinct services with different
// capabilities, quirks, and required headers.
//
// Known breakage caused by this:
//   - OpenRouter reasoning params (extended thinking) cannot be gated on
//     service identity; URL sniffing in llm/client.go is used as a workaround.
//   - Ollama's stream_options.include_usage is silently ignored; cannot be
//     suppressed without URL sniffing.
//   - Anthropic prompt caching (cache_control blocks) cannot be activated
//     for proxies that forward the Anthropic wire format (e.g. zai-anthropic)
//     without knowing whether we're talking to native Anthropic or a proxy.
//   - ProfileAllowsEmptyAPIKey uses URL-prefix matching to detect Ollama.
//
// Phase 6 fix: add a Service/Identity field (e.g. "ollama", "openrouter",
// "anthropic", "openai") alongside Provider (wire format). All per-service
// behaviour should gate on Service, not Provider.
type builtinProfileSpec struct {
	Provider  string
	BaseURL   string
	Model     string
	APIKeyEnv []string
}

var builtinProfiles = map[string]builtinProfileSpec{
	"ollama": {
		Provider: "openai",
		BaseURL:  "http://localhost:11434/v1",
		Model:    "llama3.2",
	},
	"openrouter": {
		Provider:  "openai",
		BaseURL:   "https://openrouter.ai/api/v1",
		Model:     "anthropic/claude-sonnet-4",
		APIKeyEnv: []string{"OPENROUTER_API_KEY"},
	},
	"aihubmix": {
		Provider:  "openai",
		BaseURL:   "https://aihubmix.com/v1",
		Model:     "gpt-4o",
		APIKeyEnv: []string{"AIHUBMIX_API_KEY"},
	},
	"avian": {
		Provider:  "openai",
		BaseURL:   "https://api.avian.io/v1",
		Model:     "llama3",
		APIKeyEnv: []string{"AVIAN_API_KEY"},
	},
	"copilot": {
		Provider:  "openai",
		BaseURL:   "https://api.githubcopilot.com",
		Model:     "gpt-4o",
		APIKeyEnv: []string{"GITHUB_TOKEN", "COPILOT_API_KEY"},
	},
	"cortecs": {
		Provider:  "openai",
		BaseURL:   "https://api.cortecs.ai/v1",
		Model:     "cortecs-model",
		APIKeyEnv: []string{"CORTECS_API_KEY"},
	},
	"huggingface": {
		Provider:  "openai",
		BaseURL:   "https://router.huggingface.co/v1",
		Model:     "meta-llama/Llama-3.1-8B-Instruct",
		APIKeyEnv: []string{"HUGGINGFACE_API_KEY"},
	},
	"ionet": {
		Provider:  "openai",
		BaseURL:   "https://api.intelligence.io.solutions/api/v1",
		Model:     "io-net-model",
		APIKeyEnv: []string{"IONET_API_KEY"},
	},
	"nebius": {
		Provider:  "openai",
		BaseURL:   "https://api.tokenfactory.nebius.com/v1",
		Model:     "meta-llama/Meta-Llama-3.1-70B-Instruct",
		APIKeyEnv: []string{"NEBIUS_API_KEY"},
	},
	"synthetic": {
		Provider:  "openai",
		BaseURL:   "https://api.synthetic.new/openai/v1",
		Model:     "synthetic-model",
		APIKeyEnv: []string{"SYNTHETIC_API_KEY"},
	},
	"venice": {
		Provider:  "openai",
		BaseURL:   "https://api.venice.ai/api/v1",
		Model:     "venice-model",
		APIKeyEnv: []string{"VENICE_API_KEY"},
	},
	"vercel": {
		Provider:  "openai",
		BaseURL:   "https://ai-gateway.vercel.sh/v1",
		Model:     "openai:gpt-4o",
		APIKeyEnv: []string{"VERCEL_API_KEY"},
	},
	"xai": {
		Provider:  "openai",
		BaseURL:   "https://api.x.ai/v1",
		Model:     "grok-beta",
		APIKeyEnv: []string{"XAI_API_KEY"},
	},
	"zai-openai": {
		Provider:  "openai",
		BaseURL:   "https://api.z.ai/api/coding/paas/v4",
		Model:     "GLM-4.7",
		APIKeyEnv: []string{"ZAI_API_KEY"},
	},
	"zai-anthropic": {
		Provider:  "anthropic",
		BaseURL:   "https://api.z.ai/api/anthropic",
		Model:     "GLM-4.7",
		APIKeyEnv: []string{"ZAI_API_KEY"},
	},
}

// ProfilesDir returns the directory where profiles are stored.
// It's a variable so tests can override it.
var ProfilesDir = func() string {
	return filepath.Join(BaseDir(), "profiles")
}

// SaveProfile writes a profile to disk
func SaveProfile(p Profile) error {
	dir := ProfilesDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create profiles dir: %w", err)
	}

	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal profile: %w", err)
	}

	path := filepath.Join(dir, p.Name+".json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("write profile: %w", err)
	}
	return nil
}

// LoadProfile reads a profile from disk
func LoadProfile(name string) (*Profile, error) {
	path := filepath.Join(ProfilesDir(), name+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read profile %q: %w", name, err)
	}

	var p Profile
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("parse profile %q: %w", name, err)
	}
	return &p, nil
}

// ListProfiles returns all saved profile names
func ListProfiles() []string {
	namesMap := make(map[string]struct{}, len(builtinProfiles))
	for name := range builtinProfiles {
		namesMap[name] = struct{}{}
	}

	entries, err := os.ReadDir(ProfilesDir())
	if err == nil {
		for _, e := range entries {
			if filepath.Ext(e.Name()) == ".json" {
				namesMap[strings.TrimSuffix(e.Name(), ".json")] = struct{}{}
			}
		}
	}

	var names []string
	for name := range namesMap {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// DeleteProfile removes a profile from disk
func DeleteProfile(name string) error {
	path := filepath.Join(ProfilesDir(), name+".json")
	return os.Remove(path)
}

// ApplyProfile applies a profile's settings to a config
func ApplyProfile(cfg *Config, p *Profile) {
	if p.Provider != "" {
		cfg.Provider = p.Provider
	}
	if p.BaseURL != "" {
		cfg.BaseURL = p.BaseURL
	}
	if p.APIKey != "" {
		cfg.APIKey = p.APIKey
	}
	if p.Model != "" {
		cfg.Model = p.Model
	}
}

// ProfileAllowsEmptyAPIKey reports whether the selected transport can start
// without credentials. Today that means local Ollama-compatible endpoints.
//
// TODO(Phase6): This function sniffs the base URL to recognise Ollama because
// cfg.Provider is always "openai" for every OpenAI-compat service. When
// fantasy/catwalk introduces a proper service-identity field (e.g. "ollama"),
// replace the URL-prefix check with: cfg.Service == "ollama".
func ProfileAllowsEmptyAPIKey(cfg Config) bool {
	if cfg.Provider != "openai" {
		return false
	}

	baseURL := strings.TrimSpace(strings.ToLower(cfg.BaseURL))
	return strings.HasPrefix(baseURL, "http://localhost:11434/") ||
		strings.HasPrefix(baseURL, "http://127.0.0.1:11434/")
}

// ResolveProfile loads a saved profile or falls back to built-in provider presets.
func ResolveProfile(name string) (*Profile, error) {
	if p, err := loadSavedProfile(name); err == nil {
		return p, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}

	if p, ok := builtinProfile(name); ok {
		return p, nil
	}

	return nil, fmt.Errorf("read profile %q: %w", name, os.ErrNotExist)
}

func loadSavedProfile(name string) (*Profile, error) {
	path := filepath.Join(ProfilesDir(), name+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read profile %q: %w", name, err)
	}

	var p Profile
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("parse profile %q: %w", name, err)
	}
	return &p, nil
}

func builtinProfile(name string) (*Profile, bool) {
	spec, ok := builtinProfiles[name]
	if !ok {
		return nil, false
	}

	p := &Profile{
		Name:     name,
		Provider: spec.Provider,
		BaseURL:  spec.BaseURL,
		Model:    spec.Model,
	}
	for _, envName := range spec.APIKeyEnv {
		if value := strings.TrimSpace(os.Getenv(envName)); value != "" {
			p.APIKey = value
			break
		}
	}
	return p, true
}

// MigrateDataPaths moves data from the old XDG locations to ~/.bitchtea/ if
// the old paths exist and the new ones do not. Errors are returned but
// non-fatal — callers should log and continue.
func MigrateDataPaths() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("resolve home dir: %w", err)
	}

	base := filepath.Join(home, ".bitchtea")

	moves := []struct {
		oldPath string
		newPath string
	}{
		{filepath.Join(home, ".local", "share", "bitchtea", "sessions"), filepath.Join(base, "sessions")},
		{filepath.Join(home, ".local", "share", "bitchtea", "logs"), filepath.Join(base, "logs")},
		{filepath.Join(home, ".local", "share", "bitchtea", "memory"), filepath.Join(base, "memory")},
		{filepath.Join(home, ".config", "bitchtea", "profiles"), filepath.Join(base, "profiles")},
	}

	var errs []string
	for _, m := range moves {
		if err := migrateDir(m.oldPath, m.newPath); err != nil {
			errs = append(errs, fmt.Sprintf("%s → %s: %v", m.oldPath, m.newPath, err))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("migration errors: %s", strings.Join(errs, "; "))
	}
	return nil
}

// migrateDir moves oldPath to newPath if oldPath exists and newPath does not.
func migrateDir(oldPath, newPath string) error {
	if _, err := os.Stat(oldPath); os.IsNotExist(err) {
		return nil // nothing to migrate
	}
	if _, err := os.Stat(newPath); err == nil {
		return nil // destination already exists, don't clobber
	}

	if err := os.MkdirAll(filepath.Dir(newPath), 0755); err != nil {
		return fmt.Errorf("create parent dir: %w", err)
	}
	if err := os.Rename(oldPath, newPath); err != nil {
		return fmt.Errorf("rename: %w", err)
	}
	return nil
}
