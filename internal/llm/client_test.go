package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
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

func TestStreamChatUsage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)

		chunks := []string{
			`{"choices":[{"delta":{"content":"hello"},"finish_reason":null}]}`,
			`{"choices":[],"usage":{"prompt_tokens":123,"completion_tokens":45,"total_tokens":168}}`,
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
	if usage.InputTokens != 123 {
		t.Fatalf("expected 123 input tokens, got %d", usage.InputTokens)
	}
	if usage.OutputTokens != 45 {
		t.Fatalf("expected 45 output tokens, got %d", usage.OutputTokens)
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

func TestStreamChatConnectionRefusedIncludesGuidance(t *testing.T) {
	client := NewClient("test-key", "http://127.0.0.1:3456", "test-model", "openai")
	client.HTTP = &http.Client{
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, &url.Error{
				Op:  "Post",
				URL: "http://127.0.0.1:3456/chat/completions",
				Err: errors.New("dial tcp 127.0.0.1:3456: connect: connection refused"),
			}
		}),
	}

	events := make(chan StreamEvent, 100)
	go client.StreamChat(context.Background(), []Message{{Role: "user", Content: "hi"}}, nil, events)

	var gotError error
	for ev := range events {
		if ev.Type == "error" {
			gotError = ev.Error
		}
	}

	if gotError == nil {
		t.Fatal("expected connection error")
	}
	for _, want := range []string{
		"connection refused",
		"Hint:",
		"http://127.0.0.1:3456/chat/completions",
		"make sure the server is running",
	} {
		if !strings.Contains(gotError.Error(), want) {
			t.Fatalf("expected %q in error, got %v", want, gotError)
		}
	}
}

func TestStreamChat404SuggestsAnthropicProvider(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
		fmt.Fprint(w, `{"error":{"type":"not_found","message":"Endpoint not supported: POST /chat/completions"}}`)
	}))
	defer server.Close()

	client := NewClient("test-key", server.URL, "test-model", "openai")
	events := make(chan StreamEvent, 100)
	go client.StreamChat(context.Background(), []Message{{Role: "user", Content: "hi"}}, nil, events)

	var gotError error
	for ev := range events {
		if ev.Type == "error" {
			gotError = ev.Error
		}
	}

	if gotError == nil {
		t.Fatal("expected 404 error")
	}
	for _, want := range []string{"API 404", "Try /provider anthropic", "/chat/completions"} {
		if !strings.Contains(gotError.Error(), want) {
			t.Fatalf("expected %q in error, got %v", want, gotError)
		}
	}
}

func TestStreamChat429IncludesRateLimitGuidance(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(429)
		fmt.Fprint(w, `{"error":{"message":"rate limited"}}`)
	}))
	defer server.Close()

	client := NewClient("test-key", server.URL, "test-model", "openai")
	events := make(chan StreamEvent, 100)
	go client.StreamChat(context.Background(), []Message{{Role: "user", Content: "hi"}}, nil, events)

	var gotError error
	for ev := range events {
		if ev.Type == "error" {
			gotError = ev.Error
		}
	}

	if gotError == nil {
		t.Fatal("expected 429 error")
	}
	for _, want := range []string{"API 429", "rate limited", "quota"} {
		if !strings.Contains(strings.ToLower(gotError.Error()), strings.ToLower(want)) {
			t.Fatalf("expected %q in error, got %v", want, gotError)
		}
	}
}

