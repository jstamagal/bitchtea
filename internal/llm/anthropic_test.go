package llm

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
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
