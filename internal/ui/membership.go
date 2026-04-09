package ui

import (
	"sort"
	"strings"

	"github.com/jstamagal/bitchtea/internal/session"
)

// MembershipManager tracks which personas are joined in which channels.
// Personas must be explicitly invited; they don't see channels by default.
type MembershipManager struct {
	// channels maps channel key (without '#') → set of persona names
	channels map[string]map[string]struct{}
}

// NewMembershipManager creates an empty MembershipManager.
func NewMembershipManager() *MembershipManager {
	return &MembershipManager{
		channels: make(map[string]map[string]struct{}),
	}
}

// Invite adds persona to channel. Returns true if newly added, false if already present.
func (m *MembershipManager) Invite(channelKey, persona string) bool {
	channelKey = normalizeMembershipKey(channelKey)
	persona = strings.TrimSpace(persona)
	if channelKey == "" || persona == "" {
		return false
	}
	if m.channels[channelKey] == nil {
		m.channels[channelKey] = make(map[string]struct{})
	}
	if _, exists := m.channels[channelKey][persona]; exists {
		return false
	}
	m.channels[channelKey][persona] = struct{}{}
	return true
}

// Part removes persona from channel. Returns true if they were present.
func (m *MembershipManager) Part(channelKey, persona string) bool {
	channelKey = normalizeMembershipKey(channelKey)
	persona = strings.TrimSpace(persona)
	members, ok := m.channels[channelKey]
	if !ok {
		return false
	}
	if _, present := members[persona]; !present {
		return false
	}
	delete(members, persona)
	if len(members) == 0 {
		delete(m.channels, channelKey)
	}
	return true
}

// Members returns the sorted list of personas joined to the channel.
func (m *MembershipManager) Members(channelKey string) []string {
	channelKey = normalizeMembershipKey(channelKey)
	members, ok := m.channels[channelKey]
	if !ok {
		return nil
	}
	names := make([]string, 0, len(members))
	for name := range members {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// IsJoined reports whether persona is a member of channel.
func (m *MembershipManager) IsJoined(channelKey, persona string) bool {
	channelKey = normalizeMembershipKey(channelKey)
	persona = strings.TrimSpace(persona)
	members, ok := m.channels[channelKey]
	if !ok {
		return false
	}
	_, joined := members[persona]
	return joined
}

// ToState converts to a serializable MembershipState.
func (m *MembershipManager) ToState() session.MembershipState {
	channels := make(map[string][]string, len(m.channels))
	for ch, members := range m.channels {
		names := make([]string, 0, len(members))
		for name := range members {
			names = append(names, name)
		}
		sort.Strings(names)
		channels[ch] = names
	}
	return session.MembershipState{Channels: channels}
}

// RestoreState replaces state from a persisted MembershipState.
func (m *MembershipManager) RestoreState(state session.MembershipState) {
	m.channels = make(map[string]map[string]struct{}, len(state.Channels))
	for ch, personas := range state.Channels {
		if len(personas) == 0 {
			continue
		}
		m.channels[ch] = make(map[string]struct{}, len(personas))
		for _, p := range personas {
			m.channels[ch][p] = struct{}{}
		}
	}
}

// Save persists membership state to dir.
func (m *MembershipManager) Save(dir string) error {
	return session.SaveMembership(dir, m.ToState())
}

// LoadMembershipManager reads saved membership state from dir.
// Falls back to an empty manager if no file exists.
func LoadMembershipManager(dir string) *MembershipManager {
	mgr := NewMembershipManager()
	state, err := session.LoadMembership(dir)
	if err == nil {
		mgr.RestoreState(state)
	}
	return mgr
}

// channelKeyFromCtx extracts the membership key from an IRCContext.
// Only channel and subchannel contexts are valid; returns ("", false) for direct.
func channelKeyFromCtx(ctx IRCContext) (string, bool) {
	switch ctx.Kind {
	case KindChannel:
		return ctx.Channel, true
	case KindSubchannel:
		return ctx.Channel + "." + ctx.Sub, true
	default:
		return "", false
	}
}

// normalizeMembershipKey strips leading '#' and lowercases.
func normalizeMembershipKey(key string) string {
	return strings.TrimPrefix(strings.ToLower(strings.TrimSpace(key)), "#")
}
