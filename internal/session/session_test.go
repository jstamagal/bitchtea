package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jstamagal/bitchtea/internal/llm"
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

func TestMessageRoundTripWithToolMetadata(t *testing.T) {
	assistant := llm.Message{
		Role:    "assistant",
		Content: "Reading config now.",
		ToolCalls: []llm.ToolCall{
			{
				ID:   "call_123",
				Type: "function",
				Function: llm.FunctionCall{
					Name:      "read",
					Arguments: `{"path":"README.md"}`,
				},
			},
		},
	}
	tool := llm.Message{
		Role:       "tool",
		Content:    "file contents",
		ToolCallID: "call_123",
	}

	entries := []Entry{
		EntryFromMessage(assistant),
		EntryFromMessage(tool),
	}
	roundTrip := MessagesFromEntries(entries)

	if len(roundTrip) != 2 {
		t.Fatalf("expected 2 messages after round trip, got %d", len(roundTrip))
	}
	if len(roundTrip[0].ToolCalls) != 1 {
		t.Fatalf("expected assistant tool calls to survive round trip, got %+v", roundTrip[0].ToolCalls)
	}
	if roundTrip[0].ToolCalls[0].Function.Name != "read" {
		t.Fatalf("wrong tool name after round trip: %+v", roundTrip[0].ToolCalls[0])
	}
	if roundTrip[1].ToolCallID != "call_123" {
		t.Fatalf("expected tool call id to survive round trip, got %q", roundTrip[1].ToolCallID)
	}
}

func TestMessagesFromEntriesSkipsLegacyToolEntryWithoutToolCallID(t *testing.T) {
	msgs := MessagesFromEntries([]Entry{
		{Role: "assistant", Content: "done"},
		{Role: "tool", Content: "legacy tool output"},
	})

	if len(msgs) != 1 {
		t.Fatalf("expected legacy tool entry to be skipped, got %d messages", len(msgs))
	}
	if msgs[0].Role != "assistant" {
		t.Fatalf("expected assistant message to remain, got %q", msgs[0].Role)
	}
}
