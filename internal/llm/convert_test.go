package llm

import (
	"strings"
	"testing"

	"charm.land/fantasy"
)

func TestSplitForFantasySystemAndTailUser(t *testing.T) {
	msgs := []Message{
		{Role: "system", Content: "system A"},
		{Role: "system", Content: "system B"},
		{Role: "user", Content: "first"},
		{Role: "assistant", Content: "ack"},
		{Role: "user", Content: "tail"},
	}
	prompt, prior, sys := splitForFantasy(msgs)

	if prompt != "tail" {
		t.Fatalf("prompt = %q, want %q", prompt, "tail")
	}
	if !strings.Contains(sys, "system A") || !strings.Contains(sys, "system B") {
		t.Fatalf("system prompt missing parts: %q", sys)
	}
	// prior should be: first (user), ack (assistant) — system stripped, tail stripped
	if len(prior) != 2 {
		t.Fatalf("prior len = %d, want 2: %+v", len(prior), prior)
	}
	if prior[0].Role != fantasy.MessageRoleUser {
		t.Fatalf("prior[0] role = %v, want user", prior[0].Role)
	}
	if prior[1].Role != fantasy.MessageRoleAssistant {
		t.Fatalf("prior[1] role = %v, want assistant", prior[1].Role)
	}
}

func TestSplitForFantasyAssistantTailKeepsAllMessages(t *testing.T) {
	// Transcript ends with assistant — must NOT pull an earlier user up to be
	// the new prompt. This is the codex regression: prior shape was reordered.
	msgs := []Message{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "Q"},
		{Role: "assistant", Content: "A"},
	}
	prompt, prior, sys := splitForFantasy(msgs)

	if prompt != "" {
		t.Fatalf("expected empty prompt for assistant-tail transcript, got %q", prompt)
	}
	if sys != "sys" {
		t.Fatalf("system prompt = %q, want %q", sys, "sys")
	}
	if len(prior) != 2 {
		t.Fatalf("expected both user+assistant in prior, got %d: %+v", len(prior), prior)
	}
	if prior[0].Role != fantasy.MessageRoleUser {
		t.Fatalf("prior[0] role = %v, want user", prior[0].Role)
	}
	if prior[1].Role != fantasy.MessageRoleAssistant {
		t.Fatalf("prior[1] role = %v, want assistant", prior[1].Role)
	}
}

func TestSplitForFantasyToolTailKeepsAllMessages(t *testing.T) {
	// tool-result tail (mid-flight resume) also must not promote earlier user.
	msgs := []Message{
		{Role: "user", Content: "Q"},
		{Role: "assistant", Content: "calling tool"},
		{Role: "tool", Content: "result"},
	}
	prompt, prior, _ := splitForFantasy(msgs)
	if prompt != "" {
		t.Fatalf("expected empty prompt for tool-tail transcript, got %q", prompt)
	}
	if len(prior) != 3 {
		t.Fatalf("expected 3 prior messages, got %d", len(prior))
	}
}

func TestSplitForFantasyAssistantToolCalls(t *testing.T) {
	msgs := []Message{
		{
			Role: "assistant",
			ToolCalls: []ToolCall{{
				ID: "c1", Type: "function",
				Function: FunctionCall{Name: "read", Arguments: `{"path":"x"}`},
			}},
		},
		{Role: "tool", ToolCallID: "c1", Content: "file body"},
		{Role: "user", Content: "next"},
	}
	prompt, prior, _ := splitForFantasy(msgs)
	if prompt != "next" {
		t.Fatalf("prompt = %q", prompt)
	}
	if len(prior) != 2 {
		t.Fatalf("prior len = %d", len(prior))
	}

	// assistant message must contain a ToolCallPart.
	asst := prior[0]
	if asst.Role != fantasy.MessageRoleAssistant {
		t.Fatalf("prior[0] role = %v", asst.Role)
	}
	foundCall := false
	for _, p := range asst.Content {
		if _, ok := p.(fantasy.ToolCallPart); ok {
			foundCall = true
			break
		}
	}
	if !foundCall {
		t.Fatalf("assistant prior message missing ToolCallPart: %+v", asst.Content)
	}
}

func TestFantasyToLLMTextAssistant(t *testing.T) {
	fm := fantasy.Message{
		Role:    fantasy.MessageRoleAssistant,
		Content: []fantasy.MessagePart{fantasy.TextPart{Text: "hello"}},
	}
	got := fantasyToLLM(fm)
	if got.Role != "assistant" {
		t.Fatalf("role = %q", got.Role)
	}
	if got.Content != "hello" {
		t.Fatalf("content = %q", got.Content)
	}
}

func TestFantasyToLLMToolResultRole(t *testing.T) {
	fm := fantasy.Message{
		Role: fantasy.MessageRoleTool,
		Content: []fantasy.MessagePart{fantasy.ToolResultPart{
			ToolCallID: "c1",
			Output:     fantasy.ToolResultOutputContentText{Text: "result"},
		}},
	}
	got := fantasyToLLM(fm)
	if got.Role != "tool" {
		t.Fatalf("role = %q", got.Role)
	}
	if got.ToolCallID != "c1" {
		t.Fatalf("tool_call_id = %q", got.ToolCallID)
	}
	if got.Content != "result" {
		t.Fatalf("content = %q", got.Content)
	}
}

func TestFantasyToLLMAssistantToolCallRoundTrip(t *testing.T) {
	fm := fantasy.Message{
		Role: fantasy.MessageRoleAssistant,
		Content: []fantasy.MessagePart{
			fantasy.TextPart{Text: "calling"},
			fantasy.ToolCallPart{ToolCallID: "c1", ToolName: "read", Input: `{"path":"x"}`},
		},
	}
	got := fantasyToLLM(fm)
	if got.Content != "calling" {
		t.Fatalf("content = %q", got.Content)
	}
	if len(got.ToolCalls) != 1 || got.ToolCalls[0].Function.Name != "read" {
		t.Fatalf("tool calls not preserved: %+v", got.ToolCalls)
	}
}

func TestToLLMUsageCasts(t *testing.T) {
	got := toLLMUsage(fantasy.Usage{
		InputTokens:         12,
		OutputTokens:        34,
		CacheCreationTokens: 5,
		CacheReadTokens:     6,
	})
	if got.InputTokens != 12 || got.OutputTokens != 34 || got.CacheCreationTokens != 5 || got.CacheReadTokens != 6 {
		t.Fatalf("usage cast wrong: %+v", got)
	}
}
