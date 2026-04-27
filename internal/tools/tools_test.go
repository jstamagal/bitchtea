package tools

import (
	"context"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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
	if len(defs) != 10 {
		t.Fatalf("expected 10 tool definitions, got %d", len(defs))
	}

	names := map[string]bool{}
	for _, d := range defs {
		names[d.Function.Name] = true
	}
	for _, expected := range []string{
		"read", "write", "edit", "search_memory", "bash",
		"terminal_start", "terminal_send", "terminal_snapshot", "terminal_close",
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

func terminalIDFromResult(t *testing.T, result string) string {
	t.Helper()
	fields := strings.Fields(result)
	if len(fields) < 3 || fields[0] != "terminal" || fields[1] != "session" {
		t.Fatalf("could not parse terminal id from %q", result)
	}
	return fields[2]
}
