package ui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"charm.land/fantasy"

	"github.com/jstamagal/bitchtea/internal/config"
	"github.com/jstamagal/bitchtea/internal/llm"
	"github.com/jstamagal/bitchtea/internal/session"
)

// writeJSONL writes the supplied entries as one JSON line each.
func writeJSONL(t *testing.T, path string, entries []session.Entry) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}
	defer f.Close()
	for _, e := range entries {
		data, err := json.Marshal(e)
		if err != nil {
			t.Fatalf("marshal entry: %v", err)
		}
		if _, err := f.Write(append(data, '\n')); err != nil {
			t.Fatalf("write entry: %v", err)
		}
	}
}

// findMessage returns the first ChatMessage whose Content contains needle, or
// the empty ChatMessage and false.
func findMessage(msgs []ChatMessage, needle string) (ChatMessage, bool) {
	for _, m := range msgs {
		if strings.Contains(m.Content, needle) {
			return m, true
		}
	}
	return ChatMessage{}, false
}

// TestResumeFromV0FixtureFile builds a hand-written legacy JSONL fixture on
// disk, loads it through session.Load, and verifies ResumeSession populates
// both the agent history and the chat viewport. This exercises the full
// disk-to-viewport path that --resume uses, not just the in-memory shortcut
// that the existing tests cover.
func TestResumeFromV0FixtureFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "v0.jsonl")

	fixture := strings.Join([]string{
		`{"ts":"2026-04-01T12:00:00Z","id":"1","role":"user","content":"check README"}`,
		`{"ts":"2026-04-01T12:00:01Z","id":"2","parent_id":"1","role":"assistant","content":"Reading.","tool_calls":[{"id":"call_a","type":"function","function":{"name":"read","arguments":"{\"path\":\"README.md\"}"}}]}`,
		`{"ts":"2026-04-01T12:00:02Z","id":"3","parent_id":"2","role":"tool","content":"# README contents","tool_call_id":"call_a","tool_name":"read"}`,
		`{"ts":"2026-04-01T12:00:03Z","id":"4","parent_id":"3","role":"assistant","content":"Done."}`,
	}, "\n") + "\n"

	if err := os.WriteFile(path, []byte(fixture), 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	sess, err := session.Load(path)
	if err != nil {
		t.Fatalf("load v0 fixture: %v", err)
	}
	if len(sess.Entries) != 4 {
		t.Fatalf("expected 4 entries, got %d", len(sess.Entries))
	}

	cfg := config.DefaultConfig()
	cfg.WorkDir = t.TempDir()
	cfg.SessionDir = t.TempDir()
	model := NewModel(&cfg)
	model.ResumeSession(sess)

	// RestoreMessages prepends a system prompt if the restored slice doesn't
	// already start with one, so the agent history can be entries+1 long.
	if got := len(model.agent.Messages()); got < 4 {
		t.Fatalf("expected at least 4 restored agent messages, got %d", got)
	}
	if model.lastSavedMsgIdx != 4 {
		t.Fatalf("expected lastSavedMsgIdx=4, got %d", model.lastSavedMsgIdx)
	}
	if len(model.messages) != 4 {
		t.Fatalf("expected 4 chat messages, got %d", len(model.messages))
	}
	if model.messages[2].Type != MsgTool || model.messages[2].Nick != "read" {
		t.Errorf("expected tool nick 'read', got type=%v nick=%q", model.messages[2].Type, model.messages[2].Nick)
	}
	if _, ok := findMessage(model.messages, "README contents"); !ok {
		t.Errorf("expected tool result content in viewport, messages: %+v", model.messages)
	}
}

