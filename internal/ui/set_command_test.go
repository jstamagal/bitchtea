package ui

import (
	"strings"
	"testing"
)

func TestSetCommandShowsAllSettings(t *testing.T) {
	m := newTestModel(t)
	result, _ := m.handleCommand("/set")
	msg := lastMsg(result)
	if msg.Type != MsgSystem {
		t.Fatalf("expected system message, got %v", msg.Type)
	}
	for _, want := range []string{"provider", "model", "baseurl", "apikey", "nick"} {
		if !strings.Contains(msg.Content, want) {
			t.Errorf("expected %q in /set output, got %q", want, msg.Content)
		}
	}
}

func TestSetCommandShowsSingleSetting(t *testing.T) {
	m := newTestModel(t)
	result, _ := m.handleCommand("/set provider")
	msg := lastMsg(result)
	if !strings.Contains(msg.Content, "provider = openai") {
		t.Errorf("expected 'provider = openai', got %q", msg.Content)
	}
}

func TestSetCommandSetsProvider(t *testing.T) {
	m := newTestModel(t)
	result, _ := m.handleCommand("/set provider anthropic")
	model := result.(Model)
	if model.config.Provider != "anthropic" {
		t.Errorf("provider = %q, want anthropic", model.config.Provider)
	}
	text := allMsgText(result)
	if !strings.Contains(text, "Provider set to: anthropic") {
		t.Errorf("expected provider set message, got %q", text)
	}
}

func TestSetCommandSetsModel(t *testing.T) {
	m := newTestModel(t)
	result, _ := m.handleCommand("/set model claude-opus-4-6")
	model := result.(Model)
	if model.config.Model != "claude-opus-4-6" {
		t.Errorf("model = %q, want claude-opus-4-6", model.config.Model)
	}
}

func TestSetCommandSetsAPIKey(t *testing.T) {
	m := newTestModel(t)
	result, _ := m.handleCommand("/set apikey sk-1234567890abcdef")
	model := result.(Model)
	if model.config.APIKey != "sk-1234567890abcdef" {
		t.Errorf("apikey not set correctly")
	}
}

// TestSetCommandSetsAPIKeyVerbatim confirms /set apikey passes any value through
// without validation — including short proxy tokens like "x".
func TestSetCommandSetsAPIKeyVerbatim(t *testing.T) {
	m := newTestModel(t)
	result, _ := m.handleCommand("/set apikey x")
	model := result.(Model)
	if model.config.APIKey != "x" {
		t.Errorf("apikey = %q, want %q", model.config.APIKey, "x")
	}
	for _, msg := range allMsgs(result) {
		if msg.Type == MsgError {
			t.Errorf("unexpected error: %q", msg.Content)
		}
	}
}

// TestSetCommandSetsModelVerbatim confirms /set model passes any value through
// without warnings or rejection.
func TestSetCommandSetsModelVerbatim(t *testing.T) {
	m := newTestModel(t)
	result, _ := m.handleCommand("/set model x")
	model := result.(Model)
	if model.config.Model != "x" {
		t.Errorf("model = %q, want %q", model.config.Model, "x")
	}
	for _, msg := range allMsgs(result) {
		if msg.Type == MsgError {
			t.Errorf("unexpected error: %q", msg.Content)
		}
	}
}

func TestSetCommandSetsBaseURL(t *testing.T) {
	m := newTestModel(t)
	result, _ := m.handleCommand("/set baseurl https://api.example.com/v1")
	model := result.(Model)
	if model.config.BaseURL != "https://api.example.com/v1" {
		t.Errorf("baseurl = %q", model.config.BaseURL)
	}
}

func TestSetCommandSetsNick(t *testing.T) {
	m := newTestModel(t)
	result, _ := m.handleCommand("/set nick coolguy")
	model := result.(Model)
	if model.config.UserNick != "coolguy" {
		t.Errorf("nick = %q, want coolguy", model.config.UserNick)
	}
}

func TestSetCommandUnknownKey(t *testing.T) {
	m := newTestModel(t)
	result, _ := m.handleCommand("/set bogus value")
	msg := lastMsg(result)
	if msg.Type != MsgError {
		t.Fatalf("expected error for unknown key, got %v", msg.Type)
	}
	if !strings.Contains(msg.Content, "Unknown setting") {
		t.Errorf("unexpected error: %q", msg.Content)
	}
}

func TestSetCommandShowUnknownKey(t *testing.T) {
	m := newTestModel(t)
	result, _ := m.handleCommand("/set bogus")
	msg := lastMsg(result)
	if msg.Type != MsgError {
		t.Fatalf("expected error for unknown key, got %v", msg.Type)
	}
}

func TestSetCommandMasksAPIKey(t *testing.T) {
	m := newTestModel(t)
	result, _ := m.handleCommand("/set apikey")
	msg := lastMsg(result)
	if strings.Contains(msg.Content, "sk-test-key-12345") {
		t.Errorf("API key should be masked, got %q", msg.Content)
	}
	if !strings.Contains(msg.Content, "sk-t...2345") {
		t.Errorf("expected masked key, got %q", msg.Content)
	}
}

func TestProviderAliasStillWorks(t *testing.T) {
	m := newTestModel(t)
	result, _ := m.handleCommand("/provider anthropic")
	model := result.(Model)
	if model.config.Provider != "anthropic" {
		t.Errorf("provider alias failed: %q", model.config.Provider)
	}
}

func TestModelAliasStillWorks(t *testing.T) {
	m := newTestModel(t)
	result, _ := m.handleCommand("/model gpt-4o-mini")
	model := result.(Model)
	if model.config.Model != "gpt-4o-mini" {
		t.Errorf("model alias failed: %q", model.config.Model)
	}
}
