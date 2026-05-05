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

	// Inject persona awareness into the agent's per-channel history so the
	// next turn streams with knowledge that <persona> has joined. Routing-
	// model decision: option (c) from bt-wire.4 — a "<persona> joined" user
	// message in the channel's context, mirroring how /invite reads in IRC.
	// See docs/commands.md "/invite" trace for rationale.
	if m.agent != nil {
		ctxKey := "#" + channelKey
		m.agent.InitContext(ctxKey)
		members := m.membership.Members(channelKey)
		m.agent.InjectNoteInContext(ctxKey, buildPersonaJoinNote(persona, channelKey, members))
	}

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

	// Mirror of /invite: tell the agent the persona has left this channel so
	// future turns in #<channel> stop treating them as a participant. The
	// note is scoped to the channel's per-context history; other contexts
	// are unaffected.
	if m.agent != nil {
		ctxKey := "#" + channelKey
		m.agent.InitContext(ctxKey)
		members := m.membership.Members(channelKey)
		m.agent.InjectNoteInContext(ctxKey, buildPersonaPartNote(persona, channelKey, members))
	}

	m.sysMsg(fmt.Sprintf("*** %s has been kicked from #%s", persona, channelKey))
	return m, nil
}

// buildPersonaJoinNote crafts the user-facing context note injected into the
// agent's per-channel message history when /invite adds a persona. Members is
// the post-invite member list (already includes the new persona). The note is
// the only signal the agent receives about membership — it must be explicit.
func buildPersonaJoinNote(persona, channelKey string, members []string) string {
	memberList := strings.Join(members, ", ")
	if memberList == "" {
		memberList = persona
	}
	return fmt.Sprintf(
		"[membership update] '%s' has joined #%s as a participant. Treat '%s' as another collaborator in this room — future user messages may direct work to '%s', address them by name, or expect their voice in the conversation. Current members of #%s: %s.",
		persona, channelKey, persona, persona, channelKey, memberList,
	)
}

// buildPersonaPartNote crafts the user-facing context note injected when /kick
// removes a persona. Members is the post-kick list (already excludes the kicked
// persona); when empty we say so explicitly so the agent doesn't keep modeling
// invisible participants.
func buildPersonaPartNote(persona, channelKey string, members []string) string {
	memberList := strings.Join(members, ", ")
	if memberList == "" {
		memberList = "no other personas"
	}
	return fmt.Sprintf(
		"[membership update] '%s' has been removed from #%s and is no longer a participant in this room. Stop addressing '%s' or expecting their voice here. Remaining members of #%s: %s.",
		persona, channelKey, persona, channelKey, memberList,
	)
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
