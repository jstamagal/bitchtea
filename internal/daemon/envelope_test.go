package daemon

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestJobRoundTrip(t *testing.T) {
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	in := Job{
		Kind:         "compact",
		Args:         json.RawMessage(`{"max_tokens":4096}`),
		WorkDir:      "/abs/work",
		SessionPath:  "/abs/sessions/2026-05-01.jsonl",
		Scope:        Scope{Kind: "channel", Name: "main"},
		RequestorPID: 12345,
		SubmittedAt:  now,
		Deadline:     now.Add(10 * time.Minute),
	}
	data, err := MarshalJob(in)
	if err != nil {
		t.Fatalf("MarshalJob: %v", err)
	}
	out, err := UnmarshalJob(data)
	if err != nil {
		t.Fatalf("UnmarshalJob: %v", err)
	}
	if out.Kind != in.Kind || out.WorkDir != in.WorkDir || out.SessionPath != in.SessionPath {
		t.Fatalf("scalar fields lost: %+v", out)
	}
	if out.Scope != in.Scope {
		t.Fatalf("scope lost: got %+v want %+v", out.Scope, in.Scope)
	}
	if !out.SubmittedAt.Equal(in.SubmittedAt) || !out.Deadline.Equal(in.Deadline) {
		t.Fatalf("timestamps lost: %+v", out)
	}
}

func TestUnmarshalJobRejectsUnknownFields(t *testing.T) {
	bad := []byte(`{"kind":"compact","work_dir":"/x","scope":{"kind":"root"},"submitted_at":"2026-05-01T00:00:00Z","mystery":true}`)
	_, err := UnmarshalJob(bad)
	if err == nil {
		t.Fatal("expected unknown field to fail decode")
	}
	if !strings.Contains(err.Error(), "mystery") {
		t.Fatalf("expected error to mention unknown field, got %v", err)
	}
}

func TestMarshalJobRequiresKind(t *testing.T) {
	_, err := MarshalJob(Job{WorkDir: "/x"})
	if err == nil {
		t.Fatal("expected MarshalJob to reject empty kind")
	}
}

func TestResultRoundTrip(t *testing.T) {
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	in := Result{
		Success:    true,
		Kind:       "compact",
		Output:     json.RawMessage(`"ok"`),
		StartedAt:  now,
		FinishedAt: now.Add(time.Second),
	}
	data, err := MarshalResult(in)
	if err != nil {
		t.Fatalf("MarshalResult: %v", err)
	}
	out, err := UnmarshalResult(data)
	if err != nil {
		t.Fatalf("UnmarshalResult: %v", err)
	}
	if out.Success != in.Success || out.Kind != in.Kind {
		t.Fatalf("scalars lost: %+v", out)
	}
	if !out.StartedAt.Equal(in.StartedAt) || !out.FinishedAt.Equal(in.FinishedAt) {
		t.Fatalf("timestamps lost: %+v", out)
	}
}
