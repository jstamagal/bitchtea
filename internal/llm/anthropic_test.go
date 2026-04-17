package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestStreamChatAnthropicText(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/messages" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("x-api-key") != "test-key" {
			t.Fatalf("bad api key header: %s", r.Header.Get("x-api-key"))
		}
		if r.Header.Get("anthropic-version") != "2023-06-01" {
			t.Fatalf("bad anthropic-version header: %s", r.Header.Get("anthropic-version"))
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)

		events := []string{
			`{"type":"message_start","message":{"id":"msg_01","type":"message","role":"assistant","model":"claude-sonnet-4-20250514"}}`,
			`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
			`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hello "}}`,
			`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"world"}}`,
			`{"type":"content_block_stop","index":0}`,
			`{"type":"message_stop"}`,
		}

		flusher := w.(http.Flusher)
		for _, ev := range events {
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", "message_start", ev) // event type doesn't matter, we parse from data
			flusher.Flush()
		}
	}))
	defer server.Close()

	client := NewClient("test-key", server.URL, "claude-sonnet-4-20250514", "anthropic")
	events := make(chan StreamEvent, 100)

	go client.StreamChat(context.Background(), []Message{
		{Role: "system", Content: "You are helpful"},
		{Role: "user", Content: "hi"},
	}, nil, events)

	var text string
	var gotDone bool
	for ev := range events {
		switch ev.Type {
		case "text":
			text += ev.Text
		case "done":
			gotDone = true
		case "error":
			t.Fatalf("unexpected error: %v", ev.Error)
		}
	}

	if text != "hello world" {
		t.Fatalf("expected 'hello world', got %q", text)
	}
	if !gotDone {
		t.Fatal("did not receive done event")
	}
}

func TestStreamChatAnthropicConcatenatesMultipleSystemMessages(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req anthropicRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req.System != "first instruction\n\nsecond instruction" {
			t.Fatalf("unexpected system prompt: %q", req.System)
		}
		if len(req.Messages) != 1 || req.Messages[0].Role != "user" {
			t.Fatalf("unexpected non-system messages: %#v", req.Messages)
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		fmt.Fprint(w, "data: {\"type\":\"message_stop\"}\n\n")
	}))
	defer server.Close()

	client := NewClient("test-key", server.URL, "claude-sonnet-4-20250514", "anthropic")
	events := make(chan StreamEvent, 100)

	go client.StreamChat(context.Background(), []Message{
		{Role: "system", Content: "first instruction"},
		{Role: "system", Content: "second instruction"},
		{Role: "user", Content: "hi"},
	}, nil, events)

	for ev := range events {
		if ev.Type == "error" {
			t.Fatalf("unexpected error: %v", ev.Error)
		}
	}
}

func TestStreamChatAnthropicSkipsWhitespaceOnlySystemMessages(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req anthropicRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req.System != "kept instruction" {
			t.Fatalf("unexpected system prompt with whitespace-only system messages present: %q", req.System)
		}
		if len(req.Messages) != 1 || req.Messages[0].Role != "user" {
			t.Fatalf("unexpected non-system messages: %#v", req.Messages)
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		fmt.Fprint(w, "data: {\"type\":\"message_stop\"}\n\n")
	}))
	defer server.Close()

	client := NewClient("test-key", server.URL, "claude-sonnet-4-20250514", "anthropic")
	events := make(chan StreamEvent, 100)

	go client.StreamChat(context.Background(), []Message{
		{Role: "system", Content: " \n\t "},
		{Role: "system", Content: "kept instruction"},
		{Role: "system", Content: "   "},
		{Role: "user", Content: "hi"},
	}, nil, events)

	for ev := range events {
		if ev.Type == "error" {
			t.Fatalf("unexpected error: %v", ev.Error)
		}
	}
}

