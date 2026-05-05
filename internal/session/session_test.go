package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
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

func TestSaveCheckpointWritesHiddenJSONFile(t *testing.T) {
	dir := t.TempDir()
	err := SaveCheckpoint(dir, Checkpoint{
		TurnCount: 7,
		ToolCalls: map[string]int{"read": 2, "bash": 1},
		Model:     "gpt-test",
	})
	if err != nil {
		t.Fatalf("save checkpoint: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, ".bitchtea_checkpoint.json"))
	if err != nil {
		t.Fatalf("read checkpoint: %v", err)
	}

	var checkpoint Checkpoint
	if err := json.Unmarshal(data, &checkpoint); err != nil {
		t.Fatalf("unmarshal checkpoint: %v", err)
	}
	if checkpoint.TurnCount != 7 {
		t.Fatalf("expected turn count 7, got %d", checkpoint.TurnCount)
	}
	if checkpoint.ToolCalls["read"] != 2 {
		t.Fatalf("expected read call count 2, got %d", checkpoint.ToolCalls["read"])
	}
	if checkpoint.Model != "gpt-test" {
		t.Fatalf("expected model gpt-test, got %q", checkpoint.Model)
	}
	if checkpoint.Timestamp.IsZero() {
		t.Fatal("expected checkpoint timestamp to be set")
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

func TestEntryContextRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir)
	if err != nil {
		t.Fatalf("new: %v", err)
	}

	if err := s.Append(Entry{Role: "user", Content: "hello", Context: "#main"}); err != nil {
		t.Fatalf("append: %v", err)
	}
	if err := s.Append(Entry{Role: "assistant", Content: "hi", Context: "#main"}); err != nil {
		t.Fatalf("append: %v", err)
	}

	loaded, err := Load(s.Path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded.Entries[0].Context != "#main" {
		t.Errorf("expected context #main, got %q", loaded.Entries[0].Context)
	}
	if loaded.Entries[1].Context != "#main" {
		t.Errorf("expected context #main, got %q", loaded.Entries[1].Context)
	}
}

func TestSaveFocusAndLoadFocus_roundTrip(t *testing.T) {
	dir := t.TempDir()

	state := FocusState{
		Contexts: []ContextRecord{
			{Kind: "channel", Channel: "main"},
			{Kind: "channel", Channel: "code"},
			{Kind: "direct", Target: "coding-buddy"},
		},
		ActiveIndex: 2,
	}

	if err := SaveFocus(dir, state); err != nil {
		t.Fatalf("save focus: %v", err)
	}

	got, err := LoadFocus(dir)
	if err != nil {
		t.Fatalf("load focus: %v", err)
	}
	if got.ActiveIndex != 2 {
		t.Errorf("active index: got %d, want 2", got.ActiveIndex)
	}
	if len(got.Contexts) != 3 {
		t.Fatalf("context count: got %d, want 3", len(got.Contexts))
	}
	if got.Contexts[2].Kind != "direct" || got.Contexts[2].Target != "coding-buddy" {
		t.Errorf("direct context: got %+v", got.Contexts[2])
	}
}

func TestLoadFocus_missingFileReturnsEmpty(t *testing.T) {
	dir := t.TempDir()
	state, err := LoadFocus(dir)
	if err != nil {
		t.Fatalf("expected no error for missing file, got: %v", err)
	}
	if len(state.Contexts) != 0 {
		t.Errorf("expected empty state, got %+v", state)
	}
}

func TestSaveFocus_legacySubfieldRoundTrip(t *testing.T) {
	// Verify old session files with Sub field still deserialize correctly.
	dir := t.TempDir()
	state := FocusState{
		Contexts: []ContextRecord{
			{Kind: "channel", Channel: "cornhub", Sub: "website"},
		},
		ActiveIndex: 0,
	}
	if err := SaveFocus(dir, state); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := LoadFocus(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got.Contexts[0].Channel != "cornhub" {
		t.Errorf("channel round trip: got %+v", got.Contexts[0])
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

func TestEntryBootstrapRoundTrip(t *testing.T) {
	entry := Entry{
		Role:      "assistant",
		Content:   "internal ack",
		Bootstrap: true,
	}

	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded Entry
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if !decoded.Bootstrap {
		t.Fatal("expected bootstrap flag to survive json round trip")
	}
	if decoded.Content != "internal ack" {
		t.Fatalf("expected content to survive round trip, got %q", decoded.Content)
	}
}

// TestSession_ConcurrentAppend verifies that concurrent Append calls from
// multiple goroutines are serialized correctly by Session's internal mutex
// and the flock-based cross-process lock. All entries must be present in the
// Entries slice and the on-disk file without interleaving.
func TestSession_ConcurrentAppend(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir)
	if err != nil {
		t.Fatalf("new session: %v", err)
	}

	const writers = 20
	var wg sync.WaitGroup
	errs := make(chan error, writers)
	for i := 0; i < writers; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- s.Append(Entry{Role: "user", Content: fmt.Sprintf("msg-%02d", i)})
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent Append error: %v", err)
		}
	}

	// Verify in-memory count.
	if len(s.Entries) != writers {
		t.Fatalf("expected %d entries in memory, got %d", writers, len(s.Entries))
	}

	// Verify on-disk count (replay through Load).
	loaded, err := Load(s.Path)
	if err != nil {
		t.Fatalf("load session: %v", err)
	}
	if len(loaded.Entries) != writers {
		t.Fatalf("expected %d entries on disk, got %d", writers, len(loaded.Entries))
	}

	// Verify every expected content is present.
	seen := make(map[string]bool, writers)
	for _, e := range loaded.Entries {
		seen[e.Content] = true
	}
	for i := 0; i < writers; i++ {
		want := fmt.Sprintf("msg-%02d", i)
		if !seen[want] {
			t.Fatalf("entry %q not found on disk", want)
		}
	}
}

func TestDisplayEntriesFiltersBootstrapEntries(t *testing.T) {
	display := DisplayEntries([]Entry{
		{Role: "system", Content: "prompt", Bootstrap: true},
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "world"},
		{Role: "assistant", Content: "hidden ack", Bootstrap: true},
	})

	if len(display) != 2 {
		t.Fatalf("expected 2 display entries, got %d", len(display))
	}
	if display[0].Role != "user" || display[1].Role != "assistant" {
		t.Fatalf("unexpected display entries: %+v", display)
	}
}

// --- Fixture-based resume tests ------------------------------------------------

func fixturePath(name string) string {
	return filepath.Join("testdata", name)
}

func TestLoadV0BasicFixture(t *testing.T) {
	sess, err := Load(fixturePath("v0_basic.jsonl"))
	if err != nil {
		t.Fatalf("load v0 fixture: %v", err)
	}
	if len(sess.Entries) != 4 {
		t.Fatalf("expected 4 entries, got %d", len(sess.Entries))
	}
	if sess.Entries[0].Role != "user" || !strings.Contains(sess.Entries[0].Content, "check README") {
		t.Fatalf("unexpected first entry: %+v", sess.Entries[0])
	}
	if sess.Entries[1].V != 0 {
		t.Fatalf("expected v0 entry, got v=%d", sess.Entries[1].V)
	}
	// Tool call entries carry tool_calls on v0.
	if len(sess.Entries[1].ToolCalls) != 1 || sess.Entries[1].ToolCalls[0].Function.Name != "read" {
		t.Fatalf("expected tool call entry, got %+v", sess.Entries[1])
	}
	if sess.Entries[2].ToolCallID != "call_a" {
		t.Fatalf("expected tool entry call_a, got %q", sess.Entries[2].ToolCallID)
	}
}

func TestLoadV1BasicFixture(t *testing.T) {
	sess, err := Load(fixturePath("v1_basic.jsonl"))
	if err != nil {
		t.Fatalf("load v1 fixture: %v", err)
	}
	if len(sess.Entries) != 4 {
		t.Fatalf("expected 4 entries, got %d", len(sess.Entries))
	}
	for i, e := range sess.Entries {
		if e.V != EntrySchemaVersion {
			t.Errorf("entry %d expected v1, got v=%d", i, e.V)
		}
		if e.Msg == nil {
			t.Errorf("entry %d expected Msg populated for v1", i)
		}
	}
	// Roundtrip through FantasyFromEntries should produce 4 messages.
	msgs := FantasyFromEntries(sess.Entries)
	if len(msgs) != 4 {
		t.Fatalf("expected 4 fantasy messages, got %d", len(msgs))
	}
}

func TestLoadMultiContextFixture(t *testing.T) {
	sess, err := Load(fixturePath("multi_context.jsonl"))
	if err != nil {
		t.Fatalf("load multi-context fixture: %v", err)
	}
	if len(sess.Entries) != 6 {
		t.Fatalf("expected 6 entries, got %d", len(sess.Entries))
	}
	wantContexts := []string{"#ops", "#ops", "#engineering", "#engineering", "alice", "alice"}
	for i, want := range wantContexts {
		if got := sess.Entries[i].Context; got != want {
			t.Errorf("entry %d: expected context %q, got %q", i, want, got)
		}
	}
}

func TestLoadCorruptedFixtureSkipsBadLines(t *testing.T) {
	sess, err := Load(fixturePath("corrupted.jsonl"))
	if err != nil {
		t.Fatalf("load corrupted fixture: %v", err)
	}
	// corrupted.jsonl has 5 lines: 3 valid JSON, 1 garbage text, 1 truncated JSON.
	// Load skips malformed lines, so we expect 3 valid entries.
	if len(sess.Entries) != 3 {
		t.Fatalf("expected 3 entries (skipping 2 corrupted lines), got %d", len(sess.Entries))
	}
	wantOrder := []string{"first valid", "second valid", "third valid after corruption"}
	for i, want := range wantOrder {
		if got := sess.Entries[i].Content; got != want {
			t.Errorf("entry %d = %q, want %q", i, got, want)
		}
	}
}

func TestForkWithNonexistentIDCopiesAll(t *testing.T) {
	dir := t.TempDir()

	s, err := New(dir)
	if err != nil {
		t.Fatalf("new session: %v", err)
	}

	s.Append(Entry{Role: "user", Content: "first"})
	s.Append(Entry{Role: "assistant", Content: "reply"})

	// Fork from an ID that doesn't exist — the copy loop never hits the
	// break so it copies every entry, producing a full-clone fork.
	forked, err := s.Fork("nonexistent-id")
	if err != nil {
		t.Fatalf("fork with nonexistent ID should not error: %v", err)
	}
	if len(forked.Entries) != len(s.Entries) {
		t.Fatalf("nonexistent fork ID should copy all entries, got %d want %d", len(forked.Entries), len(s.Entries))
	}
}

func TestTreeEmptySession(t *testing.T) {
	dir := t.TempDir()

	s, err := New(dir)
	if err != nil {
		t.Fatalf("new session: %v", err)
	}

	// Don't append any entries — Tree should return the empty marker.
	got := s.Tree()
	if got != "(empty session)" {
		t.Fatalf("empty session Tree = %q, want %q", got, "(empty session)")
	}
}

func TestTreeNoForks(t *testing.T) {
	dir := t.TempDir()

	s, err := New(dir)
	if err != nil {
		t.Fatalf("new session: %v", err)
	}

	s.Append(Entry{Role: "user", Content: "hello"})
	s.Append(Entry{Role: "assistant", Content: "world"})

	got := s.Tree()
	if got == "" {
		t.Fatal("expected non-empty tree for session with entries")
	}
	if !strings.Contains(got, "hello") {
		t.Fatalf("tree should contain entry content, got %q", got)
	}
	// A linear (no-fork) tree should not have branch markers.
	if strings.Contains(got, "fork") {
		t.Fatalf("linear session tree should not mention forks, got %q", got)
	}
}

