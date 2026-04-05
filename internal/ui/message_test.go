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