// TestResumeFromV1Fixture builds a session JSONL whose entries were minted
// with EntryFromFantasy and verifies ResumeSession produces the same agent
// history and viewport messages as the legacy path. The dual-write writer
// keeps the legacy fields populated, so MessagesFromEntries (which is what
// ResumeSession calls today) must still see the messages.
func TestResumeFromV1Fixture(t *testing.T) {
	entries := []session.Entry{
		session.EntryFromFantasy(fantasy.Message{
			Role:    fantasy.MessageRoleUser,
			Content: []fantasy.MessagePart{fantasy.TextPart{Text: "scan the repo"}},
		}),
		session.EntryFromFantasy(fantasy.Message{
			Role: fantasy.MessageRoleAssistant,
			Content: []fantasy.MessagePart{
				fantasy.TextPart{Text: "Looking."},
				fantasy.ToolCallPart{
					ToolCallID: "call_v1",
					ToolName:   "read",
					Input:      `{"path":"main.go"}`,
				},
			},
		}),
		session.EntryFromFantasy(fantasy.Message{
			Role: fantasy.MessageRoleTool,
			Content: []fantasy.MessagePart{fantasy.ToolResultPart{
				ToolCallID: "call_v1",
				Output:     fantasy.ToolResultOutputContentText{Text: "package main"},
			}},
		}),
		session.EntryFromFantasy(fantasy.Message{
			Role:    fantasy.MessageRoleAssistant,
			Content: []fantasy.MessagePart{fantasy.TextPart{Text: "Got it."}},
		}),
	}

	// Sanity: every entry should be v1 with Msg populated.
	for i, e := range entries {
		if e.V != session.EntrySchemaVersion || e.Msg == nil {
			t.Fatalf("entry %d not v1: %+v", i, e)
		}
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "v1.jsonl")
	writeJSONL(t, path, entries)

	sess, err := session.Load(path)
	if err != nil {
		t.Fatalf("load v1 fixture: %v", err)
	}
	if len(sess.Entries) != 4 {
		t.Fatalf("expected 4 entries, got %d", len(sess.Entries))
	}
	for i, e := range sess.Entries {
		if e.V != session.EntrySchemaVersion {
			t.Errorf("entry %d expected v1 after roundtrip, got v=%d", i, e.V)
		}
		if e.Msg == nil {
			t.Errorf("entry %d expected Msg populated after roundtrip", i)
		}
	}

	cfg := config.DefaultConfig()
	cfg.WorkDir = t.TempDir()
	cfg.SessionDir = t.TempDir()
	model := NewModel(&cfg)
	model.ResumeSession(sess)

	// RestoreMessages prepends a system prompt; we only assert the entry
	// count is at least the number of fixture entries.
	if got := len(model.agent.Messages()); got < 4 {
		t.Fatalf("expected at least 4 restored agent messages, got %d", got)
	}
	if got := len(model.messages); got != 4 {
		t.Fatalf("expected 4 chat messages, got %d", got)
	}

	// Message-by-message rendering parity.
	if model.messages[0].Type != MsgUser || !strings.Contains(model.messages[0].Content, "scan the repo") {
		t.Errorf("user message wrong: %+v", model.messages[0])
	}
	if model.messages[1].Type != MsgAgent || !strings.Contains(model.messages[1].Content, "Looking.") {
		t.Errorf("assistant message wrong: %+v", model.messages[1])
	}
	if model.messages[2].Type != MsgTool {
		t.Errorf("expected tool message, got type=%v", model.messages[2].Type)
	}
	// The tool nick must resolve from the prior assistant tool_call entry.
	if model.messages[2].Nick != "read" {
		t.Errorf("expected tool nick 'read', got %q", model.messages[2].Nick)
	}
	if !strings.Contains(model.messages[2].Content, "package main") {
		t.Errorf("expected tool result content, got %q", model.messages[2].Content)
	}
}

