package llm

import (
	"bytes"
	"strings"
	"testing"

	"charm.land/fantasy"
	"charm.land/fantasy/providers/anthropic"
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

// --- LLMToFantasy + round-trip coverage (bt-p3-model-types) ---------------
//
// These tests pin the bt-p3-agent-boundary contract: every llm.Message shape
// the agent currently produces must round-trip through fantasy and back to
// an equivalent llm.Message. The "assistant-tail transcript" case is the
// load-bearing one — interrupted-turn replay depends on it.

func messagesEqual(a, b Message) bool {
	if a.Role != b.Role || a.Content != b.Content || a.ToolCallID != b.ToolCallID {
		return false
	}
	if len(a.ToolCalls) != len(b.ToolCalls) {
		return false
	}
	for i := range a.ToolCalls {
		x, y := a.ToolCalls[i], b.ToolCalls[i]
		if x.ID != y.ID || x.Type != y.Type || x.Function.Name != y.Function.Name || x.Function.Arguments != y.Function.Arguments {
			return false
		}
	}
	return true
}

func roundTripLLM(t *testing.T, name string, in Message) {
	t.Helper()
	got := fantasyToLLM(LLMToFantasy(in))
	if !messagesEqual(in, got) {
		t.Fatalf("%s: round trip mismatch\n in=%+v\nout=%+v", name, in, got)
	}
}

func TestLLMToFantasyRoundTripUserText(t *testing.T) {
	roundTripLLM(t, "user text", Message{Role: "user", Content: "hello world"})
}

func TestLLMToFantasyRoundTripAssistantText(t *testing.T) {
	roundTripLLM(t, "assistant text", Message{Role: "assistant", Content: "sure thing"})
}

func TestLLMToFantasyRoundTripSystem(t *testing.T) {
	roundTripLLM(t, "system", Message{Role: "system", Content: "you are bitchtea"})
}

func TestLLMToFantasyRoundTripAssistantSingleToolCall(t *testing.T) {
	roundTripLLM(t, "assistant single tool call", Message{
		Role: "assistant",
		ToolCalls: []ToolCall{{
			ID: "c1", Type: "function",
			Function: FunctionCall{Name: "read", Arguments: `{"path":"x"}`},
		}},
	})
}

func TestLLMToFantasyRoundTripAssistantMultipleToolCallsOrder(t *testing.T) {
	in := Message{
		Role: "assistant",
		ToolCalls: []ToolCall{
			{ID: "a", Type: "function", Function: FunctionCall{Name: "read", Arguments: `{"path":"1"}`}},
			{ID: "b", Type: "function", Function: FunctionCall{Name: "read", Arguments: `{"path":"2"}`}},
			{ID: "c", Type: "function", Function: FunctionCall{Name: "write", Arguments: `{"path":"3"}`}},
		},
	}
	roundTripLLM(t, "assistant 3 tool calls", in)
	// And confirm order in the intermediate fantasy form.
	fm := LLMToFantasy(in)
	var ids []string
	for _, p := range fm.Content {
		if tc, ok := p.(fantasy.ToolCallPart); ok {
			ids = append(ids, tc.ToolCallID)
		}
	}
	if len(ids) != 3 || ids[0] != "a" || ids[1] != "b" || ids[2] != "c" {
		t.Fatalf("tool call order lost in fantasy form: %v", ids)
	}
}

func TestLLMToFantasyRoundTripAssistantMixedTextAndToolCalls(t *testing.T) {
	in := Message{
		Role:    "assistant",
		Content: "calling now",
		ToolCalls: []ToolCall{
			{ID: "t1", Type: "function", Function: FunctionCall{Name: "read", Arguments: `{}`}},
			{ID: "t2", Type: "function", Function: FunctionCall{Name: "bash", Arguments: `{"cmd":"ls"}`}},
		},
	}
	roundTripLLM(t, "assistant mixed", in)

	// Intermediate must place the text part before the tool calls so providers
	// see "thinking..." preceding the calls.
	fm := LLMToFantasy(in)
	if len(fm.Content) != 3 {
		t.Fatalf("expected 3 parts (text + 2 tool calls), got %d: %+v", len(fm.Content), fm.Content)
	}
	if _, ok := fm.Content[0].(fantasy.TextPart); !ok {
		t.Fatalf("first part should be TextPart, got %T", fm.Content[0])
	}
}

func TestLLMToFantasyRoundTripToolResult(t *testing.T) {
	roundTripLLM(t, "tool result", Message{
		Role:       "tool",
		ToolCallID: "c1",
		Content:    "file body line 1\nfile body line 2",
	})
}

func TestLLMToFantasyRoundTripToolResultEmptyBody(t *testing.T) {
	// An empty-body tool result still needs the ToolCallID to survive the
	// round trip — the agent uses it to correlate calls and results.
	roundTripLLM(t, "tool result empty", Message{
		Role:       "tool",
		ToolCallID: "c-empty",
		Content:    "",
	})
}

func TestLLMToFantasyRoundTripAssistantTailTranscript(t *testing.T) {
	// The interrupted-turn shape: a transcript that ends with an assistant
	// message whose tool calls have NOT yet been answered. Resume must
	// preserve this verbatim — every message including the dangling tool
	// calls survives a fantasy round trip with order intact.
	in := []Message{
		{Role: "system", Content: "you are bitchtea"},
		{Role: "user", Content: "read foo and bar"},
		{
			Role:    "assistant",
			Content: "reading",
			ToolCalls: []ToolCall{
				{ID: "call_foo", Type: "function", Function: FunctionCall{Name: "read", Arguments: `{"path":"foo"}`}},
			},
		},
		{Role: "tool", ToolCallID: "call_foo", Content: "FOO BODY"},
		{
			// Trailing assistant turn with two NEW tool calls and NO
			// matching tool results — the "interrupted" shape.
			Role: "assistant",
			ToolCalls: []ToolCall{
				{ID: "call_bar", Type: "function", Function: FunctionCall{Name: "read", Arguments: `{"path":"bar"}`}},
				{ID: "call_baz", Type: "function", Function: FunctionCall{Name: "read", Arguments: `{"path":"baz"}`}},
			},
		},
	}

	fms := LLMSliceToFantasy(in)
	if len(fms) != len(in) {
		t.Fatalf("slice length changed: got %d, want %d", len(fms), len(in))
	}
	got := FantasySliceToLLM(fms)
	if len(got) != len(in) {
		t.Fatalf("round-trip length changed: got %d, want %d", len(got), len(in))
	}
	for i := range in {
		if !messagesEqual(in[i], got[i]) {
			t.Fatalf("assistant-tail round trip mismatch at %d:\n in=%+v\nout=%+v", i, in[i], got[i])
		}
	}

	// And the trailing assistant message specifically preserves both
	// dangling tool calls in source order (this is what resume depends on).
	tail := got[len(got)-1]
	if tail.Role != "assistant" {
		t.Fatalf("tail role = %q, want assistant", tail.Role)
	}
	if len(tail.ToolCalls) != 2 || tail.ToolCalls[0].ID != "call_bar" || tail.ToolCalls[1].ID != "call_baz" {
		t.Fatalf("dangling tool calls mangled: %+v", tail.ToolCalls)
	}
}

// --- requested conversion fidelity coverage (bt-test.5) -------------------

func TestFantasyToLLMRoundTrip(t *testing.T) {
	in := []fantasy.Message{
		{
			Role:    fantasy.MessageRoleSystem,
			Content: []fantasy.MessagePart{fantasy.TextPart{Text: "system bytes\nwith newline"}},
		},
		{
			Role:    fantasy.MessageRoleUser,
			Content: []fantasy.MessagePart{fantasy.TextPart{Text: "user asks for \x00 exact bytes"}},
		},
		{
			Role: fantasy.MessageRoleAssistant,
			Content: []fantasy.MessagePart{
				fantasy.TextPart{Text: "calling tools"},
				fantasy.ToolCallPart{ToolCallID: "call_1", ToolName: "read", Input: `{"path":"a.txt"}`},
				fantasy.ToolCallPart{ToolCallID: "call_2", ToolName: "bash", Input: `{"cmd":"printf hi"}`},
			},
		},
		{
			Role: fantasy.MessageRoleTool,
			Content: []fantasy.MessagePart{fantasy.ToolResultPart{
				ToolCallID: "call_1",
				Output:     fantasy.ToolResultOutputContentText{Text: "tool bytes\nline 2"},
			}},
		},
	}

	llmMsgs := FantasySliceToLLM(in)
	roundTripped := LLMSliceToFantasy(llmMsgs)
	got := FantasySliceToLLM(roundTripped)

	if len(got) != len(llmMsgs) {
		t.Fatalf("round-trip length = %d, want %d", len(got), len(llmMsgs))
	}
	for i := range llmMsgs {
		if !messageBytesEqual(llmMsgs[i], got[i]) {
			t.Fatalf("message %d round-trip mismatch\nin=%+v\nout=%+v", i, llmMsgs[i], got[i])
		}
	}
}

func TestConvertPreservesToolResponseContent(t *testing.T) {
	want := "stdout line\nstderr-like bytes:\x00\x01\x02\nunicode snowman: ☃"
	in := fantasy.Message{
		Role: fantasy.MessageRoleTool,
		Content: []fantasy.MessagePart{fantasy.ToolResultPart{
			ToolCallID: "tool_123",
			Output:     fantasy.ToolResultOutputContentText{Text: want},
		}},
	}

	llmMsg := fantasyToLLM(in)
	got := fantasyToLLM(LLMToFantasy(llmMsg))

	if got.ToolCallID != "tool_123" {
		t.Fatalf("tool_call_id = %q, want tool_123", got.ToolCallID)
	}
	if !bytes.Equal([]byte(got.Content), []byte(want)) {
		t.Fatalf("tool response bytes changed\nwant=%q\ngot =%q", want, got.Content)
	}
}

func TestConvertHandlesMultipleAssistantToolCalls(t *testing.T) {
	in := Message{
		Role:    "assistant",
		Content: "fan out",
		ToolCalls: []ToolCall{
			{ID: "call_a", Type: "function", Function: FunctionCall{Name: "read", Arguments: `{"path":"a"}`}},
			{ID: "call_b", Type: "function", Function: FunctionCall{Name: "write", Arguments: `{"path":"b","content":"B"}`}},
			{ID: "call_c", Type: "function", Function: FunctionCall{Name: "bash", Arguments: `{"cmd":"pwd"}`}},
		},
	}

	got := fantasyToLLM(LLMToFantasy(in))
	if !messageBytesEqual(in, got) {
		t.Fatalf("assistant multi-tool round-trip mismatch\nin=%+v\nout=%+v", in, got)
	}
	for i, want := range []string{"call_a", "call_b", "call_c"} {
		if got.ToolCalls[i].ID != want {
			t.Fatalf("tool call order changed at %d: got %q, want %q (%+v)", i, got.ToolCalls[i].ID, want, got.ToolCalls)
		}
	}
}

func TestConvertSystemMessageWithProviderBlocks(t *testing.T) {
	sendReasoning := true
	providerOpts := fantasy.ProviderOptions{
		anthropic.Name: &anthropic.ProviderOptions{SendReasoning: &sendReasoning},
	}
	in := fantasy.Message{
		Role:            fantasy.MessageRoleSystem,
		ProviderOptions: providerOpts,
		Content: []fantasy.MessagePart{fantasy.TextPart{
			Text:            "system prompt with provider cache metadata",
			ProviderOptions: providerOpts,
		}},
	}

	llmMsg := fantasyToLLM(in)
	if llmMsg.Role != "system" {
		t.Fatalf("role = %q, want system", llmMsg.Role)
	}
	if !bytes.Equal([]byte(llmMsg.Content), []byte("system prompt with provider cache metadata")) {
		t.Fatalf("system content changed: %q", llmMsg.Content)
	}

	got := LLMToFantasy(llmMsg)
	if len(got.ProviderOptions) != 0 {
		t.Fatalf("legacy round-trip should drop message ProviderOptions, got %+v", got.ProviderOptions)
	}
	if len(got.Content) != 1 {
		t.Fatalf("round-tripped system content len = %d, want 1", len(got.Content))
	}
	text, ok := got.Content[0].(fantasy.TextPart)
	if !ok {
		t.Fatalf("round-tripped system part = %T, want TextPart", got.Content[0])
	}
	if !bytes.Equal([]byte(text.Text), []byte("system prompt with provider cache metadata")) {
		t.Fatalf("system text changed: %q", text.Text)
	}
	if len(text.ProviderOptions) != 0 {
		t.Fatalf("legacy round-trip should drop text ProviderOptions, got %+v", text.ProviderOptions)
	}
}

func TestConvertLossyDetection(t *testing.T) {
	sendReasoning := true
	providerOpts := fantasy.ProviderOptions{
		anthropic.Name: &anthropic.ProviderOptions{SendReasoning: &sendReasoning},
	}

	t.Run("assistant drops non-legacy fantasy fields", func(t *testing.T) {
		in := fantasy.Message{
			Role:            fantasy.MessageRoleAssistant,
			ProviderOptions: providerOpts,
			Content: []fantasy.MessagePart{
				fantasy.TextPart{Text: "visible", ProviderOptions: providerOpts},
				fantasy.ReasoningPart{Text: "reasoning dropped", ProviderOptions: providerOpts},
				fantasy.FilePart{Filename: "image.png", Data: []byte{1, 2, 3}, MediaType: "image/png", ProviderOptions: providerOpts},
				fantasy.ToolCallPart{
					ToolCallID:       "call_lossy",
					ToolName:         "read",
					Input:            `{"path":"x"}`,
					ProviderExecuted: true,
					ProviderOptions:  providerOpts,
				},
			},
		}

		got := LLMToFantasy(fantasyToLLM(in))
		if len(got.ProviderOptions) != 0 {
			t.Fatalf("message ProviderOptions survived unexpectedly: %+v", got.ProviderOptions)
		}
		if len(got.Content) != 2 {
			t.Fatalf("round-tripped assistant parts = %d, want text + tool call: %+v", len(got.Content), got.Content)
		}
		if text, ok := got.Content[0].(fantasy.TextPart); !ok || text.Text != "visible" || len(text.ProviderOptions) != 0 {
			t.Fatalf("text part mismatch after lossy round trip: %#v", got.Content[0])
		}
		call, ok := got.Content[1].(fantasy.ToolCallPart)
		if !ok {
			t.Fatalf("second part = %T, want ToolCallPart", got.Content[1])
		}
		if call.ToolCallID != "call_lossy" || call.ToolName != "read" || call.Input != `{"path":"x"}` {
			t.Fatalf("tool call core fields changed: %+v", call)
		}
		if call.ProviderExecuted || len(call.ProviderOptions) != 0 {
			t.Fatalf("tool call provider-only fields should drop: %+v", call)
		}
		t.Log("lossy by design: ProviderOptions, ReasoningPart, FilePart, and ToolCallPart ProviderExecuted/provider options are not representable in llm.Message")
	})

	t.Run("tool result drops non-text output fields", func(t *testing.T) {
		in := fantasy.Message{
			Role: fantasy.MessageRoleTool,
			Content: []fantasy.MessagePart{fantasy.ToolResultPart{
				ToolCallID:       "media_result",
				ProviderExecuted: true,
				ProviderOptions:  providerOpts,
				Output: fantasy.ToolResultOutputContentMedia{
					Data:      "aGVsbG8=",
					MediaType: "image/png",
					Text:      "caption",
				},
			}},
		}

		got := LLMToFantasy(fantasyToLLM(in))
		if len(got.Content) != 1 {
			t.Fatalf("round-tripped tool parts = %d, want 1", len(got.Content))
		}
		part, ok := got.Content[0].(fantasy.ToolResultPart)
		if !ok {
			t.Fatalf("tool result part = %T, want ToolResultPart", got.Content[0])
		}
		if part.ToolCallID != "media_result" {
			t.Fatalf("tool_call_id = %q, want media_result", part.ToolCallID)
		}
		text, ok := part.Output.(fantasy.ToolResultOutputContentText)
		if !ok {
			t.Fatalf("output = %T, want ToolResultOutputContentText", part.Output)
		}
		if text.Text != "" {
			t.Fatalf("media output text = %q, want empty legacy text", text.Text)
		}
		if part.ProviderExecuted || len(part.ProviderOptions) != 0 {
			t.Fatalf("tool result provider-only fields should drop: %+v", part)
		}
		t.Log("lossy by design: media/error tool outputs collapse to legacy text output; provider execution metadata and ProviderOptions drop")
	})
}

func messageBytesEqual(a, b Message) bool {
	if a.Role != b.Role || a.ToolCallID != b.ToolCallID {
		return false
	}
	if !bytes.Equal([]byte(a.Content), []byte(b.Content)) {
		return false
	}
	if len(a.ToolCalls) != len(b.ToolCalls) {
		return false
	}
	for i := range a.ToolCalls {
		x, y := a.ToolCalls[i], b.ToolCalls[i]
		if x.ID != y.ID || x.Type != y.Type || x.Function.Name != y.Function.Name {
			return false
		}
		if !bytes.Equal([]byte(x.Function.Arguments), []byte(y.Function.Arguments)) {
			return false
		}
	}
	return true
}

func TestLLMToFantasyEmptyAssistantNoContentNoToolCalls(t *testing.T) {
	// An assistant message with neither text nor tool calls (rare but legal:
	// e.g. a model emitted only reasoning, which fantasyToLLM dropped) must
	// not panic and must round-trip cleanly.
	in := Message{Role: "assistant"}
	fm := LLMToFantasy(in)
	if fm.Role != fantasy.MessageRoleAssistant {
		t.Fatalf("role = %v, want assistant", fm.Role)
	}
	if len(fm.Content) != 0 {
		t.Fatalf("expected empty content, got %+v", fm.Content)
	}
	roundTripLLM(t, "empty assistant", in)
}

func TestLLMToFantasyEmptyUserNoPanic(t *testing.T) {
	in := Message{Role: "user"}
	fm := LLMToFantasy(in)
	if fm.Role != fantasy.MessageRoleUser {
		t.Fatalf("role = %v, want user", fm.Role)
	}
	// Empty-content user message has no parts — caller must handle that.
	if len(fm.Content) != 0 {
		t.Fatalf("expected empty content, got %+v", fm.Content)
	}
	roundTripLLM(t, "empty user", in)
}

func TestLLMToFantasyNilToolCallsClean(t *testing.T) {
	// Explicit nil ToolCalls must produce a single TextPart and round-trip
	// to a message with nil ToolCalls (not a zero-length slice that compares
	// unequal under reflect.DeepEqual).
	in := Message{Role: "assistant", Content: "just text", ToolCalls: nil}
	got := fantasyToLLM(LLMToFantasy(in))
	if got.Content != "just text" {
		t.Fatalf("content = %q, want just text", got.Content)
	}
	if len(got.ToolCalls) != 0 {
		t.Fatalf("expected no tool calls, got %+v", got.ToolCalls)
	}
}

func TestLLMSliceToFantasyNilPreservesNil(t *testing.T) {
	if got := LLMSliceToFantasy(nil); got != nil {
		t.Fatalf("nil input should yield nil slice, got %+v", got)
	}
	if got := FantasySliceToLLM(nil); got != nil {
		t.Fatalf("nil input should yield nil slice, got %+v", got)
	}
}

func TestLLMSliceToFantasyEmptyIsEmptySlice(t *testing.T) {
	got := LLMSliceToFantasy([]Message{})
	if got == nil {
		t.Fatalf("non-nil empty input should yield non-nil empty slice")
	}
	if len(got) != 0 {
		t.Fatalf("expected empty slice, got %d", len(got))
	}
}

func TestLLMToFantasyProviderOptionsLeftEmpty(t *testing.T) {
	// Per the design doc: legacy llm.Message has no provider-specific data,
	// so LLMToFantasy must NOT invent ProviderOptions. Lock that in.
	in := Message{Role: "assistant", Content: "hi"}
	fm := LLMToFantasy(in)
	if len(fm.ProviderOptions) != 0 {
		t.Fatalf("ProviderOptions should be empty for lifted legacy messages, got %+v", fm.ProviderOptions)
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
