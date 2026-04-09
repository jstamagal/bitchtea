package ui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

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

// lastMsg returns the last message from the model returned by handleCommand
func lastMsg(m tea.Model) ChatMessage {
	model := m.(Model)
	if len(model.messages) == 0 {
		return ChatMessage{}
	}
	return model.messages[len(model.messages)-1]
}

// allMsgs returns all messages from the model returned by handleCommand
func allMsgs(m tea.Model) []ChatMessage {
	model := m.(Model)
	return model.messages
}

func TestProviderValidation(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		wantError   bool
		wantContain string
	}{
		{"valid openai", "/provider openai", false, "Provider set to: openai"},
		{"valid anthropic", "/provider anthropic", false, "Provider set to: anthropic"},
		{"invalid foo", "/provider foo", true, "Invalid provider"},
		{"invalid google", "/provider google", true, "Invalid provider"},
		{"invalid short", "/provider x", true, "Invalid provider"},
		{"no arg shows current", "/provider", false, "Provider:"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newTestModel(t)
			result, _ := m.handleCommand(tt.input)
			msg := lastMsg(result)
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
		name        string
		input       string
		wantError   bool
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
			result, _ := m.handleCommand(tt.input)
			msg := lastMsg(result)
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
		name        string
		input       string
		wantError   bool
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
			result, _ := m.handleCommand(tt.input)
			msg := lastMsg(result)
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
		name        string
		input       string
		wantWarning bool
		wantContain string
	}{
		{"valid gpt-4o", "/model gpt-4o", false, "Model switched to"},
		{"valid claude-3.5-sonnet", "/model claude-3.5-sonnet", false, "Model switched to"},
		{"suspicious short", "/model x", true, "suspicious"},
		{"suspicious nodotdash", "/model foobar", true, "suspicious"},
		{"no arg shows current", "/model", false, "Current model"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newTestModel(t)
			result, _ := m.handleCommand(tt.input)

			msgs := allMsgs(result)
			hasWarning := false
			hasSwitch := false
			for _, msg := range msgs {
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
	result, _ := m.handleCommand("/provider foobar")
	model := result.(Model)
	if model.config.Provider != original {
		t.Errorf("provider should not change on invalid input, got %q", model.config.Provider)
	}
}

func TestBaseURLDoesNotMutateOnInvalidInput(t *testing.T) {
	m := newTestModel(t)
	original := m.config.BaseURL
	result, _ := m.handleCommand("/baseurl notaurl")
	model := result.(Model)
	if model.config.BaseURL != original {
		t.Errorf("baseurl should not change on invalid input, got %q", model.config.BaseURL)
	}
}

func TestAPIKeyDoesNotMutateOnInvalidInput(t *testing.T) {
	m := newTestModel(t)
	original := m.config.APIKey
	result, _ := m.handleCommand("/apikey short")
	model := result.(Model)
	if model.config.APIKey != original {
		t.Errorf("apikey should not change on invalid input, got %q", model.config.APIKey)
	}
}

func TestDebugCommand(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		wantError   bool
		wantContain string
	}{
		{"debug on", "/debug on", false, "Debug mode: ON"},
		{"debug off", "/debug off", false, "Debug mode: OFF"},
		{"debug no arg shows status", "/debug", false, "Debug mode: OFF"},
		{"debug invalid arg", "/debug maybe", true, "/debug on|off"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newTestModel(t)
			result, _ := m.handleCommand(tt.input)
			msg := lastMsg(result)
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

func TestDebugModeToggle(t *testing.T) {
	m := newTestModel(t)

	// Off by default
	if m.debugMode {
		t.Error("debug mode should be off by default")
	}

	// Turn on
	result, _ := m.handleCommand("/debug on")
	model := result.(Model)
	if !model.debugMode {
		t.Error("debug mode should be on after /debug on")
	}

	// Turn off
	result, _ = model.handleCommand("/debug off")
	model = result.(Model)
	if model.debugMode {
		t.Error("debug mode should be off after /debug off")
	}
}

func TestActivityCommand(t *testing.T) {
	t.Run("shows queued activity and marks it read", func(t *testing.T) {
		m := newTestModel(t)
		m.NotifyBackgroundActivity(BackgroundActivity{
			Time:    nowForTests(),
			Context: "#ops",
			Sender:  "coding-buddy",
			Summary: "wants you to inspect the crash log",
		})

		result, _ := m.handleCommand("/activity")
		model := result.(Model)
		msg := lastMsg(result)

		if msg.Type != MsgSystem {
			t.Fatalf("expected system message, got %v", msg.Type)
		}
		for _, want := range []string{"Background activity:", "#ops", "coding-buddy", "inspect the crash log"} {
			if !strings.Contains(msg.Content, want) {
				t.Fatalf("expected %q in activity output, got %q", want, msg.Content)
			}
		}
		if model.backgroundUnread != 0 {
			t.Fatalf("expected activity to be marked read, got %d unread", model.backgroundUnread)
		}
		if len(model.backgroundActivity) != 1 {
			t.Fatalf("expected queued activity to remain available, got %d entries", len(model.backgroundActivity))
		}
	})

	t.Run("clear empties queue", func(t *testing.T) {
		m := newTestModel(t)
		m.NotifyBackgroundActivity(BackgroundActivity{Context: "#ops", Sender: "daemon", Summary: "heartbeat failed"})

		result, _ := m.handleCommand("/activity clear")
		model := result.(Model)
		msg := lastMsg(result)

		if !strings.Contains(msg.Content, "Cleared 1 background activity notice(s).") {
			t.Fatalf("unexpected clear message: %q", msg.Content)
		}
		if len(model.backgroundActivity) != 0 {
			t.Fatalf("expected queue to be cleared, got %d entries", len(model.backgroundActivity))
		}
		if model.backgroundUnread != 0 {
			t.Fatalf("expected unread count reset, got %d", model.backgroundUnread)
		}
	})

	t.Run("invalid args error", func(t *testing.T) {
		m := newTestModel(t)
		result, _ := m.handleCommand("/activity nope")
		msg := lastMsg(result)
		if msg.Type != MsgError {
			t.Fatalf("expected error message, got %v", msg.Type)
		}
		if !strings.Contains(msg.Content, "Usage: /activity [clear]") {
			t.Fatalf("unexpected error: %q", msg.Content)
		}
	})
}

func nowForTests() time.Time {
	return time.Date(2026, 4, 8, 13, 37, 0, 0, time.UTC)
}

func TestDebugStatusShowsCurrent(t *testing.T) {
	m := newTestModel(t)

	// Enable debug first
	result, _ := m.handleCommand("/debug on")
	model := result.(Model)

	// Check status shows ON
	result, _ = model.handleCommand("/debug")
	msg := lastMsg(result)
	if !strings.Contains(msg.Content, "Debug mode: ON") {
		t.Errorf("expected 'Debug mode: ON', got %q", msg.Content)
	}
}
