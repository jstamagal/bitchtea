package agent

import (
	"strings"

	"charm.land/fantasy"

	"github.com/jstamagal/bitchtea/internal/llm"
)

// msgText returns the concatenated text projection of a fantasy.Message —
// the same shape that the legacy llm.Message.Content field carried. Tests
// use this so assertions on `msg.Content` style strings keep reading
// naturally after the Phase 3 swap.
func msgText(m fantasy.Message) string {
	var sb strings.Builder
	for _, part := range m.Content {
		switch p := part.(type) {
		case fantasy.TextPart:
			sb.WriteString(p.Text)
		case *fantasy.TextPart:
			if p != nil {
				sb.WriteString(p.Text)
			}
		case fantasy.ToolResultPart:
			if t, ok := p.Output.(fantasy.ToolResultOutputContentText); ok {
				sb.WriteString(t.Text)
			} else if t, ok := p.Output.(*fantasy.ToolResultOutputContentText); ok && t != nil {
				sb.WriteString(t.Text)
			}
		case *fantasy.ToolResultPart:
			if p == nil {
				continue
			}
			if t, ok := p.Output.(fantasy.ToolResultOutputContentText); ok {
				sb.WriteString(t.Text)
			} else if t, ok := p.Output.(*fantasy.ToolResultOutputContentText); ok && t != nil {
				sb.WriteString(t.Text)
			}
		}
	}
	return sb.String()
}

// msgToolCalls extracts the ordered list of ToolCallParts from a
// fantasy.Message as the legacy []llm.ToolCall shape — convenient for tests
// asserting "the assistant message carries a single read call to ID X".
func msgToolCalls(m fantasy.Message) []llm.ToolCall {
	var out []llm.ToolCall
	for _, part := range m.Content {
		switch p := part.(type) {
		case fantasy.ToolCallPart:
			out = append(out, llm.ToolCall{
				ID:   p.ToolCallID,
				Type: "function",
				Function: llm.FunctionCall{
					Name:      p.ToolName,
					Arguments: p.Input,
				},
			})
		case *fantasy.ToolCallPart:
			if p == nil {
				continue
			}
			out = append(out, llm.ToolCall{
				ID:   p.ToolCallID,
				Type: "function",
				Function: llm.FunctionCall{
					Name:      p.ToolName,
					Arguments: p.Input,
				},
			})
		}
	}
	return out
}

// msgToolCallID returns the first ToolResultPart's ToolCallID for a tool
// message, mirroring the legacy llm.Message.ToolCallID field.
func msgToolCallID(m fantasy.Message) string {
	for _, part := range m.Content {
		switch p := part.(type) {
		case fantasy.ToolResultPart:
			return p.ToolCallID
		case *fantasy.ToolResultPart:
			if p != nil {
				return p.ToolCallID
			}
		}
	}
	return ""
}

// fantasyTextMessage builds a single-text-part fantasy.Message with the
// given role. Test counterpart to the unexported newSystemMessage /
// newUserMessage / newAssistantMessage helpers in agent.go.
func fantasyTextMessage(role, text string) fantasy.Message {
	return fantasy.Message{
		Role:    fantasy.MessageRole(role),
		Content: []fantasy.MessagePart{fantasy.TextPart{Text: text}},
	}
}

// fantasyAssistantWithToolCall builds an assistant fantasy.Message with a
// single text part followed by a single tool call.
func fantasyAssistantWithToolCall(text, callID, toolName, args string) fantasy.Message {
	parts := []fantasy.MessagePart{}
	if text != "" {
		parts = append(parts, fantasy.TextPart{Text: text})
	}
	parts = append(parts, fantasy.ToolCallPart{
		ToolCallID: callID,
		ToolName:   toolName,
		Input:      args,
	})
	return fantasy.Message{Role: fantasy.MessageRoleAssistant, Content: parts}
}

// fantasyToolResult builds a tool-role fantasy.Message wrapping a single
// text tool-result part.
func fantasyToolResult(callID, text string) fantasy.Message {
	return fantasy.Message{
		Role: fantasy.MessageRoleTool,
		Content: []fantasy.MessagePart{fantasy.ToolResultPart{
			ToolCallID: callID,
			Output:     fantasy.ToolResultOutputContentText{Text: text},
		}},
	}
}