func TestStreamChatAnthropicToolCall(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)

		events := []string{
			`{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_01","name":"read","input":{}}}`,
			`{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"path\":"}}`,
			`{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"\"test.txt\"}"}}`,
			`{"type":"content_block_stop","index":0}`,
			`{"type":"message_stop"}`,
		}

		flusher := w.(http.Flusher)
		for _, ev := range events {
			fmt.Fprintf(w, "data: %s\n\n", ev)
			flusher.Flush()
		}
	}))
	defer server.Close()

	client := NewClient("test-key", server.URL, "claude-sonnet-4-20250514", "anthropic")
	events := make(chan StreamEvent, 100)

	go client.StreamChat(context.Background(), []Message{
		{Role: "user", Content: "read a file"},
	}, nil, events)

	var toolCalls []StreamEvent
	for ev := range events {
		if ev.Type == "tool_call" {
			toolCalls = append(toolCalls, ev)
		}
	}

	if len(toolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(toolCalls))
	}

	tc := toolCalls[0]
	if tc.ToolCallID != "toolu_01" {
		t.Fatalf("wrong tool call ID: %s", tc.ToolCallID)
	}
	if tc.ToolName != "read" {
		t.Fatalf("wrong tool name: %s", tc.ToolName)
	}
	if tc.ToolArgs != `{"path":"test.txt"}` {
		t.Fatalf("wrong tool args: %s", tc.ToolArgs)
	}
}

func TestStreamChatAnthropicToolCallUsesStartEventInputWhenNoDeltaArrives(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)

		events := []string{
			`{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_02","name":"read","input":{"path":"test.txt","limit":2}}}`,
			`{"type":"content_block_stop","index":0}`,
			`{"type":"message_stop"}`,
		}

		flusher := w.(http.Flusher)
		for _, ev := range events {
			fmt.Fprintf(w, "data: %s\n\n", ev)
			flusher.Flush()
		}
	}))
	defer server.Close()

	client := NewClient("test-key", server.URL, "claude-sonnet-4-20250514", "anthropic")
	events := make(chan StreamEvent, 100)

	go client.StreamChat(context.Background(), []Message{
		{Role: "user", Content: "read a file"},
	}, nil, events)

	var toolCalls []StreamEvent
	for ev := range events {
		if ev.Type == "tool_call" {
			toolCalls = append(toolCalls, ev)
		}
	}

	if len(toolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(toolCalls))
	}

	tc := toolCalls[0]
	if tc.ToolCallID != "toolu_02" {
		t.Fatalf("wrong tool call ID: %s", tc.ToolCallID)
	}
	if tc.ToolName != "read" {
		t.Fatalf("wrong tool name: %s", tc.ToolName)
	}
	if tc.ToolArgs != `{"path":"test.txt","limit":2}` {
		t.Fatalf("wrong tool args from start event input: %s", tc.ToolArgs)
	}
}

func TestStreamChatAnthropicInterleavesTextAndToolUseEvents(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)

		events := []string{
			`{"type":"message_start","message":{"usage":{"input_tokens":12,"output_tokens":0}}}`,
			`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":"checking "}}`,
			`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"files"}}`,
			`{"type":"content_block_stop","index":0}`,
			`{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"toolu_01","name":"read","input":{}}}`,
			`{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"path\":"}}`,
			`{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"\"test.txt\"}"}}`,
			`{"type":"content_block_stop","index":1}`,
			`{"type":"content_block_start","index":2,"content_block":{"type":"text","text":" then "}}`,
			`{"type":"content_block_delta","index":2,"delta":{"type":"text_delta","text":"summarize"}}`,
			`{"type":"content_block_stop","index":2}`,
			`{"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":9}}`,
			`{"type":"message_stop"}`,
		}

		flusher := w.(http.Flusher)
		for _, ev := range events {
			fmt.Fprintf(w, "data: %s\n\n", ev)
			flusher.Flush()
		}
	}))
	defer server.Close()

	client := NewClient("test-key", server.URL, "claude-sonnet-4-20250514", "anthropic")
	events := make(chan StreamEvent, 100)

	go client.StreamChat(context.Background(), []Message{
		{Role: "user", Content: "inspect then summarize"},
	}, nil, events)

	var got []StreamEvent
	for ev := range events {
		if ev.Type == "error" {
			t.Fatalf("unexpected error: %v", ev.Error)
		}
		got = append(got, ev)
	}

	if len(got) != 7 {
		t.Fatalf("expected 7 events, got %d: %#v", len(got), got)
	}
	if got[0].Type != "text" || got[0].Text != "checking " {
		t.Fatalf("unexpected first event: %#v", got[0])
	}
	if got[1].Type != "text" || got[1].Text != "files" {
		t.Fatalf("unexpected second event: %#v", got[1])
	}
	if got[2].Type != "tool_call" || got[2].ToolCallID != "toolu_01" || got[2].ToolName != "read" || got[2].ToolArgs != `{"path":"test.txt"}` {
		t.Fatalf("unexpected tool event: %#v", got[2])
	}
	if got[3].Type != "text" || got[3].Text != " then " {
		t.Fatalf("unexpected fourth event: %#v", got[3])
	}
	if got[4].Type != "text" || got[4].Text != "summarize" {
		t.Fatalf("unexpected fifth event: %#v", got[4])
	}
	if got[5].Type != "usage" || got[5].Usage == nil || got[5].Usage.InputTokens != 12 || got[5].Usage.OutputTokens != 9 {
		t.Fatalf("unexpected usage event: %#v", got[5])
	}
	if got[6].Type != "done" {
		t.Fatalf("unexpected final event: %#v", got[6])
	}
}