// TestResumeFromMixedV0V1Fixture exercises a session whose lines mix the
// legacy and v1 shapes — the kind of file produced after a downgrade and
// re-upgrade. All messages must still flow through ResumeSession.
func TestResumeFromMixedV0V1Fixture(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mixed.jsonl")

	// Two v0 lines (hand-written), then two v1 lines (via EntryFromFantasy).
	v0Lines := []string{
		`{"ts":"2026-04-01T12:00:00Z","id":"1","role":"user","content":"hello"}`,
		`{"ts":"2026-04-01T12:00:01Z","id":"2","parent_id":"1","role":"assistant","content":"hi"}`,
	}

	v1Entries := []session.Entry{
		session.EntryFromFantasy(fantasy.Message{
			Role:    fantasy.MessageRoleUser,
			Content: []fantasy.MessagePart{fantasy.TextPart{Text: "what's the time"}},
		}),
		session.EntryFromFantasy(fantasy.Message{
			Role:    fantasy.MessageRoleAssistant,
			Content: []fantasy.MessagePart{fantasy.TextPart{Text: "noon"}},
		}),
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}
	for _, l := range v0Lines {
		if _, err := f.WriteString(l + "\n"); err != nil {
			t.Fatalf("write v0 line: %v", err)
		}
	}
	for _, e := range v1Entries {
		data, err := json.Marshal(e)
		if err != nil {
			t.Fatalf("marshal v1: %v", err)
		}
		if _, err := f.Write(append(data, '\n')); err != nil {
			t.Fatalf("write v1: %v", err)
		}
	}
	_ = f.Close()

	sess, err := session.Load(path)
	if err != nil {
		t.Fatalf("load mixed fixture: %v", err)
	}
	if len(sess.Entries) != 4 {
		t.Fatalf("expected 4 entries, got %d", len(sess.Entries))
	}
	if sess.Entries[0].V != 0 || sess.Entries[1].V != 0 {
		t.Errorf("expected first two entries v0, got v=%d/%d", sess.Entries[0].V, sess.Entries[1].V)
	}
	if sess.Entries[2].V != session.EntrySchemaVersion || sess.Entries[3].V != session.EntrySchemaVersion {
		t.Errorf("expected last two entries v1, got v=%d/%d", sess.Entries[2].V, sess.Entries[3].V)
	}

	cfg := config.DefaultConfig()
	cfg.WorkDir = t.TempDir()
	cfg.SessionDir = t.TempDir()
	model := NewModel(&cfg)
	model.ResumeSession(sess)

	if got := len(model.messages); got != 4 {
		t.Fatalf("expected 4 chat messages, got %d (%+v)", got, model.messages)
	}
	wantOrder := []string{"hello", "hi", "what's the time", "noon"}
	for i, want := range wantOrder {
		if !strings.Contains(model.messages[i].Content, want) {
			t.Errorf("message %d: expected %q, got %q", i, want, model.messages[i].Content)
		}
	}
}

// TestForkV1Session forks a session whose entries are all v1 and verifies
// the fork file contains v1 lines verbatim (Msg / V / LegacyLossy preserved)
// and is resumable.
func TestForkV1Session(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.WorkDir = t.TempDir()
	cfg.SessionDir = t.TempDir()

	sess, err := session.New(cfg.SessionDir)
	if err != nil {
		t.Fatalf("new session: %v", err)
	}

	// Append a few v1 entries via the same path the live writer will use.
	v1Entries := []session.Entry{
		session.EntryFromFantasy(fantasy.Message{
			Role:    fantasy.MessageRoleUser,
			Content: []fantasy.MessagePart{fantasy.TextPart{Text: "first turn"}},
		}),
		session.EntryFromFantasy(fantasy.Message{
			Role:    fantasy.MessageRoleAssistant,
			Content: []fantasy.MessagePart{fantasy.TextPart{Text: "ack"}},
		}),
		session.EntryFromFantasy(fantasy.Message{
			Role:    fantasy.MessageRoleUser,
			Content: []fantasy.MessagePart{fantasy.TextPart{Text: "fork here"}},
		}),
	}
	for _, e := range v1Entries {
		if err := sess.Append(e); err != nil {
			t.Fatalf("append v1: %v", err)
		}
	}

	forkID := sess.Entries[len(sess.Entries)-1].ID
	forked, err := sess.Fork(forkID)
	if err != nil {
		t.Fatalf("fork: %v", err)
	}
	if len(forked.Entries) != 3 {
		t.Fatalf("expected 3 forked entries, got %d", len(forked.Entries))
	}

	// Reload from disk to confirm v1 fields survived the fork write path.
	loaded, err := session.Load(forked.Path)
	if err != nil {
		t.Fatalf("load fork: %v", err)
	}
	if len(loaded.Entries) != 3 {
		t.Fatalf("expected 3 entries on disk, got %d", len(loaded.Entries))
	}
	for i, e := range loaded.Entries {
		if e.V != session.EntrySchemaVersion {
			t.Errorf("forked entry %d: expected v1, got v=%d", i, e.V)
		}
		if e.Msg == nil {
			t.Errorf("forked entry %d: expected Msg populated", i)
		}
	}

	// Forked session must also resume cleanly.
	model := NewModel(&cfg)
	model.ResumeSession(loaded)
	if got := len(model.agent.Messages()); got < 3 {
		t.Fatalf("expected at least 3 restored agent messages from fork, got %d", got)
	}
	if len(model.messages) != 3 {
		t.Fatalf("expected 3 viewport messages from fork, got %d", len(model.messages))
	}
	if !strings.Contains(model.messages[2].Content, "fork here") {
		t.Errorf("expected fork-point user message in viewport, got %q", model.messages[2].Content)
	}
}

