package config

import (
	"os"
	"os/user"
	"path/filepath"
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
