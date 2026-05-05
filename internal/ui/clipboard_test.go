package ui

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestCopyCommandCopiesLastAssistantResponse(t *testing.T) {
	restore := stubClipboard(t)
	defer restore()

	var copied string
	stdoutIsTerminal = func() bool { return true }
	writeOSC52Clipboard = func(text string) error {
		copied = text
		return nil
	}

	m, _ := testModel(t)
	m.messages = []ChatMessage{
		{Time: time.Now(), Type: MsgUser, Content: "hi"},
		{Time: time.Now(), Type: MsgAgent, Content: "first"},
		{Time: time.Now(), Type: MsgAgent, Content: "second"},
	}

	result, _ := m.handleCommand("/copy")
	msg := lastMsg(result)

	if copied != "second" {
		t.Fatalf("expected last assistant response to be copied, got %q", copied)
	}
	if msg.Type != MsgSystem {
		t.Fatalf("expected system message, got %v", msg.Type)
	}
	if !strings.Contains(msg.Content, "Copied last assistant response via OSC 52.") {
		t.Fatalf("unexpected status message: %q", msg.Content)
	}
}

func TestCopyCommandCopiesSelectedAssistantResponse(t *testing.T) {
	restore := stubClipboard(t)
	defer restore()

	var copied string
	stdoutIsTerminal = func() bool { return false }
	lookPath = func(name string) (string, error) {
		if name == "xclip" {
			return "/usr/bin/xclip", nil
		}
		return "", fmt.Errorf("missing %s", name)
	}
	runClipboardCommand = func(name string, args []string, text string) error {
		if name != "xclip" {
			t.Fatalf("expected xclip fallback, got %s", name)
		}
		if strings.Join(args, " ") != "-selection clipboard" {
			t.Fatalf("unexpected args: %v", args)
		}
		copied = text
		return nil
	}

	m, _ := testModel(t)
	m.messages = []ChatMessage{
		{Time: time.Now(), Type: MsgAgent, Content: "one"},
		{Time: time.Now(), Type: MsgSystem, Content: "ignore"},
		{Time: time.Now(), Type: MsgAgent, Content: "two"},
	}

	result, _ := m.handleCommand("/copy 1")
	msg := lastMsg(result)

	if copied != "one" {
		t.Fatalf("expected first assistant response to be copied, got %q", copied)
	}
	if !strings.Contains(msg.Content, "Copied assistant response 1 via xclip.") {
		t.Fatalf("unexpected status message: %q", msg.Content)
	}
}

func TestCopyCommandRequiresAssistantMessage(t *testing.T) {
	restore := stubClipboard(t)
	defer restore()

	m, _ := testModel(t)
	m.messages = []ChatMessage{
		{Time: time.Now(), Type: MsgUser, Content: "hi"},
	}

	result, _ := m.handleCommand("/copy")
	msg := lastMsg(result)
	if msg.Type != MsgError {
		t.Fatalf("expected error message, got %v", msg.Type)
	}
	if !strings.Contains(msg.Content, "No assistant responses available") {
		t.Fatalf("unexpected error: %q", msg.Content)
	}
}

func TestCopyCommandRejectsInvalidSelection(t *testing.T) {
	restore := stubClipboard(t)
	defer restore()

	m, _ := testModel(t)
	m.messages = []ChatMessage{
		{Time: time.Now(), Type: MsgAgent, Content: "one"},
	}

	result, _ := m.handleCommand("/copy nope")
	msg := lastMsg(result)
	if msg.Type != MsgError {
		t.Fatalf("expected error message, got %v", msg.Type)
	}
	if !strings.Contains(msg.Content, "Usage: /copy [n]") {
		t.Fatalf("unexpected error: %q", msg.Content)
	}
}

func TestCopyCommandRejectsOutOfRangeSelection(t *testing.T) {
	restore := stubClipboard(t)
	defer restore()

	m, _ := testModel(t)
	m.messages = []ChatMessage{
		{Time: time.Now(), Type: MsgAgent, Content: "one"},
	}

	result, _ := m.handleCommand("/copy 2")
	msg := lastMsg(result)
	if msg.Type != MsgError {
		t.Fatalf("expected error message, got %v", msg.Type)
	}
	if !strings.Contains(msg.Content, "Assistant message 2 does not exist") {
		t.Fatalf("unexpected error: %q", msg.Content)
	}
}

func stubClipboard(t *testing.T) func() {
	t.Helper()

	origStdoutIsTerminal := stdoutIsTerminal
	origWriteOSC52Clipboard := writeOSC52Clipboard
	origLookPath := lookPath
	origRunClipboardCommand := runClipboardCommand

	return func() {
		stdoutIsTerminal = origStdoutIsTerminal
		writeOSC52Clipboard = origWriteOSC52Clipboard
		lookPath = origLookPath
		runClipboardCommand = origRunClipboardCommand
	}
}
