package ui

import (
	"testing"
)

// --- IRCContext constructors and Label ---

func TestChannel(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"general", "#general"},
		{"#general", "#general"},
		{"#General", "#general"},
		{"  #MAIN  ", "#main"},
		{"", "#main"},
	}
	for _, tt := range tests {
		c := Channel(tt.input)
		if c.Kind != KindChannel {
			t.Errorf("Channel(%q).Kind = %d, want KindChannel", tt.input, c.Kind)
		}
		if got := c.Label(); got != tt.want {
			t.Errorf("Channel(%q).Label() = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestSubchannel(t *testing.T) {
	c := Subchannel("#cornhub", "website")
	if c.Kind != KindSubchannel {
		t.Errorf("Subchannel kind = %d, want KindSubchannel", c.Kind)
	}
	if got := c.Label(); got != "#cornhub.website" {
		t.Errorf("Subchannel label = %q, want %q", got, "#cornhub.website")
	}
	if c.Channel != "cornhub" {
		t.Errorf("Channel field = %q, want %q", c.Channel, "cornhub")
	}
	if c.Sub != "website" {
		t.Errorf("Sub field = %q, want %q", c.Sub, "website")
	}
}

func TestDirect(t *testing.T) {
	c := Direct("  coding-buddy  ")
	if c.Kind != KindDirect {
		t.Errorf("Direct kind = %d, want KindDirect", c.Kind)
	}
	if got := c.Label(); got != "coding-buddy" {
		t.Errorf("Direct label = %q, want %q", got, "coding-buddy")
	}
}

// --- FocusManager ---

func TestNewFocusManager_defaultIsMainChannel(t *testing.T) {
	f := NewFocusManager()
	if f.ActiveLabel() != "#main" {
		t.Errorf("default active = %q, want #main", f.ActiveLabel())
	}
	if len(f.All()) != 1 {
		t.Errorf("initial context count = %d, want 1", len(f.All()))
	}
}

func TestFocusManager_SetFocus_existingContext(t *testing.T) {
	f := NewFocusManager()
	f.SetFocus(Channel("code"))
	f.SetFocus(Channel("main")) // should switch back, not duplicate
	if f.ActiveLabel() != "#main" {
		t.Errorf("active = %q, want #main", f.ActiveLabel())
	}
	if len(f.All()) != 2 {
		t.Errorf("context count = %d, want 2", len(f.All()))
	}
}

func TestFocusManager_SetFocus_newContext(t *testing.T) {
	f := NewFocusManager()
	f.SetFocus(Channel("code"))
	if f.ActiveLabel() != "#code" {
		t.Errorf("active = %q, want #code", f.ActiveLabel())
	}
	if len(f.All()) != 2 {
		t.Errorf("context count = %d, want 2", len(f.All()))
	}
}

func TestFocusManager_Ensure_doesNotChangeFocus(t *testing.T) {
	f := NewFocusManager()
	f.Ensure(Channel("lurk"))
	if f.ActiveLabel() != "#main" {
		t.Errorf("focus changed unexpectedly: %q", f.ActiveLabel())
	}
	if len(f.All()) != 2 {
		t.Errorf("context count = %d, want 2", len(f.All()))
	}
	// Ensure again is idempotent
	f.Ensure(Channel("lurk"))
	if len(f.All()) != 2 {
		t.Errorf("context count after duplicate Ensure = %d, want 2", len(f.All()))
	}
}

func TestFocusManager_Remove_shiftsFocus(t *testing.T) {
	f := NewFocusManager()
	f.SetFocus(Channel("code"))
	f.SetFocus(Channel("ops"))
	// Active is #ops (index 2). Remove #ops → focus shifts to last = #code.
	ok := f.Remove(Channel("ops"))
	if !ok {
		t.Fatal("Remove returned false unexpectedly")
	}
	if f.ActiveLabel() != "#code" {
		t.Errorf("after remove active = %q, want #code", f.ActiveLabel())
	}
	if len(f.All()) != 2 {
		t.Errorf("context count = %d, want 2", len(f.All()))
	}
}

func TestFocusManager_Remove_lastContextRefused(t *testing.T) {
	f := NewFocusManager()
	ok := f.Remove(Channel("main"))
	if ok {
		t.Error("Remove of last context should return false")
	}
	if len(f.All()) != 1 {
		t.Errorf("context count = %d, want 1", len(f.All()))
	}
}

func TestFocusManager_Remove_unknownContext(t *testing.T) {
	f := NewFocusManager()
	f.Ensure(Channel("code"))
	ok := f.Remove(Channel("nope"))
	if ok {
		t.Error("Remove of unknown context should return false")
	}
}

func TestFocusManager_DirectAndSubchannel(t *testing.T) {
	f := NewFocusManager()
	f.SetFocus(Direct("coding-buddy"))
	if f.ActiveLabel() != "coding-buddy" {
		t.Errorf("direct active = %q, want coding-buddy", f.ActiveLabel())
	}
	f.SetFocus(Subchannel("cornhub", "website"))
	if f.ActiveLabel() != "#cornhub.website" {
		t.Errorf("subchannel active = %q, want #cornhub.website", f.ActiveLabel())
	}
}

func TestFocusManager_All_isSnapshot(t *testing.T) {
	f := NewFocusManager()
	snap := f.All()
	f.SetFocus(Channel("code"))
	if len(snap) != 1 {
		t.Errorf("snapshot was mutated: len = %d, want 1", len(snap))
	}
}
