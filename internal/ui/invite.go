package ui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/jstamagal/bitchtea/internal/session"
)

// handleInviteCommand invites a persona into a channel.
// Usage: /invite <persona> [#channel]
func handleInviteCommand(m Model, _ string, parts []string) (Model, tea.Cmd) {
	if len(parts) < 2 {
		m.errMsg("Usage: /invite <persona> [#channel]")
		return m, nil
	}

	persona := parts[1]
	var channelKey string

	if len(parts) >= 3 && strings.HasPrefix(parts[2], "#") {
		channelKey = strings.TrimPrefix(parts[2], "#")
	} else {
		ctx := m.focus.Active()
		key, ok := channelKeyFromCtx(ctx)
		if !ok {
			m.errMsg("Cannot /invite in a DM context. Switch to a channel first.")
			return m, nil
		}
		channelKey = key
	}

	if m.membership.IsJoined(channelKey, persona) {
		m.sysMsg(fmt.Sprintf("%s is already in #%s", persona, channelKey))
		return m, nil
	}

	m.membership.Invite(channelKey, persona)
	_ = m.membership.Save(m.config.SessionDir)

	m.addMessage(ChatMessage{
		Time:    time.Now(),
		Type:    MsgSystem,
		Content: fmt.Sprintf("*** %s joined #%s", persona, channelKey),
	})

	catchup := buildChannelCatchup(m.session, "#"+channelKey, 50)
	m.addMessage(ChatMessage{
		Time:    time.Now(),
		Type:    MsgSystem,
		Content: catchup,
	})
	m.refreshViewport()
	return m, nil
}

// handleKickCommand removes a persona from the current channel.
// Usage: /kick <persona>
func handleKickCommand(m Model, _ string, parts []string) (Model, tea.Cmd) {
	if len(parts) < 2 {
		m.errMsg("Usage: /kick <persona>")
		return m, nil
	}

	persona := parts[1]
	ctx := m.focus.Active()
	channelKey, ok := channelKeyFromCtx(ctx)
	if !ok {
		channelKey = "main"
	}

	if !m.membership.IsJoined(channelKey, persona) {
		m.errMsg(fmt.Sprintf("%s is not in #%s", persona, channelKey))
		return m, nil
	}

	m.membership.Part(channelKey, persona)
	_ = m.membership.Save(m.config.SessionDir)

	m.sysMsg(fmt.Sprintf("*** %s has been kicked from #%s", persona, channelKey))
	return m, nil
}

// buildChannelCatchup builds a catch-up summary from session history for a channel.
// It returns the last maxLines non-tool messages from the given channel context.
func buildChannelCatchup(sess *session.Session, channel string, maxLines int) string {
	if sess == nil {
		return "Catch-up: no session history available."
	}

	var filtered []session.Entry
	for _, e := range sess.Entries {
		if e.Context != channel {
			continue
		}
		if e.Role == "tool" || e.ToolCallID != "" {
			continue
		}
		filtered = append(filtered, e)
	}

	if len(filtered) == 0 {
		return fmt.Sprintf("Catch-up for %s: no prior conversation found.", channel)
	}

	// Take last maxLines entries
	start := 0
	if len(filtered) > maxLines {
		start = len(filtered) - maxLines
	}
	tail := filtered[start:]

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Catch-up for %s (%d messages):\n", channel, len(tail)))
	for _, e := range tail {
		sb.WriteString(fmt.Sprintf("  [%s] %s\n", e.Role, e.Content))
	}
	return strings.TrimRight(sb.String(), "\n")
}
