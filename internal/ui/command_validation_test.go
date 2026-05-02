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
	dataDir := t.TempDir()
	cfg := &config.Config{
		APIKey:     "sk-test-key-12345",
		BaseURL:    "https://api.openai.com/v1",
		Model:      "gpt-4o",
		Provider:   "openai",
		WorkDir:    dataDir,
		SessionDir: dataDir + "/sessions",
		LogDir:     dataDir + "/logs",
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

func allMsgText(m tea.Model) string {
	var parts []string
	for _, msg := range allMsgs(m) {
		parts = append(parts, msg.Content)
	}
	return strings.Join(parts, "\n")
}

func TestProviderAcceptsAnyValueVerbatim(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		wantContain string
		wantStored  string
	}{
		{"openai", "/provider openai", "Provider set to: openai", "openai"},
		{"anthropic", "/provider anthropic", "Provider set to: anthropic", "anthropic"},
		{"arbitrary value", "/provider foo", "Provider set to: foo", "foo"},
		{"single char", "/provider x", "Provider set to: x", "x"},
		{"no arg shows current", "/provider", "Provider:", "openai"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newTestModel(t)
			result, _ := m.handleCommand(tt.input)
			model := result.(Model)
			msgs := allMsgs(result)
			found := false
			for _, msg := range msgs {
				if strings.Contains(msg.Content, tt.wantContain) {
					found = true
				}
				if msg.Type == MsgError {
					t.Errorf("unexpected error message: %q", msg.Content)
				}
			}
			if !found {
				t.Errorf("expected message containing %q, got %#v", tt.wantContain, msgs)
			}
			if model.config.Provider != tt.wantStored {
				t.Errorf("provider = %q, want %q", model.config.Provider, tt.wantStored)
			}
		})
	}
}

func TestBaseURLAcceptsAnyValueVerbatim(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		wantContain string
		wantStored  string
	}{
		{"https", "/baseurl https://api.example.com/v1", "Base URL set to", "https://api.example.com/v1"},
		{"http", "/baseurl http://localhost:8080", "Base URL set to", "http://localhost:8080"},
		{"no scheme", "/baseurl api.example.com", "Base URL set to", "api.example.com"},
		{"random text", "/baseurl notaurl", "Base URL set to", "notaurl"},
		{"ftp", "/baseurl ftp://example.com", "Base URL set to", "ftp://example.com"},
		{"no arg shows current", "/baseurl", "Base URL:", "https://api.openai.com/v1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newTestModel(t)
			result, _ := m.handleCommand(tt.input)
			model := result.(Model)
			msgs := allMsgs(result)
			found := false
			for _, msg := range msgs {
				if strings.Contains(msg.Content, tt.wantContain) {
					found = true
				}
				if msg.Type == MsgError {
					t.Errorf("unexpected error: %q", msg.Content)
				}
			}
			if !found {
				t.Errorf("expected message containing %q, got %#v", tt.wantContain, msgs)
			}
			if model.config.BaseURL != tt.wantStored {
				t.Errorf("baseurl = %q, want %q", model.config.BaseURL, tt.wantStored)
			}
		})
	}
}

func TestAPIKeyAcceptsAnyValueVerbatim(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		wantContain string
		wantStored  string
	}{
		{"long key", "/apikey sk-1234567890abcdef", "API key set", "sk-1234567890abcdef"},
		{"single char x", "/apikey x", "API key set", "x"},
		{"nine chars", "/apikey 123456789", "API key set", "123456789"},
		{"ten chars", "/apikey 1234567890", "API key set", "1234567890"},
		{"no arg shows current", "/apikey", "API Key:", "sk-test-key-12345"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newTestModel(t)
			result, _ := m.handleCommand(tt.input)
			model := result.(Model)
			msg := lastMsg(result)
			if !strings.Contains(msg.Content, tt.wantContain) {
				t.Errorf("expected message containing %q, got %q", tt.wantContain, msg.Content)
			}
			if msg.Type == MsgError {
				t.Errorf("unexpected error: %q", msg.Content)
			}
			if model.config.APIKey != tt.wantStored {
				t.Errorf("apikey = %q, want %q", model.config.APIKey, tt.wantStored)
			}
		})
	}
}

