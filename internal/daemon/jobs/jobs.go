// Package jobs implements concrete daemon job handlers wired into the
// dispatch path in internal/daemon/run.go. Each handler is a pure function
// of (ctx, daemon.Job) -> daemon.Result; the daemon main loop is responsible
// for moving the envelope to done/ or failed/ based on the returned Result.
//
// Per the acyclic dep graph (see CLAUDE.md), this package may import
// internal/session, internal/memory, and internal/config — but NOT
// internal/agent, internal/llm, or internal/ui. Handlers re-derive whatever
// in-process state they need from the on-disk envelope alone.
//
// Acceptance contract for any handler added here (bt-p7-session-jobs):
//   - Idempotent: running the same envelope twice produces the same final
//     on-disk state.
//   - Bounded: the handler installs its own context.WithTimeout so a single
//     stuck job cannot block the daemon's drain budget.
//   - Cancellation-aware: ctx.Err() is checked at every I/O boundary so a
//     SIGTERM-driven shutdown aborts cleanly.
package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jstamagal/bitchtea/internal/daemon"
)

// Handler is the dispatch function signature. Implementations live in
// sibling files (checkpoint.go, memory_consolidate.go) and are wired into
// the registry by Handle() below.
type Handler func(ctx context.Context, job daemon.Job) daemon.Result

// Kind constants mirror the docs/phase-7-process-model.md taxonomy. They
// are duplicated here (rather than in internal/daemon) because only the
// jobs package needs to know the concrete vocabulary; the daemon scaffold
// stays kind-agnostic.
const (
	KindSessionCheckpoint  = "session-checkpoint"
	KindMemoryConsolidate  = "memory-consolidate"
	KindSessionStitch      = "session-stitch"
)

// Handle dispatches a job envelope to the registered handler for its kind.
// Unknown kinds return a success=false Result whose Error matches the
// scaffold's "no handler registered" sentinel so older callers that grep
// for that string keep working.
//
// Handle never panics on a valid envelope; handlers themselves are expected
// to recover and return an error Result if they hit something unrecoverable.
func Handle(ctx context.Context, job daemon.Job) daemon.Result {
	started := time.Now().UTC()
	h, ok := registry[job.Kind]
	if !ok {
		return daemon.Result{
			Success:    false,
			Kind:       job.Kind,
			Error:      fmt.Sprintf("no handler registered for kind=%q", job.Kind),
			StartedAt:  started,
			FinishedAt: time.Now().UTC(),
		}
	}
	res := h(ctx, job)
	if res.Kind == "" {
		res.Kind = job.Kind
	}
	if res.StartedAt.IsZero() {
		res.StartedAt = started
	}
	if res.FinishedAt.IsZero() {
		res.FinishedAt = time.Now().UTC()
	}
	return res
}

// registry is keyed by job.Kind. Add new handlers here when you implement
// them; do not export the map — callers should go through Handle().
var registry = map[string]Handler{
	KindSessionCheckpoint: handleSessionCheckpoint,
	KindMemoryConsolidate: handleMemoryConsolidate,
	KindSessionStitch:     handleSessionStitch,
}

// successResult builds a success=true Result with output set to the JSON
// encoding of out. If out cannot be marshaled the result reports the error
// instead — handlers should not return un-marshalable values, but we don't
// want a panic to take the daemon down.
func successResult(kind string, started time.Time, out any) daemon.Result {
	finished := time.Now().UTC()
	if out == nil {
		return daemon.Result{Success: true, Kind: kind, StartedAt: started, FinishedAt: finished}
	}
	data, err := json.Marshal(out)
	if err != nil {
		return daemon.Result{
			Success:    false,
			Kind:       kind,
			Error:      fmt.Sprintf("marshal output: %v", err),
			StartedAt:  started,
			FinishedAt: finished,
		}
	}
	return daemon.Result{
		Success:    true,
		Kind:       kind,
		Output:     data,
		StartedAt:  started,
		FinishedAt: finished,
	}
}

// errorResult is the failure-Result shorthand. Handlers use it for any
// error (cancellation, bad args, I/O failure) so the daemon can move the
// envelope to failed/ with a useful diagnostic.
func errorResult(kind string, started time.Time, err error) daemon.Result {
	return daemon.Result{
		Success:    false,
		Kind:       kind,
		Error:      err.Error(),
		StartedAt:  started,
		FinishedAt: time.Now().UTC(),
	}
}
