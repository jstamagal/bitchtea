package ui

import "testing"

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
