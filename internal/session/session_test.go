package session

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewAndAppend(t *testing.T) {
	dir := t.TempDir()

	s, err := New(dir)
	if err != nil {
		t.Fatalf("new session: %v", err)
	}

	err = s.Append(Entry{Role: "user", Content: "hello"})
	if err != nil {
		t.Fatalf("append: %v", err)
	}

	err = s.Append(Entry{Role: "assistant", Content: "world"})
	if err != nil {
		t.Fatalf("append: %v", err)
	}

	if len(s.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(s.Entries))
	}

	// Verify file exists
	if _, err := os.Stat(s.Path); err != nil {
		t.Fatalf("session file not created: %v", err)
	}
}

func TestLoadSession(t *testing.T) {
	dir := t.TempDir()

	s, err := New(dir)
	if err != nil {
		t.Fatalf("new: %v", err)
	}

	s.Append(Entry{Role: "user", Content: "test msg"})
	s.Append(Entry{Role: "assistant", Content: "test reply"})

	// Load it back
	loaded, err := Load(s.Path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if len(loaded.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(loaded.Entries))
	}
	if loaded.Entries[0].Content != "test msg" {
		t.Fatalf("wrong content: %q", loaded.Entries[0].Content)
	}
}

func TestListSessions(t *testing.T) {
	dir := t.TempDir()

	// Create some session files
	os.WriteFile(filepath.Join(dir, "2026-01-01_120000.jsonl"), []byte("{}"), 0644)
	os.WriteFile(filepath.Join(dir, "2026-01-02_120000.jsonl"), []byte("{}"), 0644)
	os.WriteFile(filepath.Join(dir, "not-a-session.txt"), []byte("{}"), 0644)

	sessions, err := List(dir)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(sessions))
	}
}

func TestListNonexistentDir(t *testing.T) {
	sessions, err := List("/tmp/nonexistent-bitchtea-test-dir")
	if err != nil {
		t.Fatalf("should not error: %v", err)
	}
	if sessions != nil {
		t.Fatalf("expected nil sessions, got %v", sessions)
	}
}
