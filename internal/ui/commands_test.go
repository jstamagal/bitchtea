package ui

import (
	"strings"
	"testing"
)

func TestLookupSlashCommandSupportsAliases(t *testing.T) {
	helpHandler, ok := lookupSlashCommand("/help")
	if !ok {
		t.Fatal("expected /help to be registered")
	}

	aliasHandler, ok := lookupSlashCommand("/h")
	if !ok {
		t.Fatal("expected /h alias to be registered")
	}

	if helpHandler == nil || aliasHandler == nil {
		t.Fatal("expected non-nil handlers")
	}

	baseModel := newTestModel(t)
	helpResult, _ := helpHandler(baseModel, "/help", []string{"/help"})
	aliasResult, _ := aliasHandler(baseModel, "/h", []string{"/h"})

	helpMsg := lastMsg(helpResult)
	aliasMsg := lastMsg(aliasResult)
	if helpMsg.Type != aliasMsg.Type || helpMsg.Content != aliasMsg.Content {
		t.Fatalf("expected /help and /h to behave the same, got %q vs %q", helpMsg.Content, aliasMsg.Content)
	}
}

func TestHandleCommandUsesAliasRegistry(t *testing.T) {
	m := newTestModel(t)

	result, _ := m.handleCommand("/h")
	msg := lastMsg(result)

	if msg.Type != MsgSystem {
		t.Fatalf("expected system message, got %v", msg.Type)
	}
	if !strings.Contains(msg.Content, "Commands:") {
		t.Fatalf("expected help output, got %q", msg.Content)
	}
	if !strings.Contains(msg.Content, "/activity [clear]") {
		t.Fatalf("expected /activity in help output, got %q", msg.Content)
	}
}

func TestHandleCommandUnknownCommandStillErrors(t *testing.T) {
	m := newTestModel(t)

	result, _ := m.handleCommand("/definitely-not-real")
	msg := lastMsg(result)

	if msg.Type != MsgError {
		t.Fatalf("expected error message, got %v", msg.Type)
	}
	if !strings.Contains(msg.Content, "Unknown command: /definitely-not-real") {
		t.Fatalf("unexpected error message: %q", msg.Content)
	}
}

// --- IRC routing commands ---
// --- IRC routing commands ---

func TestJoinCommandSwitchesActiveContext(t *testing.T) {
	m := newTestModel(t)
	result, _ := handleJoinCommand(m, "/join #code", []string{"/join", "#code"})
	if result.focus.ActiveLabel() != "#code" {
		t.Errorf("active = %q, want #code", result.focus.ActiveLabel())
	}
}

func TestJoinCommandMissingArgErrors(t *testing.T) {
	m := newTestModel(t)
	result, _ := handleJoinCommand(m, "/join", []string{"/join"})
	if len(result.messages) == 0 || result.messages[len(result.messages)-1].Type != MsgError {
		t.Error("expected error message")
	}
}

func TestPartCommandLeavesCurrentContext(t *testing.T) {
	m := newTestModel(t)
	m2, _ := handleJoinCommand(m, "/join #code", []string{"/join", "#code"})
	m3, _ := handlePartCommand(m2, "/part", []string{"/part"})
	if m3.focus.ActiveLabel() == "#code" {
		t.Error("still in #code after /part")
	}
	if len(m3.focus.All()) != 1 {
		t.Errorf("expected 1 context after part, got %d", len(m3.focus.All()))
	}
}

func TestPartCommandRefusesLastContext(t *testing.T) {
	m := newTestModel(t)
	result, _ := handlePartCommand(m, "/part", []string{"/part"})
	if len(result.messages) == 0 || result.messages[len(result.messages)-1].Type != MsgError {
		t.Error("expected error when parting last context")
	}
}

func TestQueryCommandOpensDirect(t *testing.T) {
	m := newTestModel(t)
	result, _ := handleQueryCommand(m, "/query buddy", []string{"/query", "buddy"})
	if result.focus.ActiveLabel() != "buddy" {
		t.Errorf("active = %q, want buddy", result.focus.ActiveLabel())
	}
}

func TestChannelsCommandListsAll(t *testing.T) {
	m := newTestModel(t)
	m.focus.SetFocus(Channel("code"))
	m.focus.SetFocus(Channel("ops"))
	result, _ := handleChannelsCommand(m, "/channels", []string{"/channels"})
	content := result.messages[len(result.messages)-1].Content
	if !strings.Contains(content, "#code") || !strings.Contains(content, "#ops") || !strings.Contains(content, "#main") {
		t.Errorf("channels list missing contexts: %q", content)
	}
}

func TestJoinPersistsFocusState(t *testing.T) {
	m := newTestModel(t)
	result, _ := handleJoinCommand(m, "/join #persist", []string{"/join", "#persist"})
	restored := LoadFocusManager(result.config.SessionDir)
	for _, ctx := range restored.All() {
		if ctx.Label() == "#persist" {
			return // found
		}
	}
	t.Error("persisted focus state missing #persist after /join")
}
