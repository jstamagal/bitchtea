package session

import (
	"os"
	"path/filepath"
	"strings"
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

func TestFork(t *testing.T) {
	dir := t.TempDir()

	s, err := New(dir)
	if err != nil {
		t.Fatalf("new: %v", err)
	}

	s.Append(Entry{Role: "user", Content: "first"})
	s.Append(Entry{Role: "assistant", Content: "reply"})
	s.Append(Entry{Role: "user", Content: "second"})

	// Fork from second entry
	forkID := s.Entries[1].ID
	forked, err := s.Fork(forkID)
	if err != nil {
		t.Fatalf("fork: %v", err)
	}

	if len(forked.Entries) != 2 {
		t.Fatalf("expected 2 entries in fork, got %d", len(forked.Entries))
	}
	if forked.Entries[1].Content != "reply" {
		t.Fatalf("wrong fork content: %q", forked.Entries[1].Content)
	}
	if forked.Path == s.Path {
		t.Fatal("fork should have different path")
	}
}

func TestTree(t *testing.T) {
	dir := t.TempDir()

	s, _ := New(dir)
	s.Append(Entry{Role: "user", Content: "hello"})
	s.Append(Entry{Role: "assistant", Content: "world"})

	tree := s.Tree()
	if tree == "" {
		t.Fatal("expected non-empty tree")
	}
	if !strings.Contains(tree, "user") {
		t.Error("tree should contain 'user' role")
	}
	if !strings.Contains(tree, "hello") {
		t.Error("tree should contain message content")
	}
}

func TestLatest(t *testing.T) {
	dir := t.TempDir()

	// No sessions
	if latest := Latest(dir); latest != "" {
		t.Fatalf("expected empty latest, got %q", latest)
	}

	// Create a session
	s, _ := New(dir)
	s.Append(Entry{Role: "user", Content: "test"})

	latest := Latest(dir)
	if latest == "" {
		t.Fatal("expected non-empty latest")
	}
}

func TestInfo(t *testing.T) {
	dir := t.TempDir()
	s, _ := New(dir)
	s.Append(Entry{Role: "user", Content: "hello world"})
	s.Append(Entry{Role: "assistant", Content: "hey"})

	info := Info(s.Path)
	if !strings.Contains(info, "2 entries") {
		t.Errorf("info should mention 2 entries: %q", info)
	}
	if !strings.Contains(info, "1 user msgs") {
		t.Errorf("info should mention 1 user msg: %q", info)
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
