package ui

import (
	"fmt"
	"time"
)

// MsgType determines the visual style of a message
type MsgType int

const (
	MsgUser   MsgType = iota // green nick, normal text
	MsgAgent                 // magenta nick, streamed text
	MsgSystem                // yellow *** prefix
	MsgError                 // red *** prefix
	MsgTool                  // cyan tool output
	MsgThink                 // magenta italic thinking
	MsgRaw                   // raw ANSI passthrough (splash art etc)
)

// ChatMessage represents a single line/block in the viewport
type ChatMessage struct {
	Time    time.Time
	Type    MsgType
	Nick    string // sender nick (user/agent name)
	Content string // may contain ANSI codes for raw messages
}

// Format renders a ChatMessage for display in the viewport
func (m ChatMessage) Format() string {
	ts := TimestampStyle.Render(fmt.Sprintf("[%s]", m.Time.Format("15:04")))

	switch m.Type {
	case MsgUser:
		nick := UserNickStyle.Render(fmt.Sprintf("<%s>", m.Nick))
		return fmt.Sprintf(" %s %s %s", ts, nick, m.Content)

	case MsgAgent:
		nick := AgentNickStyle.Render(fmt.Sprintf("<%s>", m.Nick))
		// Render markdown in agent responses
		content := RenderMarkdown(m.Content, 100)
		return fmt.Sprintf(" %s %s %s", ts, nick, content)

	case MsgSystem:
		return fmt.Sprintf(" %s %s %s", ts, SystemMsgStyle.Render("***"), SystemMsgStyle.Render(m.Content))

	case MsgError:
		return fmt.Sprintf(" %s %s %s", ts, ErrorMsgStyle.Render("!!!"), ErrorMsgStyle.Render(m.Content))

	case MsgTool:
		prefix := ToolCallStyle.Render(fmt.Sprintf("  → %s:", m.Nick))
		return fmt.Sprintf(" %s %s %s", ts, prefix, ToolOutputStyle.Render(m.Content))

	case MsgThink:
		return fmt.Sprintf(" %s %s %s", ts, ThinkingStyle.Render("💭"), ThinkingStyle.Render(m.Content))

	case MsgRaw:
		return m.Content

	default:
		return m.Content
	}
}
