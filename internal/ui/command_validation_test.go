package ui

import (
	"strings"
	"testing"

	"github.com/jstamagal/bitchtea/internal/config"
)

func newTestModel(t *testing.T) Model {
	t.Helper()
	cfg := &config.Config{
		APIKey:   "sk-test-key-12345",
		BaseURL:  "https://api.openai.com/v1",
		Model:    "gpt-4o",
		Provider: "openai",
	}
	return NewModel(cfg)
}

func lastMessage(m Model) ChatMessage {
	if len(m.messages) == 0 {
		return ChatMessage{}
	}
	return m.messages[len(m.messages)-1]
}

func TestProviderValidation(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantError bool
		wantContain string
	}{
		{"valid openai", "/provider openai", false, "Provider set to: openai"},
		{"valid anthropic", "/provider anthropic", false, "Provider set to: anthropic"},
		{"invalid foo", "/provider foo", true, "Invalid provider"},
		{"invalid google", "/provider google", true, "Invalid provider"},
		{"invalid empty-ish", "/provider x", true, "Invalid provider"},
		{"no arg shows current", "/provider", false, "Provider:"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newTestModel(t)
			m.handleCommand(tt.input)
			msg := lastMessage(m)
			if !strings.Contains(msg.Content, tt.wantContain) {
				t.Errorf("expected message containing %q, got %q", tt.wantContain, msg.Content)
			}
			if tt.wantError && msg.Type != MsgError {
				t.Errorf("expected MsgError, got %v", msg.Type)
			}
			if !tt.wantError && msg.Type == MsgError {
				t.Errorf("unexpected error: %q", msg.Content)
			}
		})
	}
}

func TestBaseURLValidation(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantError bool
		wantContain string
	}{
		{"valid https", "/baseurl https://api.example.com/v1", false, "Base URL set to"},
		{"valid http", "/baseurl http://localhost:8080", false, "Base URL set to"},
		{"invalid no scheme", "/baseurl api.example.com", true, "Invalid URL"},
		{"invalid random text", "/baseurl notaurl", true, "Invalid URL"},
		{"invalid ftp", "/baseurl ftp://example.com", true, "Invalid URL"},
		{"no arg shows current", "/baseurl", false, "Base URL:"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newTestModel(t)
			m.handleCommand(tt.input)
			msg := lastMessage(m)
			if !strings.Contains(msg.Content, tt.wantContain) {
				t.Errorf("expected message containing %q, got %q", tt.wantContain, msg.Content)
			}
			if tt.wantError && msg.Type != MsgError {
				t.Errorf("expected MsgError, got %v", msg.Type)
			}
			if !tt.wantError && msg.Type == MsgError {
				t.Errorf("unexpected error: %q", msg.Content)
			}
		})
	}
}

func TestAPIKeyValidation(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantError bool
		wantContain string
	}{
		{"valid long key", "/apikey sk-1234567890abcdef", false, "API key set"},
		{"too short 1 char", "/apikey x", true, "API key too short"},
		{"too short 9 chars", "/apikey 123456789", true, "API key too short"},
		{"exactly 10 chars ok", "/apikey 1234567890", false, "API key set"},
		{"no arg shows current", "/apikey", false, "API Key:"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newTestModel(t)
			m.handleCommand(tt.input)
			msg := lastMessage(m)
			if !strings.Contains(msg.Content, tt.wantContain) {
				t.Errorf("expected message containing %q, got %q", tt.wantContain, msg.Content)
			}
			if tt.wantError && msg.Type != MsgError {
				t.Errorf("expected MsgError, got %v", msg.Type)
			}
			if !tt.wantError && msg.Type == MsgError {
				t.Errorf("unexpected error: %q", msg.Content)
			}
		})
	}
}

func TestModelNameWarning(t *testing.T) {
	tests := []struct {
		name         string
		input        string
		wantWarning  bool
		wantContain  string
	}{
		{"valid gpt-4o", "/model gpt-4o", false, "Model switched to"},
		{"valid claude-3.5-sonnet", "/model claude-3.5-sonnet", false, "Model switched to"},
		{"suspicious short", "/model x", true, "suspicious"},
		{"suspicious with space", "/model my model", true, "suspicious"},
		{"no arg shows current", "/model", false, "Current model"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newTestModel(t)
			m.handleCommand(tt.input)

			// Check all messages for the warning
			hasWarning := false
			hasSwitch := false
			for _, msg := range m.messages {
				if strings.Contains(msg.Content, "suspicious") && msg.Type == MsgError {
					hasWarning = true
				}
				if strings.Contains(msg.Content, tt.wantContain) {
					hasSwitch = true
				}
			}

			if !hasSwitch {
				t.Errorf("expected a message containing %q", tt.wantContain)
			}
			if tt.wantWarning && !hasWarning {
				t.Errorf("expected suspicious model warning")
			}
			if !tt.wantWarning && hasWarning {
				t.Error("unexpected suspicious model warning")
			}
		})
	}
}

func TestProviderDoesNotMutateOnInvalidInput(t *testing.T) {
	m := newTestModel(t)
	original := m.config.Provider
	m.handleCommand("/provider foobar")
	if m.config.Provider != original {
		t.Errorf("provider should not change on invalid input, got %q", m.config.Provider)
	}
}

func TestBaseURLDoesNotMutateOnInvalidInput(t *testing.T) {
	m := newTestModel(t)
	original := m.config.BaseURL
	m.handleCommand("/baseurl notaurl")
	if m.config.BaseURL != original {
		t.Errorf("baseurl should not change on invalid input, got %q", m.config.BaseURL)
	}
}

func TestAPIKeyDoesNotMutateOnInvalidInput(t *testing.T) {
	m := newTestModel(t)
	original := m.config.APIKey
	m.handleCommand("/apikey short")
	if m.config.APIKey != original {
		t.Errorf("apikey should not change on invalid input, got %q", m.config.APIKey)
	}
}
