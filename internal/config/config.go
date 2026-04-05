package config

import (
	"encoding/json"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"sort"
	"strings"
)

// Config holds all runtime configuration
type Config struct {
	// API
	APIKey   string
	BaseURL  string
	Model    string
	Provider string

	// Agent behavior
	AutoNextSteps bool
	AutoNextIdea  bool
	MaxTokens     int

	// UI
	UserNick  string
	AgentNick string

	// Paths
	WorkDir    string
	SessionDir string
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

		UserNick:  username,
		AgentNick: "bitchtea",

		WorkDir:    wd,
		SessionDir: filepath.Join(home, ".local", "share", "bitchtea", "sessions"),
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
	}
	if key := os.Getenv("OPENAI_API_KEY"); key != "" {
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

// ProfilesDir returns the directory where profiles are stored.
// It's a variable so tests can override it.
var ProfilesDir = func() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "bitchtea", "profiles")
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
	entries, err := os.ReadDir(ProfilesDir())
	if err != nil {
		return nil
	}

	var names []string
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".json" {
			names = append(names, strings.TrimSuffix(e.Name(), ".json"))
		}
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
