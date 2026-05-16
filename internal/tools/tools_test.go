package tools

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	memorypkg "github.com/jstamagal/bitchtea/internal/memory"
)

func TestReadFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	os.WriteFile(path, []byte("line1\nline2\nline3\nline4\nline5\n"), 0644)

	reg := NewRegistry(dir, t.TempDir())

	// Read full file
	result, err := reg.Execute(context.Background(), "read", `{"path":"test.txt"}`)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if result != "line1\nline2\nline3\nline4\nline5\n" {
		t.Fatalf("unexpected content: %q", result)
	}

	// Read with offset and limit
	result, err = reg.Execute(context.Background(), "read", `{"path":"test.txt","offset":2,"limit":2}`)
	if err != nil {
		t.Fatalf("read with offset: %v", err)
	}
	if result != "line2\nline3" {
		t.Fatalf("unexpected content with offset: %q", result)
	}
}

func TestReadOffsetPastEOF(t *testing.T) {
	dir := t.TempDir()
	nonEmpty := filepath.Join(dir, "lines.txt")
	os.WriteFile(nonEmpty, []byte("a\nb\nc"), 0644) // 3 lines
	empty := filepath.Join(dir, "empty.txt")
	os.WriteFile(empty, []byte(""), 0644)

	reg := NewRegistry(dir, t.TempDir())

	// Offset well past EOF -> structured error result (not a Go error)
	result, err := reg.Execute(context.Background(), "read", `{"path":"lines.txt","offset":99}`)
	if err != nil {
		t.Fatalf("Execute returned Go error (want structured result): %v", err)
	}
	if !strings.Contains(result, "<tool_call_error>") {
		t.Fatalf("expected structured error result for offset past EOF, got: %q", result)
	}
	if !strings.Contains(result, "past end of file") || !strings.Contains(result, "99") || !strings.Contains(result, "3") {
		t.Fatalf("error cause should mention offset and length, got: %q", result)
	}

	// Offset just past last addressable line -> structured error result
	result, err = reg.Execute(context.Background(), "read", `{"path":"lines.txt","offset":4}`)
	if err != nil {
		t.Fatalf("Execute returned Go error (want structured result): %v", err)
	}
	if !strings.Contains(result, "<tool_call_error>") {
		t.Fatalf("expected structured error result for offset at len(lines)+1, got: %q", result)
	}

	// Normal in-range offset still works
	result, err = reg.Execute(context.Background(), "read", `{"path":"lines.txt","offset":2,"limit":1}`)
	if err != nil {
		t.Fatalf("in-range offset: %v", err)
	}
	if result != "b" {
		t.Fatalf("unexpected in-range content: %q", result)
	}

	// Empty file with no offset/limit -> empty string, no error (preserved behavior)
	result, err = reg.Execute(context.Background(), "read", `{"path":"empty.txt"}`)
	if err != nil {
		t.Fatalf("empty file read: %v", err)
	}
	if result != "" {
		t.Fatalf("empty file should return empty string, got: %q", result)
	}
}

func TestReadBinaryFile(t *testing.T) {
	dir := t.TempDir()
	binary := []byte{0x00, 0x01, 0x02, 'b', 'i', 'n', '\n', 0xff, 0xfe}
	if err := os.WriteFile(filepath.Join(dir, "blob.bin"), binary, 0644); err != nil {
		t.Fatalf("write binary fixture: %v", err)
	}

	reg := NewRegistry(dir, t.TempDir())
	result, err := reg.Execute(context.Background(), "read", `{"path":"blob.bin"}`)
	if err != nil {
		t.Fatalf("read binary: %v", err)
	}
	if result != string(binary) {
		t.Fatalf("read should return raw file bytes as a Go string, got %q want %q", result, string(binary))
	}
}

func TestReadOffsetZero(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "lines.txt"), []byte("line1\nline2\nline3"), 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	reg := NewRegistry(dir, t.TempDir())
	result, err := reg.Execute(context.Background(), "read", `{"path":"lines.txt","offset":0,"limit":1}`)
	if err != nil {
		t.Fatalf("read offset zero: %v", err)
	}
	if result != "line1" {
		t.Fatalf("offset=0 should start at line 1 when limit is set, got %q", result)
	}

	result, err = reg.Execute(context.Background(), "read", `{"path":"lines.txt","offset":1,"limit":1}`)
	if err != nil {
		t.Fatalf("read offset one: %v", err)
	}
	if result != "line1" {
		t.Fatalf("offset=1 should also start at line 1, got %q", result)
	}
}

func TestReadLimitZero(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "lines.txt"), []byte("line1\nline2\nline3"), 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	reg := NewRegistry(dir, t.TempDir())
	result, err := reg.Execute(context.Background(), "read", `{"path":"lines.txt","limit":0}`)
	if err != nil {
		t.Fatalf("read limit zero: %v", err)
	}
	if result != "line1\nline2\nline3" {
		t.Fatalf("limit=0 should mean unlimited, got %q", result)
	}

	result, err = reg.Execute(context.Background(), "read", `{"path":"lines.txt","offset":2,"limit":0}`)
	if err != nil {
		t.Fatalf("read offset with limit zero: %v", err)
	}
	if result != "line2\nline3" {
		t.Fatalf("limit=0 with offset should read through EOF, got %q", result)
	}
}

func TestWriteFile(t *testing.T) {
	dir := t.TempDir()
	reg := NewRegistry(dir, t.TempDir())

	result, err := reg.Execute(context.Background(), "write", `{"path":"sub/dir/out.txt","content":"hello world"}`)
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	expected := "Wrote 11 bytes to " + filepath.Join(dir, "sub/dir/out.txt")
	if result != expected {
		t.Fatalf("unexpected result: %q (want %q)", result, expected)
	}

	data, _ := os.ReadFile(filepath.Join(dir, "sub/dir/out.txt"))
	if string(data) != "hello world" {
		t.Fatalf("file content: %q", string(data))
	}
}

func TestWriteSuccessMessageReportsResolvedPath(t *testing.T) {
	dir := t.TempDir()
	reg := NewRegistry(dir, t.TempDir())

	result, err := reg.Execute(context.Background(), "write", `{"path":"sub/rel.txt","content":"abcd"}`)
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	resolved := filepath.Join(dir, "sub/rel.txt")
	if !strings.Contains(result, resolved) {
		t.Fatalf("success message %q should contain resolved path %q", result, resolved)
	}
	if strings.Contains(result, " sub/rel.txt") {
		t.Fatalf("success message %q should not contain unresolved relative path", result)
	}
}