func TestStreamChatAnthropicParsesMultilineSSEDataFrames(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)

		flusher := w.(http.Flusher)
		fmt.Fprint(w, "data: {\"type\":\"content_block_delta\",\"index\":0,\n")
		fmt.Fprint(w, "data: \"delta\":{\"type\":\"text_delta\",\"text\":\"hello\"}}\n\n")
		flusher.Flush()
		fmt.Fprint(w, "data: {\"type\":\"message_stop\"}\n\n")
		flusher.Flush()
	}))
	defer server.Close()

	client := NewClient("test-key", server.URL, "claude-sonnet-4-20250514", "anthropic")
	events := make(chan StreamEvent, 100)

	go client.StreamChat(context.Background(), []Message{
		{Role: "user", Content: "hi"},
	}, nil, events)

	var got []StreamEvent
	for ev := range events {
		if ev.Type == "error" {
			t.Fatalf("unexpected error: %v", ev.Error)
		}
		got = append(got, ev)
	}

	if len(got) != 2 {
		t.Fatalf("expected text and done events, got %d: %#v", len(got), got)
	}
	if got[0].Type != "text" || got[0].Text != "hello" {
		t.Fatalf("unexpected first event: %#v", got[0])
	}
	if got[1].Type != "done" {
		t.Fatalf("unexpected final event: %#v", got[1])
	}
}

func TestStreamChatAnthropicEmitsThinkingEvents(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)

		events := []string{
			`{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":"plan: "}}`,
			`{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"inspect file"}}`,
			`{"type":"content_block_stop","index":0}`,
			`{"type":"message_stop"}`,
		}

		flusher := w.(http.Flusher)
		for _, ev := range events {
			fmt.Fprintf(w, "data: %s\n\n", ev)
			flusher.Flush()
		}
	}))
	defer server.Close()

	client := NewClient("test-key", server.URL, "claude-sonnet-4-20250514", "anthropic")
	events := make(chan StreamEvent, 100)

	go client.StreamChat(context.Background(), []Message{
		{Role: "user", Content: "think first"},
	}, nil, events)

	var got []StreamEvent
	for ev := range events {
		if ev.Type == "error" {
			t.Fatalf("unexpected error: %v", ev.Error)
		}
		got = append(got, ev)
	}

	if len(got) != 3 {
		t.Fatalf("expected thinking, thinking, done events, got %d: %#v", len(got), got)
	}
	if got[0].Type != "thinking" || got[0].Text != "plan: " {
		t.Fatalf("unexpected first event: %#v", got[0])
	}
	if got[1].Type != "thinking" || got[1].Text != "inspect file" {
		t.Fatalf("unexpected second event: %#v", got[1])
	}
	if got[2].Type != "done" {
		t.Fatalf("unexpected final event: %#v", got[2])
	}
}

func TestStreamChatAnthropicUsage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)

		events := []string{
			`{"type":"message_start","message":{"usage":{"input_tokens":210,"output_tokens":0}}}`,
			`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
			`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hello"}}`,
			`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":34}}`,
			`{"type":"message_stop"}`,
		}

		flusher := w.(http.Flusher)
		for _, ev := range events {
			fmt.Fprintf(w, "data: %s\n\n", ev)
			flusher.Flush()
		}
	}))
	defer server.Close()

	client := NewClient("test-key", server.URL, "claude-sonnet-4-20250514", "anthropic")
	events := make(chan StreamEvent, 100)

	go client.StreamChat(context.Background(), []Message{
		{Role: "user", Content: "hi"},
	}, nil, events)

	var usage *TokenUsage
	for ev := range events {
		switch ev.Type {
		case "usage":
			usage = ev.Usage
		case "error":
			t.Fatalf("unexpected error: %v", ev.Error)
		}
	}

	if usage == nil {
		t.Fatal("expected usage event")
	}
	if usage.InputTokens != 210 {
		t.Fatalf("expected 210 input tokens, got %d", usage.InputTokens)
	}
	if usage.OutputTokens != 34 {
		t.Fatalf("expected 34 output tokens, got %d", usage.OutputTokens)
	}
}

func TestStreamChatAnthropicAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(429)
		fmt.Fprint(w, `{"error":{"type":"rate_limit_error","message":"Rate limited"}}`)
	}))
	defer server.Close()

	client := NewClient("test-key", server.URL, "claude-sonnet-4-20250514", "anthropic")
	events := make(chan StreamEvent, 100)

	go client.StreamChat(context.Background(), []Message{
		{Role: "user", Content: "hi"},
	}, nil, events)

	var gotError bool
	for ev := range events {
		if ev.Type == "error" {
			gotError = true
		}
	}

	if !gotError {
		t.Fatal("expected error event for 429 response")
	}
}

func TestStreamChatAnthropic404SuggestsOpenAIProvider(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
		fmt.Fprint(w, `{"error":{"type":"not_found","message":"Endpoint not supported: POST /messages"}}`)
	}))
	defer server.Close()

	client := NewClient("test-key", server.URL, "claude-sonnet-4-20250514", "anthropic")
	events := make(chan StreamEvent, 100)

	go client.StreamChat(context.Background(), []Message{
		{Role: "user", Content: "hi"},
	}, nil, events)

	var gotError error
	for ev := range events {
		if ev.Type == "error" {
			gotError = ev.Error
		}
	}

	if gotError == nil {
		t.Fatal("expected 404 error")
	}
	for _, want := range []string{"API 404", "Try /provider openai", "/messages"} {
		if !strings.Contains(gotError.Error(), want) {
			t.Fatalf("expected %q in error, got %v", want, gotError)
		}
	}
}

func TestEnsureAlternating(t *testing.T) {
	msgs := []anthropicMessage{
		{Role: "user", Content: []interface{}{anthropicTextBlock{Type: "text", Text: "a"}}},
		{Role: "user", Content: []interface{}{anthropicTextBlock{Type: "text", Text: "b"}}},
		{Role: "assistant", Content: []interface{}{anthropicTextBlock{Type: "text", Text: "c"}}},
	}

	result := ensureAlternating(msgs)
	if len(result) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(result))
	}
	if result[0].Role != "user" {
		t.Fatalf("first message role: %s", result[0].Role)
	}
	if len(result[0].Content) != 2 {
		t.Fatalf("first message should have 2 content blocks, got %d", len(result[0].Content))
	}
}

func TestEnsureAlternatingEmpty(t *testing.T) {
	result := ensureAlternating(nil)
	if result == nil {
		t.Fatal("expected empty slice for nil input")
	}
	if len(result) != 0 {
		t.Fatalf("expected empty slice for nil input, got %d", len(result))
	}

	result = ensureAlternating([]anthropicMessage{})
	if len(result) != 0 {
		t.Fatalf("expected empty slice, got %d", len(result))
	}
}

func TestEnsureAlternatingSingle(t *testing.T) {
	msgs := []anthropicMessage{
		{Role: "user", Content: []interface{}{anthropicTextBlock{Type: "text", Text: "single"}}},
	}

	result := ensureAlternating(msgs)
	if len(result) != 1 {
		t.Fatalf("expected 1 message, got %d", len(result))
	}
	if result[0].Role != "user" {
		t.Fatalf("expected user role, got %s", result[0].Role)
	}
}

func TestEnsureAlternatingThreeSameRole(t *testing.T) {
	msgs := []anthropicMessage{
		{Role: "user", Content: []interface{}{anthropicTextBlock{Type: "text", Text: "a"}}},
		{Role: "user", Content: []interface{}{anthropicTextBlock{Type: "text", Text: "b"}}},
		{Role: "user", Content: []interface{}{anthropicTextBlock{Type: "text", Text: "c"}}},
	}

	result := ensureAlternating(msgs)
	if len(result) != 1 {
		t.Fatalf("expected 1 merged message, got %d", len(result))
	}
	if len(result[0].Content) != 3 {
		t.Fatalf("expected 3 content blocks, got %d", len(result[0].Content))
	}
}

