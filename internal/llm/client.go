package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// Client is an OpenAI-compatible streaming chat client
type Client struct {
	APIKey  string
	BaseURL string
	Model   string
	HTTP    *http.Client
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
}

// ChatRequest is the request body for chat completions
type ChatRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
	Tools    []ToolDef `json:"tools,omitempty"`
	Stream   bool      `json:"stream"`
}

// StreamChat sends a streaming chat completion request
// Events are sent to the channel as they arrive
func (c *Client) StreamChat(ctx context.Context, messages []Message, tools []ToolDef, events chan<- StreamEvent) {
	defer close(events)

	reqBody := ChatRequest{
		Model:    c.Model,
		Messages: messages,
		Stream:   true,
	}
	if len(tools) > 0 {
		reqBody.Tools = tools
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		events <- StreamEvent{Type: "error", Error: fmt.Errorf("marshal: %w", err)}
		return
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.BaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		events <- StreamEvent{Type: "error", Error: fmt.Errorf("request: %w", err)}
		return
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	req.Header.Set("Accept", "text/event-stream")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		events <- StreamEvent{Type: "error", Error: fmt.Errorf("http: %w", err)}
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		errBody, _ := io.ReadAll(resp.Body)
		events <- StreamEvent{Type: "error", Error: fmt.Errorf("API %d: %s", resp.StatusCode, string(errBody))}
		return
	}

	// Parse SSE stream
	scanner := bufio.NewScanner(resp.Body)
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
			Choices []struct {
				Delta struct {
					Content   string     `json:"content"`
					ToolCalls []struct {
						Index    int          `json:"index"`
						ID       string       `json:"id"`
						Type     string       `json:"type"`
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

	if err := scanner.Err(); err != nil {
		events <- StreamEvent{Type: "error", Error: fmt.Errorf("stream: %w", err)}
	}
}

// NewClient creates a new LLM client
func NewClient(apiKey, baseURL, model string) *Client {
	return &Client{
		APIKey:  apiKey,
		BaseURL: baseURL,
		Model:   model,
		HTTP:    &http.Client{},
	}
}
