package tools

// Failure-mode tests asserting exact error wraps and sentinels for the tool
// registry. See bd issue bt-test.16 — these tests pin the *shape* of error
// messages produced by execRead/execWrite/execEdit so accidental refactors
// that drop a wrap or change a prefix get caught at test time.

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestErrorMessage_Read_OfDirectory verifies that reading a path that is a
// directory surfaces a wrapped fs.PathError preserving the user-supplied path
// and the underlying syscall reason. The agent loop relies on this prefix to
// route the error back to the model intelligibly.
func TestErrorMessage_Read_OfDirectory(t *testing.T) {
	dir := t.TempDir()
	subdir := filepath.Join(dir, "is-a-dir")
	if err := os.Mkdir(subdir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	reg := NewRegistry(dir, t.TempDir())
	_, err := reg.Execute(context.Background(), "read", `{"path":"is-a-dir"}`)
	if err == nil {
		t.Fatal("expected error reading a directory, got nil")
	}

	// The wrap layer in execRead prepends "read <relpath>: ".
	if !strings.HasPrefix(err.Error(), "read is-a-dir:") {
		t.Fatalf("error should be prefixed with 'read is-a-dir:', got %q", err.Error())
	}

	// The underlying error from os.ReadFile must remain reachable through
	// the wrap chain — callers use errors.As to pull out the path.
	var pathErr *fs.PathError
	if !errors.As(err, &pathErr) {
		t.Fatalf("expected wrapped *fs.PathError, got %T: %v", err, err)
	}
	if pathErr.Path != subdir {
		t.Fatalf("PathError.Path = %q, want %q", pathErr.Path, subdir)
	}
}

// TestErrorMessage_Write_ReadOnlyDir verifies that writing into a directory
// that the process cannot create children in produces an error wrapping
// fs.ErrPermission with a "write <relpath>:" prefix.
func TestErrorMessage_Write_ReadOnlyDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission semantics differ on windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root bypasses unix permission checks")
	}
	dir := t.TempDir()
	roDir := filepath.Join(dir, "ro")
	if err := os.Mkdir(roDir, 0o755); err != nil {
		t.Fatalf("mkdir ro: %v", err)
	}
	// Strip write+exec from the parent directory so creating a new file
	// inside it fails with EACCES.
	if err := os.Chmod(roDir, 0o555); err != nil {
		t.Fatalf("chmod ro: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(roDir, 0o755) })

	reg := NewRegistry(dir, t.TempDir())
	rel := filepath.Join("ro", "out.txt")
	_, err := reg.Execute(context.Background(), "write", `{"path":"`+rel+`","content":"hi"}`)
	if err == nil {
		t.Fatal("expected error writing into read-only dir, got nil")
	}

	if !strings.HasPrefix(err.Error(), "write "+rel+":") {
		t.Fatalf("error should be prefixed with 'write %s:', got %q", rel, err.Error())
	}
	if !errors.Is(err, fs.ErrPermission) {
		t.Fatalf("expected error to wrap fs.ErrPermission, got %v", err)
	}
}

// TestErrorMessage_Edit_OldTextEmpty pins the exact human-facing message used
// by the edit tool to redirect the model toward the write tool. The wording
// is part of the agent contract — changing it without updating tests is a
// regression risk.
func TestErrorMessage_Edit_OldTextEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	reg := NewRegistry(dir, t.TempDir())
	_, err := reg.Execute(context.Background(), "edit", `{"path":"f.txt","edits":[{"oldText":"","newText":"x"}]}`)
	if err == nil {
		t.Fatal("expected error for empty oldText, got nil")
	}
	got := err.Error()
	want := "edit: oldText must not be empty (use the write tool to create a new file or replace its contents)"
	if got != want {
		t.Fatalf("error message mismatch:\n got: %q\nwant: %q", got, want)
	}
}

// TestErrorMessage_Edit_OnEmptyFile verifies that attempting to edit an empty
// file with a non-empty oldText surfaces a "not found" message that includes
// the relative path. The empty-file case must not be silently treated as a
// match on the empty string.
func TestErrorMessage_Edit_OnEmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.txt")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	reg := NewRegistry(dir, t.TempDir())
	_, err := reg.Execute(context.Background(), "edit", `{"path":"empty.txt","edits":[{"oldText":"foo","newText":"bar"}]}`)
	if err == nil {
		t.Fatal("expected error for missing oldText in empty file, got nil")
	}
	if !strings.HasPrefix(err.Error(), "oldText not found in empty.txt:") {
		t.Fatalf("expected prefix 'oldText not found in empty.txt:', got %q", err.Error())
	}
}