func TestEnsureAlternatingAlreadyAlternating(t *testing.T) {
	msgs := []anthropicMessage{
		{Role: "user", Content: []interface{}{anthropicTextBlock{Type: "text", Text: "q"}}},
		{Role: "assistant", Content: []interface{}{anthropicTextBlock{Type: "text", Text: "a1"}}},
		{Role: "user", Content: []interface{}{anthropicTextBlock{Type: "text", Text: "q2"}}},
		{Role: "assistant", Content: []interface{}{anthropicTextBlock{Type: "text", Text: "a2"}}},
	}

	result := ensureAlternating(msgs)
	if len(result) != 4 {
		t.Fatalf("expected 4 messages (already alternating), got %d", len(result))
	}
}

func TestEnsureAlternatingSanitizesNilContentBlocks(t *testing.T) {
	msgs := []anthropicMessage{
		{Role: "user", Content: nil},
		{Role: "user", Content: []interface{}{nil, anthropicTextBlock{Type: "text", Text: "ok"}}},
		{Role: "assistant", Content: []interface{}{nil}},
	}

	result := ensureAlternating(msgs)
	if len(result) != 1 {
		t.Fatalf("expected empty assistant message to be dropped, got %d messages", len(result))
	}
	if result[0].Content == nil {
		t.Fatal("expected sanitized user content to be non-nil")
	}
	if len(result[0].Content) != 1 {
		t.Fatalf("expected nil blocks removed from user content, got %d blocks", len(result[0].Content))
	}
	if block, ok := result[0].Content[0].(anthropicTextBlock); !ok || block.Text != "ok" {
		t.Fatalf("unexpected remaining user block: %#v", result[0].Content[0])
	}
}

func TestEnsureAlternatingDropsEmptyMessagesBeforeMerge(t *testing.T) {
	msgs := []anthropicMessage{
		{Role: "user", Content: []interface{}{anthropicTextBlock{Type: "text", Text: "a"}}},
		{Role: "assistant", Content: []interface{}{nil}},
		{Role: "user", Content: []interface{}{anthropicTextBlock{Type: "text", Text: "b"}}},
	}

	result := ensureAlternating(msgs)
	if len(result) != 1 {
		t.Fatalf("expected empty middle message to be dropped and users to merge, got %d messages", len(result))
	}
	if result[0].Role != "user" {
		t.Fatalf("expected merged user message, got %s", result[0].Role)
	}
	if len(result[0].Content) != 2 {
		t.Fatalf("expected 2 merged user blocks, got %d", len(result[0].Content))
	}
}

func TestEnsureAlternatingDropsWhitespaceOnlyTextBlocks(t *testing.T) {
	msgs := []anthropicMessage{
		{
			Role: "user",
			Content: []interface{}{
				anthropicTextBlock{Type: "text", Text: " \n\t "},
				anthropicToolResultBlock{Type: "tool_result", ToolUseID: "call_1", Content: "alpha"},
				anthropicTextBlock{Type: "text", Text: "follow up"},
			},
		},
		{
			Role: "assistant",
			Content: []interface{}{
				anthropicTextBlock{Type: "text", Text: "   "},
				anthropicToolUseBlock{Type: "tool_use", ID: "call_2", Name: "read", Input: map[string]interface{}{"path": "b.txt"}},
			},
		},
	}

	result := ensureAlternating(msgs)
	if len(result) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(result))
	}
	if len(result[0].Content) != 2 {
		t.Fatalf("expected whitespace-only user text to be dropped, got %d blocks", len(result[0].Content))
	}
	if block, ok := result[0].Content[0].(anthropicToolResultBlock); !ok || block.ToolUseID != "call_1" {
		t.Fatalf("unexpected first user block: %#v", result[0].Content[0])
	}
	if block, ok := result[0].Content[1].(anthropicTextBlock); !ok || block.Text != "follow up" {
		t.Fatalf("unexpected second user block: %#v", result[0].Content[1])
	}
	if len(result[1].Content) != 1 {
		t.Fatalf("expected whitespace-only assistant text to be dropped, got %d blocks", len(result[1].Content))
	}
	if block, ok := result[1].Content[0].(anthropicToolUseBlock); !ok || block.ID != "call_2" {
		t.Fatalf("unexpected assistant block: %#v", result[1].Content[0])
	}
}

