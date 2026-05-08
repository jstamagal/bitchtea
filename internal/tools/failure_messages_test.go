package tools

// Failure-mode tests asserting the shape of error results for the tool
// registry. See bd issue bt-test.16 — these tests pin the *shape* of error
// messages produced by execRead/execWrite/execEdit so accidental refactors
// that drop a wrap or change a prefix get caught at test time.
//
// Pattern 1 (structured error result): Execute no longer returns Go errors for
// tool-level failures; instead it returns a <tool_call_error> XML result. These
// tests assert on the result string shape rather than on the Go error value.

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// assertToolError is a helper that asserts Execute returned a structured
// <tool_call_error> result (not a Go error) whose <cause> contains wantCause.
func assertToolError(t *testing.T, result string, err error, wantCause string) {
	t.Helper()
	if err != nil {
		t.Fatalf("Execute returned a Go error (want structured result): %v", err)
	}
	if !strings.Contains(result, "<tool_call_error>") {
		t.Fatalf("result missing <tool_call_error> wrapper, got: %q", result)
	}
	if !strings.Contains(result, "<cause>") || !strings.Contains(result, "</cause>") {
		t.Fatalf("result missing <cause> tags, got: %q", result)
	}
	if !strings.Contains(result, "<reflection>") {
		t.Fatalf("result missing <reflection> tag, got: %q", result)
	}
	if wantCause != "" && !strings.Contains(result, wantCause) {
		t.Fatalf("result <cause> does not contain %q, got: %q", wantCause, result)
	}
}

// TestErrorMessage_Read_OfDirectory verifies that reading a path that is a
// directory surfaces a structured error result whose cause contains the
// "read is-a-dir:" prefix.
func TestErrorMessage_Read_OfDirectory(t *testing.T) {
	dir := t.TempDir()
	subdir := filepath.Join(dir, "is-a-dir")
	if err := os.Mkdir(subdir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	reg := NewRegistry(dir, t.TempDir())
	result, err := reg.Execute(context.Background(), "read", `{"path":"is-a-dir"}`)
	assertToolError(t, result, err, "read is-a-dir:")
}

// TestErrorMessage_Write_ReadOnlyDir verifies that writing into a read-only
// directory surfaces a structured error result whose cause contains the
// "write <relpath>:" prefix.
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
	if err := os.Chmod(roDir, 0o555); err != nil {
		t.Fatalf("chmod ro: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(roDir, 0o755) })

	reg := NewRegistry(dir, t.TempDir())
	rel := filepath.Join("ro", "out.txt")
	// New-file write into a read-only dir — no pre-read needed (it's a new file).
	result, err := reg.Execute(context.Background(), "write", `{"path":"`+rel+`","content":"hi"}`)
	assertToolError(t, result, err, "write "+rel+":")
}

// TestErrorMessage_Edit_OldTextEmpty pins the exact human-facing message used
// by the edit tool to redirect the model toward the write tool.
func TestErrorMessage_Edit_OldTextEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	reg := NewRegistry(dir, t.TempDir())
	// Pattern 2: read before edit.
	if _, err := reg.Execute(context.Background(), "read", `{"path":"f.txt"}`); err != nil {
		t.Fatalf("read: %v", err)
	}
	result, err := reg.Execute(context.Background(), "edit", `{"path":"f.txt","edits":[{"oldText":"","newText":"x"}]}`)
	assertToolError(t, result, err, "edit: oldText must not be empty (use the write tool to create a new file or replace its contents)")
}

// TestErrorMessage_Edit_OnEmptyFile verifies that attempting to edit an empty
// file with a non-empty oldText surfaces a structured error result whose cause
// contains the "not found" prefix with the relative path.
func TestErrorMessage_Edit_OnEmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.txt")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	reg := NewRegistry(dir, t.TempDir())
	// Pattern 2: read before edit.
	if _, err := reg.Execute(context.Background(), "read", `{"path":"empty.txt"}`); err != nil {
		t.Fatalf("read: %v", err)
	}
	result, err := reg.Execute(context.Background(), "edit", `{"path":"empty.txt","edits":[{"oldText":"foo","newText":"bar"}]}`)
	assertToolError(t, result, err, "oldText not found in empty.txt:")
}