func TestStreamChatToolCallLargeArguments(t *testing.T) {
	largeValue := strings.Repeat("a", 70*1024)
	argObject, err := json.Marshal(map[string]string{"content": largeValue})
	if err != nil {
		t.Fatalf("marshal arg object: %v", err)
	}
	argString, err := json.Marshal(string(argObject))
	if err != nil {
		t.Fatalf("marshal arg string: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)

		chunk := fmt.Sprintf(
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_big","type":"function","function":{"name":"write","arguments":%s}}]},"finish_reason":null}]}`,
			argString,
		)

		flusher := w.(http.Flusher)
		fmt.Fprintf(w, "data: %s\n\n", chunk)
		flusher.Flush()
		fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer server.Close()

	client := NewClient("test-key", server.URL, "test-model", "openai")
	events := make(chan StreamEvent, 100)

	go client.StreamChat(context.Background(), []Message{
		{Role: "user", Content: "write a big file"},
	}, nil, events)

	var gotToolCall bool
	for ev := range events {
		switch ev.Type {
		case "tool_call":
			gotToolCall = true
			if !strings.Contains(ev.ToolArgs, largeValue[:1024]) {
				t.Fatalf("expected large argument content in tool args, got %q", ev.ToolArgs[:min(len(ev.ToolArgs), 128)])
			}
		case "error":
			t.Fatalf("unexpected error for large tool call: %v", ev.Error)
		}
	}

	if !gotToolCall {
		t.Fatal("expected large tool call event")
	}
}

func TestDebugHookOpenAI(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		flusher := w.(http.Flusher)
		fmt.Fprintf(w, "data: %s\n\n", `{"choices":[{"delta":{"content":"ok"},"finish_reason":null}]}`)
		flusher.Flush()
		fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer server.Close()

	client := NewClient("test-key", server.URL, "test-model", "openai")

	var captured *DebugInfo
	client.DebugHook = func(info DebugInfo) {
		captured = &info
	}

	events := make(chan StreamEvent, 100)
	go client.StreamChat(context.Background(), []Message{
		{Role: "user", Content: "hi"},
	}, nil, events)

	for range events {
	}

	if captured == nil {
		t.Fatal("DebugHook was not called")
	}
	if captured.Method != "POST" {
		t.Errorf("expected POST, got %s", captured.Method)
	}
	if !strings.Contains(captured.URL, "/chat/completions") {
		t.Errorf("expected URL containing /chat/completions, got %s", captured.URL)
	}
	if captured.StatusCode != 200 {
		t.Errorf("expected status 200, got %d", captured.StatusCode)
	}
	if captured.RequestHeaders["Authorization"] != "Bearer [REDACTED]" {
		t.Errorf("expected redacted auth header, got %s", captured.RequestHeaders["Authorization"])
	}
	if !strings.Contains(captured.RequestBody, "test-model") {
		t.Errorf("expected request body to contain model name, got %s", captured.RequestBody)
	}
}

func TestDebugHookNilByDefault(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		flusher := w.(http.Flusher)
		fmt.Fprintf(w, "data: %s\n\n", `{"choices":[{"delta":{"content":"ok"},"finish_reason":null}]}`)
		flusher.Flush()
		fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer server.Close()

	client := NewClient("test-key", server.URL, "test-model", "openai")
	// DebugHook is nil by default - should not panic
	events := make(chan StreamEvent, 100)
	go client.StreamChat(context.Background(), []Message{
		{Role: "user", Content: "hi"},
	}, nil, events)

	for range events {
	}
	// If we get here without panic, the test passes
}

func TestStreamChatOpenRouterHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("HTTP-Referer"); got != "https://github.com/jstamagal/bitchtea" {
			t.Fatalf("unexpected HTTP-Referer header: %q", got)
		}
		if got := r.Header.Get("X-Title"); got != "bitchtea" {
			t.Fatalf("unexpected X-Title header: %q", got)
		}
		if got := r.Header.Get("X-OpenRouter-Title"); got != "bitchtea" {
			t.Fatalf("unexpected X-OpenRouter-Title header: %q", got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Fatalf("unexpected auth header: %q", got)
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		flusher := w.(http.Flusher)
		fmt.Fprintf(w, "data: %s\n\n", `{"choices":[{"delta":{"content":"ok"},"finish_reason":null}]}`)
		flusher.Flush()
		fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer server.Close()

	serverURL := serverURLParts(t, server.URL)
	client := NewClient("test-key", "https://openrouter.ai/api/v1", "test-model", "openai")
	client.HTTP = &http.Client{
		Transport: rewriteHostTransport{
			targetScheme: serverURL.scheme,
			targetHost:   serverURL.host,
			base:         http.DefaultTransport,
		},
	}

	events := make(chan StreamEvent, 100)
	go client.StreamChat(context.Background(), []Message{{Role: "user", Content: "hi"}}, nil, events)

	for ev := range events {
		if ev.Type == "error" {
			t.Fatalf("unexpected error: %v", ev.Error)
		}
	}
}

func TestStreamChatOpenAIAllowsMissingAuthorization(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "" {
			t.Fatalf("expected no Authorization header, got %q", got)
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		flusher := w.(http.Flusher)
		fmt.Fprintf(w, "data: %s\n\n", `{"choices":[{"delta":{"content":"ok"},"finish_reason":null}]}`)
		flusher.Flush()
		fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer server.Close()

	client := NewClient("", server.URL, "test-model", "openai")
	events := make(chan StreamEvent, 100)
	go client.StreamChat(context.Background(), []Message{{Role: "user", Content: "hi"}}, nil, events)

	for ev := range events {
		if ev.Type == "error" {
			t.Fatalf("unexpected error: %v", ev.Error)
		}
	}
}

func TestNewClientConfiguresHTTPTimeouts(t *testing.T) {
	client := NewClient("test-key", "https://api.openai.com/v1", "test-model", "openai")
	timeoutCfg := defaultHTTPClientTimeouts()

	if client.HTTP == nil {
		t.Fatal("expected HTTP client to be configured")
	}
	if client.HTTP.Timeout != timeoutCfg.requestTimeout {
		t.Fatalf("expected request timeout %v, got %v", timeoutCfg.requestTimeout, client.HTTP.Timeout)
	}

	transport, ok := client.HTTP.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("expected *http.Transport, got %T", client.HTTP.Transport)
	}
	if transport.ResponseHeaderTimeout != timeoutCfg.responseHeaderTimeout {
		t.Fatalf("expected response header timeout %v, got %v", timeoutCfg.responseHeaderTimeout, transport.ResponseHeaderTimeout)
	}
	if transport.TLSHandshakeTimeout != timeoutCfg.tlsHandshakeTimeout {
		t.Fatalf("expected TLS handshake timeout %v, got %v", timeoutCfg.tlsHandshakeTimeout, transport.TLSHandshakeTimeout)
	}
	if transport.IdleConnTimeout != timeoutCfg.idleConnTimeout {
		t.Fatalf("expected idle connection timeout %v, got %v", timeoutCfg.idleConnTimeout, transport.IdleConnTimeout)
	}
	if transport.ExpectContinueTimeout != timeoutCfg.expectContinueTimeout {
		t.Fatalf("expected expect-continue timeout %v, got %v", timeoutCfg.expectContinueTimeout, transport.ExpectContinueTimeout)
	}
	if transport.MaxIdleConns != timeoutCfg.maxIdleConns {
		t.Fatalf("expected max idle conns %d, got %d", timeoutCfg.maxIdleConns, transport.MaxIdleConns)
	}
	if transport.MaxIdleConnsPerHost != timeoutCfg.maxIdleConnsPerHost {
		t.Fatalf("expected max idle conns per host %d, got %d", timeoutCfg.maxIdleConnsPerHost, transport.MaxIdleConnsPerHost)
	}
}

func TestStreamChatResponseHeaderTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
	}))
	defer server.Close()

	client := NewClient("test-key", server.URL, "test-model", "openai")
	client.HTTP = newHTTPClient(httpClientTimeouts{
		dialTimeout:           50 * time.Millisecond,
		keepAlive:             30 * time.Second,
		tlsHandshakeTimeout:   50 * time.Millisecond,
		responseHeaderTimeout: 50 * time.Millisecond,
		expectContinueTimeout: 10 * time.Millisecond,
		idleConnTimeout:       time.Second,
		requestTimeout:        time.Second,
		maxIdleConns:          10,
		maxIdleConnsPerHost:   2,
	})

	events := make(chan StreamEvent, 100)
	start := time.Now()
	go client.StreamChat(context.Background(), []Message{{Role: "user", Content: "hi"}}, nil, events)

	var gotError error
	for ev := range events {
		if ev.Type == "error" {
			gotError = ev.Error
		}
	}

	if gotError == nil {
		t.Fatal("expected timeout error")
	}
	if time.Since(start) >= time.Second {
		t.Fatalf("expected fast failure on hung response headers, got %v", time.Since(start))
	}
	if !strings.Contains(gotError.Error(), "timeout") {
		t.Fatalf("expected timeout-related error, got %v", gotError)
	}
}