func TestStreamChatAnthropicEncodesToolOnlyAssistantWithoutNullContent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req anthropicRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if len(req.Messages) != 2 {
			t.Fatalf("expected 2 anthropic messages, got %d", len(req.Messages))
		}
		if req.Messages[1].Role != "assistant" {
			t.Fatalf("expected assistant tool-use message, got %s", req.Messages[1].Role)
		}
		if req.Messages[1].Content == nil {
			t.Fatal("assistant content should never be nil")
		}
		if len(req.Messages[1].Content) != 1 {
			t.Fatalf("expected 1 tool-use block, got %d", len(req.Messages[1].Content))
		}
		block, ok := req.Messages[1].Content[0].(map[string]interface{})
		if !ok {
			t.Fatalf("expected decoded tool-use block map, got %#v", req.Messages[1].Content[0])
		}
		if block["type"] != "tool_use" {
			t.Fatalf("expected tool_use block, got %#v", block)
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		fmt.Fprint(w, "data: {\"type\":\"message_stop\"}\n\n")
	}))
	defer server.Close()

	client := NewClient("test-key", server.URL, "claude-sonnet-4-20250514", "anthropic")
	events := make(chan StreamEvent, 100)

	go client.StreamChat(context.Background(), []Message{
		{Role: "system", Content: "You are helpful"},
		{Role: "user", Content: "call the tool"},
		{Role: "assistant", ToolCalls: []ToolCall{{
			ID:   "call_1",
			Type: "function",
			Function: FunctionCall{
				Name:      "read",
				Arguments: `{"path":"test.txt"}`,
			},
		}}},
	}, nil, events)

	for ev := range events {
		if ev.Type == "error" {
			t.Fatalf("unexpected error: %v", ev.Error)
		}
	}
}

func TestStreamChatAnthropicRejectsEmptyNonSystemPayloadWithoutHTTPCall(t *testing.T) {
	var requests int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requests, 1)
		t.Fatalf("unexpected HTTP request for normalized-empty Anthropic payload: %s %s", r.Method, r.URL.Path)
	}))
	defer server.Close()

	tests := []struct {
		name     string
		messages []Message
	}{
		{
			name: "system only",
			messages: []Message{
				{Role: "system", Content: "system only"},
			},
		},
		{
			name: "system plus empty assistant",
			messages: []Message{
				{Role: "system", Content: "system only"},
				{Role: "assistant", Content: ""},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient("test-key", server.URL, "claude-sonnet-4-20250514", "anthropic")
			events := make(chan StreamEvent, 10)

			go client.StreamChat(context.Background(), tt.messages, nil, events)

			var got []StreamEvent
			for ev := range events {
				got = append(got, ev)
			}

			if len(got) != 1 {
				t.Fatalf("expected exactly 1 event, got %d: %#v", len(got), got)
			}
			if got[0].Type != "error" {
				t.Fatalf("expected error event, got %#v", got[0])
			}
			if got[0].Error == nil {
				t.Fatalf("expected error payload, got %#v", got[0])
			}
			if !strings.Contains(got[0].Error.Error(), "at least one non-system message") {
				t.Fatalf("unexpected error: %v", got[0].Error)
			}
			if n := atomic.LoadInt32(&requests); n != 0 {
				t.Fatalf("expected no HTTP requests, got %d", n)
			}
		})
	}
}

