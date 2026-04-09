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
