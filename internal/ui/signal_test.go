package ui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestSuspendMsgHandling(t *testing.T) {
	m := testModel(t)

	// Send SuspendMsg and verify it returns tea.Suspend command
	updatedModel, cmd := m.Update(tea.SuspendMsg{})
	if cmd == nil {
		t.Error("Expected non-nil command for SuspendMsg")
	}

	// The command should be tea.Suspend which returns tea.SuspendMsg
	msg := cmd()
	if _, ok := msg.(tea.SuspendMsg); !ok {
		t.Errorf("Expected tea.SuspendMsg from command, got %T", msg)
	}

	// Model should be unchanged
	if updatedModel.(Model).streaming {
		t.Error("Model should not be streaming after suspend")
	}
}

func TestQuitMsgCancelsStreaming(t *testing.T) {
	m := testModel(t)

	// Simulate streaming state
	m.streaming = true
	cancelCalled := false
	m.cancel = func() {
		cancelCalled = true
	}

	// Send QuitMsg and verify it cancels streaming
	updatedModel, cmd := m.Update(tea.QuitMsg{})

	if !cancelCalled {
		t.Error("Expected cancel function to be called on QuitMsg during streaming")
	}

	model := updatedModel.(Model)
	if model.streaming {
		t.Error("Streaming should be false after QuitMsg")
	}

	if cmd == nil {
		t.Error("Expected non-nil command (tea.Quit)")
	}
}

func TestQuitMsgWhenNotStreaming(t *testing.T) {
	m := testModel(t)

	// Ensure not streaming
	m.streaming = false
	m.cancel = nil

	// Send QuitMsg - should just quit without panic
	_, cmd := m.Update(tea.QuitMsg{})

	if cmd == nil {
		t.Error("Expected non-nil command (tea.Quit)")
	}
}