package session

import (
	"testing"

	"charm.land/fantasy"
)

// BenchmarkFantasyFromEntries measures FantasyFromEntries over a synthetic
// session entry slice that mixes v1 (fantasy-native) and v0 (legacy) entries
// with text, tool calls, and tool results.
func BenchmarkFantasyFromEntries(b *testing.B) {
	const n = 500
	entries := make([]Entry, 0, n)

	msgUser := fantasy.Message{
		Role:    fantasy.MessageRoleUser,
		Content: []fantasy.MessagePart{fantasy.TextPart{Text: "write a Go benchmark function"}},
	}

	msgAssistant := fantasy.Message{
		Role: fantasy.MessageRoleAssistant,
		Content: []fantasy.MessagePart{
			fantasy.TextPart{Text: "I'll write a benchmark."},
			fantasy.ToolCallPart{
				ToolCallID: "call_1",
				ToolName:   "read",
				Input:      `{"path":"main.go"}`,
			},
			fantasy.ToolCallPart{
				ToolCallID: "call_2",
				ToolName:   "bash",
				Input:      `{"command":"go test -bench=."}`,
			},
		},
	}

	msgTool1 := fantasy.Message{
		Role: fantasy.MessageRoleTool,
		Content: []fantasy.MessagePart{fantasy.ToolResultPart{
			ToolCallID: "call_1",
			Output:     fantasy.ToolResultOutputContentText{Text: "package main\n\nfunc main() {}"},
		}},
	}

	msgTool2 := fantasy.Message{
		Role: fantasy.MessageRoleTool,
		Content: []fantasy.MessagePart{fantasy.ToolResultPart{
			ToolCallID: "call_2",
			Output:     fantasy.ToolResultOutputContentText{Text: "BenchmarkFoo-8  1000  1234567 ns/op"},
		}},
	}

	for i := 0; i < n/5; i++ {
		entries = append(entries,
			EntryFromFantasy(msgUser),
			EntryFromFantasy(msgAssistant),
			EntryFromFantasy(msgTool1),
			EntryFromFantasy(msgTool2),
			Entry{Role: "user", Content: "legacy v0 entry"}, // v0 fallback path
		)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		msgs := FantasyFromEntries(entries)
		if len(msgs) != len(entries) {
			b.Fatalf("expected %d messages, got %d", len(entries), len(msgs))
		}
	}
}
