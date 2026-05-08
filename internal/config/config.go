package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
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
	Provider string // wire format / dialect: "openai" or "anthropic"
	Service  string // upstream service identity: "openai", "anthropic", "ollama", "openrouter", "zai-openai", ... or "custom"
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

	// Persona
	PersonaFile string // Path to an external persona file; overrides the compiled-in default when set.

	// Sampling params — pointer types so nil means "unset / use provider default".
	// Per-service gating in internal/llm/stream.go skips forwarding for providers
	// that return 400 on non-default values (Anthropic-direct).
	TopK               *int
	TopP               *float64
	Temperature        *float64
	RepetitionPenalty  *float64 // alias: repetition_penalty

	// Tool verbosity controls how much detail the tool panel shows.
	// Values: "terse", "normal" (default), "verbose"
	ToolVerbosity string

	// Banner controls whether the splash art / *** banner is shown on startup.
	Banner bool

	// Bare mode: skip persona, skip context files, skip persona_file.
	Bare bool

	// Effort is the Anthropic-native "output_config.effort" hint forwarded
	// to fantasy when Service == "anthropic". Adaptive thinking is auto-
	// attached by the fantasy anthropic provider whenever Effort is set
	// (see charm.land/fantasy/providers/anthropic/anthropic.go ~line 324).
	// Valid values: "low", "medium", "high", "max". Empty string means
	// "leave unset" (provider default). "high" is the default for Opus 4.7
	// intelligence-sensitive work.
	Effort string

	// ToolTimeout is the per-tool wall-clock limit in seconds applied to every
	// tool that doesn't manage its own timeout (bash manages its own). Sourced
	// from /set tool_timeout <seconds> or the .bitchtearc rc file. Default 300.
	ToolTimeout int
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
		Service:  "openai",

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

		ToolVerbosity: "normal",
		Banner:        true,

		Effort:      "high",
		ToolTimeout: 300,
	}
}