func TestStreamChatAnthropicPreservesMixedBlockOrdering(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req anthropicRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if len(req.Messages) != 3 {
			t.Fatalf("expected 3 anthropic messages, got %d", len(req.Messages))
		}

		if req.Messages[0].Role != "user" {
			t.Fatalf("expected first message to stay user, got %s", req.Messages[0].Role)
		}
		firstBlock, ok := req.Messages[0].Content[0].(map[string]interface{})
		if !ok || firstBlock["type"] != "text" || firstBlock["text"] != "inspect both files" {
			t.Fatalf("unexpected first message content: %#v", req.Messages[0].Content)
		}

		if req.Messages[1].Role != "assistant" {
			t.Fatalf("expected second message to stay assistant, got %s", req.Messages[1].Role)
		}
		if len(req.Messages[1].Content) != 3 {
			t.Fatalf("expected assistant text + 2 tool_use blocks, got %d blocks", len(req.Messages[1].Content))
		}
		for i, want := range []string{"text", "tool_use", "tool_use"} {
			block, ok := req.Messages[1].Content[i].(map[string]interface{})
			if !ok || block["type"] != want {
				t.Fatalf("assistant block %d: expected %s, got %#v", i, want, req.Messages[1].Content[i])
			}
		}

		if req.Messages[2].Role != "user" {
			t.Fatalf("expected final merged message to be user, got %s", req.Messages[2].Role)
		}
		if len(req.Messages[2].Content) != 3 {
			t.Fatalf("expected tool_result, tool_result, text ordering, got %d blocks", len(req.Messages[2].Content))
		}
		for i, want := range []string{"tool_result", "tool_result", "text"} {
			block, ok := req.Messages[2].Content[i].(map[string]interface{})
			if !ok || block["type"] != want {
				t.Fatalf("final block %d: expected %s, got %#v", i, want, req.Messages[2].Content[i])
			}
		}
		if textBlock := req.Messages[2].Content[2].(map[string]interface{}); textBlock["text"] != "summarize the results" {
			t.Fatalf("unexpected final text block: %#v", textBlock)
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		fmt.Fprint(w, "data: {\"type\":\"message_stop\"}\n\n")
	}))
	defer server.Close()

	client := NewClient("test-key", server.URL, "claude-sonnet-4-20250514", "anthropic")
	events := make(chan StreamEvent, 100)

	go client.StreamChat(context.Background(), []Message{
		{Role: "system", Content: "You are helpful"},
		{Role: "user", Content: "inspect both files"},
		{
			Role:    "assistant",
			Content: "checking",
			ToolCalls: []ToolCall{
				{
					ID:   "call_1",
					Type: "function",
					Function: FunctionCall{
						Name:      "read",
						Arguments: `{"path":"a.txt"}`,
					},
				},
				{
					ID:   "call_2",
					Type: "function",
					Function: FunctionCall{
						Name:      "read",
						Arguments: `{"path":"b.txt"}`,
					},
				},
			},
		},
		{Role: "tool", ToolCallID: "call_1", Content: "alpha"},
		{Role: "tool", ToolCallID: "call_2", Content: "beta"},
		{Role: "user", Content: "summarize the results"},
	}, nil, events)

	for ev := range events {
		if ev.Type == "error" {
			t.Fatalf("unexpected error: %v", ev.Error)
		}
	}
}

func TestStreamChatAnthropicOmitsWhitespaceOnlyTextBlocksInRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req anthropicRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if len(req.Messages) != 3 {
			t.Fatalf("expected 3 anthropic messages, got %d", len(req.Messages))
		}
		if req.Messages[0].Role != "user" || len(req.Messages[0].Content) != 1 {
			t.Fatalf("unexpected first message: %#v", req.Messages[0])
		}
		firstBlock, ok := req.Messages[0].Content[0].(map[string]interface{})
		if !ok || firstBlock["type"] != "text" || firstBlock["text"] != "inspect" {
			t.Fatalf("unexpected first content block: %#v", req.Messages[0].Content[0])
		}
		if req.Messages[1].Role != "assistant" || len(req.Messages[1].Content) != 1 {
			t.Fatalf("expected assistant whitespace text to be stripped, got %#v", req.Messages[1])
		}
		secondBlock, ok := req.Messages[1].Content[0].(map[string]interface{})
		if !ok || secondBlock["type"] != "tool_use" {
			t.Fatalf("unexpected assistant block: %#v", req.Messages[1].Content[0])
		}
		if req.Messages[2].Role != "user" || len(req.Messages[2].Content) != 1 {
			t.Fatalf("expected trailing whitespace user text to be stripped, got %#v", req.Messages[2])
		}
		thirdBlock, ok := req.Messages[2].Content[0].(map[string]interface{})
		if !ok || thirdBlock["type"] != "tool_result" {
			t.Fatalf("unexpected final user block: %#v", req.Messages[2].Content[0])
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		fmt.Fprint(w, "data: {\"type\":\"message_stop\"}\n\n")
	}))
	defer server.Close()

	client := NewClient("test-key", server.URL, "claude-sonnet-4-20250514", "anthropic")
	events := make(chan StreamEvent, 100)

	go client.StreamChat(context.Background(), []Message{
		{Role: "system", Content: "You are helpful"},
		{Role: "user", Content: "inspect"},
		{
			Role:    "assistant",
			Content: " \n\t ",
			ToolCalls: []ToolCall{{
				ID:   "call_1",
				Type: "function",
				Function: FunctionCall{
					Name:      "read",
					Arguments: `{"path":"a.txt"}`,
				},
			}},
		},
		{Role: "tool", ToolCallID: "call_1", Content: "alpha"},
		{Role: "user", Content: "   "},
	}, nil, events)

	for ev := range events {
		if ev.Type == "error" {
			t.Fatalf("unexpected error: %v", ev.Error)
		}
	}
}