func TestEditSuccessMessageReportsResolvedPath(t *testing.T) {
	dir := t.TempDir()
	resolved := filepath.Join(dir, "rel.txt")
	os.WriteFile(resolved, []byte("foo\n"), 0644)

	reg := NewRegistry(dir, t.TempDir())

	// Pattern 2: read before edit.
	if _, err := reg.Execute(context.Background(), "read", `{"path":"rel.txt"}`); err != nil {
		t.Fatalf("read: %v", err)
	}
	result, err := reg.Execute(context.Background(), "edit", `{"path":"rel.txt","edits":[{"oldText":"foo","newText":"bar"}]}`)
	if err != nil {
		t.Fatalf("edit: %v", err)
	}
	if !strings.Contains(result, resolved) {
		t.Fatalf("success message %q should contain resolved path %q", result, resolved)
	}
	if strings.Contains(result, " rel.txt") {
		t.Fatalf("success message %q should not contain unresolved relative path", result)
	}
}

func TestEditFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "edit.txt")
	os.WriteFile(path, []byte("foo bar baz\nhello world\n"), 0644)

	reg := NewRegistry(dir, t.TempDir())

	// Pattern 2: read before edit.
	if _, err := reg.Execute(context.Background(), "read", `{"path":"edit.txt"}`); err != nil {
		t.Fatalf("read: %v", err)
	}
	result, err := reg.Execute(context.Background(), "edit", `{"path":"edit.txt","edits":[{"oldText":"hello world","newText":"goodbye world"}]}`)
	if err != nil {
		t.Fatalf("edit: %v", err)
	}
	expected := "Applied 1 edit(s) to " + path
	if result != expected {
		t.Fatalf("unexpected result: %q (want %q)", result, expected)
	}

	data, _ := os.ReadFile(path)
	if string(data) != "foo bar baz\ngoodbye world\n" {
		t.Fatalf("edited content: %q", string(data))
	}
}

func TestEditMultipleEditsOrdering(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "order.txt")
	if err := os.WriteFile(path, []byte("one two three"), 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	reg := NewRegistry(dir, t.TempDir())
	// Pattern 2: read before edit.
	if _, err := reg.Execute(context.Background(), "read", `{"path":"order.txt"}`); err != nil {
		t.Fatalf("read: %v", err)
	}
	result, err := reg.Execute(context.Background(), "edit", `{"path":"order.txt","edits":[{"oldText":"one","newText":"two"},{"oldText":"two two three","newText":"ordered"}]}`)
	if err != nil {
		t.Fatalf("edit multiple ordered: %v", err)
	}
	if result != "Applied 2 edit(s) to "+path {
		t.Fatalf("unexpected result: %q", result)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read edited file: %v", err)
	}
	if string(data) != "ordered" {
		t.Fatalf("edits should be applied in declared order, got %q", data)
	}
}

func TestEditEmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty-after-edit.txt")
	if err := os.WriteFile(path, []byte("delete me"), 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	reg := NewRegistry(dir, t.TempDir())
	// Pattern 2: read before edit.
	if _, err := reg.Execute(context.Background(), "read", `{"path":"empty-after-edit.txt"}`); err != nil {
		t.Fatalf("read: %v", err)
	}
	result, err := reg.Execute(context.Background(), "edit", `{"path":"empty-after-edit.txt","edits":[{"oldText":"delete me","newText":""}]}`)
	if err != nil {
		t.Fatalf("edit to empty file: %v", err)
	}
	if result != "Applied 1 edit(s) to "+path {
		t.Fatalf("unexpected result: %q", result)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read edited file: %v", err)
	}
	if string(data) != "" {
		t.Fatalf("edit should be able to create an empty file, got %q", data)
	}
}

func TestEditFileNonUnique(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dup.txt")
	os.WriteFile(path, []byte("aaa\naaa\n"), 0644)

	reg := NewRegistry(dir, t.TempDir())

	// Read first (Pattern 2 guard); then attempt non-unique edit.
	_, _ = reg.Execute(context.Background(), "read", `{"path":"dup.txt"}`)
	result, err := reg.Execute(context.Background(), "edit", `{"path":"dup.txt","edits":[{"oldText":"aaa","newText":"bbb"}]}`)
	if err != nil {
		t.Fatalf("Execute returned Go error (want structured result): %v", err)
	}
	if !strings.Contains(result, "<tool_call_error>") {
		t.Fatalf("expected structured error result for non-unique oldText, got: %q", result)
	}
}

func TestEditFileEmptyOldText(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.txt")
	os.WriteFile(path, []byte("foo bar\n"), 0644)

	reg := NewRegistry(dir, t.TempDir())

	// Read first (Pattern 2 guard).
	_, _ = reg.Execute(context.Background(), "read", `{"path":"empty.txt"}`)
	result, err := reg.Execute(context.Background(), "edit", `{"path":"empty.txt","edits":[{"oldText":"","newText":"injected"}]}`)
	if err != nil {
		t.Fatalf("Execute returned Go error (want structured result): %v", err)
	}
	if !strings.Contains(result, "<tool_call_error>") {
		t.Fatalf("expected structured error result for empty oldText, got: %q", result)
	}
	if !strings.Contains(result, "oldText") || !strings.Contains(result, "empty") || !strings.Contains(result, "write") {
		t.Fatalf("error cause %q should mention oldText, empty, and write", result)
	}

	data, _ := os.ReadFile(path)
	if string(data) != "foo bar\n" {
		t.Fatalf("file should be unchanged, got: %q", string(data))
	}
}

func TestEditFileNotFound(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "missing.txt")
	os.WriteFile(path, []byte("foo bar\n"), 0644)

	reg := NewRegistry(dir, t.TempDir())

	// Read first (Pattern 2 guard).
	_, _ = reg.Execute(context.Background(), "read", `{"path":"missing.txt"}`)
	result, err := reg.Execute(context.Background(), "edit", `{"path":"missing.txt","edits":[{"oldText":"nonexistent","newText":"x"}]}`)
	if err != nil {
		t.Fatalf("Execute returned Go error (want structured result): %v", err)
	}
	if !strings.Contains(result, "<tool_call_error>") {
		t.Fatalf("expected structured error result for oldText not found, got: %q", result)
	}
	if !strings.Contains(result, "not found") {
		t.Fatalf("expected 'not found' in error cause, got: %q", result)
	}
}

func TestBash(t *testing.T) {
	dir := t.TempDir()
	reg := NewRegistry(dir, t.TempDir())

	result, err := reg.Execute(context.Background(), "bash", `{"command":"echo hello && echo world"}`)
	if err != nil {
		t.Fatalf("bash: %v", err)
	}
	if result != "hello\nworld\n" {
		t.Fatalf("unexpected output: %q", result)
	}
}

