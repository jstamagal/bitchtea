package llm

import (
	"testing"

	"charm.land/fantasy"
)

// BenchmarkFantasySliceToLLM measures the cost of converting a realistic
// fantasy.Message slice (text, tool calls, tool results) back into the legacy
// llm.Message shape.
func BenchmarkFantasySliceToLLM(b *testing.B) {
	const n = 200
	messages := make([]fantasy.Message, 0, n)

	userMsg := fantasy.Message{
		Role:    fantasy.MessageRoleUser,
		Content: []fantasy.MessagePart{fantasy.TextPart{Text: "Refactor the stream loop to use context for cancellation."}},
	}

	assistantMsg := fantasy.Message{
		Role: fantasy.MessageRoleAssistant,
		Content: []fantasy.MessagePart{
			fantasy.TextPart{Text: "I'll look at the stream file first."},
			fantasy.ToolCallPart{
				ToolCallID: "tc_read_stream",
				ToolName:   "read",
				Input:      `{"path":"internal/llm/stream.go"}`,
			},
			fantasy.ToolCallPart{
				ToolCallID: "tc_bash_build",
				ToolName:   "bash",
				Input:      `{"command":"go build ./..."}`,
			},
		},
	}

	toolResultRead := fantasy.Message{
		Role: fantasy.MessageRoleTool,
		Content: []fantasy.MessagePart{fantasy.ToolResultPart{
			ToolCallID: "tc_read_stream",
			Output:     fantasy.ToolResultOutputContentText{Text: "func stream(ctx context.Context, ..."},
		}},
	}

	toolResultBash := fantasy.Message{
		Role: fantasy.MessageRoleTool,
		Content: []fantasy.MessagePart{fantasy.ToolResultPart{
			ToolCallID: "tc_bash_build",
			Output:     fantasy.ToolResultOutputContentText{Text: "ok"},
		}},
	}

	for i := 0; i < n/4; i++ {
		messages = append(messages, userMsg, assistantMsg, toolResultRead, toolResultBash)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		out := FantasySliceToLLM(messages)
		if len(out) != len(messages) {
			b.Fatalf("expected %d messages, got %d", len(messages), len(out))
		}
	}
}
