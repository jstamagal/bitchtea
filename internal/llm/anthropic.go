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

type anthropicUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// anthropicRequest is the request body for the Anthropic Messages API
type anthropicRequest struct {
	Model     string             `json:"model"`
	MaxTokens int                `json:"max_tokens"`
	Stream    bool               `json:"stream"`
	System    string             `json:"system,omitempty"`
	Messages  []anthropicMessage `json:"messages"`
	Tools     []anthropicTool    `json:"tools,omitempty"`
}

type anthropicMessage struct {
	Role    string        `json:"role"`
	Content []interface{} `json:"content"`
}

type anthropicTextBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type anthropicToolUseBlock struct {
	Type  string      `json:"type"`
	ID    string      `json:"id"`
	Name  string      `json:"name"`
	Input interface{} `json:"input"`
}

type anthropicToolResultBlock struct {
	Type      string `json:"type"`
	ToolUseID string `json:"tool_use_id"`
	Content   string `json:"content"`
}

type anthropicTool struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	InputSchema interface{} `json:"input_schema"`
}

// StreamChatAnthropic sends a streaming request to the Anthropic Messages API
func (c *Client) StreamChatAnthropic(ctx context.Context, messages []Message, tools []ToolDef, events chan<- StreamEvent) {
	defer close(events)

	// Extract system message and convert messages
	var systemParts []string
	var anthropicMsgs []anthropicMessage

	for _, m := range messages {
		switch m.Role {
		case "system":
			if strings.TrimSpace(m.Content) != "" {
				systemParts = append(systemParts, m.Content)
			}

		case "user":
			anthropicMsgs = append(anthropicMsgs, anthropicMessage{
				Role:    "user",
				Content: []interface{}{anthropicTextBlock{Type: "text", Text: m.Content}},
			})

		case "assistant":
			var blocks []interface{}
			if m.Content != "" {
				blocks = append(blocks, anthropicTextBlock{Type: "text", Text: m.Content})
			}
			for _, tc := range m.ToolCalls {
				// Parse the JSON arguments into a raw object
				var input interface{}
				if err := json.Unmarshal([]byte(tc.Function.Arguments), &input); err != nil {
					input = map[string]interface{}{}
				}
				blocks = append(blocks, anthropicToolUseBlock{
					Type:  "tool_use",
					ID:    tc.ID,
					Name:  tc.Function.Name,
					Input: input,
				})
			}
			if len(blocks) > 0 {
				anthropicMsgs = append(anthropicMsgs, anthropicMessage{
					Role:    "assistant",
					Content: blocks,
				})
			}

		case "tool":
			// Anthropic expects tool results as user messages with tool_result blocks
			// Merge consecutive tool results into one user message
			block := anthropicToolResultBlock{
				Type:      "tool_result",
				ToolUseID: m.ToolCallID,
				Content:   m.Content,
			}
			// Check if last message is a user message with tool_result blocks
			if len(anthropicMsgs) > 0 {
				last := &anthropicMsgs[len(anthropicMsgs)-1]
				if last.Role == "user" && len(last.Content) > 0 {
					// Check if all blocks are tool_result
					allToolResult := true
					for _, b := range last.Content {
						if _, ok := b.(anthropicToolResultBlock); !ok {
							allToolResult = false
							break
						}
					}
					if allToolResult {
						last.Content = append(last.Content, block)
						continue
					}
				}
			}
			anthropicMsgs = append(anthropicMsgs, anthropicMessage{
				Role:    "user",
				Content: []interface{}{block},
			})
		}
	}

	system := strings.Join(systemParts, "\n\n")

	// Ensure messages alternate user/assistant (Anthropic requirement)
	anthropicMsgs = ensureAlternating(anthropicMsgs)
	if len(anthropicMsgs) == 0 {
		events <- StreamEvent{Type: "error", Error: fmt.Errorf("anthropic request requires at least one non-system message with content")}
		return
	}

	// Convert tools
	var anthropicTools []anthropicTool
	for _, t := range tools {
		anthropicTools = append(anthropicTools, anthropicTool{
			Name:        t.Function.Name,
			Description: t.Function.Description,
			InputSchema: t.Function.Parameters,
		})
	}

	// TODO(Phase6): Anthropic prompt caching is not implemented.
	// To activate it, the system prompt and the first large user message
	// (the AGENTS.md/context injection) should carry a
	//   "cache_control": {"type": "ephemeral"}
	// block. This requires changing anthropicRequest.System from a plain
	// string to []anthropicTextBlock (each block can carry cache_control),
	// and marking the bootstrap user messages in the message list similarly.
	// Gating should check that the resolved service is native "anthropic" —
	// proxies like OpenRouter pass the Anthropic wire format but may not
	// honour cache_control, so this needs Phase 6's service-identity field.
	reqBody := anthropicRequest{
		Model:     c.Model,
		MaxTokens: 8192,
		Stream:    true,
		System:    system,
		Messages:  anthropicMsgs,
	}
	if len(anthropicTools) > 0 {
		reqBody.Tools = anthropicTools
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		events <- StreamEvent{Type: "error", Error: fmt.Errorf("marshal: %w", err)}
		return
	}

	// Anthropic uses /messages endpoint (base URL should be https://api.anthropic.com/v1)
	apiURL := strings.TrimSuffix(c.BaseURL, "/") + "/messages"

	// Execute request with retry on rate limits and server errors
	var resp *http.Response
	var lastStatusErr *apiStatusError
	retryCfg := DefaultRetryConfig()

	attempts, err := RetryWithBackoff(ctx, retryCfg, func() (bool, error) {
		req, err := http.NewRequestWithContext(ctx, "POST", apiURL, bytes.NewReader(body))
		if err != nil {
			return false, fmt.Errorf("request: %w", err)
		}
		for key, value := range c.anthropicHeaders() {
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
		lastStatusErr = &apiStatusError{StatusCode: resp.StatusCode, Body: string(respBody)}

		if !IsRetryable(resp.StatusCode) {
			return false, lastStatusErr
		}

		// Rate limited or server error - retry
		return true, nil
	})

	if err != nil {
		events <- StreamEvent{Type: "error", Error: explainRequestFailure(c.Provider, c.BaseURL, attempts, err)}
		return
	}

	if resp.StatusCode != 200 {
		events <- StreamEvent{Type: "error", Error: explainRequestFailure(c.Provider, c.BaseURL, attempts, lastStatusErr)}
		return
	}

	// Notify if we retried (attempts > 1 means we retried at least once)
	if attempts > 1 {
		events <- StreamEvent{Type: "text", Text: fmt.Sprintf("\n[retried %d time(s) due to rate limit]\n", attempts-1)}
	}
	defer resp.Body.Close()

	// Debug logging
	if c.DebugHook != nil {
		headers := c.debugAnthropicHeaders()
		c.DebugHook(DebugInfo{
			Method:         "POST",
			URL:            apiURL,
			RequestHeaders: headers,
			RequestBody:    string(body),
			StatusCode:     resp.StatusCode,
		})
	}

	// Parse Anthropic SSE stream
	scanner := bufio.NewScanner(resp.Body)
	// Increase scanner buffer for large tool call arguments
	scanner.Buffer(make([]byte, 0, 256*1024), 256*1024)

	// Tool call accumulators
	type toolAccum struct {
		id         string
		name       string
		argsStr    string
		startInput string
	}
	var activeTools []toolAccum
	var usage TokenUsage
	var usageSeen bool

	processData := func(data string) bool {
		var event struct {
			Type  string          `json:"type"`
			Index int             `json:"index"`
			Usage *anthropicUsage `json:"usage"`
			Delta struct {
				Type        string `json:"type"`
				Text        string `json:"text"`
				Thinking    string `json:"thinking"`
				PartialJSON string `json:"partial_json"`
				StopReason  string `json:"stop_reason"`
			} `json:"delta"`
			ContentBlock struct {
				Type     string          `json:"type"`
				ID       string          `json:"id"`
				Name     string          `json:"name"`
				Text     string          `json:"text"`
				Thinking string          `json:"thinking"`
				Input    json.RawMessage `json:"input"`
			} `json:"content_block"`
			Message struct {
				StopReason string          `json:"stop_reason"`
				Usage      *anthropicUsage `json:"usage"`
			} `json:"message"`
		}

		if err := json.Unmarshal([]byte(data), &event); err != nil {
			return false
		}

		switch event.Type {
		case "message_start":
			if event.Message.Usage != nil {
				usage = TokenUsage{
					InputTokens:  event.Message.Usage.InputTokens,
					OutputTokens: event.Message.Usage.OutputTokens,
				}
				usageSeen = true
			}

		case "content_block_start":
			switch event.ContentBlock.Type {
			case "text":
				if event.ContentBlock.Text != "" {
					events <- StreamEvent{Type: "text", Text: event.ContentBlock.Text}
				}
			case "thinking":
				if event.ContentBlock.Thinking != "" {
					events <- StreamEvent{Type: "thinking", Text: event.ContentBlock.Thinking}
				}
			case "tool_use":
				// Start accumulating a tool call
				for len(activeTools) <= event.Index {
					activeTools = append(activeTools, toolAccum{})
				}
				activeTools[event.Index] = toolAccum{
					id:         event.ContentBlock.ID,
					name:       event.ContentBlock.Name,
					startInput: compactAnthropicJSON(event.ContentBlock.Input),
				}
			}

		case "content_block_delta":
			switch event.Delta.Type {
			case "text_delta":
				if event.Delta.Text != "" {
					events <- StreamEvent{Type: "text", Text: event.Delta.Text}
				}
			case "thinking_delta":
				if event.Delta.Thinking != "" {
					events <- StreamEvent{Type: "thinking", Text: event.Delta.Thinking}
				}
			case "input_json_delta":
				if event.Index < len(activeTools) {
					activeTools[event.Index].argsStr += event.Delta.PartialJSON
				}
			}

		case "content_block_stop":
			if event.Index < len(activeTools) {
				tc := activeTools[event.Index]
				if tc.name != "" {
					toolArgs := tc.argsStr
					if toolArgs == "" {
						toolArgs = tc.startInput
					}
					events <- StreamEvent{
						Type:       "tool_call",
						ToolCallID: tc.id,
						ToolName:   tc.name,
						ToolArgs:   toolArgs,
					}
				}
			}

		case "message_delta":
			if event.Usage != nil {
				usage.OutputTokens = event.Usage.OutputTokens
				usageSeen = true
			}

		case "message_stop":
			if usageSeen {
				usageCopy := usage
				events <- StreamEvent{Type: "usage", Usage: &usageCopy}
			}
			events <- StreamEvent{Type: "done"}
			return true

		case "error":
			events <- StreamEvent{Type: "error", Error: fmt.Errorf("stream error: %s", data)}
			return true
		}
		return false
	}

	var dataLines []string
	flushFrame := func() bool {
		if len(dataLines) == 0 {
			return false
		}
		data := strings.Join(dataLines, "\n")
		dataLines = dataLines[:0]
		return processData(data)
	}

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if flushFrame() {
				return
			}
			continue
		}

		if !strings.HasPrefix(line, "data:") {
			continue
		}

		data := strings.TrimPrefix(line, "data:")
		if len(data) > 0 && data[0] == ' ' {
			data = data[1:]
		}
		dataLines = append(dataLines, data)
	}
	if flushFrame() {
		return
	}

	if err := scanner.Err(); err != nil {
		events <- StreamEvent{Type: "error", Error: fmt.Errorf("stream: %w", err)}
	}
}

