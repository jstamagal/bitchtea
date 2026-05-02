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

// --- fantasyToLLM edge-case coverage (bt-oqu) ---------------------------
//
// These tests exercise the branches of fantasyToLLM that the existing happy-
// path tests miss: pointer variants of every part type, multi-tool-call
// assistant messages, mixed text+tool_call assistant messages, the nil-pointer
// guards, and the `*ToolResultOutputContentText` inner-pointer branch. They
// assert *current* behavior — if you intend to change fantasyToLLM, update or
// add tests rather than mutating these to pass.

func TestFantasyToLLMEmptyPartsNoPanic(t *testing.T) {
	got := fantasyToLLM(fantasy.Message{
		Role:    fantasy.MessageRoleAssistant,
		Content: nil,
	})
	if got.Role != "assistant" {
		t.Fatalf("role = %q, want assistant", got.Role)
	}
	if got.Content != "" {
		t.Fatalf("content = %q, want empty", got.Content)
	}
	if len(got.ToolCalls) != 0 {
		t.Fatalf("tool calls should be empty, got %+v", got.ToolCalls)
	}
	if got.ToolCallID != "" {
		t.Fatalf("tool_call_id should be empty, got %q", got.ToolCallID)
	}
}

func TestFantasyToLLMPointerTextPart(t *testing.T) {
	fm := fantasy.Message{
		Role: fantasy.MessageRoleAssistant,
		Content: []fantasy.MessagePart{
			&fantasy.TextPart{Text: "ptr-hello"},
		},
	}
	got := fantasyToLLM(fm)
	if got.Content != "ptr-hello" {
		t.Fatalf("content = %q, want ptr-hello", got.Content)
	}
}

func TestFantasyToLLMPointerTextPartNilSafe(t *testing.T) {
	// A typed-nil *TextPart in the content slice must not panic and must not
	// contribute any text.
	var nilText *fantasy.TextPart
	fm := fantasy.Message{
		Role: fantasy.MessageRoleAssistant,
		Content: []fantasy.MessagePart{
			fantasy.TextPart{Text: "before"},
			nilText,
			&fantasy.TextPart{Text: "after"},
		},
	}
	got := fantasyToLLM(fm)
	if got.Content != "beforeafter" {
		t.Fatalf("content = %q, want beforeafter", got.Content)
	}
}

func TestFantasyToLLMPointerToolCallPart(t *testing.T) {
	fm := fantasy.Message{
		Role: fantasy.MessageRoleAssistant,
		Content: []fantasy.MessagePart{
			&fantasy.ToolCallPart{
				ToolCallID: "c-ptr",
				ToolName:   "bash",
				Input:      `{"cmd":"ls"}`,
			},
		},
	}
	got := fantasyToLLM(fm)
	if len(got.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(got.ToolCalls))
	}
	tc := got.ToolCalls[0]
	if tc.ID != "c-ptr" || tc.Type != "function" || tc.Function.Name != "bash" || tc.Function.Arguments != `{"cmd":"ls"}` {
		t.Fatalf("tool call malformed: %+v", tc)
	}
}

func TestFantasyToLLMPointerToolCallPartNilSafe(t *testing.T) {
	var nilCall *fantasy.ToolCallPart
	fm := fantasy.Message{
		Role: fantasy.MessageRoleAssistant,
		Content: []fantasy.MessagePart{
			nilCall,
			&fantasy.ToolCallPart{ToolCallID: "kept", ToolName: "n", Input: "{}"},
		},
	}
	got := fantasyToLLM(fm)
	if len(got.ToolCalls) != 1 || got.ToolCalls[0].ID != "kept" {
		t.Fatalf("nil pointer tool call should be skipped: %+v", got.ToolCalls)
	}
}

func TestFantasyToLLMPointerToolResultPart(t *testing.T) {
	fm := fantasy.Message{
		Role: fantasy.MessageRoleTool,
		Content: []fantasy.MessagePart{
			&fantasy.ToolResultPart{
				ToolCallID: "c-ptr",
				Output:     fantasy.ToolResultOutputContentText{Text: "ptr-result"},
			},
		},
	}
	got := fantasyToLLM(fm)
	if got.Role != "tool" {
		t.Fatalf("role = %q", got.Role)
	}
	if got.ToolCallID != "c-ptr" {
		t.Fatalf("tool_call_id = %q", got.ToolCallID)
	}
	if got.Content != "ptr-result" {
		t.Fatalf("content = %q", got.Content)
	}
}

func TestFantasyToLLMPointerToolResultPartNilSafe(t *testing.T) {
	var nilRes *fantasy.ToolResultPart
	fm := fantasy.Message{
		Role: fantasy.MessageRoleTool,
		Content: []fantasy.MessagePart{
			nilRes,
			&fantasy.ToolResultPart{
				ToolCallID: "ok",
				Output:     fantasy.ToolResultOutputContentText{Text: "body"},
			},
		},
	}
	got := fantasyToLLM(fm)
	if got.ToolCallID != "ok" || got.Content != "body" {
		t.Fatalf("nil pointer result should be skipped: %+v", got)
	}
}

func TestFantasyToLLMPointerToolResultOutputText(t *testing.T) {
	// Inner *ToolResultOutputContentText pointer branch — both inside the
	// value ToolResultPart and inside the *ToolResultPart variants.
	fm := fantasy.Message{
		Role: fantasy.MessageRoleTool,
		Content: []fantasy.MessagePart{
			fantasy.ToolResultPart{
				ToolCallID: "c1",
				Output:     &fantasy.ToolResultOutputContentText{Text: "via-value-part"},
			},
			&fantasy.ToolResultPart{
				ToolCallID: "c2",
				Output:     &fantasy.ToolResultOutputContentText{Text: "via-ptr-part"},
			},
		},
	}
	got := fantasyToLLM(fm)
	if got.ToolCallID != "c1" {
		// First non-empty wins.
		t.Fatalf("tool_call_id = %q, want c1", got.ToolCallID)
	}
	if got.Content != "via-value-partvia-ptr-part" {
		t.Fatalf("content = %q", got.Content)
	}
}

