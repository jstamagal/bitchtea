package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// MembershipState captures all channel memberships for persistence.
// Channels maps channel key (without '#') to sorted list of persona names.
// Persisted to .bitchtea_membership.json in the session directory.
type MembershipState struct {
	Channels map[string][]string `json:"channels"`
}

// SaveMembership writes membership state to .bitchtea_membership.json in dir.
func SaveMembership(dir string, state MembershipState) error {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create session dir: %w", err)
	}
	if state.Channels == nil {
		state.Channels = map[string][]string{}
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal membership: %w", err)
	}
	path := filepath.Join(dir, ".bitchtea_membership.json")
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("write membership: %w", err)
	}
	return nil
}

// LoadMembership reads membership state from .bitchtea_membership.json in dir.
// Returns a zero-value MembershipState (no error) when the file does not exist.
func LoadMembership(dir string) (MembershipState, error) {
	path := filepath.Join(dir, ".bitchtea_membership.json")
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return MembershipState{}, nil
	}
	if err != nil {
		return MembershipState{}, fmt.Errorf("read membership: %w", err)
	}
	var state MembershipState
	if err := json.Unmarshal(data, &state); err != nil {
		return MembershipState{}, fmt.Errorf("unmarshal membership: %w", err)
	}
	return state, nil
}
