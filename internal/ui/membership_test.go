package ui

import (
	"testing"
)

func TestMembershipManager_Invite(t *testing.T) {
	m := NewMembershipManager()
	if !m.Invite("main", "reviewer") {
		t.Error("first invite should return true")
	}
	if !m.IsJoined("main", "reviewer") {
		t.Error("expected reviewer to be joined in main")
	}
}

func TestMembershipManager_Invite_idempotent(t *testing.T) {
	m := NewMembershipManager()
	m.Invite("main", "reviewer")
	if m.Invite("main", "reviewer") {
		t.Error("second invite should return false (already present)")
	}
	if len(m.Members("main")) != 1 {
		t.Errorf("expected 1 member, got %d", len(m.Members("main")))
	}
}

func TestMembershipManager_Invite_hashStripped(t *testing.T) {
	m := NewMembershipManager()
	m.Invite("#main", "reviewer")
	if !m.IsJoined("main", "reviewer") {
		t.Error("key should be normalized (# stripped)")
	}
	if !m.IsJoined("#main", "reviewer") {
		t.Error("IsJoined should accept # prefix too")
	}
}

func TestMembershipManager_Part(t *testing.T) {
	m := NewMembershipManager()
	m.Invite("ops", "oncall")
	m.Invite("ops", "backup")

	if !m.Part("ops", "oncall") {
		t.Error("Part should return true when persona was present")
	}
	if m.IsJoined("ops", "oncall") {
		t.Error("oncall should be gone after Part")
	}
	if !m.IsJoined("ops", "backup") {
		t.Error("backup should still be joined")
	}
}

func TestMembershipManager_Part_notPresent(t *testing.T) {
	m := NewMembershipManager()
	if m.Part("ops", "ghost") {
		t.Error("Part of non-member should return false")
	}
}

func TestMembershipManager_Part_cleansUpEmptyChannel(t *testing.T) {
	m := NewMembershipManager()
	m.Invite("ops", "solo")
	m.Part("ops", "solo")
	if len(m.Members("ops")) != 0 {
		t.Errorf("expected empty member list, got %v", m.Members("ops"))
	}
}

func TestMembershipManager_Members_sorted(t *testing.T) {
	m := NewMembershipManager()
	m.Invite("main", "zebra")
	m.Invite("main", "alpha")
	m.Invite("main", "middle")

	members := m.Members("main")
	if len(members) != 3 {
		t.Fatalf("expected 3 members, got %d", len(members))
	}
	if members[0] != "alpha" || members[1] != "middle" || members[2] != "zebra" {
		t.Errorf("members not sorted: %v", members)
	}
}

func TestMembershipManager_Members_unknownChannel(t *testing.T) {
	m := NewMembershipManager()
	if m.Members("nope") != nil {
		t.Error("expected nil for unknown channel")
	}
}

func TestMembershipManager_ToState_RestoreState_roundTrip(t *testing.T) {
	m := NewMembershipManager()
	m.Invite("main", "reviewer")
	m.Invite("main", "debugger")
	m.Invite("ops", "oncall")

	state := m.ToState()

	m2 := NewMembershipManager()
	m2.RestoreState(state)

	if !m2.IsJoined("main", "reviewer") {
		t.Error("reviewer should be joined in main after restore")
	}
	if !m2.IsJoined("main", "debugger") {
		t.Error("debugger should be joined in main after restore")
	}
	if !m2.IsJoined("ops", "oncall") {
		t.Error("oncall should be joined in ops after restore")
	}
	if m2.IsJoined("main", "ghost") {
		t.Error("ghost should not be joined")
	}
}

func TestMembershipManager_SaveAndLoad(t *testing.T) {
	dir := t.TempDir()

	m := NewMembershipManager()
	m.Invite("main", "reviewer")
	m.Invite("ops", "oncall")

	if err := m.Save(dir); err != nil {
		t.Fatalf("save: %v", err)
	}

	restored := LoadMembershipManager(dir)
	if !restored.IsJoined("main", "reviewer") {
		t.Error("reviewer should be in main after load")
	}
	if !restored.IsJoined("ops", "oncall") {
		t.Error("oncall should be in ops after load")
	}
}

func TestLoadMembershipManager_noFile(t *testing.T) {
	dir := t.TempDir()
	m := LoadMembershipManager(dir)
	if m == nil {
		t.Fatal("expected non-nil manager")
	}
	if len(m.Members("main")) != 0 {
		t.Error("expected empty membership when no file")
	}
}

func TestChannelKeyFromCtx(t *testing.T) {
	tests := []struct {
		ctx      IRCContext
		wantKey  string
		wantBool bool
	}{
		{Channel("main"), "main", true},
		{Channel("#ops"), "ops", true},
		{Subchannel("hub", "web"), "hub.web", true},
		{Direct("buddy"), "", false},
	}
	for _, tt := range tests {
		key, ok := channelKeyFromCtx(tt.ctx)
		if ok != tt.wantBool {
			t.Errorf("channelKeyFromCtx(%v) ok=%v, want %v", tt.ctx.Label(), ok, tt.wantBool)
		}
		if key != tt.wantKey {
			t.Errorf("channelKeyFromCtx(%v) key=%q, want %q", tt.ctx.Label(), key, tt.wantKey)
		}
	}
}
