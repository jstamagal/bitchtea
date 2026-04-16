package ui

import (
	"testing"

	"github.com/jstamagal/bitchtea/internal/session"
)

func TestExecuteStartupCommandRunsSilently(t *testing.T) {
	m := newTestModel(t)

	m.ExecuteStartupCommand("join #code")

	if got := m.focus.ActiveLabel(); got != "#code" {
		t.Fatalf("active context = %q, want #code", got)
	}
	if len(m.messages) != 0 {
		t.Fatalf("expected silent startup command to add no messages, got %d", len(m.messages))
	}
}

func TestResumeThenExecuteStartupCommandKeepsMessagesAndUpdatesFocus(t *testing.T) {
	m := newTestModel(t)
	sess := &session.Session{
		Path: "resume.jsonl",
		Entries: []session.Entry{
			{Role: "user", Content: "old hello"},
			{Role: "assistant", Content: "old reply"},
		},
	}

	m.ResumeSession(sess)
	m.ExecuteStartupCommand("join #code")

	if got := m.focus.ActiveLabel(); got != "#code" {
		t.Fatalf("active context = %q, want #code", got)
	}
	if len(m.messages) != 2 {
		t.Fatalf("expected resumed messages to remain visible, got %d", len(m.messages))
	}
	if m.messages[0].Content != "old hello" || m.messages[1].Content != "old reply" {
		t.Fatalf("unexpected resumed messages: %#v", m.messages)
	}
}
