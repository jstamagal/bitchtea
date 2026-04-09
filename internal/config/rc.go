package config

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// RCPath returns the path to the startup rc file.
func RCPath() string {
	return filepath.Join(BaseDir(), "bitchtearc")
}

// ParseRC reads the rc file and returns non-blank, non-comment lines.
// Each line is a command without a leading slash.
// Returns nil (not an error) if the file does not exist.
func ParseRC() []string {
	return parseRCFile(RCPath())
}

func parseRCFile(path string) []string {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		lines = append(lines, line)
	}
	return lines
}

// ApplyRCSetCommands processes "set" lines from the rc file and applies them
// to cfg. Non-set lines are returned for later execution by the TUI.
func ApplyRCSetCommands(cfg *Config, lines []string) []string {
	var remaining []string
	for _, line := range lines {
		parts := strings.Fields(line)
		if len(parts) < 2 || parts[0] != "set" {
			remaining = append(remaining, line)
			continue
		}
		key := parts[1]
		value := ""
		if len(parts) >= 3 {
			value = strings.Join(parts[2:], " ")
		}
		if !applySetToConfig(cfg, key, value) {
			remaining = append(remaining, line)
		}
	}
	return remaining
}

// ApplySet applies a single set key/value pair to cfg. Returns true if the
// key was recognised. Exported so the TUI /set command can reuse the logic.
func ApplySet(cfg *Config, key, value string) bool {
	return applySetToConfig(cfg, key, value)
}

// SetKeys returns the list of recognised /set keys.
func SetKeys() []string {
	return []string{
		"provider", "model", "apikey", "baseurl", "nick",
		"profile", "sound", "auto-next", "auto-idea",
	}
}

// GetSetting returns the current value of a recognised /set key.
// Returns ("", false) for unknown keys.
func GetSetting(cfg *Config, key string) (string, bool) {
	switch key {
	case "provider":
		return cfg.Provider, true
	case "model":
		return cfg.Model, true
	case "apikey":
		if len(cfg.APIKey) > 8 {
			return cfg.APIKey[:4] + "..." + cfg.APIKey[len(cfg.APIKey)-4:], true
		}
		if cfg.APIKey == "" {
			return "<unset>", true
		}
		return cfg.APIKey, true
	case "baseurl":
		return cfg.BaseURL, true
	case "nick":
		return cfg.UserNick, true
	case "profile":
		if cfg.Profile == "" {
			return "<none>", true
		}
		return cfg.Profile, true
	case "sound":
		return boolSettingStr(cfg.NotificationSound), true
	case "auto-next":
		return boolSettingStr(cfg.AutoNextSteps), true
	case "auto-idea":
		return boolSettingStr(cfg.AutoNextIdea), true
	default:
		return "", false
	}
}

func boolSettingStr(v bool) string {
	if v {
		return "on"
	}
	return "off"
}

func applySetToConfig(cfg *Config, key, value string) bool {
	switch key {
	case "provider":
		if value == "openai" || value == "anthropic" {
			cfg.Provider = value
		}
	case "model":
		if value != "" {
			cfg.Model = value
		}
	case "apikey":
		if value != "" {
			cfg.APIKey = value
		}
	case "baseurl":
		if value != "" {
			cfg.BaseURL = value
		}
	case "nick":
		if value != "" {
			cfg.UserNick = value
		}
	case "profile":
		if value != "" {
			p, err := ResolveProfile(value)
			if err == nil {
				ApplyProfile(cfg, p)
				cfg.Profile = value
			}
		}
	case "sound":
		cfg.NotificationSound = parseBoolSetting(value)
	case "auto-next":
		cfg.AutoNextSteps = parseBoolSetting(value)
	case "auto-idea":
		cfg.AutoNextIdea = parseBoolSetting(value)
	default:
		return false
	}
	return true
}

func parseBoolSetting(value string) bool {
	switch strings.ToLower(value) {
	case "on", "true", "1", "yes":
		return true
	default:
		return false
	}
}
