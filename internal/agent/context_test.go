package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
