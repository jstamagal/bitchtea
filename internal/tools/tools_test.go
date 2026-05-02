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

	// Offset well past EOF on a non-empty file -> error mentioning offset/length
	_, err := reg.Execute(context.Background(), "read", `{"path":"lines.txt","offset":99}`)
	if err == nil {
		t.Fatalf("expected error for offset past EOF, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "past end of file") || !strings.Contains(msg, "99") || !strings.Contains(msg, "3") {
		t.Fatalf("error should mention offset and length, got: %q", msg)
	}

	// Offset just past last addressable line -> error
	// File has 3 lines; strings.Split yields 3 elements, so offset 4 -> start=3 >= 3 -> error.
	_, err = reg.Execute(context.Background(), "read", `{"path":"lines.txt","offset":4}`)
	if err == nil {
		t.Fatalf("expected error for offset at len(lines)+1, got nil")
	}

	// Normal in-range offset still works
	result, err := reg.Execute(context.Background(), "read", `{"path":"lines.txt","offset":2,"limit":1}`)
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

func TestEditFileEmptyOldText(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.txt")
	os.WriteFile(path, []byte("foo bar\n"), 0644)

	reg := NewRegistry(dir, t.TempDir())

	_, err := reg.Execute(context.Background(), "edit", `{"path":"empty.txt","edits":[{"oldText":"","newText":"injected"}]}`)
	if err == nil {
		t.Fatal("expected error for empty oldText")
	}
	msg := err.Error()
	if !strings.Contains(msg, "oldText") || !strings.Contains(msg, "empty") || !strings.Contains(msg, "write") {
		t.Fatalf("error message %q should mention oldText, empty, and write", msg)
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

	_, err := reg.Execute(context.Background(), "edit", `{"path":"missing.txt","edits":[{"oldText":"nonexistent","newText":"x"}]}`)
	if err == nil {
		t.Fatal("expected error for oldText not found")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected 'not found' error, got: %v", err)
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

	// Missing content fails.
	if _, err := reg.Execute(context.Background(), "write_memory", `{"content":"   "}`); err == nil {
		t.Fatal("expected error for empty content")
	}
	// Channel scope without name fails.
	if _, err := reg.Execute(context.Background(), "write_memory", `{"content":"x","scope":"channel"}`); err == nil {
		t.Fatal("expected error when channel scope missing name")
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

func terminalIDFromResult(t *testing.T, result string) string {
	t.Helper()
	fields := strings.Fields(result)
	if len(fields) < 3 || fields[0] != "terminal" || fields[1] != "session" {
		t.Fatalf("could not parse terminal id from %q", result)
	}
	return fields[2]
}
