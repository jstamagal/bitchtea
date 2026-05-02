package daemon

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func newTestJob() Job {
	return Job{
		Kind:         "compact",
		Args:         json.RawMessage(`{"k":"v"}`),
		WorkDir:      "/tmp/work",
		SessionPath:  "/tmp/sessions/2026.jsonl",
		Scope:        Scope{Kind: "root"},
		RequestorPID: os.Getpid(),
		SubmittedAt:  time.Now().UTC(),
	}
}

func TestSubmitAndList(t *testing.T) {
	mb := New(t.TempDir())
	id, err := mb.Submit(newTestJob())
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if len(id) != 26 {
		t.Fatalf("expected 26-char ULID, got %q", id)
	}
	jobs, parseErrs, err := mb.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(parseErrs) != 0 {
		t.Fatalf("unexpected parse errors: %v", parseErrs)
	}
	if len(jobs) != 1 {
		t.Fatalf("expected 1 job, got %d", len(jobs))
	}
	if jobs[0].ID != id {
		t.Fatalf("job ID = %q, want %q", jobs[0].ID, id)
	}
	if jobs[0].Kind != "compact" {
		t.Fatalf("kind = %q", jobs[0].Kind)
	}
}

func TestListSortedByULID(t *testing.T) {
	mb := New(t.TempDir())
	var ids []string
	for i := 0; i < 3; i++ {
		id, err := mb.Submit(newTestJob())
		if err != nil {
			t.Fatalf("Submit: %v", err)
		}
		ids = append(ids, id)
		time.Sleep(2 * time.Millisecond) // keep ULID timestamps monotonic
	}
	jobs, _, err := mb.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(jobs) != 3 {
		t.Fatalf("len(jobs) = %d", len(jobs))
	}
	for i, j := range jobs {
		if j.ID != ids[i] {
			t.Fatalf("jobs[%d].ID = %q, want %q (sort order)", i, j.ID, ids[i])
		}
	}
}

func TestComplete(t *testing.T) {
	base := t.TempDir()
	mb := New(base)
	id, err := mb.Submit(newTestJob())
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	now := time.Now().UTC()
	if err := mb.Complete(id, Result{
		Success:    true,
		Kind:       "compact",
		Output:     json.RawMessage(`"done"`),
		StartedAt:  now,
		FinishedAt: now.Add(time.Second),
	}); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if _, err := os.Stat(filepath.Join(mb.Paths().MailDir, id+".json")); !os.IsNotExist(err) {
		t.Fatalf("mail file should be gone after Complete: %v", err)
	}
	donePath := filepath.Join(mb.Paths().DoneDir, id+".json")
	data, err := os.ReadFile(donePath)
	if err != nil {
		t.Fatalf("read done: %v", err)
	}
	res, err := UnmarshalResult(data)
	if err != nil {
		t.Fatalf("UnmarshalResult: %v", err)
	}
	if !res.Success || res.Kind != "compact" {
		t.Fatalf("result lost: %+v", res)
	}
}

func TestFail(t *testing.T) {
	mb := New(t.TempDir())
	id, err := mb.Submit(newTestJob())
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if err := mb.Fail(id, "no handler"); err != nil {
		t.Fatalf("Fail: %v", err)
	}
	if _, err := os.Stat(filepath.Join(mb.Paths().MailDir, id+".json")); !os.IsNotExist(err) {
		t.Fatalf("mail file should be gone after Fail: %v", err)
	}
	failedPath := filepath.Join(mb.Paths().FailedDir, id+".json")
	data, err := os.ReadFile(failedPath)
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	res, err := UnmarshalResult(data)
	if err != nil {
		t.Fatalf("UnmarshalResult: %v", err)
	}
	if res.Success {
		t.Fatal("Fail should record success=false")
	}
	if res.Error != "no handler" {
		t.Fatalf("error reason = %q", res.Error)
	}
}

func TestListSkipsTmpAndDirs(t *testing.T) {
	mb := New(t.TempDir())
	if err := mb.EnsureMailDir(); err != nil {
		t.Fatalf("EnsureMailDir: %v", err)
	}
	// Drop a .tmp file (mid-write) and a stray directory; List should ignore both.
	if err := os.WriteFile(filepath.Join(mb.Paths().MailDir, "01ABC.json.tmp"), []byte("partial"), 0o600); err != nil {
		t.Fatalf("write tmp: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(mb.Paths().MailDir, "subdir"), 0o700); err != nil {
		t.Fatalf("mkdir subdir: %v", err)
	}
	jobs, parseErrs, err := mb.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(jobs) != 0 {
		t.Fatalf("expected 0 jobs, got %d", len(jobs))
	}
	if len(parseErrs) != 0 {
		t.Fatalf("expected 0 parse errors, got %v", parseErrs)
	}
}
