package session

// Failure-mode tests asserting exact error wraps and sentinels for session
// load/append. See bd issue bt-test.16. Append's wrap chain is what the TUI
// uses to detect "session disk gone" vs "marshal bug" vs "lock contention",
// so the prefix shape matters.

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestErrorMessage_Load_TruncatedJSONL documents the contract that Load is
// permissive: malformed lines are skipped silently and Load returns no error
// as long as the file itself can be read. A future change that promotes
// malformed lines to a hard error would break resume for users with
// partial-write crashes, so this test pins the current shape.
func TestErrorMessage_Load_TruncatedJSONL(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "trunc.jsonl")

	// Two valid entries surrounding a truncated line. The truncated line
	// is missing its closing brace.
	contents := `{"role":"user","content":"hello"}` + "\n" +
		`{"role":"assistant","content":"this line is truncated mid-` + "\n" +
		`{"role":"user","content":"world"}` + "\n"
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	s, err := Load(path)
	if err != nil {
		t.Fatalf("Load: unexpected error %v", err)
	}
	if got := len(s.Entries); got != 2 {
		t.Fatalf("Load skipped malformed line incorrectly: got %d entries, want 2", got)
	}
	if s.Entries[0].Content != "hello" || s.Entries[1].Content != "world" {
		t.Fatalf("Load lost good entries: %+v", s.Entries)
	}
}

// TestErrorMessage_Load_MissingFile verifies that Load on a path that doesn't
// exist returns an error wrapping fs.ErrNotExist with the "read session:"
// prefix.
func TestErrorMessage_Load_MissingFile(t *testing.T) {
	dir := t.TempDir()
	_, err := Load(filepath.Join(dir, "does-not-exist.jsonl"))
	if err == nil {
		t.Fatal("expected error loading missing session, got nil")
	}
	if !strings.HasPrefix(err.Error(), "read session:") {
		t.Fatalf("error should be prefixed with 'read session:', got %q", err.Error())
	}
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("error should wrap fs.ErrNotExist, got %v", err)
	}
}

// TestErrorMessage_Append_ReadOnlyFile verifies that Append surfaces a
// permission error wrapping fs.ErrPermission with the "open session file:"
// prefix when the session file (or its parent dir) is unwritable.
func TestErrorMessage_Append_ReadOnlyFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission semantics differ on windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root bypasses unix permission checks")
	}

	dir := t.TempDir()
	roDir := filepath.Join(dir, "ro")
	if err := os.Mkdir(roDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	s := &Session{Path: filepath.Join(roDir, "session.jsonl"), Entries: []Entry{}}

	// Strip write+exec from the parent dir so OpenFile O_CREATE fails.
	if err := os.Chmod(roDir, 0o555); err != nil {
		t.Fatalf("chmod ro: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(roDir, 0o755) })

	err := s.Append(Entry{Role: "user", Content: "x"})
	if err == nil {
		t.Fatal("expected error appending to unwritable parent dir, got nil")
	}
	if !strings.HasPrefix(err.Error(), "open session file:") {
		t.Fatalf("error should be prefixed with 'open session file:', got %q", err.Error())
	}
	if !errors.Is(err, fs.ErrPermission) {
		t.Fatalf("error should wrap fs.ErrPermission, got %v", err)
	}
}