func TestStreamChatAnthropicRetryOn429(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount < 3 {
			// First 2 calls return 429 rate limit
			w.WriteHeader(429)
			fmt.Fprint(w, `{"error":{"type":"rate_limit_error","message":"Overloaded"}}`)
			return
		}

		// Third call succeeds
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)

		events := []string{
			`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
			`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"success"}}`,
			`{"type":"content_block_stop","index":0}`,
			`{"type":"message_stop"}`,
		}

		flusher := w.(http.Flusher)
		for _, ev := range events {
			fmt.Fprintf(w, "data: %s\n\n", ev)
			flusher.Flush()
		}
	}))
	defer server.Close()

	client := NewClient("test-key", server.URL, "claude-sonnet-4-20250514", "anthropic")
	events := make(chan StreamEvent, 100)

	go client.StreamChat(context.Background(), []Message{
		{Role: "user", Content: "hi"},
	}, nil, events)

	var text string
	var gotDone bool
	for ev := range events {
		switch ev.Type {
		case "text":
			text += ev.Text
		case "done":
			gotDone = true
		case "error":
			t.Fatalf("unexpected error: %v", ev.Error)
		}
	}

	if callCount != 3 {
		t.Fatalf("expected 3 calls (2 retries), got %d", callCount)
	}
	if !strings.Contains(text, "success") {
		t.Fatalf("expected text to contain 'success', got %q", text)
	}
	if !strings.Contains(text, "retried 2 time") {
		t.Fatalf("expected retry notification in text, got %q", text)
	}
	if !gotDone {
		t.Fatal("did not receive done event")
	}
}

func TestStreamChatAnthropicRetryOn5xx(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount < 2 {
			// First call returns 500
			w.WriteHeader(500)
			fmt.Fprint(w, `{"error":{"message":"Internal server error"}}`)
			return
		}

		// Second call succeeds
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)

		events := []string{
			`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
			`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"recovered"}}`,
			`{"type":"content_block_stop","index":0}`,
			`{"type":"message_stop"}`,
		}

		flusher := w.(http.Flusher)
		for _, ev := range events {
			fmt.Fprintf(w, "data: %s\n\n", ev)
			flusher.Flush()
		}
	}))
	defer server.Close()

	client := NewClient("test-key", server.URL, "claude-sonnet-4-20250514", "anthropic")
	events := make(chan StreamEvent, 100)

	go client.StreamChat(context.Background(), []Message{
		{Role: "user", Content: "hi"},
	}, nil, events)

	var text string
	for ev := range events {
		switch ev.Type {
		case "text":
			text += ev.Text
		case "error":
			t.Fatalf("unexpected error: %v", ev.Error)
		}
	}

	if callCount != 2 {
		t.Fatalf("expected 2 calls (1 retry), got %d", callCount)
	}
	if !strings.Contains(text, "recovered") {
		t.Fatalf("expected text to contain 'recovered', got %q", text)
	}
	if !strings.Contains(text, "retried 1 time") {
		t.Fatalf("expected retry notification in text, got %q", text)
	}
}

func TestStreamChatAnthropicNoRetryOn4xx(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		// 400 should not be retried
		w.WriteHeader(400)
		fmt.Fprint(w, `{"error":{"type":"invalid_request_error","message":"Bad request"}}`)
	}))
	defer server.Close()

	client := NewClient("test-key", server.URL, "claude-sonnet-4-20250514", "anthropic")
	events := make(chan StreamEvent, 100)

	go client.StreamChat(context.Background(), []Message{
		{Role: "user", Content: "hi"},
	}, nil, events)

	var gotError bool
	for ev := range events {
		if ev.Type == "error" {
			gotError = true
		}
	}

	if callCount != 1 {
		t.Fatalf("expected 1 call (no retry for 4xx), got %d", callCount)
	}
	if !gotError {
		t.Fatal("expected error event for 400 response")
	}
}
