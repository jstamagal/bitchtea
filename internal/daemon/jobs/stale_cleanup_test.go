package jobs

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jstamagal/bitchtea/internal/daemon"
)

// staleFixture builds a session_dir + archive_dir pair under one t.TempDir
// so callers can seed JSONL files with custom mtimes and assert which ones
// migrate.
type staleFixture struct {
	root       string
	sessionDir string
	archiveDir string
}

func newStaleFixture(t *testing.T) staleFixture {
	t.Helper()
	root := t.TempDir()
	sessionDir := filepath.Join(root, "sessions")
	archiveDir := filepath.Join(root, "archive")
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatalf("mkdir sessionDir: %v", err)
	}
	return staleFixture{root: root, sessionDir: sessionDir, archiveDir: archiveDir}
}

// writeSession writes a fake session JSONL with the supplied basename and
// then back-dates its mtime so the cleanup handler sees it as `age` old.
func (f staleFixture) writeSession(t *testing.T, name string, age time.Duration) string {
	t.Helper()
	path := filepath.Join(f.sessionDir, name)
	if err := os.WriteFile(path, []byte(`{"role":"user","content":"hi"}`+"\n"), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	when := time.Now().Add(-age)
	if err := os.Chtimes(path, when, when); err != nil {
		t.Fatalf("chtimes %s: %v", name, err)
	}
	return path
}

func staleArgs(f staleFixture, days int) json.RawMessage {
	a := staleCleanupArgs{
		SessionDir: f.sessionDir,
		ArchiveDir: f.archiveDir,
		MaxAgeDays: days,
	}
	data, _ := json.Marshal(a)
	return data
}

func TestStaleCleanupArchivesAgedFiles(t *testing.T) {
	f := newStaleFixture(t)

	old := f.writeSession(t, "2026-01-01_080000.jsonl", 30*24*time.Hour)
	medium := f.writeSession(t, "2026-04-15_080000.jsonl", 5*24*time.Hour)
	fresh := f.writeSession(t, "2026-05-04_080000.jsonl", 1*time.Hour)

	res := Handle(context.Background(), daemon.Job{
		Kind: KindStaleCleanup,
		Args: staleArgs(f, 7),
	})
	if !res.Success {
		t.Fatalf("stale-cleanup: %+v", res)
	}

	// Old file moved.
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatalf("old session should have been moved, stat err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(f.archiveDir, filepath.Base(old))); err != nil {
		t.Fatalf("old session missing in archive: %v", err)
	}

	// Medium (5 days < 7 day cutoff) and fresh both stay put.
	if _, err := os.Stat(medium); err != nil {
		t.Fatalf("medium session should remain in source: %v", err)
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Fatalf("fresh session should remain in source: %v", err)
	}
	for _, name := range []string{filepath.Base(medium), filepath.Base(fresh)} {
		if _, err := os.Stat(filepath.Join(f.archiveDir, name)); !os.IsNotExist(err) {
			t.Fatalf("%s should NOT be in archive yet: %v", name, err)
		}
	}

	var out staleCleanupOutput
	if err := json.Unmarshal(res.Output, &out); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	if out.Scanned != 3 {
		t.Fatalf("Scanned = %d, want 3", out.Scanned)
	}
	if out.Archived != 1 {
		t.Fatalf("Archived = %d, want 1", out.Archived)
	}
	if out.Skipped != 0 {
		t.Fatalf("Skipped = %d, want 0", out.Skipped)
	}
}

func TestStaleCleanupIdempotent(t *testing.T) {
	f := newStaleFixture(t)
	f.writeSession(t, "2026-01-01_080000.jsonl", 30*24*time.Hour)
	f.writeSession(t, "2026-01-02_080000.jsonl", 29*24*time.Hour)

	job := daemon.Job{Kind: KindStaleCleanup, Args: staleArgs(f, 7)}

	res1 := Handle(context.Background(), job)
	if !res1.Success {
		t.Fatalf("first run: %+v", res1)
	}
	first, err := os.ReadDir(f.archiveDir)
	if err != nil {
		t.Fatalf("read archive: %v", err)
	}
	firstNames := dirNames(first)

	res2 := Handle(context.Background(), job)
	if !res2.Success {
		t.Fatalf("second run: %+v", res2)
	}
	second, err := os.ReadDir(f.archiveDir)
	if err != nil {
		t.Fatalf("read archive: %v", err)
	}
	secondNames := dirNames(second)

	if !equalStringSlices(firstNames, secondNames) {
		t.Fatalf("archive layout drifted between runs:\nfirst:  %v\nsecond: %v", firstNames, secondNames)
	}

	var out2 staleCleanupOutput
	if err := json.Unmarshal(res2.Output, &out2); err != nil {
		t.Fatalf("unmarshal out2: %v", err)
	}
	if out2.Scanned != 0 {
		t.Fatalf("second-run Scanned = %d, want 0 (everything already archived)", out2.Scanned)
	}
	if out2.Archived != 0 {
		t.Fatalf("second-run Archived = %d, want 0", out2.Archived)
	}
}

func TestStaleCleanupRespectsConflicts(t *testing.T) {
	f := newStaleFixture(t)
	src := f.writeSession(t, "2026-01-01_080000.jsonl", 30*24*time.Hour)

	// Pre-populate archive with a same-named file containing different
	// content. Cleanup must NOT overwrite it.
	if err := os.MkdirAll(f.archiveDir, 0o755); err != nil {
		t.Fatalf("mkdir archive: %v", err)
	}
	dst := filepath.Join(f.archiveDir, filepath.Base(src))
	preexisting := []byte("PREEXISTING\n")
	if err := os.WriteFile(dst, preexisting, 0o644); err != nil {
		t.Fatalf("seed dst: %v", err)
	}

	res := Handle(context.Background(), daemon.Job{Kind: KindStaleCleanup, Args: staleArgs(f, 7)})
	if !res.Success {
		t.Fatalf("stale-cleanup: %+v", res)
	}

	// Source still present, archive untouched.
	if _, err := os.Stat(src); err != nil {
		t.Fatalf("source should remain on conflict: %v", err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read dst: %v", err)
	}
	if string(got) != string(preexisting) {
		t.Fatalf("archive was clobbered:\ngot:  %q\nwant: %q", got, preexisting)
	}

	var out staleCleanupOutput
	if err := json.Unmarshal(res.Output, &out); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	if out.Skipped != 1 {
		t.Fatalf("Skipped = %d, want 1", out.Skipped)
	}
	if len(out.Conflicts) != 1 || out.Conflicts[0] != filepath.Base(src) {
		t.Fatalf("Conflicts = %v, want [%s]", out.Conflicts, filepath.Base(src))
	}
}

func TestStaleCleanupHonorsCancellation(t *testing.T) {
	f := newStaleFixture(t)
	f.writeSession(t, "2026-01-01_080000.jsonl", 30*24*time.Hour)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	res := Handle(ctx, daemon.Job{Kind: KindStaleCleanup, Args: staleArgs(f, 7)})
	if res.Success {
		t.Fatalf("pre-cancelled ctx: want failure, got success")
	}
	if !strings.Contains(res.Error, "canceled") {
		t.Fatalf("error = %q", res.Error)
	}
}

func TestStaleCleanupRequiresSessionDir(t *testing.T) {
	args, _ := json.Marshal(staleCleanupArgs{ArchiveDir: t.TempDir(), MaxAgeDays: 7})
	res := Handle(context.Background(), daemon.Job{Kind: KindStaleCleanup, Args: args})
	if res.Success {
		t.Fatalf("missing session_dir: want failure")
	}
	if !strings.Contains(res.Error, "session_dir") {
		t.Fatalf("error = %q", res.Error)
	}
}

func TestStaleCleanupRequiresArchiveDir(t *testing.T) {
	args, _ := json.Marshal(staleCleanupArgs{SessionDir: t.TempDir(), MaxAgeDays: 7})
	res := Handle(context.Background(), daemon.Job{Kind: KindStaleCleanup, Args: args})
	if res.Success {
		t.Fatalf("missing archive_dir: want failure")
	}
	if !strings.Contains(res.Error, "archive_dir") {
		t.Fatalf("error = %q", res.Error)
	}
}

func TestStaleCleanupRejectsNegativeMaxAge(t *testing.T) {
	f := newStaleFixture(t)
	res := Handle(context.Background(), daemon.Job{
		Kind: KindStaleCleanup,
		Args: staleArgs(f, -1),
	})
	if res.Success {
		t.Fatalf("negative max_age_days: want failure")
	}
	if !strings.Contains(res.Error, "max_age_days") {
		t.Fatalf("error = %q", res.Error)
	}
}

func TestStaleCleanupRejectsArchiveInsideSessionDir(t *testing.T) {
	root := t.TempDir()
	sessionDir := filepath.Join(root, "sessions")
	archiveDir := filepath.Join(sessionDir, "old") // inside session_dir
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	args, _ := json.Marshal(staleCleanupArgs{
		SessionDir: sessionDir,
		ArchiveDir: archiveDir,
		MaxAgeDays: 7,
	})
	res := Handle(context.Background(), daemon.Job{Kind: KindStaleCleanup, Args: args})
	if res.Success {
		t.Fatalf("archive inside session: want failure")
	}
	if !strings.Contains(res.Error, "inside session_dir") {
		t.Fatalf("error = %q", res.Error)
	}
}

func TestStaleCleanupHandlesMissingSessionDir(t *testing.T) {
	root := t.TempDir()
	args, _ := json.Marshal(staleCleanupArgs{
		SessionDir: filepath.Join(root, "does-not-exist"),
		ArchiveDir: filepath.Join(root, "archive"),
		MaxAgeDays: 7,
	})
	res := Handle(context.Background(), daemon.Job{Kind: KindStaleCleanup, Args: args})
	if !res.Success {
		t.Fatalf("missing session_dir should be no-op success, got %+v", res)
	}
	var out staleCleanupOutput
	if err := json.Unmarshal(res.Output, &out); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	if out.Scanned != 0 || out.Archived != 0 {
		t.Fatalf("missing session_dir should report nothing scanned or archived, got %+v", out)
	}
}

func TestStaleCleanupSkipsNonJSONLFiles(t *testing.T) {
	f := newStaleFixture(t)
	jsonlPath := f.writeSession(t, "old.jsonl", 30*24*time.Hour)

	// Drop a non-jsonl file with an old mtime; cleanup must ignore it.
	otherPath := filepath.Join(f.sessionDir, ".bitchtea_checkpoint.json")
	if err := os.WriteFile(otherPath, []byte("{}"), 0o644); err != nil {
		t.Fatalf("write checkpoint: %v", err)
	}
	when := time.Now().Add(-30 * 24 * time.Hour)
	if err := os.Chtimes(otherPath, when, when); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	res := Handle(context.Background(), daemon.Job{Kind: KindStaleCleanup, Args: staleArgs(f, 7)})
	if !res.Success {
		t.Fatalf("stale-cleanup: %+v", res)
	}

	// The .jsonl moved.
	if _, err := os.Stat(jsonlPath); !os.IsNotExist(err) {
		t.Fatalf("jsonl should have moved: %v", err)
	}
	// The non-jsonl stayed put.
	if _, err := os.Stat(otherPath); err != nil {
		t.Fatalf("checkpoint sidecar should remain: %v", err)
	}
	if _, err := os.Stat(filepath.Join(f.archiveDir, ".bitchtea_checkpoint.json")); !os.IsNotExist(err) {
		t.Fatalf("non-jsonl should NOT have been archived")
	}

	var out staleCleanupOutput
	_ = json.Unmarshal(res.Output, &out)
	if out.Scanned != 1 {
		t.Fatalf("Scanned = %d, want 1 (only the .jsonl)", out.Scanned)
	}
}

func dirNames(entries []os.DirEntry) []string {
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}

func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
