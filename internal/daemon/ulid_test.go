package daemon

import (
	"strings"
	"testing"
	"time"
)

func TestNewULIDLength(t *testing.T) {
	id := NewULID()
	if len(id) != 26 {
		t.Fatalf("ULID length = %d, want 26 (got %q)", len(id), id)
	}
}

func TestNewULIDAlphabet(t *testing.T) {
	id := NewULID()
	for _, c := range id {
		if !strings.ContainsRune(ulidEncoding, c) {
			t.Fatalf("ULID %q contains non-Crockford char %q", id, c)
		}
	}
}

func TestULIDsAreOrderedByTime(t *testing.T) {
	earlier := newULIDAt(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	later := newULIDAt(time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC))
	if earlier[:10] >= later[:10] {
		t.Fatalf("expected earlier ts prefix < later: %q vs %q", earlier, later)
	}
}