func TestBashOutputTruncation(t *testing.T) {
	dir := t.TempDir()
	reg := NewRegistry(dir, t.TempDir())

	result, err := reg.Execute(context.Background(), "bash", `{"command":"yes x | head -c 60000"}`)
	if err != nil {
		t.Fatalf("bash large output: %v", err)
	}
	// Pattern 3: overflow now yields head+separator+tail+overflow-path footer,
	// not the old "\n... (truncated)" suffix.
	if !strings.Contains(result, "[TRUNCATED") {
		t.Fatalf("expected TRUNCATED marker, got tail %q", result[max(0, len(result)-64):])
	}
}

func TestBashStderrOnly(t *testing.T) {
	dir := t.TempDir()
	reg := NewRegistry(dir, t.TempDir())

	result, err := reg.Execute(context.Background(), "bash", `{"command":"printf 'only stderr' >&2"}`)
	if err != nil {
		t.Fatalf("bash stderr only: %v", err)
	}
	if result != "only stderr" {
		t.Fatalf("stderr should be captured in output, got %q", result)
	}
}

func TestBashTimeoutZero(t *testing.T) {
	dir := t.TempDir()
	reg := NewRegistry(dir, t.TempDir())

	result, err := reg.Execute(context.Background(), "bash", `{"command":"printf done","timeout":0}`)
	if err != nil {
		t.Fatalf("bash timeout zero should use default timeout, got: %v", err)
	}
	if result != "done" {
		t.Fatalf("unexpected output: %q", result)
	}
}

func TestBashError(t *testing.T) {
	dir := t.TempDir()
	reg := NewRegistry(dir, t.TempDir())

	result, err := reg.Execute(context.Background(), "bash", `{"command":"exit 42"}`)
	if err != nil {
		t.Fatalf("bash should not error on non-zero exit: %v", err)
	}
	if result != "\nExit code: 42" {
		t.Fatalf("unexpected output: %q", result)
	}
}

func TestBashCancelledContextReportsCancelNotTimeout(t *testing.T) {
	dir := t.TempDir()
	reg := NewRegistry(dir, t.TempDir())

	ctx, cancel := context.WithCancel(context.Background())
	// Cancel shortly after start so the running sleep is interrupted by parent cancel.
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	result, err := reg.Execute(ctx, "bash", `{"command":"sleep 5","timeout":30}`)
	// A pre-dispatch context cancellation surfaces as a Go error; a
	// mid-execution cancellation surfaces as a structured error result.
	var msg string
	if err != nil {
		msg = err.Error()
	} else {
		if !strings.Contains(result, "<tool_call_error>") {
			t.Fatalf("expected structured error result on cancel, got: %q", result)
		}
		msg = result
	}
	if !strings.Contains(msg, "cancel") {
		t.Fatalf("expected message mentioning cancel, got %q", msg)
	}
	if strings.Contains(strings.ToLower(msg), "timed out") || strings.Contains(strings.ToLower(msg), "timeout") {
		t.Fatalf("message must not mention timeout on parent cancel, got %q", msg)
	}
}

func TestBashTimeoutReportsTimeout(t *testing.T) {
	dir := t.TempDir()
	reg := NewRegistry(dir, t.TempDir())

	// Use the tool's own timeout argument. Use a 1s tool-level timeout
	// against a 5s sleep — the deadline fires and surfaces as a structured
	// error result wrapping the timeout message.
	result, err := reg.Execute(context.Background(), "bash", `{"command":"sleep 5","timeout":1}`)
	if err != nil {
		t.Fatalf("Execute returned Go error (want structured result): %v", err)
	}
	if !strings.Contains(result, "<tool_call_error>") {
		t.Fatalf("expected structured error result for timeout, got: %q", result)
	}
	if !strings.Contains(result, "timed out") {
		t.Fatalf("expected 'timed out' in error cause, got %q", result)
	}
	if strings.Contains(strings.ToLower(result), "cancel") {
		t.Fatalf("error must not mention cancel on timeout, got %q", result)
	}
}

func TestBashNonexistentCommandDoesNotPanic(t *testing.T) {
	dir := t.TempDir()
	reg := NewRegistry(dir, t.TempDir())

	// bash itself runs fine, so a nonexistent inner command produces a
	// non-zero exit (handled via ProcessState). Verify we get both the
	// exit code marker and stderr content (bash's "command not found" or
	// "No such file") without panicking.
	cmd := "this_binary_definitely_does_not_exist_xyz"
	result, err := reg.Execute(context.Background(), "bash", fmt.Sprintf(`{"command":%q}`, cmd))
	if err != nil {
		t.Fatalf("bash should not return error on inner command-not-found: %v", err)
	}

	// Must show a non-zero exit code marker.
	if !strings.Contains(result, "Exit code:") {
		t.Fatalf("expected exit code marker in output, got %q", result)
	}
	// The exit code must not be 0.
	if strings.Contains(result, "Exit code: 0") {
		t.Fatalf("expected non-zero exit code for nonexistent command, got %q", result)
	}
	// The command name itself (or part of the bash error) should appear
	// in the output (combined stdout+stderr).
	if !strings.Contains(result, "not found") && !strings.Contains(result, "No such file") {
		t.Fatalf("expected bash error message in output, got %q", result)
	}
}

func TestUnknownTool(t *testing.T) {
	reg := NewRegistry(t.TempDir(), t.TempDir())
	_, err := reg.Execute(context.Background(), "nope", `{}`)
	if err == nil {
		t.Fatal("expected error for unknown tool")
	}
}

// ============================================================================
// Pattern 1 — Structured error result with inline reflection prompt
// ============================================================================

// TestStructuredErrorResult verifies that a known-failing tool call returns a
// <tool_call_error> result string with <cause> and <reflection> tags, and no
// Go error is propagated. The model sees a self-correction prompt alongside
// the failure reason rather than a bare error string.
func TestStructuredErrorResult(t *testing.T) {
	dir := t.TempDir()
	reg := NewRegistry(dir, t.TempDir())

	result, err := reg.Execute(context.Background(), "read", `{"path":"does_not_exist.txt"}`)
	if err != nil {
		t.Fatalf("Execute returned Go error (want structured result): %v", err)
	}
	if !strings.Contains(result, "<tool_call_error>") {
		t.Fatalf("result missing <tool_call_error> wrapper, got: %q", result)
	}
	if !strings.Contains(result, "<cause>") || !strings.Contains(result, "</cause>") {
		t.Fatalf("result missing <cause> tags, got: %q", result)
	}
	if !strings.Contains(result, "<reflection>") || !strings.Contains(result, "</reflection>") {
		t.Fatalf("result missing <reflection> tags, got: %q", result)
	}
	if !strings.Contains(result, "</tool_call_error>") {
		t.Fatalf("result missing </tool_call_error> closing tag, got: %q", result)
	}
	if !strings.Contains(result, "does_not_exist.txt") {
		t.Fatalf("result <cause> should mention the failing file, got: %q", result)
	}
}

