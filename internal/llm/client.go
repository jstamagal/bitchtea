package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// DebugInfo holds request/response details for debug logging
type DebugInfo struct {
	Method         string
	URL            string
	RequestHeaders map[string]string
	RequestBody    string
	StatusCode     int
	ResponseBody   string
}

// Client is a streaming chat client supporting OpenAI and Anthropic APIs
type Client struct {
	APIKey   string
	BaseURL  string
	Model    string
	Provider string // "openai" or "anthropic"
	HTTP     *http.Client

	// DebugHook, when non-nil, is called with request/response details before
	// streaming begins. It is only invoked on successful (200) responses.
	DebugHook func(DebugInfo)
}

// ChatStreamer is the minimal streaming surface used by the agent loop.
type ChatStreamer interface {
	StreamChat(ctx context.Context, messages []Message, tools []ToolDef, events chan<- StreamEvent)
}

// Message represents a chat message
type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

// ToolCall represents a tool call from the model
type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function FunctionCall `json:"function"`
}

// FunctionCall is the function name + args within a tool call
type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// ToolDef defines a tool for the API
type ToolDef struct {
	Type     string      `json:"type"`
	Function ToolFuncDef `json:"function"`
}

// ToolFuncDef is the function schema within a tool definition
type ToolFuncDef struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Parameters  interface{} `json:"parameters"`
}

// StreamEvent is emitted during streaming
type StreamEvent struct {
	Type string // "text", "tool_call", "thinking", "done", "error"

	// For text events
	Text string

	// For tool_call events
	ToolCallID   string
	ToolName     string
	ToolArgs     string // accumulated JSON args
	ToolArgDelta string // incremental delta

	// For error events
	Error error

	// For usage events
	Usage *TokenUsage
}

// ChatRequest is the request body for chat completions
type ChatRequest struct {
	Model         string             `json:"model"`
	Messages      []Message          `json:"messages"`
	Tools         []ToolDef          `json:"tools,omitempty"`
	Stream        bool               `json:"stream"`
	StreamOptions *chatStreamOptions `json:"stream_options,omitempty"`
}

type chatStreamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

// StreamChat sends a streaming chat completion request, dispatching to the
// correct provider based on c.Provider.
func (c *Client) StreamChat(ctx context.Context, messages []Message, tools []ToolDef, events chan<- StreamEvent) {
	if c.Provider == "anthropic" {
		c.StreamChatAnthropic(ctx, messages, tools, events)
		return
	}
	c.streamChatOpenAI(ctx, messages, tools, events)
}

