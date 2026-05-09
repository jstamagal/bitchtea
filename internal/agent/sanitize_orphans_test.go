package agent

import (
	"testing"

	"charm.land/fantasy"
)

// TestSanitizeOrphanedToolUses_NoOpOnCleanTranscript proves the sanitizer
// is a no-op when every assistant tool_use already has a matching
// tool_result on the very next message — bt-p6i must not perturb healthy
// transcripts.
func TestSanitizeOrphanedToolUses_NoOpOnCleanTranscript(t *testing.T) {
	msgs := []fantasy.Message{
		newUserMessage("hi"),
		{
			Role: fantasy.MessageRoleAssistant,
			Content: []fantasy.MessagePart{
				fantasy.TextPart{Text: "calling bash"},
				fantasy.ToolCallPart{ToolCallID: "call_1", ToolName: "bash", Input: `{"cmd":"ls"}`},
			},
		},
		{
			Role: fantasy.MessageRoleTool,
			Content: []fantasy.MessagePart{
				fantasy.ToolResultPart{ToolCallID: "call_1", Output: fantasy.ToolResultOutputContentText{Text: "file.txt"}},
			},
		},
	}
	before := len(msgs)
	inserted := sanitizeOrphanedToolUsesIn(&msgs)
	if inserted != 0 || len(msgs) != before {
		t.Fatalf("clean transcript was modified: inserted=%d before=%d after=%d", inserted, before, len(msgs))
	}
}

// TestSanitizeOrphanedToolUses_InsertsForOrphanAtEnd is the canonical
// bt-p6i symptom: a turn errored after the assistant emitted a tool_use
// but before the tool_result came back. Sanitizer must splice in a
// synthetic tool message so the next API call validates.
func TestSanitizeOrphanedToolUses_InsertsForOrphanAtEnd(t *testing.T) {
	msgs := []fantasy.Message{
		newUserMessage("hi"),
		{
			Role: fantasy.MessageRoleAssistant,
			Content: []fantasy.MessagePart{
				fantasy.TextPart{Text: "calling bash"},
				fantasy.ToolCallPart{ToolCallID: "call_1", ToolName: "bash", Input: `{"cmd":"ls"}`},
			},
		},
	}
	inserted := sanitizeOrphanedToolUsesIn(&msgs)
	if inserted != 1 {
		t.Fatalf("expected 1 synthetic result, got %d", inserted)
	}
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages after sanitize, got %d", len(msgs))
	}
	last := msgs[2]
	if last.Role != fantasy.MessageRoleTool {
		t.Fatalf("synthetic message role = %q, want tool", last.Role)
	}
	if len(last.Content) != 1 {
		t.Fatalf("synthetic message should have 1 part, got %d", len(last.Content))
	}
	rp, ok := last.Content[0].(fantasy.ToolResultPart)
	if !ok {
		t.Fatalf("synthetic part is not a ToolResultPart: %T", last.Content[0])
	}
	if rp.ToolCallID != "call_1" {
		t.Fatalf("synthetic ToolCallID = %q, want call_1", rp.ToolCallID)
	}
	txt, ok := rp.Output.(fantasy.ToolResultOutputContentText)
	if !ok {
		t.Fatalf("synthetic output is not text: %T", rp.Output)
	}
	if txt.Text != "<error>turn cancelled</error>" {
		t.Fatalf("synthetic text = %q", txt.Text)
	}
}