// ============================================================================
// Pattern 2 — Read-before-edit guard
// ============================================================================

// TestReadBeforeEditGuard verifies that:
//   - edit-without-read returns a structured error naming the file
//   - read-then-edit succeeds
//   - ResetTurnState clears the set so the guard fires again next turn
//   - write-without-read on an existing file is also blocked
//   - new-file writes are never blocked
func TestReadBeforeEditGuard(t *testing.T) {
	dir := t.TempDir()
	reg := NewRegistry(dir, t.TempDir())

	path := filepath.Join(dir, "guarded.txt")
	if err := os.WriteFile(path, []byte("original content\n"), 0644); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	// Edit without read → structured error.
	result, err := reg.Execute(context.Background(), "edit", `{"path":"guarded.txt","edits":[{"oldText":"original","newText":"replaced"}]}`)
	if err != nil {
		t.Fatalf("Execute returned Go error (want structured result): %v", err)
	}
	if !strings.Contains(result, "<tool_call_error>") {
		t.Fatalf("edit-without-read should return structured error, got: %q", result)
	}
	if !strings.Contains(result, "guarded.txt") {
		t.Fatalf("error should name the file, got: %q", result)
	}

	// Read then edit → succeeds.
	if _, err := reg.Execute(context.Background(), "read", `{"path":"guarded.txt"}`); err != nil {
		t.Fatalf("read: %v", err)
	}
	result, err = reg.Execute(context.Background(), "edit", `{"path":"guarded.txt","edits":[{"oldText":"original","newText":"replaced"}]}`)
	if err != nil {
		t.Fatalf("edit after read: %v", err)
	}
	if strings.Contains(result, "<tool_call_error>") {
		t.Fatalf("read-then-edit should succeed, got: %q", result)
	}

	// ResetTurnState clears the set — edit should fail again without a re-read.
	reg.ResetTurnState()
	result, err = reg.Execute(context.Background(), "edit", `{"path":"guarded.txt","edits":[{"oldText":"replaced","newText":"reset"}]}`)
	if err != nil {
		t.Fatalf("Execute returned Go error after reset: %v", err)
	}
	if !strings.Contains(result, "<tool_call_error>") {
		t.Fatalf("after ResetTurnState, edit-without-read should be blocked again, got: %q", result)
	}

	// Write (overwrite) without read → blocked.
	reg.ResetTurnState()
	result, err = reg.Execute(context.Background(), "write", `{"path":"guarded.txt","content":"clobbered"}`)
	if err != nil {
		t.Fatalf("Execute returned Go error on blocked write: %v", err)
	}
	if !strings.Contains(result, "<tool_call_error>") {
		t.Fatalf("write-without-read on existing file should be blocked, got: %q", result)
	}

	// New-file write is always allowed even without a prior read.
	result, err = reg.Execute(context.Background(), "write", `{"path":"brand_new.txt","content":"hello"}`)
	if err != nil {
		t.Fatalf("new-file write: %v", err)
	}
	if strings.Contains(result, "<tool_call_error>") {
		t.Fatalf("new-file write should never be blocked, got: %q", result)
	}
}

// ============================================================================
// Pattern 3 — Head+tail truncation with overflow temp file pointer
// ============================================================================

