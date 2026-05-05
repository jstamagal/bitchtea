package ui

import (
	"path/filepath"
	"strings"
	"testing"

	"charm.land/fantasy"

	"github.com/jstamagal/bitchtea/internal/llm"
	"github.com/jstamagal/bitchtea/internal/session"
)

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

// TestResumeFromV0FixtureFile loads the canonical v0 JSONL fixture from
// disk, runs it through session.Load, and verifies ResumeSession populates
// both the agent history and the chat viewport. This exercises the full
// disk-to-viewport path that --resume uses, not just the in-memory shortcut
// that the existing tests cover. The fixture lives in
// internal/session/testdata/v0.jsonl so the UI and session packages share
// the same on-disk shape.
func TestResumeFromV0FixtureFile(t *testing.T) {
	sess, err := session.Load(sessionFixturePath(t, "v0.jsonl"))
	if err != nil {
		t.Fatalf("load v0 fixture: %v", err)
	}
	if len(sess.Entries) != 4 {
		t.Fatalf("expected 4 entries, got %d", len(sess.Entries))
	}

	model, _ := testModel(t)
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

// TestResumeFromV1Fixture loads the canonical v1 fixture (entries minted via
// EntryFromFantasy and serialized to JSONL) and verifies ResumeSession
// produces the expected agent history and viewport messages. ResumeSession
// goes through FantasyFromEntries, but the dual-write writer still populates
// the legacy fields so a downgraded reader could see them — the fixture file
// captures both shapes verbatim.
//
// To regenerate the fixture: `go run ./cmd/genfixtures internal/session/testdata`.
func TestResumeFromV1Fixture(t *testing.T) {
	sess, err := session.Load(sessionFixturePath(t, "v1.jsonl"))
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

	model, _ := testModel(t)
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
// re-upgrade. The fixture has two v0 lines followed by two v1 lines; all
// four messages must still flow through ResumeSession in order.
func TestResumeFromMixedV0V1Fixture(t *testing.T) {
	sess, err := session.Load(sessionFixturePath(t, "mixed_v0_v1.jsonl"))
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

	model, _ := testModel(t)
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
	model, cfg := testModel(t)

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

	// Forked session must also resume cleanly. Reuse the testModel above —
	// it was built with the same SessionDir we just forked into.
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

	model, cfg := testModel(t)
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

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("ResumeSession panicked on legacy_lossy entry: %v", r)
		}
	}()

	model, cfg := testModel(t)
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

	model, cfg := testModel(t)
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

// TestResumeFromCorruptedFixture loads a JSONL file that contains a mix of
// valid and malformed lines. session.Load is documented to skip malformed
// lines silently (see Load in internal/session/session.go), so the resumed
// session should drop only the bad lines and ResumeSession must succeed
// without panicking. The fixture has three valid entries and two garbage
// lines; a regression that started failing the entire load on one bad line
// would surface here.
func TestResumeFromCorruptedFixture(t *testing.T) {
	sess, err := session.Load(sessionFixturePath(t, "corrupted.jsonl"))
	if err != nil {
		t.Fatalf("load corrupted fixture: %v", err)
	}
	if got := len(sess.Entries); got != 3 {
		t.Fatalf("expected 3 valid entries (2 garbage lines skipped), got %d", got)
	}

	model, _ := testModel(t)
	model.ResumeSession(sess)

	if got := len(model.messages); got != 3 {
		t.Fatalf("expected 3 chat messages from valid entries, got %d", got)
	}
	wantOrder := []string{"first valid", "second valid", "third valid after corruption"}
	for i, want := range wantOrder {
		if !strings.Contains(model.messages[i].Content, want) {
			t.Errorf("message %d: expected %q in viewport, got %q", i, want, model.messages[i].Content)
		}
	}
}

// TestResumeFromMultiContextFixture loads a fixture whose entries span three
// IRC routing contexts (#ops, #engineering, alice). session.Load preserves
// the Context field verbatim and ResumeSession flattens all six entries
// into the agent's main history. Per-context re-routing on resume is a
// separate feature tracked in bt-wire.* — this test just pins that the
// Context field survives load and the flattened history is intact.
func TestResumeFromMultiContextFixture(t *testing.T) {
	sess, err := session.Load(sessionFixturePath(t, "multi_context.jsonl"))
	if err != nil {
		t.Fatalf("load multi-context fixture: %v", err)
	}
	if got := len(sess.Entries); got != 6 {
		t.Fatalf("expected 6 entries, got %d", got)
	}
	wantContexts := []string{"#ops", "#ops", "#engineering", "#engineering", "alice", "alice"}
	for i, want := range wantContexts {
		if got := sess.Entries[i].Context; got != want {
			t.Errorf("entry %d: expected context %q, got %q", i, want, got)
		}
	}

	model, _ := testModel(t)
	model.ResumeSession(sess)

	if got := len(model.messages); got != 6 {
		t.Fatalf("expected 6 flattened chat messages, got %d", got)
	}
	if !strings.Contains(model.messages[0].Content, "deploy status") {
		t.Errorf("expected first message to be #ops user msg, got %q", model.messages[0].Content)
	}
	if !strings.Contains(model.messages[5].Content, "pong") {
		t.Errorf("expected last message to be alice assistant msg, got %q", model.messages[5].Content)
	}
}

// Compile-time assertion: keep the legacy llm import alive in case future
// resume tests need to compare against llm.Message — convert from the
// fantasy.Message canonical shape via llm.FantasySliceToLLM at the call site.
var _ = llm.Message{}