func TestModelAcceptsAnyValueVerbatim(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		wantContain string
		wantStored  string
	}{
		{"gpt-4o", "/model gpt-4o", "Model switched to", "gpt-4o"},
		{"claude-3.5-sonnet", "/model claude-3.5-sonnet", "Model switched to", "claude-3.5-sonnet"},
		{"single char x", "/model x", "Model switched to", "x"},
		{"no dot or dash", "/model foobar", "Model switched to", "foobar"},
		{"no arg shows current", "/model", "Current model", "gpt-4o"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newTestModel(t)
			result, _ := m.handleCommand(tt.input)
			model := result.(Model)

			msgs := allMsgs(result)
			hasSwitch := false
			for _, msg := range msgs {
				if strings.Contains(msg.Content, tt.wantContain) {
					hasSwitch = true
				}
				if msg.Type == MsgError {
					t.Errorf("unexpected error: %q", msg.Content)
				}
			}

			if !hasSwitch {
				t.Errorf("expected a message containing %q", tt.wantContain)
			}
			if model.config.Model != tt.wantStored {
				t.Errorf("model = %q, want %q", model.config.Model, tt.wantStored)
			}
		})
	}
}

func TestProviderChangeWarnsWhenBaseURLLooksOpenAICompatible(t *testing.T) {
	m := newTestModel(t)
	m.config.BaseURL = "http://127.0.0.1:3456"
	m.agent.SetBaseURL(m.config.BaseURL)

	result, _ := m.handleCommand("/provider anthropic")
	msgs := allMsgs(result)
	if len(msgs) < 2 {
		t.Fatalf("expected provider change plus warning, got %d messages", len(msgs))
	}
	if !strings.Contains(msgs[0].Content, "requests -> http://127.0.0.1:3456/messages") {
		t.Fatalf("expected endpoint preview, got %q", msgs[0].Content)
	}
	if !strings.Contains(msgs[len(msgs)-1].Content, "If this server is OpenAI-compatible, switch with /provider openai.") {
		t.Fatalf("expected transport mismatch guidance, got %q", msgs[len(msgs)-1].Content)
	}
}

func TestBaseURLChangeWarnsWhenProviderLikelyMismatched(t *testing.T) {
	m := newTestModel(t)
	m.config.Provider = "anthropic"
	m.agent.SetProvider("anthropic")

	result, _ := m.handleCommand("/baseurl http://127.0.0.1:3456")
	msgs := allMsgs(result)
	if len(msgs) < 2 {
		t.Fatalf("expected baseurl change plus warning, got %d messages", len(msgs))
	}
	if !strings.Contains(msgs[0].Content, "requests -> http://127.0.0.1:3456/messages") {
		t.Fatalf("expected endpoint preview, got %q", msgs[0].Content)
	}
	if !strings.Contains(msgs[len(msgs)-1].Content, "anthropic transport sends requests to /messages") {
		t.Fatalf("expected anthropic transport warning, got %q", msgs[len(msgs)-1].Content)
	}
}

func TestProfileNameThatMatchesProviderSuggestsProviderCommand(t *testing.T) {
	m := newTestModel(t)

	result, _ := m.handleCommand("/profile anthropic")
	msg := lastMsg(result)
	if msg.Type != MsgError {
		t.Fatalf("expected error message, got %v", msg.Type)
	}
	if !strings.Contains(msg.Content, "Use /provider anthropic") {
		t.Fatalf("expected provider guidance, got %q", msg.Content)
	}
}

func TestProfileLoadNameThatMatchesProviderSuggestsProviderCommand(t *testing.T) {
	m := newTestModel(t)

	result, _ := m.handleCommand("/profile load openai")
	msg := lastMsg(result)
	if msg.Type != MsgError {
		t.Fatalf("expected error message, got %v", msg.Type)
	}
	if !strings.Contains(msg.Content, "Use /provider openai") {
		t.Fatalf("expected provider guidance, got %q", msg.Content)
	}
}

func TestProfileLoadMasksAPIKeyAndAvoidsDuplicateMessages(t *testing.T) {
	t.Setenv("OPENROUTER_API_KEY", "sk-or-v1-1234567890ABCD")

	m := newTestModel(t)
	result, _ := m.handleCommand("/profile load openrouter")
	msgs := allMsgs(result)
	if len(msgs) != 1 {
		t.Fatalf("expected single profile load message, got %d", len(msgs))
	}
	msg := msgs[0]
	if !strings.Contains(msg.Content, "apikey=sk-o...ABCD") {
		t.Fatalf("expected masked api key, got %q", msg.Content)
	}
	if strings.Contains(msg.Content, "sk-or-v1-1234567890ABCD") {
		t.Fatalf("expected full api key to stay hidden, got %q", msg.Content)
	}
}