// DetectProvider figures out which API to use based on env vars
func DetectProvider(cfg *Config) {
	if key := os.Getenv("ANTHROPIC_API_KEY"); key != "" && cfg.APIKey == "" {
		cfg.APIKey = key
		cfg.BaseURL = envOr("ANTHROPIC_BASE_URL", "https://api.anthropic.com/v1")
		cfg.Provider = "anthropic"
		cfg.Service = "anthropic"
		if cfg.Model == "gpt-4o" {
			cfg.Model = "claude-sonnet-4-20250514"
		}
	} else if key := os.Getenv("OPENROUTER_API_KEY"); key != "" && cfg.APIKey == "" {
		cfg.APIKey = key
		cfg.Provider = builtinProfiles["openrouter"].Provider
		cfg.BaseURL = builtinProfiles["openrouter"].BaseURL
		cfg.Service = builtinProfiles["openrouter"].Service
		cfg.Profile = "openrouter"
		if cfg.Model == "gpt-4o" {
			cfg.Model = builtinProfiles["openrouter"].Model
		}
	} else if key := os.Getenv("ZAI_API_KEY"); key != "" && cfg.APIKey == "" {
		cfg.APIKey = key
		cfg.Provider = builtinProfiles["zai-openai"].Provider
		cfg.BaseURL = builtinProfiles["zai-openai"].BaseURL
		cfg.Service = builtinProfiles["zai-openai"].Service
		cfg.Profile = "zai-openai"
		if cfg.Model == "gpt-4o" {
			cfg.Model = builtinProfiles["zai-openai"].Model
		}
	} else if key := os.Getenv("OPENAI_API_KEY"); key != "" && cfg.APIKey == "" {
		cfg.APIKey = key
		cfg.Provider = "openai"
		cfg.Service = "openai"
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// Profile is a saved connection configuration.
//
// Provider encodes the wire format / dialect ("openai" or "anthropic"). Service
// encodes the upstream service identity ("openai", "anthropic", "ollama",
// "openrouter", "zai-openai", ... or "custom"). See docs/phase-9-service-identity.md.
//
// Service uses omitempty so old profiles without the field round-trip cleanly,
// and is filled in lazily on load via deriveService when missing.
type Profile struct {
	Name     string `json:"name"`
	Provider string `json:"provider"`
	Service  string `json:"service,omitempty"`
	BaseURL  string `json:"base_url"`
	APIKey   string `json:"api_key"`
	Model    string `json:"model"`
}

// builtinProfileSpec is the in-memory definition of a built-in profile.
// Provider is the wire format; Service is the upstream service identity used
// for per-service behavior gates (cache_control, reasoning forwarding, empty
// API key allowance, etc.).
type builtinProfileSpec struct {
	Provider  string
	Service   string
	BaseURL   string
	Model     string
	APIKeyEnv []string
}

var builtinProfiles = map[string]builtinProfileSpec{
	// cliproxyapi is LO's local CLIProxyAPI daemon (router-for-me/CLIProxyAPI,
	// https://github.com/router-for-me/CLIProxyAPI) running at
	// http://127.0.0.1:8317/v1.
	//
	// Wire-format: OpenAI Chat Completions (provider="openai"). The proxy routes
	// upstream to Claude, Codex, Gemini, etc. depending on its own config.
	//
	// Cache markers: the proxy auto-injects ephemeral cache_control markers on
	// tools[], system[], and the last user message; it runs its own 4-breakpoint
	// enforcement. bitchtea MUST NOT send any cache_control markers — if it does,
	// the proxy detects countCacheControls > 0 and skips smart placement entirely.
	// The gate in applyAnthropicCacheMarkers (cache.go) already no-ops for any
	// service that is not "anthropic", so this is handled by the default path.
	//
	// Effort / reasoning: the proxy translates the top-level OpenAI field
	// `reasoning_effort` into Claude's adaptive-thinking config upstream. Send
	// effort as openaicompat.ProviderOptions.ReasoningEffort — see stream.go.
	//
	// Model alias: the proxy exposes "claude-opus-4-7" as an alias. Verify via
	// GET /v1/models on the live daemon if the alias ever stops resolving; the
	// proxy's config.yaml maps it to the upstream Anthropic model ID.
	//
	// APIKeyEnv: CLIPROXYAPI_KEY is the preferred env var for a dedicated key;
	// OPENAI_API_KEY is accepted as a fallback (the proxy may require either or
	// may accept an empty/placeholder value depending on its auth config).
	"cliproxyapi": {
		Provider:  "openai",
		Service:   "cliproxyapi",
		BaseURL:   "http://127.0.0.1:8317/v1",
		Model:     "claude-opus-4-7",
		APIKeyEnv: []string{"CLIPROXYAPI_KEY", "OPENAI_API_KEY"},
	},
	"ollama": {
		Provider: "openai",
		Service:  "ollama",
		BaseURL:  "http://localhost:11434/v1",
		Model:    "llama3.2",
	},
	"openrouter": {
		Provider:  "openai",
		Service:   "openrouter",
		BaseURL:   "https://openrouter.ai/api/v1",
		Model:     "anthropic/claude-sonnet-4",
		APIKeyEnv: []string{"OPENROUTER_API_KEY"},
	},
	"aihubmix": {
		Provider:  "openai",
		Service:   "aihubmix",
		BaseURL:   "https://aihubmix.com/v1",
		Model:     "gpt-4o",
		APIKeyEnv: []string{"AIHUBMIX_API_KEY"},
	},
	"avian": {
		Provider:  "openai",
		Service:   "avian",
		BaseURL:   "https://api.avian.io/v1",
		Model:     "llama3",
		APIKeyEnv: []string{"AVIAN_API_KEY"},
	},
	"copilot": {
		Provider:  "openai",
		Service:   "copilot",
		BaseURL:   "https://api.githubcopilot.com",
		Model:     "gpt-4o",
		APIKeyEnv: []string{"GITHUB_TOKEN", "COPILOT_API_KEY"},
	},
	"cortecs": {
		Provider:  "openai",
		Service:   "cortecs",
		BaseURL:   "https://api.cortecs.ai/v1",
		Model:     "cortecs-model",
		APIKeyEnv: []string{"CORTECS_API_KEY"},
	},
	"huggingface": {
		Provider:  "openai",
		Service:   "huggingface",
		BaseURL:   "https://router.huggingface.co/v1",
		Model:     "meta-llama/Llama-3.1-8B-Instruct",
		APIKeyEnv: []string{"HUGGINGFACE_API_KEY"},
	},
	"ionet": {
		Provider:  "openai",
		Service:   "ionet",
		BaseURL:   "https://api.intelligence.io.solutions/api/v1",
		Model:     "io-net-model",
		APIKeyEnv: []string{"IONET_API_KEY"},
	},
	"nebius": {
		Provider:  "openai",
		Service:   "nebius",
		BaseURL:   "https://api.tokenfactory.nebius.com/v1",
		Model:     "meta-llama/Meta-Llama-3.1-70B-Instruct",
		APIKeyEnv: []string{"NEBIUS_API_KEY"},
	},
	"synthetic": {
		Provider:  "openai",
		Service:   "synthetic",
		BaseURL:   "https://api.synthetic.new/openai/v1",
		Model:     "synthetic-model",
		APIKeyEnv: []string{"SYNTHETIC_API_KEY"},
	},
	"venice": {
		Provider:  "openai",
		Service:   "venice",
		BaseURL:   "https://api.venice.ai/api/v1",
		Model:     "venice-model",
		APIKeyEnv: []string{"VENICE_API_KEY"},
	},
	"vercel": {
		Provider:  "openai",
		Service:   "vercel",
		BaseURL:   "https://ai-gateway.vercel.sh/v1",
		Model:     "openai:gpt-4o",
		APIKeyEnv: []string{"VERCEL_API_KEY"},
	},
	"xai": {
		Provider:  "openai",
		Service:   "xai",
		BaseURL:   "https://api.x.ai/v1",
		Model:     "grok-beta",
		APIKeyEnv: []string{"XAI_API_KEY"},
	},
	"zai-openai": {
		Provider:  "openai",
		Service:   "zai-openai",
		BaseURL:   "https://api.z.ai/api/coding/paas/v4",
		Model:     "GLM-4.7",
		APIKeyEnv: []string{"ZAI_API_KEY"},
	},
	"zai-anthropic": {
		Provider:  "anthropic",
		Service:   "zai-anthropic",
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
	if p.Service == "" {
		p.Service = deriveService(p)
	}
	return &p, nil
}

// ListServices returns the sorted, unique upstream service identities known
// to the built-in profile registry (e.g. "openai", "anthropic", "ollama",
// "openrouter", "zai-openai", ...). Used for `/set service` enumeration.
// Custom services configured via `/set service <name>` are not enumerated
// because they have no registry entry to discover.
func ListServices() []string {
	seen := make(map[string]struct{}, len(builtinProfiles))
	for _, p := range builtinProfiles {
		if p.Service != "" {
			seen[p.Service] = struct{}{}
		}
	}
	out := make([]string, 0, len(seen))
	for s := range seen {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
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
	if p.Service != "" {
		cfg.Service = p.Service
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
// without credentials. Gated on Service identity (Phase 9) — only "ollama"
// is allowed to run without an API key.
func ProfileAllowsEmptyAPIKey(cfg Config) bool {
	return cfg.Service == "ollama"
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
	if p.Service == "" {
		p.Service = deriveService(p)
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
		Service:  spec.Service,
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

// deriveService computes a default Service identity for a profile loaded from
// disk that lacks the field. The lookup order is:
//  1. profile name matches a built-in key,
//  2. base URL host matches a built-in's host,
//  3. fall back to "custom".
//
// This is the lazy migration path described in docs/phase-9-service-identity.md.
// It does not rewrite the file; the next /profile save will persist the field.
func deriveService(p Profile) string {
	if spec, ok := builtinProfiles[p.Name]; ok {
		return spec.Service
	}
	if host := hostOf(p.BaseURL); host != "" {
		for _, spec := range builtinProfiles {
			if hostOf(spec.BaseURL) == host {
				return spec.Service
			}
		}
	}
	return "custom"
}

// hostOf returns the lowercased host of a URL string, or "" if it can't be
// parsed or has no host.
func hostOf(rawURL string) string {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return ""
	}
	return strings.ToLower(u.Host)
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
