package llm

import (
	"strings"

	"charm.land/fantasy"
)

// splitForFantasy splits the current transcript into fantasy's current Prompt
// plus prior Messages. The last user message is the prompt; system messages are
// omitted because the caller passes the system prompt through fantasy options.
func splitForFantasy(msgs []Message) (prompt string, prior []fantasy.Message) {
	lastUser := -1
	for i, m := range msgs {
		if m.Role == "user" {
			lastUser = i
		}
	}
	if lastUser >= 0 {
		prompt = msgs[lastUser].Content
	}

	prior = make([]fantasy.Message, 0, len(msgs))
	for i, m := range msgs {
		if i == lastUser {
			continue
		}

		switch m.Role {
		case "system":
			continue
		case "user":
			prior = append(prior, fantasy.Message{
				Role:    fantasy.MessageRoleUser,
				Content: []fantasy.MessagePart{fantasy.TextPart{Text: m.Content}},
			})
		case "assistant":
			parts := make([]fantasy.MessagePart, 0, 1+len(m.ToolCalls))
			if m.Content != "" {
				parts = append(parts, fantasy.TextPart{Text: m.Content})
			}
			for _, tc := range m.ToolCalls {
				parts = append(parts, fantasy.ToolCallPart{
					ToolCallID: tc.ID,
					ToolName:   tc.Function.Name,
					Input:      tc.Function.Arguments,
				})
			}
			prior = append(prior, fantasy.Message{
				Role:    fantasy.MessageRoleAssistant,
				Content: parts,
			})
		case "tool":
			prior = append(prior, fantasy.Message{
				Role: fantasy.MessageRoleTool,
				Content: []fantasy.MessagePart{fantasy.ToolResultPart{
					ToolCallID: m.ToolCallID,
					Output:     fantasy.ToolResultOutputContentText{Text: m.Content},
				}},
			})
		}
	}

	return prompt, prior
}

// fantasyToLLM converts a fantasy message back into bitchtea's JSONL-stable
// message shape.
func fantasyToLLM(fm fantasy.Message) Message {
	m := Message{Role: string(fm.Role)}
	var text strings.Builder

	for _, part := range fm.Content {
		switch p := part.(type) {
		case fantasy.TextPart:
			text.WriteString(p.Text)
		case *fantasy.TextPart:
			if p != nil {
				text.WriteString(p.Text)
			}
		case fantasy.ToolCallPart:
			m.ToolCalls = append(m.ToolCalls, ToolCall{
				ID:   p.ToolCallID,
				Type: "function",
				Function: FunctionCall{
					Name:      p.ToolName,
					Arguments: p.Input,
				},
			})
		case *fantasy.ToolCallPart:
			if p != nil {
				m.ToolCalls = append(m.ToolCalls, ToolCall{
					ID:   p.ToolCallID,
					Type: "function",
					Function: FunctionCall{
						Name:      p.ToolName,
						Arguments: p.Input,
					},
				})
			}
		case fantasy.ToolResultPart:
			if m.ToolCallID == "" {
				m.ToolCallID = p.ToolCallID
			}
			switch out := p.Output.(type) {
			case fantasy.ToolResultOutputContentText:
				text.WriteString(out.Text)
			case *fantasy.ToolResultOutputContentText:
				if out != nil {
					text.WriteString(out.Text)
				}
			}
		case *fantasy.ToolResultPart:
			if p != nil {
				if m.ToolCallID == "" {
					m.ToolCallID = p.ToolCallID
				}
				switch out := p.Output.(type) {
				case fantasy.ToolResultOutputContentText:
					text.WriteString(out.Text)
				case *fantasy.ToolResultOutputContentText:
					if out != nil {
						text.WriteString(out.Text)
					}
				}
			}
		}
	}

	m.Content = text.String()
	return m
}

// toLLMUsage converts fantasy token usage into bitchtea's public usage shape.
func toLLMUsage(u fantasy.Usage) TokenUsage {
	return TokenUsage{
		InputTokens:         int(u.InputTokens),
		OutputTokens:        int(u.OutputTokens),
		CacheCreationTokens: int(u.CacheCreationTokens),
		CacheReadTokens:     int(u.CacheReadTokens),
	}
}