func TestProfileCommandHintsWhenProviderNameIsUsed(t *testing.T) {
	m := newTestModel(t)

	result, _ := m.handleCommand("/profile anthropic")
	msg := lastMsg(result)

	if msg.Type != MsgError {
		t.Fatalf("expected error message, got %v", msg.Type)
	}
	for _, want := range []string{"provider, not a profile", "/provider anthropic"} {
		if !strings.Contains(msg.Content, want) {
			t.Fatalf("expected %q in message, got %q", want, msg.Content)
		}
	}
}

func TestProfileDirectLoadMasksAPIKeyAndEmitsSingleMessage(t *testing.T) {
	t.Setenv("OPENROUTER_API_KEY", "sk-or-v1-1234567890abcdef")

	m := newTestModel(t)
	result, _ := m.handleCommand("/profile openrouter")
	model := result.(Model)

	if got := len(model.messages); got != 1 {
		t.Fatalf("expected single profile status message, got %d", got)
	}

	msg := lastMsg(result)
	if msg.Type != MsgSystem {
		t.Fatalf("expected system message, got %v", msg.Type)
	}
	if !strings.Contains(msg.Content, "Profile loaded: openrouter") {
		t.Fatalf("expected profile loaded message, got %q", msg.Content)
	}
	if strings.Contains(msg.Content, "1234567890abcdef") {
		t.Fatalf("expected API key to be masked, got %q", msg.Content)
	}
	if model.config.Profile != "openrouter" {
		t.Fatalf("expected loaded profile to be recorded, got %q", model.config.Profile)
	}
}

func TestManualConnectionChangeClearsLoadedProfile(t *testing.T) {
	t.Setenv("OPENROUTER_API_KEY", "sk-or-v1-1234567890abcdef")

	m := newTestModel(t)
	result, _ := m.handleCommand("/profile openrouter")
	model := result.(Model)
	if model.config.Profile != "openrouter" {
		t.Fatalf("expected openrouter profile, got %q", model.config.Profile)
	}

	result, _ = model.handleCommand("/provider anthropic")
	model = result.(Model)
	if model.config.Profile != "" {
		t.Fatalf("expected manual provider change to clear profile, got %q", model.config.Profile)
	}
}

func TestProviderMessageShowsEffectiveEndpoint(t *testing.T) {
	m := newTestModel(t)

	result, _ := m.handleCommand("/provider anthropic")
	text := allMsgText(result)

	if !strings.Contains(text, "/messages") {
		t.Fatalf("expected anthropic endpoint hint, got %q", text)
	}
}

func TestBaseURLMessageShowsEffectiveEndpoint(t *testing.T) {
	m := newTestModel(t)

	result, _ := m.handleCommand("/baseurl https://api.example.com/v1")
	text := allMsgText(result)

	if !strings.Contains(text, "https://api.example.com/v1/chat/completions") {
		t.Fatalf("expected openai endpoint hint, got %q", text)
	}
}

func TestProfileLoadVerboseMasksAPIKeyAndShowsEndpoint(t *testing.T) {
	t.Setenv("OPENROUTER_API_KEY", "sk-or-v1-1234567890abcdef")

	m := newTestModel(t)
	result, _ := m.handleCommand("/profile load openrouter")
	model := result.(Model)

	if got := len(model.messages); got != 1 {
		t.Fatalf("expected single profile status message, got %d", got)
	}

	msg := lastMsg(result)
	for _, want := range []string{"endpoint=https://openrouter.ai/api/v1/chat/completions", "sk-o...cdef"} {
		if !strings.Contains(msg.Content, want) {
			t.Fatalf("expected %q in message, got %q", want, msg.Content)
		}
	}
	if strings.Contains(msg.Content, "1234567890abcdef") {
		t.Fatalf("expected API key to be masked, got %q", msg.Content)
	}
}

func TestBaseURLWarnsWhenEndpointSuffixIsIncluded(t *testing.T) {
	m := newTestModel(t)

	result, _ := m.handleCommand("/baseurl https://api.example.com/v1/chat/completions")
	msg := lastMsg(result)

	for _, want := range []string{"warning ->", "omit /chat/completions"} {
		if !strings.Contains(msg.Content, want) {
			t.Fatalf("expected %q in message, got %q", want, msg.Content)
		}
	}
}

func TestProviderWarnsForSuspiciousOpenAIBaseURLUnderAnthropic(t *testing.T) {
	m := newTestModel(t)

	result, _ := m.handleCommand("/provider anthropic")
	model := result.(Model)
	result, _ = model.handleCommand("/baseurl https://api.openai.com/v1")
	msg := lastMsg(result)

	for _, want := range []string{"warning ->", "Anthropic transport with an OpenAI-style base URL looks suspicious"} {
		if !strings.Contains(msg.Content, want) {
			t.Fatalf("expected %q in message, got %q", want, msg.Content)
		}
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
