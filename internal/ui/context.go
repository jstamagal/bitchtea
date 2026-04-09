package ui

import "strings"

// ContextKind identifies what type of IRC-style routing destination a context is.
type ContextKind uint8

const (
	KindChannel    ContextKind = iota // #channel
	KindSubchannel                    // #channel.sub (inherits parent, writes to own)
	KindDirect                        // persona or nick (DM-style routing)
)

// IRCContext is a named routing destination: a channel, subchannel, or direct target.
// Comparison by value (==) is safe and is the canonical way to check identity.
type IRCContext struct {
	Kind    ContextKind
	Channel string // channel name without '#'; set for KindChannel and KindSubchannel
	Sub     string // subchannel qualifier; set only for KindSubchannel
	Target  string // persona or nick; set only for KindDirect
}

// Label returns the canonical display label for the context.
//
//	KindChannel    →  "#channel"
//	KindSubchannel →  "#channel.sub"
//	KindDirect     →  "target"
func (c IRCContext) Label() string {
	switch c.Kind {
	case KindChannel:
		return "#" + c.Channel
	case KindSubchannel:
		return "#" + c.Channel + "." + c.Sub
	case KindDirect:
		return c.Target
	default:
		return "#main"
	}
}

// Channel constructs a channel context. The name is lowercased and any leading
// '#' is stripped so callers can pass either "general" or "#general".
func Channel(name string) IRCContext {
	name = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(name)), "#")
	if name == "" {
		name = "main"
	}
	return IRCContext{Kind: KindChannel, Channel: name}
}

// Subchannel constructs a subchannel context. Names are lowercased.
func Subchannel(channel, sub string) IRCContext {
	channel = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(channel)), "#")
	sub = strings.ToLower(strings.TrimSpace(sub))
	if channel == "" {
		channel = "main"
	}
	return IRCContext{Kind: KindSubchannel, Channel: channel, Sub: sub}
}

// Direct constructs a direct-target context for a persona or nick.
// The target name is preserved as-is (case sensitive — persona names matter).
func Direct(target string) IRCContext {
	return IRCContext{Kind: KindDirect, Target: strings.TrimSpace(target)}
}

// defaultContext is the initial context when no other context has been set.
var defaultContext = Channel("main")

// FocusManager maintains the ordered list of known contexts and which one is
// currently active. There is always exactly one active context.
type FocusManager struct {
	contexts []IRCContext
	active   int // index into contexts; always in-bounds
}

// NewFocusManager creates a FocusManager with the default main channel active.
func NewFocusManager() *FocusManager {
	return &FocusManager{
		contexts: []IRCContext{defaultContext},
		active:   0,
	}
}

// Active returns the currently focused context.
func (f *FocusManager) Active() IRCContext {
	return f.contexts[f.active]
}

// ActiveLabel returns the display label of the active context.
func (f *FocusManager) ActiveLabel() string {
	return f.Active().Label()
}

// SetFocus makes ctx the active context. If ctx is not yet known it is added.
func (f *FocusManager) SetFocus(ctx IRCContext) {
	for i, c := range f.contexts {
		if c == ctx {
			f.active = i
			return
		}
	}
	f.contexts = append(f.contexts, ctx)
	f.active = len(f.contexts) - 1
}

// Ensure adds ctx to the known list without changing focus.
// Does nothing if ctx is already known.
func (f *FocusManager) Ensure(ctx IRCContext) {
	for _, c := range f.contexts {
		if c == ctx {
			return
		}
	}
	f.contexts = append(f.contexts, ctx)
}

// Remove removes ctx from the known list. If it was active, focus shifts left
// (wraps to the last context if the first was removed). The last remaining
// context cannot be removed; returns false in that case.
func (f *FocusManager) Remove(ctx IRCContext) bool {
	if len(f.contexts) <= 1 {
		return false
	}
	for i, c := range f.contexts {
		if c == ctx {
			f.contexts = append(f.contexts[:i], f.contexts[i+1:]...)
			if f.active >= len(f.contexts) {
				f.active = len(f.contexts) - 1
			}
			return true
		}
	}
	return false
}

// All returns a snapshot of all known contexts in join order.
func (f *FocusManager) All() []IRCContext {
	out := make([]IRCContext, len(f.contexts))
	copy(out, f.contexts)
	return out
}
