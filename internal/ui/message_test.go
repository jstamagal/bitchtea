package ui

import (
	"strings"
	"testing"
	"time"
)

func TestChatMessageFormat(t *testing.T) {
	now := time.Date(2026, 1, 1, 14, 30, 0, 0, time.UTC)

	tests := []struct {
		name     string
		msg      ChatMessage
		contains []string
	}{
		{
			name:     "user message",
			msg:      ChatMessage{Time: now, Type: MsgUser, Nick: "anon", Content: "hello"},
			contains: []string{"14:30", "anon", "hello"},
		},
		{
			name:     "agent message",
			msg:      ChatMessage{Time: now, Type: MsgAgent, Nick: "bitchtea", Content: "yo"},
			contains: []string{"14:30", "bitchtea", "yo"},
		},
		{
			name:     "system message",
			msg:      ChatMessage{Time: now, Type: MsgSystem, Content: "connected"},
			contains: []string{"14:30", "***", "connected"},
		},
		{
			name:     "error message",
			msg:      ChatMessage{Time: now, Type: MsgError, Content: "oh no"},
			contains: []string{"14:30", "!!!", "oh no"},
		},
		{
			name:     "tool message",
			msg:      ChatMessage{Time: now, Type: MsgTool, Nick: "bash", Content: "output"},
			contains: []string{"14:30", "bash", "output"},
		},
		{
			name:     "raw message",
			msg:      ChatMessage{Time: now, Type: MsgRaw, Content: "raw content"},
			contains: []string{"raw content"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			formatted := tt.msg.Format()
			for _, s := range tt.contains {
				if !strings.Contains(formatted, s) {
					t.Errorf("format() = %q, missing %q", formatted, s)
				}
			}
		})
	}
}

func TestAllMsgTypesFormatNonEmpty(t *testing.T) {
	now := time.Date(2026, 1, 1, 14, 30, 0, 0, time.UTC)

	tests := []ChatMessage{
		{Time: now, Type: MsgUser, Nick: "anon", Content: "hello"},
		{Time: now, Type: MsgAgent, Nick: "bitchtea", Content: "hello", Width: 80},
		{Time: now, Type: MsgSystem, Content: "system"},
		{Time: now, Type: MsgError, Content: "error"},
		{Time: now, Type: MsgTool, Nick: "bash", Content: "output"},
		{Time: now, Type: MsgThink, Content: "thinking"},
		{Time: now, Type: MsgRaw, Content: "raw"},
	}

	for _, msg := range tests {
		formatted := msg.Format()
		if formatted == "" {
			t.Fatalf("expected non-empty formatted output for type %v", msg.Type)
		}
	}
}

func TestChatMessageFormatLongContentNoPanic(t *testing.T) {
	now := time.Date(2026, 1, 1, 14, 30, 0, 0, time.UTC)
	longContent := strings.Repeat("long content ", 200)

	tests := []ChatMessage{
		{Time: now, Type: MsgUser, Nick: "anon", Content: longContent},
		{Time: now, Type: MsgAgent, Nick: "bitchtea", Content: "## Heading\n\n" + longContent, Width: 40},
		{Time: now, Type: MsgSystem, Content: longContent},
		{Time: now, Type: MsgError, Content: longContent},
		{Time: now, Type: MsgTool, Nick: "bash", Content: longContent},
		{Time: now, Type: MsgThink, Content: longContent},
		{Time: now, Type: MsgRaw, Content: longContent},
	}

	for _, msg := range tests {
		assertNoPanicFormat(t, msg)
	}
}

func TestChatMessageFormatEmptyContentNoPanic(t *testing.T) {
	now := time.Date(2026, 1, 1, 14, 30, 0, 0, time.UTC)

	tests := []ChatMessage{
		{Time: now, Type: MsgUser, Nick: "anon"},
		{Time: now, Type: MsgAgent, Nick: "bitchtea", Width: 40},
		{Time: now, Type: MsgSystem},
		{Time: now, Type: MsgError},
		{Time: now, Type: MsgTool, Nick: "bash"},
		{Time: now, Type: MsgThink},
		{Time: now, Type: MsgRaw},
	}

	for _, msg := range tests {
		assertNoPanicFormat(t, msg)
	}
}

func TestAgentMessageFormatsMarkdown(t *testing.T) {
	now := time.Date(2026, 1, 1, 14, 30, 0, 0, time.UTC)
	msg := ChatMessage{
		Time:    now,
		Type:    MsgAgent,
		Nick:    "bitchtea",
		Content: "## Heading\n- alpha\n- beta",
		Width:   40,
	}

	formatted := msg.Format()
	for _, want := range []string{"14:30", "bitchtea", "Heading", "alpha", "beta"} {
		if !strings.Contains(formatted, want) {
			t.Fatalf("expected formatted agent markdown to contain %q, got %q", want, formatted)
		}
	}
}

func assertNoPanicFormat(t *testing.T, msg ChatMessage) {
	t.Helper()

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Format panicked for type %v: %v", msg.Type, r)
		}
	}()

	_ = msg.Format()
}