// TestSanitizeOrphanedToolUses_ExtendsExistingPartialTool covers the case
// where the tool message exists but is missing one of the two tool_use
// IDs the assistant emitted.
func TestSanitizeOrphanedToolUses_ExtendsExistingPartialTool(t *testing.T) {
	msgs := []fantasy.Message{
		newUserMessage("hi"),
		{
			Role: fantasy.MessageRoleAssistant,
			Content: []fantasy.MessagePart{
				fantasy.ToolCallPart{ToolCallID: "call_a", ToolName: "bash", Input: `{}`},
				fantasy.ToolCallPart{ToolCallID: "call_b", ToolName: "bash", Input: `{}`},
			},
		},
		{
			Role: fantasy.MessageRoleTool,
			Content: []fantasy.MessagePart{
				fantasy.ToolResultPart{ToolCallID: "call_a", Output: fantasy.ToolResultOutputContentText{Text: "ok"}},
			},
		},
	}
	inserted := sanitizeOrphanedToolUsesIn(&msgs)
	if inserted != 1 {
		t.Fatalf("expected 1 synthetic result, got %d", inserted)
	}
	if len(msgs) != 3 {
		t.Fatalf("messages length should stay 3 (extended in place), got %d", len(msgs))
	}
	tool := msgs[2]
	if len(tool.Content) != 2 {
		t.Fatalf("tool message should now have 2 parts, got %d", len(tool.Content))
	}
	rp, ok := tool.Content[1].(fantasy.ToolResultPart)
	if !ok {
		t.Fatalf("appended part not a ToolResultPart: %T", tool.Content[1])
	}
	if rp.ToolCallID != "call_b" {
		t.Fatalf("appended ToolCallID = %q, want call_b", rp.ToolCallID)
	}
}

// TestSanitizeOrphanedToolUses_HandlesMultipleAssistantTurns ensures the
// walker processes every assistant turn, not just the last, and skips
// over the synthetic message it just inserted (no infinite loop).
func TestSanitizeOrphanedToolUses_HandlesMultipleAssistantTurns(t *testing.T) {
	msgs := []fantasy.Message{
		newUserMessage("first"),
		{
			Role: fantasy.MessageRoleAssistant,
			Content: []fantasy.MessagePart{
				fantasy.ToolCallPart{ToolCallID: "t1", ToolName: "bash", Input: `{}`},
			},
		},
		// orphan #1 — no tool message follows
		newUserMessage("second"),
		{
			Role: fantasy.MessageRoleAssistant,
			Content: []fantasy.MessagePart{
				fantasy.ToolCallPart{ToolCallID: "t2", ToolName: "bash", Input: `{}`},
			},
		},
		// orphan #2 — no tool message follows
	}
	inserted := sanitizeOrphanedToolUsesIn(&msgs)
	if inserted != 2 {
		t.Fatalf("expected 2 synthetic results, got %d", inserted)
	}
	// Layout should be: user, assistant(t1), tool(t1), user, assistant(t2), tool(t2)
	if len(msgs) != 6 {
		t.Fatalf("expected 6 messages, got %d", len(msgs))
	}
	if msgs[2].Role != fantasy.MessageRoleTool {
		t.Fatalf("msgs[2] role = %q, want tool", msgs[2].Role)
	}
	if msgs[5].Role != fantasy.MessageRoleTool {
		t.Fatalf("msgs[5] role = %q, want tool", msgs[5].Role)
	}
}

// TestSanitizeOrphanedToolUses_Idempotent confirms running the sanitizer
// twice yields the same transcript — important because the agent calls
// it on every error/cancel/done path and we must not multiply synthetics.
func TestSanitizeOrphanedToolUses_Idempotent(t *testing.T) {
	msgs := []fantasy.Message{
		newUserMessage("hi"),
		{
			Role: fantasy.MessageRoleAssistant,
			Content: []fantasy.MessagePart{
				fantasy.ToolCallPart{ToolCallID: "call_x", ToolName: "bash", Input: `{}`},
			},
		},
	}
	first := sanitizeOrphanedToolUsesIn(&msgs)
	lenAfterFirst := len(msgs)
	second := sanitizeOrphanedToolUsesIn(&msgs)
	if first != 1 {
		t.Fatalf("first pass inserted %d, want 1", first)
	}
	if second != 0 {
		t.Fatalf("second pass inserted %d, want 0 (idempotent)", second)
	}
	if len(msgs) != lenAfterFirst {
		t.Fatalf("transcript length changed on second pass: %d → %d", lenAfterFirst, len(msgs))
	}
}
