package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
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

func TestWriteFile(t *testing.T) {
	dir := t.TempDir()
	reg := NewRegistry(dir, t.TempDir())

	result, err := reg.Execute(context.Background(), "write", `{"path":"sub/dir/out.txt","content":"hello world"}`)
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	if result != "Wrote 11 bytes to sub/dir/out.txt" {
		t.Fatalf("unexpected result: %q", result)
	}

	data, _ := os.ReadFile(filepath.Join(dir, "sub/dir/out.txt"))
	if string(data) != "hello world" {
		t.Fatalf("file content: %q", string(data))
	}
}

func TestEditFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "edit.txt")
	os.WriteFile(path, []byte("foo bar baz\nhello world\n"), 0644)

	reg := NewRegistry(dir, t.TempDir())

	result, err := reg.Execute(context.Background(), "edit", `{"path":"edit.txt","edits":[{"oldText":"hello world","newText":"goodbye world"}]}`)
	if err != nil {
		t.Fatalf("edit: %v", err)
	}
	if result != "Applied 1 edit(s) to edit.txt" {
		t.Fatalf("unexpected result: %q", result)
	}

	data, _ := os.ReadFile(path)
	if string(data) != "foo bar baz\ngoodbye world\n" {
		t.Fatalf("edited content: %q", string(data))
	}
}

func TestEditFileNonUnique(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dup.txt")
	os.WriteFile(path, []byte("aaa\naaa\n"), 0644)

	reg := NewRegistry(dir, t.TempDir())

	_, err := reg.Execute(context.Background(), "edit", `{"path":"dup.txt","edits":[{"oldText":"aaa","newText":"bbb"}]}`)
	if err == nil {
		t.Fatal("expected error for non-unique oldText")
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

func TestUnknownTool(t *testing.T) {
	reg := NewRegistry(t.TempDir(), t.TempDir())
	_, err := reg.Execute(context.Background(), "nope", `{}`)
	if err == nil {
		t.Fatal("expected error for unknown tool")
	}
}

func TestDefinitions(t *testing.T) {
	reg := NewRegistry(t.TempDir(), t.TempDir())
	defs := reg.Definitions()
	if len(defs) != 5 {
		t.Fatalf("expected 5 tool definitions, got %d", len(defs))
	}

	names := map[string]bool{}
	for _, d := range defs {
		names[d.Function.Name] = true
	}
	for _, expected := range []string{"read", "write", "edit", "search_memory", "bash"} {
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
