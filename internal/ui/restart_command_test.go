package ui

import (
	"strings"
	"testing"
	"time"
)

func TestRestartCommandClearsHistoryAndDisplay(t *testing.T) {
	m := testModel(t)

	// Seed some chat display + agent state.
	m.addMessage(ChatMessage{Time: time.Now(), Type: MsgUser, Nick: "tj", Content: "hello"})
	m.addMessage(ChatMessage{Time: time.Now(), Type: MsgAgent, Nick: "bt", Content: "hi"})
	m.queued = append(m.queued, queuedMsg{text: "queued msg", queuedAt: time.Now()})
	bootstrapBefore := m.agent.MessageCount()
	originalSession := m.session

	result, cmd := m.handleCommand("/restart")
	model := result.(Model)

	if cmd != nil {
		t.Errorf("expected /restart to return nil cmd, got %T", cmd)
	}

	// Display should be cleared except for the system confirmation message.
	if len(model.messages) != 1 {
		t.Fatalf("expected 1 chat message after restart, got %d", len(model.messages))
	}
	last := model.messages[0]
	if last.Type != MsgSystem || !strings.Contains(last.Content, "restarted") {
		t.Errorf("expected restart system message, got %+v", last)
	}

	// Agent history should be back to bootstrap (or smaller if discovery differs).
	if got := model.agent.MessageCount(); got > bootstrapBefore {
		t.Errorf("expected agent message count to drop on restart, before=%d after=%d", bootstrapBefore, got)
	}
	if model.agent.TurnCount != 0 {
		t.Errorf("expected TurnCount reset, got %d", model.agent.TurnCount)
	}
	if model.agent.BootstrapMessageCount() != model.agent.MessageCount() {
		t.Errorf("expected bootstrap count to equal current message count after reset, got bootstrap=%d msgs=%d",
			model.agent.BootstrapMessageCount(), model.agent.MessageCount())
	}

	if len(model.queued) != 0 {
		t.Errorf("expected queued messages cleared, got %d", len(model.queued))
	}
	if model.lastSavedMsgIdx != 0 {
		t.Errorf("expected lastSavedMsgIdx reset to 0, got %d", model.lastSavedMsgIdx)
	}

	// A fresh session should be started (different path) when one was active.
	if originalSession != nil && model.session == originalSession {
		t.Errorf("expected /restart to swap in a fresh session log")
	}
}

func TestRestartCommandRegisteredInHelp(t *testing.T) {
	m := testModel(t)
	result, _ := m.handleCommand("/help")
	msg := lastMsg(result)
	if !strings.Contains(msg.Content, "/restart") {
		t.Errorf("expected /restart in help output, got %q", msg.Content)
	}
}
