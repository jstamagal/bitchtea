package llm

import (
	"strings"

	"charm.land/fantasy"
)

// splitForFantasy splits the current transcript into fantasy's current Prompt,
// prior Messages, and the system prompt. The TAIL user message becomes the
// prompt — only if the transcript actually ends with a user turn. If the
// transcript ends with an assistant or tool message (e.g., bootstrap, restored
// session, partial replay), prompt is empty and every message stays in prior
// in original order. System messages are concatenated into systemPrompt and
// passed via fantasy.WithSystemPrompt at the call site.
func splitForFantasy(msgs []Message) (prompt string, prior []fantasy.Message, systemPrompt string) {
	tailUser := -1
	for i := len(msgs) - 1; i >= 0; i-- {
		switch msgs[i].Role {
		case "user":
			tailUser = i
		case "assistant", "tool":
			// transcript ends with non-user turn → no prompt to extract
		default:
			continue
		}
		break
	}
	if tailUser >= 0 {
		prompt = msgs[tailUser].Content
	}

	var systemParts []string
	prior = make([]fantasy.Message, 0, len(msgs))
	for i, m := range msgs {
		if i == tailUser {
			continue
		}

		switch m.Role {
		case "system":
			if m.Content != "" {
				systemParts = append(systemParts, m.Content)
			}
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

	systemPrompt = strings.Join(systemParts, "\n\n")
	return prompt, prior, systemPrompt
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