// TestResumeV1ToolCallPopulatesPanelStats verifies that resuming a session
// containing a v1 assistant tool_call + tool_result pair routes the call
// into the chat viewport with the right tool nick and that the surfaced
// result text matches the v1 entry. ResumeSession does not replay live
// "tool_start" events — that is by design — so the tool panel's running
// list stays empty, but the message viewport must reflect the call. This
// test pins that contract for the v1 shape.
func TestResumeV1ToolCallPopulatesPanelStats(t *testing.T) {
	entries := []session.Entry{
		session.EntryFromFantasy(fantasy.Message{
			Role:    fantasy.MessageRoleUser,
			Content: []fantasy.MessagePart{fantasy.TextPart{Text: "list files"}},
		}),
		session.EntryFromFantasy(fantasy.Message{
			Role: fantasy.MessageRoleAssistant,
			Content: []fantasy.MessagePart{
				fantasy.TextPart{Text: "running ls"},
				fantasy.ToolCallPart{
					ToolCallID: "call_ls",
					ToolName:   "bash",
					Input:      `{"cmd":"ls"}`,
				},
			},
		}),
		session.EntryFromFantasy(fantasy.Message{
			Role: fantasy.MessageRoleTool,
			Content: []fantasy.MessagePart{fantasy.ToolResultPart{
				ToolCallID: "call_ls",
				Output:     fantasy.ToolResultOutputContentText{Text: "main.go\nREADME.md"},
			}},
		}),
	}

	cfg := config.DefaultConfig()
	cfg.WorkDir = t.TempDir()
	cfg.SessionDir = t.TempDir()
	model := NewModel(&cfg)
	model.ResumeSession(&session.Session{Path: filepath.Join(cfg.SessionDir, "v1tool.jsonl"), Entries: entries})

	// Find the tool result chat message and verify it was rendered with the
	// 'bash' nick (resolved via the prior tool_call) and the result body.
	tool, ok := findMessage(model.messages, "main.go")
	if !ok {
		t.Fatalf("expected tool result in viewport, messages: %+v", model.messages)
	}
	if tool.Type != MsgTool {
		t.Errorf("expected MsgTool, got %v", tool.Type)
	}
	if tool.Nick != "bash" {
		t.Errorf("expected tool nick 'bash' from preceding v1 tool_call, got %q", tool.Nick)
	}

	// Agent history must include the assistant tool_call so the next turn
	// can chain on it. The legacy projection round-trip is the contract.
	// Skip any leading system prompt the agent prepends on restore.
	msgs := model.agent.Messages()
	if len(msgs) < 3 {
		t.Fatalf("expected at least 3 agent messages, got %d", len(msgs))
	}
	start := 0
	if msgs[0].Role == "system" {
		start = 1
	}
	if len(msgs)-start != 3 {
		t.Fatalf("expected 3 non-system agent messages, got %d", len(msgs)-start)
	}
	asst := msgs[start+1]
	if asst.Role != fantasy.MessageRoleAssistant {
		t.Errorf("v1 tool_call did not survive: expected assistant role, got %q", asst.Role)
	}
	asstToolCalls := extractToolCalls(asst)
	if len(asstToolCalls) != 1 || asstToolCalls[0].ToolCallID != "call_ls" {
		t.Errorf("v1 tool_call did not survive into agent history: %+v", asst)
	}
	tr := msgs[start+2]
	if tr.Role != fantasy.MessageRoleTool || extractToolCallID(tr) != "call_ls" {
		t.Errorf("v1 tool result did not survive into agent history: %+v", tr)
	}
}

// extractToolCalls returns ToolCallParts inside a fantasy.Message in source
// order. Used by the resume tests to check the assistant tool-call shape
// after the Phase 3 swap to fantasy-native agent.messages.
func extractToolCalls(m fantasy.Message) []fantasy.ToolCallPart {
	var out []fantasy.ToolCallPart
	for _, part := range m.Content {
		switch p := part.(type) {
		case fantasy.ToolCallPart:
			out = append(out, p)
		case *fantasy.ToolCallPart:
			if p != nil {
				out = append(out, *p)
			}
		}
	}
	return out
}

