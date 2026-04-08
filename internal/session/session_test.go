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

func TestForkFromFirstEntryPersistsSingleRootEntry(t *testing.T) {
	dir := t.TempDir()

	s, err := New(dir)
	if err != nil {
		t.Fatalf("new: %v", err)
	}

	if err := s.Append(Entry{Role: "user", Content: "root"}); err != nil {
		t.Fatalf("append root: %v", err)
	}
	if err := s.Append(Entry{Role: "assistant", Content: "reply"}); err != nil {
		t.Fatalf("append reply: %v", err)
	}

	forked, err := s.Fork(s.Entries[0].ID)
	if err != nil {
		t.Fatalf("fork: %v", err)
	}

	if len(forked.Entries) != 1 {
		t.Fatalf("expected 1 entry in fork, got %d", len(forked.Entries))
	}
	if forked.Entries[0].ID != s.Entries[0].ID {
		t.Fatalf("expected root entry id %q, got %q", s.Entries[0].ID, forked.Entries[0].ID)
	}
	if forked.Entries[0].ParentID != "" {
		t.Fatalf("expected root entry parent id to stay empty, got %q", forked.Entries[0].ParentID)
	}

	loaded, err := Load(forked.Path)
	if err != nil {
		t.Fatalf("load fork: %v", err)
	}
	if len(loaded.Entries) != 1 {
		t.Fatalf("expected 1 loaded entry in fork, got %d", len(loaded.Entries))
	}
	if loaded.Entries[0].ID != s.Entries[0].ID {
		t.Fatalf("expected loaded root entry id %q, got %q", s.Entries[0].ID, loaded.Entries[0].ID)
	}
}

func TestForkFromMiddlePreservesParentChainAndTruncatesTail(t *testing.T) {
	dir := t.TempDir()

	s, err := New(dir)
	if err != nil {
		t.Fatalf("new: %v", err)
	}

	for _, entry := range []Entry{
		{Role: "user", Content: "one"},
		{Role: "assistant", Content: "two"},
		{Role: "user", Content: "three"},
		{Role: "assistant", Content: "four"},
	} {
		if err := s.Append(entry); err != nil {
			t.Fatalf("append %q: %v", entry.Content, err)
		}
	}

	forked, err := s.Fork(s.Entries[1].ID)
	if err != nil {
		t.Fatalf("fork: %v", err)
	}

	if len(forked.Entries) != 2 {
		t.Fatalf("expected 2 entries in fork, got %d", len(forked.Entries))
	}
	if forked.Entries[1].ParentID != forked.Entries[0].ID {
		t.Fatalf("expected fork parent chain %q -> %q, got parent %q", forked.Entries[0].ID, forked.Entries[1].ID, forked.Entries[1].ParentID)
	}

	loaded, err := Load(forked.Path)
	if err != nil {
		t.Fatalf("load fork: %v", err)
	}
	if len(loaded.Entries) != 2 {
		t.Fatalf("expected 2 loaded entries in fork, got %d", len(loaded.Entries))
	}
	if loaded.Entries[1].ID != s.Entries[1].ID {
		t.Fatalf("expected fork tip id %q, got %q", s.Entries[1].ID, loaded.Entries[1].ID)
	}
	if loaded.Entries[1].ParentID != s.Entries[0].ID {
		t.Fatalf("expected loaded fork parent id %q, got %q", s.Entries[0].ID, loaded.Entries[1].ParentID)
	}
}

func TestAppendAfterForkUsesForkTipAsParent(t *testing.T) {
	dir := t.TempDir()

	s, err := New(dir)
	if err != nil {
		t.Fatalf("new: %v", err)
	}

	for _, entry := range []Entry{
		{Role: "user", Content: "one"},
		{Role: "assistant", Content: "two"},
		{Role: "user", Content: "three"},
	} {
		if err := s.Append(entry); err != nil {
			t.Fatalf("append %q: %v", entry.Content, err)
		}
	}

	forked, err := s.Fork(s.Entries[0].ID)
	if err != nil {
		t.Fatalf("fork: %v", err)
	}

	if err := forked.Append(Entry{Role: "assistant", Content: "branch reply"}); err != nil {
		t.Fatalf("append to fork: %v", err)
	}

	if len(forked.Entries) != 2 {
		t.Fatalf("expected 2 entries after append, got %d", len(forked.Entries))
	}
	if forked.Entries[1].ParentID != forked.Entries[0].ID {
		t.Fatalf("expected appended fork entry parent id %q, got %q", forked.Entries[0].ID, forked.Entries[1].ParentID)
	}
	if forked.Entries[1].ParentID == s.Entries[len(s.Entries)-1].ID {
		t.Fatalf("fork append should not point at original session tail %q", s.Entries[len(s.Entries)-1].ID)
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

func TestMessagesFromEntriesRoundTripThroughFork(t *testing.T) {
	dir := t.TempDir()

	s, err := New(dir)
	if err != nil {
		t.Fatalf("new: %v", err)
	}

	source := []llm.Message{
		{Role: "user", Content: "inspect README"},
		{
			Role:    "assistant",
			Content: "I will read the file.",
			ToolCalls: []llm.ToolCall{
				{
					ID:   "call_readme",
					Type: "function",
					Function: llm.FunctionCall{
						Name:      "read",
						Arguments: `{"path":"README.md"}`,
					},
				},
			},
		},
		{Role: "tool", Content: "README contents", ToolCallID: "call_readme"},
		{Role: "assistant", Content: "done"},
	}

	for _, msg := range source {
		if err := s.Append(EntryFromMessage(msg)); err != nil {
			t.Fatalf("append %q: %v", msg.Role, err)
		}
	}

	forked, err := s.Fork(s.Entries[2].ID)
	if err != nil {
		t.Fatalf("fork: %v", err)
	}

	loaded, err := Load(forked.Path)
	if err != nil {
		t.Fatalf("load fork: %v", err)
	}

	roundTrip := MessagesFromEntries(loaded.Entries)
	if len(roundTrip) != 3 {
		t.Fatalf("expected 3 messages after fork round trip, got %d", len(roundTrip))
	}
	if roundTrip[1].Role != "assistant" || len(roundTrip[1].ToolCalls) != 1 {
		t.Fatalf("expected assistant tool call metadata to survive fork round trip, got %+v", roundTrip[1])
	}
	if roundTrip[1].ToolCalls[0].ID != "call_readme" {
		t.Fatalf("expected tool call id to survive fork round trip, got %+v", roundTrip[1].ToolCalls[0])
	}
	if roundTrip[2].Role != "tool" || roundTrip[2].ToolCallID != "call_readme" {
		t.Fatalf("expected tool result metadata to survive fork round trip, got %+v", roundTrip[2])
	}
}