func compactAnthropicJSON(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var buf bytes.Buffer
	if err := json.Compact(&buf, raw); err != nil {
		return strings.TrimSpace(string(raw))
	}
	return buf.String()
}

func (c *Client) anthropicHeaders() map[string]string {
	headers := map[string]string{
		"Content-Type":      "application/json",
		"anthropic-version": "2023-06-01",
	}
	if strings.TrimSpace(c.APIKey) != "" {
		headers["x-api-key"] = c.APIKey
		headers["Authorization"] = "Bearer " + c.APIKey
	}
	return headers
}

func (c *Client) debugAnthropicHeaders() map[string]string {
	headers := c.anthropicHeaders()
	if _, ok := headers["x-api-key"]; ok {
		headers["x-api-key"] = "[REDACTED]"
	}
	if _, ok := headers["Authorization"]; ok {
		headers["Authorization"] = "Bearer [REDACTED]"
	}
	return headers
}

// ensureAlternating ensures messages alternate between user and assistant.
// Anthropic requires this. We merge consecutive same-role messages.
func ensureAlternating(msgs []anthropicMessage) []anthropicMessage {
	result := make([]anthropicMessage, 0, len(msgs))
	for _, msg := range msgs {
		msg.Content = sanitizeAnthropicContent(msg.Content)
		if len(msg.Content) == 0 {
			continue
		}
		if len(result) > 0 && result[len(result)-1].Role == msg.Role {
			last := &result[len(result)-1]
			last.Content = sanitizeAnthropicContent(append(last.Content, msg.Content...))
			continue
		}
		result = append(result, msg)
	}
	return result
}

func sanitizeAnthropicContent(blocks []interface{}) []interface{} {
	if len(blocks) == 0 {
		return []interface{}{}
	}
	sanitized := make([]interface{}, 0, len(blocks))
	for _, block := range blocks {
		if block == nil {
			continue
		}
		if textBlock, ok := block.(anthropicTextBlock); ok && strings.TrimSpace(textBlock.Text) == "" {
			continue
		}
		sanitized = append(sanitized, block)
	}
	return sanitized
}
