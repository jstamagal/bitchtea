// Package daemon scaffolds the bitchtea background daemon: lock acquisition,
// pidfile management, and the file-mailbox IPC contract described in
// docs/phase-7-process-model.md. This package intentionally does not import
// internal/agent, internal/llm, internal/session, internal/ui, or any other
// runtime package — it is a self-contained substrate that the daemon binary
// (cmd/daemon) and the bitchtea CLI (`bitchtea daemon ...`) sit on top of.
package daemon

import (
	"encoding/json"
	"fmt"
	"time"
)

// Scope mirrors the agent's MemoryScope without importing it. The daemon
// process is forbidden from depending on internal/agent (see CLAUDE.md acyclic
// dep graph), so we re-declare the wire shape here. Job handlers (in
// internal/daemon/jobs) are responsible for translating this into an in-process
// scope value when they need one.
type Scope struct {
	Kind string `json:"kind"`           // "root" | "channel" | "query"
	Name string `json:"name,omitempty"` // channel/query name; empty for root
}

// Job is the on-disk envelope written to ~/.bitchtea/daemon/mail/<ulid>.json
// by the TUI (and any future requestor). Field order and tags mirror the
// design doc — keep them in sync.
type Job struct {
	ID           string          `json:"-"` // ULID derived from filename, not encoded
	Kind         string          `json:"kind"`
	Args         json.RawMessage `json:"args,omitempty"`
	WorkDir      string          `json:"work_dir"`
	SessionPath  string          `json:"session_path,omitempty"`
	Scope        Scope           `json:"scope"`
	RequestorPID int             `json:"requestor_pid"`
	SubmittedAt  time.Time       `json:"submitted_at"`
	Deadline     time.Time       `json:"deadline,omitempty"`
}

// Result is the on-disk envelope the daemon writes to done/<ulid>.json after
// running a handler. failed/<ulid>.json reuses the same shape for jobs the
// daemon refused to run (unknown kind, malformed envelope, shutdown deadline).
type Result struct {
	Success    bool            `json:"success"`
	Kind       string          `json:"kind"`
	Output     json.RawMessage `json:"output,omitempty"`
	Error      string          `json:"error,omitempty"`
	StartedAt  time.Time       `json:"started_at"`
	FinishedAt time.Time       `json:"finished_at"`
}

// MarshalJob serializes a Job using the same DisallowUnknownFields-friendly
// shape the daemon expects on read. We keep this in one place so test code
// and real callers cannot drift.
func MarshalJob(j Job) ([]byte, error) {
	if j.Kind == "" {
		return nil, fmt.Errorf("daemon: job kind is required")
	}
	return json.MarshalIndent(j, "", "  ")
}

// UnmarshalJob is the strict reader: unknown fields fail loudly, matching the
// mcp.json convention from phase 6.
func UnmarshalJob(data []byte) (Job, error) {
	dec := json.NewDecoder(bytesReader(data))
	dec.DisallowUnknownFields()
	var j Job
	if err := dec.Decode(&j); err != nil {
		return Job{}, fmt.Errorf("daemon: decode job: %w", err)
	}
	if j.Kind == "" {
		return Job{}, fmt.Errorf("daemon: job kind is required")
	}
	return j, nil
}

// MarshalResult mirrors MarshalJob.
func MarshalResult(r Result) ([]byte, error) {
	return json.MarshalIndent(r, "", "  ")
}

// UnmarshalResult mirrors UnmarshalJob.
func UnmarshalResult(data []byte) (Result, error) {
	dec := json.NewDecoder(bytesReader(data))
	dec.DisallowUnknownFields()
	var r Result
	if err := dec.Decode(&r); err != nil {
		return Result{}, fmt.Errorf("daemon: decode result: %w", err)
	}
	return r, nil
}
