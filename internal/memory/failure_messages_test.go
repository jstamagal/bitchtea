package memory

// Failure-mode tests asserting exact error wraps and sentinels for the memory
// package. See bd issue bt-test.16. The agent's search_memory tool surfaces
// these errors verbatim to the model, so the prefixes are part of the user
// contract.

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// TestErrorMessage_SearchInScope_EmptyQuery pins the exact "query is required"
// message used by SearchInScope when handed whitespace-only input.
func TestErrorMessage_SearchInScope_EmptyQuery(t *testing.T) {
	dir := t.TempDir()
	sessionDir := filepath.Join(dir, "sessions")
	workDir := filepath.Join(dir, "work")

	cases := []struct{ name, query string }{
		{"empty", ""},
		{"whitespace", "   \t\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := SearchInScope(sessionDir, workDir, RootScope(), tc.query, 5)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if err.Error() != "query is required" {
				t.Fatalf("error message: got %q, want %q", err.Error(), "query is required")
			}
		})
	}
}

// TestErrorMessage_SearchInScope_UnreadableDailyDir verifies that when the
// daily-memory directory exists but is not readable, SearchInScope surfaces
// an error wrapping fs.ErrPermission with the "read daily memory dir:"
// prefix.
func TestErrorMessage_SearchInScope_UnreadableDailyDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission semantics differ on windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root bypasses unix permission checks")
	}

	base := t.TempDir()
	sessionDir := filepath.Join(base, ".bitchtea", "sessions")
	workDir := filepath.Join(base, "work")
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatalf("mkdir session: %v", err)
	}
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatalf("mkdir work: %v", err)
	}

	// Pre-create the daily-memory directory for the root scope and strip
	// read permission so os.ReadDir fails with EACCES.
	now := time.Now()
	dailyDir := filepath.Dir(DailyPath(sessionDir, workDir, now))
	if err := os.MkdirAll(dailyDir, 0o755); err != nil {
		t.Fatalf("mkdir daily: %v", err)
	}
	// Write at least one file to make sure the dir isn't empty.
	if err := os.WriteFile(filepath.Join(dailyDir, now.Format("2006-01-02")+".md"), []byte("# x\nfindme\n"), 0o644); err != nil {
		t.Fatalf("seed daily: %v", err)
	}
	if err := os.Chmod(dailyDir, 0o000); err != nil {
		t.Fatalf("chmod 000: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dailyDir, 0o755) })

	_, err := SearchInScope(sessionDir, workDir, RootScope(), "findme", 5)
	if err == nil {
		t.Fatal("expected error from unreadable daily dir, got nil")
	}
	if !strings.HasPrefix(err.Error(), "read daily memory dir:") {
		t.Fatalf("error should be prefixed with 'read daily memory dir:', got %q", err.Error())
	}
	if !errors.Is(err, fs.ErrPermission) {
		t.Fatalf("error should wrap fs.ErrPermission, got %v", err)
	}
}

// TestErrorMessage_AppendHot_ParentDirIsAFile verifies that AppendHot fails
// with a wrapped fs.PathError when the would-be parent directory of HOT.md
// is occupied by a regular file (so MkdirAll cannot create it). This
// substitutes for the ENOSPC ("full disk") case mentioned in the issue
// brief, which is impractical to reproduce in unit tests.
func TestErrorMessage_AppendHot_ParentDirIsAFile(t *testing.T) {
	base := t.TempDir()
	sessionDir := filepath.Join(base, ".bitchtea", "sessions")
	workDir := filepath.Join(base, "work")
	if err := os.MkdirAll(filepath.Dir(memoryBaseDir(sessionDir, workDir)), 0o755); err != nil {
		t.Fatalf("mkdir parent: %v", err)
	}

	// Place a regular file where MkdirAll wants a directory.
	occupied := memoryBaseDir(sessionDir, workDir)
	if err := os.WriteFile(occupied, []byte("not a directory"), 0o644); err != nil {
		t.Fatalf("seed occupant: %v", err)
	}

	scope := ChannelScope("dev", nil)
	err := AppendHot(sessionDir, workDir, scope, time.Now(), "title", "content")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.HasPrefix(err.Error(), "create hot memory dir:") {
		t.Fatalf("error should be prefixed with 'create hot memory dir:', got %q", err.Error())
	}
	// The underlying mkdir error from os.MkdirAll is a *fs.PathError;
	// callers can errors.As it for the offending path.
	var pathErr *fs.PathError
	if !errors.As(err, &pathErr) {
		t.Fatalf("expected wrapped *fs.PathError, got %T: %v", err, err)
	}
}