// streamChatOpenAI sends a streaming chat completion request via the OpenAI API
func (c *Client) streamChatOpenAI(ctx context.Context, messages []Message, tools []ToolDef, events chan<- StreamEvent) {
	defer close(events)

	reqBody := ChatRequest{
		Model:    c.Model,
		Messages: messages,
		Stream:   true,
		StreamOptions: &chatStreamOptions{
			IncludeUsage: true,
		},
	}
	if len(tools) > 0 {
		reqBody.Tools = tools
	}

	reqBodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		events <- StreamEvent{Type: "error", Error: fmt.Errorf("marshal: %w", err)}
		return
	}

	// Execute request with retry on rate limits and server errors
	var resp *http.Response
	retryCfg := DefaultRetryConfig()

	attempts, err := RetryWithBackoff(ctx, retryCfg, func() (bool, error) {
		req, err := http.NewRequestWithContext(ctx, "POST", c.BaseURL+"/chat/completions", bytes.NewReader(reqBodyBytes))
		if err != nil {
			return false, fmt.Errorf("request: %w", err)
		}
		for key, value := range c.openAIHeaders() {
			req.Header.Set(key, value)
		}

		resp, err = c.HTTP.Do(req)
		if err != nil {
			return false, fmt.Errorf("http: %w", err)
		}

		if resp.StatusCode == 200 {
			return false, nil // success
		}

		// Read body before potentially closing
		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if !IsRetryable(resp.StatusCode) {
			return false, fmt.Errorf("API %d: %s", resp.StatusCode, string(respBody))
		}

		// Rate limited or server error - retry
		return true, nil
	})

	if err != nil {
		events <- StreamEvent{Type: "error", Error: fmt.Errorf("after %d attempts: %w", attempts, err)}
		return
	}

	if resp.StatusCode != 200 {
		// This shouldn't happen, but handle it
		events <- StreamEvent{Type: "error", Error: fmt.Errorf("unexpected status: %d", resp.StatusCode)}
		return
	}

	// Notify if we retried (attempts > 1 means we retried at least once)
	if attempts > 1 {
		events <- StreamEvent{Type: "text", Text: fmt.Sprintf("\n[retried %d time(s) due to rate limit]\n", attempts-1)}
	}

	// Debug logging
	if c.DebugHook != nil {
		headers := c.debugOpenAIHeaders()
		c.DebugHook(DebugInfo{
			Method:         "POST",
			URL:            c.BaseURL + "/chat/completions",
			RequestHeaders: headers,
			RequestBody:    string(reqBodyBytes),
			StatusCode:     resp.StatusCode,
		})
	}

	// Parse SSE stream
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 256*1024), 256*1024)
	// Tool call accumulators
	toolCalls := make(map[int]*StreamEvent)

	for scanner.Scan() {
		line := scanner.Text()

		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			// Flush any accumulated tool calls
			for _, tc := range toolCalls {
				events <- *tc
			}
			events <- StreamEvent{Type: "done"}
			return
		}

		var chunk struct {
			Usage *struct {
				PromptTokens     int `json:"prompt_tokens"`
				CompletionTokens int `json:"completion_tokens"`
				TotalTokens      int `json:"total_tokens"`
			} `json:"usage"`
			Choices []struct {
				Delta struct {
					Content   string `json:"content"`
					ToolCalls []struct {
						Index    int    `json:"index"`
						ID       string `json:"id"`
						Type     string `json:"type"`
						Function struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						} `json:"function"`
					} `json:"tool_calls"`
				} `json:"delta"`
				FinishReason *string `json:"finish_reason"`
			} `json:"choices"`
		}

		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue // skip malformed chunks
		}

		if chunk.Usage != nil {
			events <- StreamEvent{
				Type: "usage",
				Usage: &TokenUsage{
					InputTokens:  chunk.Usage.PromptTokens,
					OutputTokens: chunk.Usage.CompletionTokens,
				},
			}
		}

		if len(chunk.Choices) == 0 {
			continue
		}

		choice := chunk.Choices[0]

		// Text content
		if choice.Delta.Content != "" {
			events <- StreamEvent{Type: "text", Text: choice.Delta.Content}
		}

		// Tool calls (accumulated across chunks)
		for _, tc := range choice.Delta.ToolCalls {
			existing, ok := toolCalls[tc.Index]
			if !ok {
				existing = &StreamEvent{
					Type:       "tool_call",
					ToolCallID: tc.ID,
					ToolName:   tc.Function.Name,
				}
				toolCalls[tc.Index] = existing
			}
			if tc.ID != "" {
				existing.ToolCallID = tc.ID
			}
			if tc.Function.Name != "" {
				existing.ToolName = tc.Function.Name
			}
			existing.ToolArgs += tc.Function.Arguments
			existing.ToolArgDelta = tc.Function.Arguments
		}
	}

	resp.Body.Close()

	if err := scanner.Err(); err != nil {
		events <- StreamEvent{Type: "error", Error: fmt.Errorf("stream: %w", err)}
	}
}

// NewClient creates a new LLM client
func NewClient(apiKey, baseURL, model, provider string) *Client {
	return &Client{
		APIKey:   apiKey,
		BaseURL:  baseURL,
		Model:    model,
		Provider: provider,
		HTTP:     &http.Client{},
	}
}

func (c *Client) openAIHeaders() map[string]string {
	headers := map[string]string{
		"Content-Type": "application/json",
		"Accept":       "text/event-stream",
	}
	if strings.TrimSpace(c.APIKey) != "" {
		headers["Authorization"] = "Bearer " + c.APIKey
	}
	if isOpenRouterBaseURL(c.BaseURL) {
		headers["HTTP-Referer"] = "https://github.com/jstamagal/bitchtea"
		headers["X-Title"] = "bitchtea"
		headers["X-OpenRouter-Title"] = "bitchtea"
	}
	return headers
}

func (c *Client) debugOpenAIHeaders() map[string]string {
	headers := c.openAIHeaders()
	if _, ok := headers["Authorization"]; ok {
		headers["Authorization"] = "Bearer [REDACTED]"
	}
	return headers
}

func isOpenRouterBaseURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}

	host := strings.ToLower(u.Hostname())
	return host == "openrouter.ai" || strings.HasSuffix(host, ".openrouter.ai")
}
