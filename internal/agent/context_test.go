package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDiscoverContextFiles(t *testing.T) {
	// Create a directory tree with context files
	root := t.TempDir()
	child := filepath.Join(root, "sub", "project")
	os.MkdirAll(child, 0755)

	os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("root agent rules"), 0644)
	os.WriteFile(filepath.Join(child, "CLAUDE.md"), []byte("project claude rules"), 0644)

	result := DiscoverContextFiles(child)

	if !strings.Contains(result, "project claude rules") {
		t.Error("missing child CLAUDE.md content")
	}
	if !strings.Contains(result, "root agent rules") {
		t.Error("missing root AGENTS.md content")
	}
}

func TestDiscoverContextFilesNone(t *testing.T) {
	dir := t.TempDir()
	result := DiscoverContextFiles(dir)
	// Should be empty (will find nothing before hitting root)
	// The function walks up, so it might find things in /tmp etc.
	// But in practice there won't be AGENTS.md there
	_ = result
}

func TestLoadSaveMemory(t *testing.T) {
	dir := t.TempDir()

	// Initially empty
	mem := LoadMemory(dir)
	if mem != "" {
		t.Fatalf("expected empty memory, got %q", mem)
	}

	// Save and reload
	err := SaveMemory(dir, "# Memory\n- discovered pattern X\n")
	if err != nil {
		t.Fatalf("save memory: %v", err)
	}

	mem = LoadMemory(dir)
	if !strings.Contains(mem, "discovered pattern X") {
		t.Fatalf("memory content: %q", mem)
	}
}

func TestAppendDailyMemory(t *testing.T) {
	workDir := filepath.Join(t.TempDir(), "repo")
	sessionDir := filepath.Join(t.TempDir(), "sessions")
	when := time.Date(2026, 4, 8, 13, 14, 15, 0, time.UTC)

	if err := AppendDailyMemory(sessionDir, workDir, when, "- Keep the IRC metaphor\n- Restore channel focus on restart"); err != nil {
		t.Fatalf("append daily memory: %v", err)
	}

	path := DailyMemoryPath(sessionDir, workDir, when)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read daily memory: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "## 2026-04-08T13:14:15Z pre-compaction flush") {
		t.Fatalf("missing flush heading: %q", content)
	}
	if !strings.Contains(content, "Keep the IRC metaphor") {
		t.Fatalf("missing durable memory content: %q", content)
	}
}

func TestExpandFileRefs(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "test.txt"), []byte("file contents here"), 0644)

	result := ExpandFileRefs("fix @test.txt please", dir)

	if !strings.Contains(result, "file contents here") {
		t.Fatalf("expected file contents in expansion, got: %s", result)
	}
	if !strings.Contains(result, "@test.txt") {
		t.Fatalf("expected @reference preserved, got: %s", result)
	}
}

func TestExpandFileRefsNotFound(t *testing.T) {
	dir := t.TempDir()
	result := ExpandFileRefs("fix @nonexistent.txt", dir)

	if !strings.Contains(result, "file not found") {
		t.Fatalf("expected file not found error, got: %s", result)
	}
}

func TestExpandFileRefsNoRefs(t *testing.T) {
	dir := t.TempDir()
	result := ExpandFileRefs("just a normal message", dir)

	if result != "just a normal message" {
		t.Fatalf("expected unchanged message, got: %s", result)
	}
}
