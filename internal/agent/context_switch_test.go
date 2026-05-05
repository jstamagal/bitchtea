package agent

import (
	"strings"
	"testing"

	"github.com/jstamagal/bitchtea/internal/config"
)

// TestInjectNoteInContextUpdatesActiveSlice exercises the slice-header
// regression that bt-wire.4's /invite path uncovered: when InjectNoteInContext
// is called with the agent's currentContext key, both contextMsgs[key] and
// a.messages must reflect the appended note. Before the fix, append() on the
// map entry could grow the backing array, so a.messages was left pointing at
// the old (shorter) array — and the next streamed turn never saw the note.
func TestInjectNoteInContextUpdatesActiveSlice(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.WorkDir = t.TempDir()
	cfg.SessionDir = t.TempDir()

	a := NewAgentWithStreamer(&cfg, &fakeStreamer{})
	before := len(a.Messages())
	if a.ContextKey() != DefaultContextKey {
		t.Fatalf("expected currentContext=%q, got %q", DefaultContextKey, a.ContextKey())
	}

	a.InjectNoteInContext(DefaultContextKey, "alice has joined the room")

	got := a.Messages()
	if len(got) != before+2 {
		t.Fatalf("expected active slice to grow by 2 (user+ack), got %d -> %d", before, len(got))
	}

	// The active slice must include the injected note.
	transcript := messageText(got[len(got)-2]) + "\n" + messageText(got[len(got)-1])
	if !strings.Contains(transcript, "alice has joined the room") {
		t.Fatalf("active slice missing injected note; got: %q", transcript)
	}

	// The map entry for the same key must match the active slice exactly so
	// a later SetContext()/save round-trip preserves the note.
	stored := a.contextMsgs[DefaultContextKey]
	if len(stored) != len(got) {
		t.Fatalf("contextMsgs[%q] out of sync: map=%d active=%d", DefaultContextKey, len(stored), len(got))
	}
}

// TestInjectNoteInContextOtherKeyDoesNotTouchActive verifies the symmetric
// case: injecting into a different context's key must NOT mutate the active
// slice. This is what makes /invite #engineering safe when the user is
// focused on #ops.
func TestInjectNoteInContextOtherKeyDoesNotTouchActive(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.WorkDir = t.TempDir()
	cfg.SessionDir = t.TempDir()

	a := NewAgentWithStreamer(&cfg, &fakeStreamer{})
	a.InitContext("#engineering")
	beforeActive := len(a.Messages())
	beforeOther := len(a.contextMsgs["#engineering"])

	a.InjectNoteInContext("#engineering", "oncall has joined")

	if got := len(a.Messages()); got != beforeActive {
		t.Errorf("active slice grew unexpectedly: %d -> %d", beforeActive, got)
	}
	if got := len(a.contextMsgs["#engineering"]); got != beforeOther+2 {
		t.Errorf("expected #engineering slice +2, got %d -> %d", beforeOther, got)
	}
}

// TestInjectNoteInContextUnknownKeyAppendsToActive covers the fallback when
// the requested key doesn't exist in the map yet. The note lands in the
// current context AND the map is kept in sync so a subsequent SetContext
// round-trip doesn't drop it.
func TestInjectNoteInContextUnknownKeyAppendsToActive(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.WorkDir = t.TempDir()
	cfg.SessionDir = t.TempDir()

	a := NewAgentWithStreamer(&cfg, &fakeStreamer{})
	before := len(a.Messages())

	a.InjectNoteInContext("#never-initialized", "fallback note")

	got := a.Messages()
	if len(got) != before+2 {
		t.Fatalf("expected active slice +2, got %d -> %d", before, len(got))
	}
	stored := a.contextMsgs[a.ContextKey()]
	if len(stored) != len(got) {
		t.Fatalf("active context map entry out of sync after fallback: map=%d active=%d", len(stored), len(got))
	}
}
