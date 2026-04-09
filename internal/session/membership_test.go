package session

import (
	"testing"
)

func TestSaveMembership_roundTrip(t *testing.T) {
	dir := t.TempDir()

	state := MembershipState{
		Channels: map[string][]string{
			"main": {"debugger", "reviewer"},
			"ops":  {"oncall"},
		},
	}

	if err := SaveMembership(dir, state); err != nil {
		t.Fatalf("save: %v", err)
	}

	got, err := LoadMembership(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if len(got.Channels) != 2 {
		t.Fatalf("expected 2 channels, got %d", len(got.Channels))
	}
	if len(got.Channels["main"]) != 2 {
		t.Errorf("expected 2 members in main, got %d", len(got.Channels["main"]))
	}
	if len(got.Channels["ops"]) != 1 {
		t.Errorf("expected 1 member in ops, got %d", len(got.Channels["ops"]))
	}
}

func TestLoadMembership_missingFileReturnsEmpty(t *testing.T) {
	dir := t.TempDir()

	state, err := LoadMembership(dir)
	if err != nil {
		t.Fatalf("expected no error for missing file, got: %v", err)
	}
	if len(state.Channels) != 0 {
		t.Errorf("expected empty channels, got %+v", state.Channels)
	}
}

func TestSaveMembership_nilChannelsAllowed(t *testing.T) {
	dir := t.TempDir()

	if err := SaveMembership(dir, MembershipState{}); err != nil {
		t.Fatalf("save nil channels: %v", err)
	}

	got, err := LoadMembership(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got.Channels == nil {
		t.Error("expected non-nil Channels map after load")
	}
}