func TestStreamChatRequestTimeoutWhileStreaming(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		w.(http.Flusher).Flush()
		time.Sleep(200 * time.Millisecond)
	}))
	defer server.Close()

	client := NewClient("test-key", server.URL, "test-model", "openai")
	client.HTTP = newHTTPClient(httpClientTimeouts{
		dialTimeout:           50 * time.Millisecond,
		keepAlive:             30 * time.Second,
		tlsHandshakeTimeout:   50 * time.Millisecond,
		responseHeaderTimeout: 50 * time.Millisecond,
		expectContinueTimeout: 10 * time.Millisecond,
		idleConnTimeout:       time.Second,
		requestTimeout:        75 * time.Millisecond,
		maxIdleConns:          10,
		maxIdleConnsPerHost:   2,
	})

	events := make(chan StreamEvent, 100)
	go client.StreamChat(context.Background(), []Message{{Role: "user", Content: "hi"}}, nil, events)

	var gotError error
	for ev := range events {
		if ev.Type == "error" {
			gotError = ev.Error
		}
	}

	if gotError == nil {
		t.Fatal("expected request timeout error")
	}
	if !strings.Contains(gotError.Error(), "timeout") && !strings.Contains(gotError.Error(), "deadline exceeded") {
		t.Fatalf("expected timeout-related error, got %v", gotError)
	}
}

type parsedURLParts struct {
	scheme string
	host   string
}

func serverURLParts(t *testing.T, raw string) parsedURLParts {
	t.Helper()
	if raw == "" {
		return parsedURLParts{}
	}
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse url %q: %v", raw, err)
	}
	return parsedURLParts{scheme: u.Scheme, host: u.Host}
}

type rewriteHostTransport struct {
	targetScheme string
	targetHost   string
	base         http.RoundTripper
}

func (t rewriteHostTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	clone.URL.Scheme = t.targetScheme
	clone.URL.Host = t.targetHost
	return t.base.RoundTrip(clone)
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
