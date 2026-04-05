package llm

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestStreamChatText(t *testing.T) {
	// Mock SSE server that streams text tokens
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Fatalf("bad auth header: %s", r.Header.Get("Authorization"))
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)

		chunks := []string{
			`{"choices":[{"delta":{"content":"hello "},"finish_reason":null}]}`,
			`{"choices":[{"delta":{"content":"world"},"finish_reason":null}]}`,
			`{"choices":[{"delta":{},"finish_reason":"stop"}]}`,
		}

		flusher := w.(http.Flusher)
		for _, chunk := range chunks {
			fmt.Fprintf(w, "data: %s\n\n", chunk)
			flusher.Flush()
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer server.Close()

	client := NewClient("test-key", server.URL, "test-model", "openai")
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

	if text != "hello world" {
		t.Fatalf("expected 'hello world', got %q", text)
	}
	if !gotDone {
		t.Fatal("did not receive done event")
	}
}

func TestStreamChatToolCall(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)

		chunks := []string{
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_123","type":"function","function":{"name":"read","arguments":""}}]},"finish_reason":null}]}`,
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"","type":"","function":{"name":"","arguments":"{\"path\":"}}]},"finish_reason":null}]}`,
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"","type":"","function":{"name":"","arguments":"\"test.txt\"}"}}]},"finish_reason":null}]}`,
			`{"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
		}

		flusher := w.(http.Flusher)
		for _, chunk := range chunks {
			fmt.Fprintf(w, "data: %s\n\n", chunk)
			flusher.Flush()
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer server.Close()

	client := NewClient("test-key", server.URL, "test-model", "openai")
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
	if tc.ToolCallID != "call_123" {
		t.Fatalf("wrong tool call ID: %s", tc.ToolCallID)
	}
	if tc.ToolName != "read" {
		t.Fatalf("wrong tool name: %s", tc.ToolName)
	}
	if tc.ToolArgs != `{"path":"test.txt"}` {
		t.Fatalf("wrong tool args: %s", tc.ToolArgs)
	}
}

func TestStreamChatAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(429)
		fmt.Fprint(w, `{"error":{"message":"rate limited"}}`)
	}))
	defer server.Close()

	client := NewClient("test-key", server.URL, "test-model", "openai")
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
