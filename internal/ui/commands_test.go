package ui

import (
	"strings"
	"testing"
)

// TestLookupSlashCommandIsCaseInsensitive pins the contract that bitchtearc
// lines (which become `/SET FOO bar` after ExecuteStartupCommand prepends a
// slash) reach their handler regardless of case. Previously the registry
// lookup was a direct map index, so /SET silently routed to the
// "Unknown command" path and every uppercase rc line was dropped twice.
func TestLookupSlashCommandIsCaseInsensitive(t *testing.T) {
	for _, name := range []string{"/SET", "/Set", "/set", "/HELP", "/Quit"} {
		if _, ok := lookupSlashCommand(name); !ok {
			t.Errorf("lookupSlashCommand(%q) = not found; should be case-insensitive", name)
		}
	}
}

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

	baseModel, _ := testModel(t)
	helpResult, _ := helpHandler(baseModel, "/help", []string{"/help"})
	aliasResult, _ := aliasHandler(baseModel, "/h", []string{"/h"})

	helpMsg := lastMsg(helpResult)
	aliasMsg := lastMsg(aliasResult)
	if helpMsg.Type != aliasMsg.Type || helpMsg.Content != aliasMsg.Content {
		t.Fatalf("expected /help and /h to behave the same, got %q vs %q", helpMsg.Content, aliasMsg.Content)
	}
}

func TestHandleCommandUsesAliasRegistry(t *testing.T) {
	m, _ := testModel(t)

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
	m, _ := testModel(t)

	result, _ := m.handleCommand("/definitely-not-real")
	msg := lastMsg(result)

	if msg.Type != MsgError {
		t.Fatalf("expected error message, got %v", msg.Type)
	}
	if !strings.Contains(msg.Content, "Unknown command: /definitely-not-real") {
		t.Fatalf("unexpected error message: %q", msg.Content)
	}
}

// TestRemovedRootCommandsAreNotRegistered guards against re-introducing root
// commands that are now expected to live behind /set. /auto-next, /auto-idea,
// and /sound were removed (bt-4sw) because they were thin toggle wrappers.
// /apikey, /baseurl, /provider, and /model were removed (bt-0k0) because they
// duplicated /set apikey, /set baseurl, /set provider, /set model.
func TestRemovedRootCommandsAreNotRegistered(t *testing.T) {
	removed := []string{
		"/auto-next", "/auto-idea", "/sound",
		"/apikey", "/baseurl", "/provider", "/model",
	}
	for _, name := range removed {
		if _, ok := lookupSlashCommand(name); ok {
			t.Errorf("%s should no longer be a root slash command — use /set instead", name)
		}
	}

	m, _ := testModel(t)
	for _, cmd := range removed {
		result, _ := m.handleCommand(cmd)
		msg := lastMsg(result)
		if msg.Type != MsgError {
			t.Fatalf("%s should return MsgError after removal, got %v: %q", cmd, msg.Type, msg.Content)
		}
		if !strings.Contains(msg.Content, "Unknown command") {
			t.Fatalf("%s should error with 'Unknown command', got %q", cmd, msg.Content)
		}
	}
}

// TestSetRoutesToRemovedRootHandlersStillWork confirms that /set apikey,
// /set baseurl, /set provider, and /set model still produce verbatim writes
// after the root commands were removed in bt-0k0. They share the underlying
// handlers via handleSetCommand's routing switch.
func TestSetRoutesToRemovedRootHandlersStillWork(t *testing.T) {
	cases := []struct {
		input string
		check func(Model) bool
		desc  string
	}{
		{"/set apikey x", func(m Model) bool { return m.config.APIKey == "x" }, "apikey verbatim"},
		{"/set baseurl notaurl", func(m Model) bool { return m.config.BaseURL == "notaurl" }, "baseurl verbatim"},
		{"/set provider foo", func(m Model) bool { return m.config.Provider == "foo" }, "provider verbatim"},
		{"/set model bar", func(m Model) bool { return m.config.Model == "bar" }, "model verbatim"},
	}
	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			m, _ := testModel(t)
			result, _ := m.handleCommand(tc.input)
			model := result.(Model)
			for _, msg := range model.messages {
				if msg.Type == MsgError {
					t.Errorf("unexpected error: %q", msg.Content)
				}
			}
			if !tc.check(model) {
				t.Errorf("%s: stored value did not match", tc.desc)
			}
		})
	}
}

// --- IRC routing commands ---
// --- IRC routing commands ---

func TestJoinCommandSwitchesActiveContext(t *testing.T) {
	m, _ := testModel(t)
	result, _ := handleJoinCommand(m, "/join #code", []string{"/join", "#code"})
	if result.focus.ActiveLabel() != "#code" {
		t.Errorf("active = %q, want #code", result.focus.ActiveLabel())
	}
}

func TestJoinCommandMissingArgErrors(t *testing.T) {
	m, _ := testModel(t)
	result, _ := handleJoinCommand(m, "/join", []string{"/join"})
	if len(result.messages) == 0 || result.messages[len(result.messages)-1].Type != MsgError {
		t.Error("expected error message")
	}
}

func TestPartCommandLeavesCurrentContext(t *testing.T) {
	m, _ := testModel(t)
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
	m, _ := testModel(t)
	result, _ := handlePartCommand(m, "/part", []string{"/part"})
	if len(result.messages) == 0 || result.messages[len(result.messages)-1].Type != MsgError {
		t.Error("expected error when parting last context")
	}
}

func TestQueryCommandOpensDirect(t *testing.T) {
	m, _ := testModel(t)
	result, _ := handleQueryCommand(m, "/query buddy", []string{"/query", "buddy"})
	if result.focus.ActiveLabel() != "buddy" {
		t.Errorf("active = %q, want buddy", result.focus.ActiveLabel())
	}
}

func TestChannelsCommandListsAll(t *testing.T) {
	m, _ := testModel(t)
	m.focus.SetFocus(Channel("code"))
	m.focus.SetFocus(Channel("ops"))
	result, _ := handleChannelsCommand(m, "/channels", []string{"/channels"})
	content := result.messages[len(result.messages)-1].Content
	if !strings.Contains(content, "#code") || !strings.Contains(content, "#ops") || !strings.Contains(content, "#main") {
		t.Errorf("channels list missing contexts: %q", content)
	}
}

func TestJoinPersistsFocusState(t *testing.T) {
	m, _ := testModel(t)
	result, _ := handleJoinCommand(m, "/join #persist", []string{"/join", "#persist"})
	restored := LoadFocusManager(result.config.SessionDir)
	for _, ctx := range restored.All() {
		if ctx.Label() == "#persist" {
			return // found
		}
	}
	t.Error("persisted focus state missing #persist after /join")
}
