package jobs

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/jstamagal/bitchtea/internal/daemon"
)

// TestHandleUnknownKindReturnsNoHandlerError verifies the dispatch table
// rejects unknown kinds with the scaffold-compatible "no handler" error so
// callers grepping the sentinel keep working through the wiring change.
func TestHandleUnknownKindReturnsNoHandlerError(t *testing.T) {
	job := daemon.Job{
		Kind:        "totally_unknown_thing",
		Args:        json.RawMessage(`{}`),
		WorkDir:     t.TempDir(),
		SubmittedAt: time.Now().UTC(),
	}
	res := Handle(context.Background(), job)
	if res.Success {
		t.Fatalf("unknown kind: want success=false, got success=true: %+v", res)
	}
	if !strings.Contains(res.Error, "no handler") {
		t.Fatalf("unknown kind: want error containing 'no handler', got %q", res.Error)
	}
	if res.Kind != "totally_unknown_thing" {
		t.Fatalf("unknown kind: want Kind=totally_unknown_thing, got %q", res.Kind)
	}
	if res.StartedAt.IsZero() || res.FinishedAt.IsZero() {
		t.Fatalf("Handle should fill StartedAt/FinishedAt: %+v", res)
	}
}

// TestHandleFillsTimestamps verifies that even when a handler returns a
// Result with zero timestamps, Handle backfills them so the daemon's
// failed/done envelopes always carry a wall-clock window.
func TestHandleFillsTimestamps(t *testing.T) {
	job := daemon.Job{Kind: "totally_unknown_thing"}
	res := Handle(context.Background(), job)
	if res.StartedAt.After(res.FinishedAt) {
		t.Fatalf("StartedAt %v should not be after FinishedAt %v", res.StartedAt, res.FinishedAt)
	}
}