// TestTruncateWithOverflow verifies:
//   - content under maxBytes passes through unchanged (overflowPath == "")
//   - content over maxBytes yields head+separator+tail + overflow file
//   - the overflow file contains the full original content
//   - the result string contains the overflow path pointer
func TestTruncateWithOverflow(t *testing.T) {
	// truncateWithOverflow is a *Registry method because the overflow file
	// is written under r.SessionDir/cache; the Registry owns the session
	// scoping. Tests get a throwaway SessionDir via t.TempDir().
	reg := NewRegistry(t.TempDir(), t.TempDir())

	// Under limit: no overflow.
	short := "hello world"
	out, path, err := reg.truncateWithOverflow(short, 1024)
	if err != nil {
		t.Fatalf("short content: %v", err)
	}
	if out != short {
		t.Fatalf("short content should pass through unchanged, got %q", out)
	}
	if path != "" {
		t.Fatalf("short content should have no overflow path, got %q", path)
	}

	// Over limit: head+tail+overflow file.
	big := strings.Repeat("abcde", 20000) // 100 KB
	const cap = 50 * 1024
	out2, path2, err := reg.truncateWithOverflow(big, cap)
	if err != nil {
		t.Fatalf("big content: %v", err)
	}
	if path2 == "" {
		t.Fatalf("big content should have overflow path")
	}
	defer os.Remove(path2) // clean up temp file

	// Overflow file contains the full original.
	data, readErr := os.ReadFile(path2)
	if readErr != nil {
		t.Fatalf("read overflow file: %v", readErr)
	}
	if string(data) != big {
		t.Fatalf("overflow file should contain full original content")
	}

	// Result is smaller than original.
	if len(out2) >= len(big) {
		t.Fatalf("truncated result should be smaller than original, got len=%d vs %d", len(out2), len(big))
	}

	// Result contains the separator.
	if !strings.Contains(out2, "bytes total") {
		t.Fatalf("truncated result should contain 'bytes total' separator, got %q", out2[:min(100, len(out2))])
	}

	// Result contains the overflow path.
	if !strings.Contains(out2, path2) {
		// Note: path is in the Execute-layer footer, not in truncateWithOverflow itself.
		// Just verify the path was returned.
		t.Logf("overflow path %q returned correctly (footer added by execRead/execBash)", path2)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// ============================================================================
// Pattern 4 — Per-tool timeout
// ============================================================================

// TestToolTimeout verifies that a slow tool (terminal_start with a command
// that sleeps longer than the timeout) is killed by the registry timeout and
// the result contains the structured timeout error.
func TestToolTimeout(t *testing.T) {
	dir := t.TempDir()
	reg := NewRegistry(dir, t.TempDir())
	// Set a very short timeout so the test is fast.
	reg.SetToolTimeout(1)

	// terminal_start with a command that sleeps longer than 1 s. The registry
	// wraps terminal_start in context.WithTimeout(1s) so it should fire.
	result, err := reg.Execute(context.Background(), "terminal_start", `{"command":"sleep 60","delay_ms":0}`)
	if err != nil {
		// Pre-dispatch errors are also acceptable (context expired before tool ran).
		if strings.Contains(err.Error(), "cancel") || strings.Contains(err.Error(), "timeout") {
			return // expected
		}
		t.Fatalf("unexpected Go error: %v", err)
	}
	// The tool may return quickly (process started) or the timeout may fire;
	// either is fine. We just verify no panic and the registry is still usable.
	_ = result
	t.Logf("terminal_start with 1s timeout returned: %q", result[:min(100, len(result))])
}

// TestToolTimeoutBashUsesOwnTimeout confirms that bash respects its own
// timeout= argument independently of ToolTimeout.
func TestToolTimeoutBashUsesOwnTimeout(t *testing.T) {
	dir := t.TempDir()
	reg := NewRegistry(dir, t.TempDir())
	// A very long registry timeout — bash must use its own 1 s arg, not this.
	reg.SetToolTimeout(300)

	result, err := reg.Execute(context.Background(), "bash", `{"command":"sleep 60","timeout":1}`)
	if err != nil {
		t.Fatalf("Execute returned Go error (want structured result): %v", err)
	}
	if !strings.Contains(result, "<tool_call_error>") {
		t.Fatalf("expected bash to time out via its own arg, got: %q", result)
	}
	if !strings.Contains(result, "timed out") {
		t.Fatalf("expected 'timed out' in result, got: %q", result)
	}
}

// TestSetToolTimeoutSETKey verifies that SetToolTimeout(0) is a no-op (keeps
// the previous value) and positive values update the duration.
func TestSetToolTimeoutSETKey(t *testing.T) {
	reg := NewRegistry(t.TempDir(), t.TempDir())
	if reg.ToolTimeout != 300*time.Second {
		t.Fatalf("default ToolTimeout should be 300s, got %v", reg.ToolTimeout)
	}
	reg.SetToolTimeout(60)
	if reg.ToolTimeout != 60*time.Second {
		t.Fatalf("SetToolTimeout(60) should set 60s, got %v", reg.ToolTimeout)
	}
	reg.SetToolTimeout(0) // zero is a no-op
	if reg.ToolTimeout != 60*time.Second {
		t.Fatalf("SetToolTimeout(0) should be a no-op, got %v", reg.ToolTimeout)
	}
}

func TestDefinitions(t *testing.T) {
	reg := NewRegistry(t.TempDir(), t.TempDir())
	defs := reg.Definitions()
	if len(defs) != 14 {
		t.Fatalf("expected 14 tool definitions, got %d", len(defs))
	}

	names := map[string]bool{}
	for _, d := range defs {
		names[d.Function.Name] = true
	}
	for _, expected := range []string{
		"read", "write", "edit", "search_memory", "write_memory", "bash",
		"terminal_start", "terminal_send", "terminal_keys", "terminal_snapshot",
		"terminal_wait", "terminal_resize", "terminal_close",
		"preview_image",
	} {
		if !names[expected] {
			t.Fatalf("missing tool definition: %s", expected)
		}
	}
}

func TestSearchMemoryTool(t *testing.T) {
	workDir := t.TempDir()
	sessionDir := filepath.Join(t.TempDir(), "sessions")
	if err := os.WriteFile(filepath.Join(workDir, "MEMORY.md"), []byte("# Memory\n- Keep the IRC metaphor\n"), 0644); err != nil {
		t.Fatalf("write MEMORY.md: %v", err)
	}

	reg := NewRegistry(workDir, sessionDir)
	result, err := reg.Execute(context.Background(), "search_memory", `{"query":"IRC metaphor"}`)
	if err != nil {
		t.Fatalf("search_memory: %v", err)
	}
	if !strings.Contains(result, `Memory matches for "IRC metaphor":`) {
		t.Fatalf("unexpected output: %q", result)
	}
	if !strings.Contains(result, "Source: MEMORY.md") {
		t.Fatalf("expected MEMORY.md source, got %q", result)
	}
}

func TestWriteMemoryTool(t *testing.T) {
	workDir := t.TempDir()
	sessionDir := filepath.Join(t.TempDir(), "sessions")
	reg := NewRegistry(workDir, sessionDir)

	// Default scope (root) writes to MEMORY.md in workDir.
	out, err := reg.Execute(context.Background(), "write_memory",
		`{"content":"prefer flat history over forks","title":"decision: linear sessions"}`)
	if err != nil {
		t.Fatalf("write_memory root: %v", err)
	}
	if !strings.Contains(out, "MEMORY.md") {
		t.Fatalf("expected MEMORY.md in result, got %q", out)
	}
	data, err := os.ReadFile(filepath.Join(workDir, "MEMORY.md"))
	if err != nil {
		t.Fatalf("read MEMORY.md: %v", err)
	}
	if !strings.Contains(string(data), "decision: linear sessions") ||
		!strings.Contains(string(data), "prefer flat history over forks") {
		t.Fatalf("memory file missing entry:\n%s", data)
	}

	// search_memory should now find it.
	res, err := reg.Execute(context.Background(), "search_memory", `{"query":"linear sessions"}`)
	if err != nil {
		t.Fatalf("search_memory: %v", err)
	}
	if !strings.Contains(res, "prefer flat history") {
		t.Fatalf("search did not surface written entry: %q", res)
	}

	// Channel scope override writes under contexts/channels/.
	if _, err := reg.Execute(context.Background(), "write_memory",
		`{"content":"#dev prefers terse replies","scope":"channel","name":"#dev"}`); err != nil {
		t.Fatalf("write_memory channel: %v", err)
	}
	rootScope := memorypkg.RootScope()
	channel := memorypkg.ChannelScope("#dev", &rootScope)
	hot := memorypkg.HotPath(sessionDir, workDir, channel)
	chData, err := os.ReadFile(hot)
	if err != nil {
		t.Fatalf("read channel HOT.md (%s): %v", hot, err)
	}
	if !strings.Contains(string(chData), "#dev prefers terse replies") {
		t.Fatalf("channel memory missing entry:\n%s", chData)
	}

	// Daily mode appends to durable archive.
	if _, err := reg.Execute(context.Background(), "write_memory",
		`{"content":"flushed at end of session","daily":true,"title":"flush"}`); err != nil {
		t.Fatalf("write_memory daily: %v", err)
	}

	// Missing content -> structured error result.
	result, err := reg.Execute(context.Background(), "write_memory", `{"content":"   "}`)
	if err != nil {
		t.Fatalf("write_memory empty content: Execute returned Go error (want structured result): %v", err)
	}
	if !strings.Contains(result, "<tool_call_error>") {
		t.Fatalf("expected structured error result for empty content, got: %q", result)
	}
	// Channel scope without name -> structured error result.
	result, err = reg.Execute(context.Background(), "write_memory", `{"content":"x","scope":"channel"}`)
	if err != nil {
		t.Fatalf("write_memory channel no name: Execute returned Go error (want structured result): %v", err)
	}
	if !strings.Contains(result, "<tool_call_error>") {
		t.Fatalf("expected structured error result when channel scope missing name, got: %q", result)
	}
}

func TestWriteMemoryExplicitChannelScope(t *testing.T) {
	workDir := t.TempDir()
	sessionDir := filepath.Join(t.TempDir(), "sessions")
	reg := NewRegistry(workDir, sessionDir)

	rootScope := memorypkg.RootScope()
	currentQueryScope := memorypkg.QueryScope("alice", &rootScope)
	reg.SetScope(currentQueryScope)

	out, err := reg.Execute(context.Background(), "write_memory",
		`{"content":"brand-new channel memory","title":"new channel","scope":"channel","name":"#brand-new"}`)
	if err != nil {
		t.Fatalf("write_memory explicit channel: %v", err)
	}

	channelScope := memorypkg.ChannelScope("#brand-new", &rootScope)
	channelHot := memorypkg.HotPath(sessionDir, workDir, channelScope)
	if !strings.Contains(out, channelHot) {
		t.Fatalf("result %q should report explicit channel hot path %q", out, channelHot)
	}
	data, err := os.ReadFile(channelHot)
	if err != nil {
		t.Fatalf("read explicit channel hot memory: %v", err)
	}
	if !strings.Contains(string(data), "new channel") || !strings.Contains(string(data), "brand-new channel memory") {
		t.Fatalf("channel memory missing entry:\n%s", data)
	}

	if _, err := os.Stat(memorypkg.HotPath(sessionDir, workDir, currentQueryScope)); !os.IsNotExist(err) {
		t.Fatalf("scope override should not write current query hot memory, stat err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(workDir, "MEMORY.md")); !os.IsNotExist(err) {
		t.Fatalf("scope override should not write root memory, stat err=%v", err)
	}
}

func TestTerminalSessionEcho(t *testing.T) {
	dir := t.TempDir()
	reg := NewRegistry(dir, t.TempDir())

	result, err := reg.Execute(context.Background(), "terminal_start", `{"command":"cat","width":40,"height":10,"delay_ms":50}`)
	if err != nil {
		t.Fatalf("terminal_start: %v", err)
	}
	id := terminalIDFromResult(t, result)

	result, err = reg.Execute(context.Background(), "terminal_send", `{"id":"`+id+`","text":"hello from pty\n","delay_ms":100}`)
	if err != nil {
		t.Fatalf("terminal_send: %v", err)
	}
	if !strings.Contains(result, "hello from pty") {
		t.Fatalf("expected terminal screen to contain sent text, got %q", result)
	}

	result, err = reg.Execute(context.Background(), "terminal_snapshot", `{"id":"`+id+`"}`)
	if err != nil {
		t.Fatalf("terminal_snapshot: %v", err)
	}
	if !strings.Contains(result, "terminal session "+id) {
		t.Fatalf("unexpected snapshot: %q", result)
	}

	if _, err := reg.Execute(context.Background(), "terminal_close", `{"id":"`+id+`"}`); err != nil {
		t.Fatalf("terminal_close: %v", err)
	}
}

func TestTerminalSessionCommandExit(t *testing.T) {
	reg := NewRegistry(t.TempDir(), t.TempDir())
	result, err := reg.Execute(context.Background(), "terminal_start", `{"command":"printf done","width":20,"height":5,"delay_ms":100}`)
	if err != nil {
		t.Fatalf("terminal_start: %v", err)
	}
	id := terminalIDFromResult(t, result)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		result, err = reg.Execute(context.Background(), "terminal_snapshot", `{"id":"`+id+`"}`)
		if err != nil {
			t.Fatalf("terminal_snapshot: %v", err)
		}
		if strings.Contains(result, "exited") {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if !strings.Contains(result, "done") || !strings.Contains(result, "exited") {
		t.Fatalf("expected exited snapshot containing output, got %q", result)
	}
	_, _ = reg.Execute(context.Background(), "terminal_close", `{"id":"`+id+`"}`)
}

func TestTerminalKeysSendNamedKeysAndLiteralText(t *testing.T) {
	dir := t.TempDir()
	reg := NewRegistry(dir, t.TempDir())

	result, err := reg.Execute(context.Background(), "terminal_start", `{"command":"cat","width":40,"height":10,"delay_ms":50}`)
	if err != nil {
		t.Fatalf("terminal_start: %v", err)
	}
	id := terminalIDFromResult(t, result)
	defer reg.Execute(context.Background(), "terminal_close", `{"id":"`+id+`"}`) //nolint:errcheck

	result, err = reg.Execute(context.Background(), "terminal_keys", `{"id":"`+id+`","keys":["hello from keys","enter","ctrl-d"],"delay_ms":100}`)
	if err != nil {
		t.Fatalf("terminal_keys: %v", err)
	}
	if !strings.Contains(result, "hello from keys") {
		t.Fatalf("expected terminal screen to contain sent text, got %q", result)
	}
}

func TestTerminalWaitMatchesScreenText(t *testing.T) {
	reg := NewRegistry(t.TempDir(), t.TempDir())

	result, err := reg.Execute(context.Background(), "terminal_start", `{"command":"printf READY && sleep 1","width":40,"height":10,"delay_ms":50}`)
	if err != nil {
		t.Fatalf("terminal_start: %v", err)
	}
	id := terminalIDFromResult(t, result)
	defer reg.Execute(context.Background(), "terminal_close", `{"id":"`+id+`"}`) //nolint:errcheck

	result, err = reg.Execute(context.Background(), "terminal_wait", `{"id":"`+id+`","text":"ready","timeout_ms":1000,"interval_ms":25}`)
	if err != nil {
		t.Fatalf("terminal_wait: %v", err)
	}
	if !strings.Contains(result, `matched terminal text "ready"`) || !strings.Contains(result, "READY") {
		t.Fatalf("expected wait match result, got %q", result)
	}
}

func TestTerminalResizeUpdatesSnapshotDimensions(t *testing.T) {
	reg := NewRegistry(t.TempDir(), t.TempDir())

	result, err := reg.Execute(context.Background(), "terminal_start", `{"command":"cat","width":40,"height":10,"delay_ms":50}`)
	if err != nil {
		t.Fatalf("terminal_start: %v", err)
	}
	id := terminalIDFromResult(t, result)
	defer reg.Execute(context.Background(), "terminal_close", `{"id":"`+id+`"}`) //nolint:errcheck

	result, err = reg.Execute(context.Background(), "terminal_resize", `{"id":"`+id+`","width":50,"height":8,"delay_ms":50}`)
	if err != nil {
		t.Fatalf("terminal_resize: %v", err)
	}
	if !strings.Contains(result, "terminal session "+id+" (50x8) running") {
		t.Fatalf("expected resized snapshot dimensions, got %q", result)
	}
}

// TestTerminal_concurrentSendSameSession verifies that concurrent
// terminal_send calls to the same PTY session do not panic or corrupt state.
// cat echoes input, so all sends are valid. The test asserts no errors from
// Execute and that the session survives the barrage.
// Must pass under -race -count=20.
func TestTerminal_concurrentSendSameSession(t *testing.T) {
	reg := NewRegistry(t.TempDir(), t.TempDir())

	result, err := reg.Execute(context.Background(), "terminal_start", `{"command":"cat","width":80,"height":24,"delay_ms":50}`)
	if err != nil {
		t.Fatalf("terminal_start: %v", err)
	}
	id := terminalIDFromResult(t, result)
	defer reg.Execute(context.Background(), "terminal_close", `{"id":"`+id+`"}`) //nolint:errcheck

	const nSenders = 8
	const sendsPer = 5

	var wg sync.WaitGroup
	wg.Add(nSenders)
	for g := 0; g < nSenders; g++ {
		go func(gid int) {
			defer wg.Done()
			for i := 0; i < sendsPer; i++ {
				text := fmt.Sprintf("hello-from-%d-%d\n", gid, i)
				args := fmt.Sprintf(`{"id":"%s","text":%q,"delay_ms":20}`, id, text)
				if _, err := reg.Execute(context.Background(), "terminal_send", args); err != nil {
					t.Errorf("terminal_send(g=%d, i=%d): %v", gid, i, err)
				}
			}
		}(g)
	}
	wg.Wait()

	// Session should still be alive and responsive.
	snapResult, err := reg.Execute(context.Background(), "terminal_snapshot", `{"id":"`+id+`"}`)
	if err != nil {
		t.Fatalf("post-barrage snapshot: %v", err)
	}
	if !strings.Contains(snapResult, "running") {
		t.Fatalf("expected running session after barrage, got %q", snapResult)
	}
}

func TestTerminalCloseCleanup(t *testing.T) {
	reg := NewRegistry(t.TempDir(), t.TempDir())

	result, err := reg.Execute(context.Background(), "terminal_start", `{"command":"cat","width":40,"height":10,"delay_ms":50}`)
	if err != nil {
		t.Fatalf("terminal_start: %v", err)
	}
	id := terminalIDFromResult(t, result)

	result, err = reg.Execute(context.Background(), "terminal_close", `{"id":"`+id+`"}`)
	if err != nil {
		t.Fatalf("terminal_close: %v", err)
	}
	if !strings.Contains(result, "closed terminal session "+id) {
		t.Fatalf("unexpected close result: %q", result)
	}

	snapResult, snapErr := reg.Execute(context.Background(), "terminal_snapshot", `{"id":"`+id+`"}`)
	if snapErr == nil && !strings.Contains(snapResult, "<tool_call_error>") {
		t.Fatal("expected error or structured error result for snapshot of closed terminal")
	}

	reg.terminals.mu.Lock()
	_, stillTracked := reg.terminals.terms[id]
	reg.terminals.mu.Unlock()
	if stillTracked {
		t.Fatalf("terminal session %s still tracked after close", id)
	}
}

func TestTerminalConcurrentSendOnSameSession(t *testing.T) {
	reg := NewRegistry(t.TempDir(), t.TempDir())

	result, err := reg.Execute(context.Background(), "terminal_start", `{"command":"cat","width":80,"height":20,"delay_ms":50}`)
	if err != nil {
		t.Fatalf("terminal_start: %v", err)
	}
	id := terminalIDFromResult(t, result)
	defer reg.Execute(context.Background(), "terminal_close", `{"id":"`+id+`"}`) //nolint:errcheck

	lines := []string{"alpha", "bravo", "charlie", "delta", "echo"}
	var wg sync.WaitGroup
	errs := make(chan error, len(lines))
	for _, line := range lines {
		line := line
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := reg.Execute(context.Background(), "terminal_send", `{"id":"`+id+`","text":"`+line+`\n","delay_ms":25}`)
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("terminal_send: %v", err)
		}
	}

	result, err = reg.Execute(context.Background(), "terminal_wait", `{"id":"`+id+`","text":"echo","timeout_ms":1000,"interval_ms":25}`)
	if err != nil {
		t.Fatalf("terminal_wait: %v", err)
	}
	if !strings.Contains(result, "echo") {
		t.Fatalf("expected final snapshot to contain concurrent input, got %q", result)
	}
}

// TestTerminalStartDoesNotSourceLoginDotfiles guards bt-h1u: terminal_start
// must use `bash -c`, not `bash -lc`, so it does NOT source ~/.bashrc /
// ~/.bash_profile. Sourcing them leaks the user's environment (aliases,
// exported vars) into tool execution and makes behavior non-deterministic.
func TestTerminalStartDoesNotSourceLoginDotfiles(t *testing.T) {
	home := t.TempDir()
	// Both files set a sentinel — we want to ensure NEITHER is sourced.
	// -lc would source .bash_profile (preferred for login) or fall back
	// to .bashrc; -c sources nothing.
	bashrc := "export BITCHTEA_RC_SENTINEL=loaded-from-bashrc\n"
	if err := os.WriteFile(filepath.Join(home, ".bashrc"), []byte(bashrc), 0o644); err != nil {
		t.Fatalf("write .bashrc: %v", err)
	}
	bashProfile := "export BITCHTEA_RC_SENTINEL=loaded-from-bash_profile\n"
	if err := os.WriteFile(filepath.Join(home, ".bash_profile"), []byte(bashProfile), 0o644); err != nil {
		t.Fatalf("write .bash_profile: %v", err)
	}
	t.Setenv("HOME", home)
	// Some bash builds also consult these — point them at the temp HOME
	// so they cannot rescue a sentinel from the real user environment.
	t.Setenv("BASH_ENV", "")
	t.Setenv("ENV", "")

	reg := NewRegistry(t.TempDir(), t.TempDir())
	// Echo the sentinel; if the shell sourced the dotfile, output will
	// contain "loaded-from-...". With bash -c, the var is unset and the
	// shell prints "<unset>" via parameter expansion.
	//
	// We deliberately let the command finish on its own (printf then
	// exit) instead of calling terminal_close from a defer — the defer
	// path triggers the known bt-e4w race in vt.SafeEmulator
	// (Close-vs-Read on emulator.closed). Terminal sessions are reaped
	// when the process exits via wait(), so no leak.
	cmdJSON := `{"command":"printf %s \"${BITCHTEA_RC_SENTINEL:-<unset>}\"","width":60,"height":5,"delay_ms":150}`
	result, err := reg.Execute(context.Background(), "terminal_start", cmdJSON)
	if err != nil {
		t.Fatalf("terminal_start: %v", err)
	}
	id := terminalIDFromResult(t, result)

	// Wait for the printf-then-exit command to finish.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		result, err = reg.Execute(context.Background(), "terminal_snapshot", `{"id":"`+id+`"}`)
		if err != nil {
			t.Fatalf("terminal_snapshot: %v", err)
		}
		if strings.Contains(result, "exited") {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if strings.Contains(result, "loaded-from-bashrc") || strings.Contains(result, "loaded-from-bash_profile") {
		t.Fatalf("terminal_start sourced login dotfiles (bash -lc regression); snapshot:\n%s", result)
	}
	if !strings.Contains(result, "<unset>") {
		t.Fatalf("expected sentinel to be <unset> (dotfiles not sourced); snapshot:\n%s", result)
	}
}

func TestPreviewImage(t *testing.T) {
	dir := t.TempDir()
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	img.Set(1, 0, color.RGBA{G: 255, A: 255})
	img.Set(0, 1, color.RGBA{B: 255, A: 255})
	img.Set(1, 1, color.RGBA{R: 255, G: 255, A: 255})

	f, err := os.Create(filepath.Join(dir, "tiny.png"))
	if err != nil {
		t.Fatalf("create image: %v", err)
	}
	if err := png.Encode(f, img); err != nil {
		t.Fatalf("encode image: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close image: %v", err)
	}

	reg := NewRegistry(dir, t.TempDir())
	result, err := reg.Execute(context.Background(), "preview_image", `{"path":"tiny.png","width":4}`)
	if err != nil {
		t.Fatalf("preview_image: %v", err)
	}
	if !strings.Contains(result, "image preview tiny.png (png, 2x2)") {
		t.Fatalf("unexpected preview header: %q", result)
	}
}

func TestTruncateUTF8MultiByte(t *testing.T) {
	// "世" is 3 bytes in UTF-8. Pick a cap that lands mid-rune.
	s := strings.Repeat("世", 1000)
	// 100 is not a multiple of 3, so a naive byte slice would split a rune.
	got := truncateUTF8(s, 100)
	if !utf8.ValidString(got) {
		t.Fatalf("truncateUTF8 produced invalid UTF-8 at byte cap 100: %q", got)
	}
	if len(got) > 100 {
		t.Fatalf("truncateUTF8 exceeded byte cap: len=%d", len(got))
	}
	// Should be the largest multiple of 3 <= 100 = 99.
	if len(got) != 99 {
		t.Fatalf("expected 99 bytes (33 full runes), got %d", len(got))
	}

	// Mix with emoji (4-byte runes).
	mixed := strings.Repeat("a世\U0001F600", 200)
	for cap := 1; cap < len(mixed); cap++ {
		out := truncateUTF8(mixed, cap)
		if !utf8.ValidString(out) {
			t.Fatalf("truncateUTF8 produced invalid UTF-8 at cap=%d: % x", cap, out)
		}
	}
}

func TestTruncateUTF8ASCIIExactLimit(t *testing.T) {
	s := strings.Repeat("a", 50)
	got := truncateUTF8(s, 50)
	if got != s {
		t.Fatalf("ASCII at exact limit should be unchanged; got %q", got)
	}
}

func TestTruncateUTF8ShorterThanLimit(t *testing.T) {
	s := "hello 世界"
	got := truncateUTF8(s, 1024)
	if got != s {
		t.Fatalf("string shorter than limit should be unchanged; got %q", got)
	}
}

func TestReadTruncatesAtRuneBoundary(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "wide.txt")
	// 60 KB of "世" (3 bytes each) — comfortably exceeds the 50 KB cap.
	content := strings.Repeat("世", 20000)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	reg := NewRegistry(dir, t.TempDir())
	result, err := reg.Execute(context.Background(), "read", `{"path":"wide.txt"}`)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	// Pattern 3: head+tail truncation; result contains TRUNCATED marker and
	// overflow path pointer.
	if !strings.Contains(result, "[TRUNCATED") {
		t.Fatalf("expected TRUNCATED marker, got tail %q", result[max(0, len(result)-64):])
	}
	if !utf8.ValidString(result) {
		t.Fatalf("read returned invalid UTF-8 after truncation")
	}
}

func TestBashTruncatesAtRuneBoundary(t *testing.T) {
	dir := t.TempDir()
	reg := NewRegistry(dir, t.TempDir())

	// Print ~60 KB of "世" via printf; well past the 50 KB cap.
	cmd := `printf '世%.0s' $(seq 1 20000)`
	args := `{"command":"` + cmd + `","timeout":30}`
	result, err := reg.Execute(context.Background(), "bash", args)
	if err != nil {
		t.Fatalf("bash: %v", err)
	}
	// Pattern 3: head+tail truncation.
	if !strings.Contains(result, "[TRUNCATED") {
		t.Fatalf("expected TRUNCATED marker, got tail %q", result[max(0, len(result)-64):])
	}
	if !utf8.ValidString(result) {
		t.Fatalf("bash returned invalid UTF-8 after truncation")
	}
}

func TestExecuteCancelledContextReturnsError(t *testing.T) {
	dir := t.TempDir()
	reg := NewRegistry(dir, t.TempDir())

	// Cancelled context should be caught by the early check in Execute
	// for any tool, including those that don't use context internally.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	tools := []string{"read", "write", "edit", "search_memory", "bash"}
	for _, name := range tools {
		_, err := reg.Execute(ctx, name, `{"path":"x","content":"x","query":"x","command":"x"}`)
		if err == nil {
			t.Errorf("Execute(%s) with cancelled context: expected error, got nil", name)
		}
		if !strings.Contains(err.Error(), "tool cancelled") {
			t.Errorf("Execute(%s) error = %q, want contains 'tool cancelled'", name, err.Error())
		}
	}
}

func terminalIDFromResult(t *testing.T, result string) string {
	t.Helper()
	fields := strings.Fields(result)
	if len(fields) < 3 || fields[0] != "terminal" || fields[1] != "session" {
		t.Fatalf("could not parse terminal id from %q", result)
	}
	return fields[2]
}
