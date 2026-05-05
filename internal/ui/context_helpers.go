package ui

import (
	"github.com/jstamagal/bitchtea/internal/agent"
	"github.com/jstamagal/bitchtea/internal/session"
)

// ircContextToKey converts an IRCContext to the string key used for per-context
// message storage.
func ircContextToKey(ctx IRCContext) string {
	switch ctx.Kind {
	case KindChannel:
		return "#" + ctx.Channel
	case KindDirect:
		return ctx.Target
	default:
		return agent.DefaultContextKey
	}
}

// saveCurrentContextMessages persists any unsaved messages from the current
// context to the session file.
func (m *Model) saveCurrentContextMessages() {
	if m.session == nil {
		return
	}
	ctxKey := ircContextToKey(m.turnContext)
	msgs := m.agent.Messages()
	savedIdx := m.contextSavedIdx[ctxKey]
	for i := savedIdx; i < len(msgs); i++ {
		e := session.EntryFromFantasyWithBootstrap(
			msgs[i],
			i < m.agent.BootstrapMessageCount(),
		)
		e.Context = m.turnContext.Label()
		_ = m.session.Append(e)
	}
	m.contextSavedIdx[ctxKey] = len(msgs)
}