// extractToolCallID returns the ToolCallID from the first ToolResultPart in
// a tool-role fantasy.Message.
func extractToolCallID(m fantasy.Message) string {
	for _, part := range m.Content {
		switch p := part.(type) {
		case fantasy.ToolResultPart:
			return p.ToolCallID
		case *fantasy.ToolResultPart:
			if p != nil {
				return p.ToolCallID
			}
		}
	}
	return ""
}

// TestResumeV1LegacyLossyEntry resumes a session containing a v1 entry that
// the writer flagged legacy_lossy (multi-part text). The UI must not crash
// and must surface the text projection of the message — losing fidelity is
// expected, crashing is not.
func TestResumeV1LegacyLossyEntry(t *testing.T) {
	lossy := session.EntryFromFantasy(fantasy.Message{
		Role: fantasy.MessageRoleAssistant,
		Content: []fantasy.MessagePart{
			fantasy.ReasoningPart{Text: "internal chain of thought"},
			fantasy.TextPart{Text: "visible answer"},
		},
	})
	if !lossy.LegacyLossy {
		t.Fatalf("expected reasoning+text fantasy message to be flagged legacy_lossy")
	}

	entries := []session.Entry{
		session.EntryFromFantasy(fantasy.Message{
			Role:    fantasy.MessageRoleUser,
			Content: []fantasy.MessagePart{fantasy.TextPart{Text: "think out loud"}},
		}),
		lossy,
	}

	cfg := config.DefaultConfig()
	cfg.WorkDir = t.TempDir()
	cfg.SessionDir = t.TempDir()

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("ResumeSession panicked on legacy_lossy entry: %v", r)
		}
	}()

	model := NewModel(&cfg)
	model.ResumeSession(&session.Session{Path: filepath.Join(cfg.SessionDir, "lossy.jsonl"), Entries: entries})

	if len(model.messages) != 2 {
		t.Fatalf("expected 2 chat messages, got %d", len(model.messages))
	}
	asst := model.messages[1]
	if asst.Type != MsgAgent {
		t.Errorf("expected assistant message, got type=%v", asst.Type)
	}
	if !strings.Contains(asst.Content, "visible answer") {
		t.Errorf("expected visible text fallback, got %q", asst.Content)
	}
	if strings.Contains(asst.Content, "internal chain of thought") {
		// Reasoning is intentionally dropped from the legacy projection;
		// the v1 Msg still carries it for richer consumers, but the UI
		// only shows the text fallback today.
		t.Errorf("legacy projection should drop reasoning, got %q", asst.Content)
	}
}

// TestResumeV1MultiTextFlattens resumes a v1 message with multiple text
// parts (also legacy_lossy) and confirms the UI shows the concatenated
// text projection rather than crashing or dropping content.
func TestResumeV1MultiTextFlattens(t *testing.T) {
	multi := session.EntryFromFantasy(fantasy.Message{
		Role: fantasy.MessageRoleUser,
		Content: []fantasy.MessagePart{
			fantasy.TextPart{Text: "first paste"},
			fantasy.TextPart{Text: "second paste"},
		},
	})
	if !multi.LegacyLossy {
		t.Fatalf("multi-text message should be flagged legacy_lossy")
	}

	cfg := config.DefaultConfig()
	cfg.WorkDir = t.TempDir()
	cfg.SessionDir = t.TempDir()
	model := NewModel(&cfg)
	model.ResumeSession(&session.Session{
		Path:    filepath.Join(cfg.SessionDir, "multi.jsonl"),
		Entries: []session.Entry{multi},
	})

	if len(model.messages) != 1 {
		t.Fatalf("expected 1 chat message, got %d", len(model.messages))
	}
	got := model.messages[0].Content
	if !strings.Contains(got, "first paste") || !strings.Contains(got, "second paste") {
		t.Errorf("expected both text parts in flattened content, got %q", got)
	}
}

// Compile-time assertion: keep the legacy llm import alive in case future
// resume tests need to compare against llm.Message — convert from the
// fantasy.Message canonical shape via llm.FantasySliceToLLM at the call site.
var _ = llm.Message{}
