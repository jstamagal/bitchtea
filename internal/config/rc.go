package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
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
		"profile", "service", "sound", "auto-next", "auto-idea", "persona_file",
		"top_k", "top_p", "temperature", "repetition_penalty",
		"tool_verbosity", "banner",
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
	case "service":
		if cfg.Service == "" {
			return "<unset>", true
		}
		return cfg.Service, true
	case "sound":
		return boolSettingStr(cfg.NotificationSound), true
	case "auto-next":
		return boolSettingStr(cfg.AutoNextSteps), true
	case "auto-idea":
		return boolSettingStr(cfg.AutoNextIdea), true
	case "persona_file":
		if cfg.PersonaFile == "" {
			return "<unset>", true
		}
		return cfg.PersonaFile, true
	case "top_k":
		if cfg.TopK == nil {
			return "<unset>", true
		}
		return strconv.Itoa(*cfg.TopK), true
	case "top_p":
		if cfg.TopP == nil {
			return "<unset>", true
		}
		return fmt.Sprintf("%g", *cfg.TopP), true
	case "temperature":
		if cfg.Temperature == nil {
			return "<unset>", true
		}
		return fmt.Sprintf("%g", *cfg.Temperature), true
	case "repetition_penalty":
		if cfg.RepetitionPenalty == nil {
			return "<unset>", true
		}
		return fmt.Sprintf("%g", *cfg.RepetitionPenalty), true
	case "tool_verbosity":
		if cfg.ToolVerbosity == "" {
			return "normal", true
		}
		return cfg.ToolVerbosity, true
	case "banner":
		return boolSettingStr(cfg.Banner), true
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
			// User opted into a custom transport; per-service gates can no
			// longer trust the previous Service identity. See bt-p9 design.
			cfg.Service = "custom"
			cfg.Profile = ""
		}
	case "model":
		if value != "" {
			cfg.Model = value
			cfg.Profile = ""
		}
	case "apikey":
		if value != "" {
			cfg.APIKey = value
			cfg.Profile = ""
		}
	case "baseurl":
		if value != "" {
			cfg.BaseURL = value
			// User pointed the base URL elsewhere; service identity may no
			// longer match. Mark as custom rather than leave a stale value.
			cfg.Service = "custom"
			cfg.Profile = ""
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
	case "persona_file":
		if value != "" {
			cfg.PersonaFile = value
		}
	case "service":
		if value != "" {
			cfg.Service = value
			// /set service is a metadata relabel — does NOT clear Profile.
		}
	case "top_k":
		if value == "" || value == "<unset>" {
			cfg.TopK = nil
		} else if n, err := strconv.Atoi(value); err == nil {
			cfg.TopK = &n
		}
	case "top_p":
		if value == "" || value == "<unset>" {
			cfg.TopP = nil
		} else if f, err := strconv.ParseFloat(value, 64); err == nil {
			cfg.TopP = &f
		}
	case "temperature":
		if value == "" || value == "<unset>" {
			cfg.Temperature = nil
		} else if f, err := strconv.ParseFloat(value, 64); err == nil {
			cfg.Temperature = &f
		}
	// rep_pen is the short alias; repetition_penalty is canonical.
	case "rep_pen", "repetition_penalty":
		if value == "" || value == "<unset>" {
			cfg.RepetitionPenalty = nil
		} else if f, err := strconv.ParseFloat(value, 64); err == nil {
			cfg.RepetitionPenalty = &f
		}
	case "tool_verbosity":
		switch value {
		case "terse", "normal", "verbose":
			cfg.ToolVerbosity = value
		}
	case "banner":
		cfg.Banner = parseBoolSetting(value)
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
