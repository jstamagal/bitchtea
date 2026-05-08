package agent

import "charm.land/fantasy"

// ContextKey returns the current context key.
func (a *Agent) ContextKey() ContextKey {
	return a.currentContext
}

// SetContext switches the agent's active message history to the given context.
// The current context's messages are stored back in the map, and the target
// context's messages become active.
func (a *Agent) SetContext(key ContextKey) {
	if key == a.currentContext {
		return
	}
	a.contextMsgs[a.currentContext] = a.messages
	a.currentContext = key
	if msgs, ok := a.contextMsgs[key]; ok {
		a.messages = msgs
	} else {
		a.messages = append([]fantasy.Message(nil), a.messages[:a.bootstrapMsgCount]...)
		a.contextMsgs[key] = a.messages
	}
}

// InitContext initializes a new context with the bootstrap prefix (system
// prompt, context files, persona). If the context already exists, this is a
// no-op. Does NOT switch to the context.
func (a *Agent) InitContext(key ContextKey) {
	if _, ok := a.contextMsgs[key]; ok {
		return
	}
	bootstrap := append([]fantasy.Message(nil), a.messages[:a.bootstrapMsgCount]...)
	a.contextMsgs[key] = bootstrap
	a.contextSavedIdx[key] = 0
}

// SavedIdx returns the session-save watermark for the given context.
func (a *Agent) SavedIdx(key ContextKey) int {
	return a.contextSavedIdx[key]
}

// SetSavedIdx updates the session-save watermark for the given context.
func (a *Agent) SetSavedIdx(key ContextKey, idx int) {
	a.contextSavedIdx[key] = idx
}

// InjectNoteInContext adds a synthetic context note to a specific context's
// message history without switching to it. If the context doesn't exist in
// the map, the note is appended to the current context instead.
//
// The active a.messages slice header is kept in sync with contextMsgs when the
// target key matches currentContext — without this, an append() that grows
// the backing array leaves a.messages pointing at the old (shorter) array
// while contextMsgs holds the new one, so the next streamed turn would not
// see the injected note. bt-wire.4's /invite path is the first caller and
// regression-tests this exact case.
func (a *Agent) InjectNoteInContext(key ContextKey, note string) {
	user := newUserMessage(note)
	ack := newAssistantMessage("Understood.")
	if msgs, ok := a.contextMsgs[key]; ok {
		a.contextMsgs[key] = append(msgs, user, ack)
		if key == a.currentContext {
			a.messages = a.contextMsgs[key]
		}
		return
	}
	// Unknown context: append to the active history and keep the map in sync.
	a.messages = append(a.messages, user, ack)
	a.contextMsgs[a.currentContext] = a.messages
}

// RestoreContextMessages restores messages for a specific context without
// switching to it. The context is created if it doesn't exist. Bootstrap
// messages are forced to match the current system prompt, same as
// RestoreMessages.
//
// bt-wire.10: when a non-default context is restored from a saved session, any
// "Context memory for X:" exchange already present in those messages should be
// recorded in injectedPaths so SetScope(X) on the eventual context switch does
// not double-inject. This is additive — it does not clear markers tracked for
// other contexts.
func (a *Agent) RestoreContextMessages(key ContextKey, messages []fantasy.Message) {
	msgs := append([]fantasy.Message(nil), messages...)
	systemPrompt := buildSystemPrompt(a.config)
	if len(msgs) == 0 || msgs[0].Role != fantasy.MessageRoleSystem {
		msgs = append([]fantasy.Message{newSystemMessage(systemPrompt)}, msgs...)
	} else {
		msgs[0] = newSystemMessage(systemPrompt)
	}
	a.contextMsgs[key] = msgs
	a.scanInjectedPathsFromMessages(msgs)
}