func TestFantasyToLLMPointerToolResultOutputTextNilSafe(t *testing.T) {
	var nilOut *fantasy.ToolResultOutputContentText
	fm := fantasy.Message{
		Role: fantasy.MessageRoleTool,
		Content: []fantasy.MessagePart{
			fantasy.ToolResultPart{ToolCallID: "c1", Output: nilOut},
			&fantasy.ToolResultPart{ToolCallID: "c2", Output: nilOut},
		},
	}
	got := fantasyToLLM(fm)
	// Tool call id still latches from the first part even though Output is nil.
	if got.ToolCallID != "c1" {
		t.Fatalf("tool_call_id = %q, want c1", got.ToolCallID)
	}
	if got.Content != "" {
		t.Fatalf("content should be empty for nil outputs, got %q", got.Content)
	}
}

func TestFantasyToLLMMultiToolCallAssistant(t *testing.T) {
	fm := fantasy.Message{
		Role: fantasy.MessageRoleAssistant,
		Content: []fantasy.MessagePart{
			fantasy.ToolCallPart{ToolCallID: "a", ToolName: "read", Input: `{"path":"1"}`},
			fantasy.ToolCallPart{ToolCallID: "b", ToolName: "read", Input: `{"path":"2"}`},
			&fantasy.ToolCallPart{ToolCallID: "c", ToolName: "write", Input: `{"path":"3"}`},
		},
	}
	got := fantasyToLLM(fm)
	if got.Content != "" {
		t.Fatalf("content should be empty when no text parts, got %q", got.Content)
	}
	if len(got.ToolCalls) != 3 {
		t.Fatalf("expected 3 tool calls, got %d: %+v", len(got.ToolCalls), got.ToolCalls)
	}
	wantIDs := []string{"a", "b", "c"}
	for i, want := range wantIDs {
		if got.ToolCalls[i].ID != want {
			t.Fatalf("tool calls out of order: index %d = %q, want %q (%+v)", i, got.ToolCalls[i].ID, want, got.ToolCalls)
		}
		if got.ToolCalls[i].Type != "function" {
			t.Fatalf("tool call %d Type = %q, want function", i, got.ToolCalls[i].Type)
		}
	}
}

func TestFantasyToLLMMixedTextAndMultipleToolCalls(t *testing.T) {
	// Mixed: text part, tool call, more text, another tool call. All text
	// parts concatenate into Content, all tool calls land in ToolCalls in
	// source order.
	fm := fantasy.Message{
		Role: fantasy.MessageRoleAssistant,
		Content: []fantasy.MessagePart{
			fantasy.TextPart{Text: "thinking..."},
			fantasy.ToolCallPart{ToolCallID: "t1", ToolName: "read", Input: `{}`},
			&fantasy.TextPart{Text: "and also"},
			&fantasy.ToolCallPart{ToolCallID: "t2", ToolName: "bash", Input: `{}`},
		},
	}
	got := fantasyToLLM(fm)
	if got.Content != "thinking...and also" {
		t.Fatalf("content = %q", got.Content)
	}
	if len(got.ToolCalls) != 2 || got.ToolCalls[0].ID != "t1" || got.ToolCalls[1].ID != "t2" {
		t.Fatalf("tool calls = %+v", got.ToolCalls)
	}
}

func TestFantasyToLLMToolCallIDLatchesFirst(t *testing.T) {
	// When multiple ToolResultParts appear, ToolCallID should latch to the
	// first non-empty value and remain stable.
	fm := fantasy.Message{
		Role: fantasy.MessageRoleTool,
		Content: []fantasy.MessagePart{
			fantasy.ToolResultPart{ToolCallID: "first", Output: fantasy.ToolResultOutputContentText{Text: "a"}},
			fantasy.ToolResultPart{ToolCallID: "second", Output: fantasy.ToolResultOutputContentText{Text: "b"}},
			&fantasy.ToolResultPart{ToolCallID: "third", Output: fantasy.ToolResultOutputContentText{Text: "c"}},
		},
	}
	got := fantasyToLLM(fm)
	if got.ToolCallID != "first" {
		t.Fatalf("tool_call_id = %q, want first", got.ToolCallID)
	}
	if got.Content != "abc" {
		t.Fatalf("content = %q, want abc", got.Content)
	}
}

func TestFantasyToLLMUnknownPartTypeIgnored(t *testing.T) {
	// ReasoningPart (and anything else not in the type switch) currently
	// falls through silently. Locking in that behavior.
	fm := fantasy.Message{
		Role: fantasy.MessageRoleAssistant,
		Content: []fantasy.MessagePart{
			fantasy.TextPart{Text: "kept"},
			fantasy.ReasoningPart{Text: "hidden chain-of-thought"},
			fantasy.FilePart{Filename: "x.png", MediaType: "image/png"},
		},
	}
	got := fantasyToLLM(fm)
	if got.Content != "kept" {
		t.Fatalf("content = %q, want only the text part, got reasoning/file leakage", got.Content)
	}
	if len(got.ToolCalls) != 0 {
		t.Fatalf("expected no tool calls, got %+v", got.ToolCalls)
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
