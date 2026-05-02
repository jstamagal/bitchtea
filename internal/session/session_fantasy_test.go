package session

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"charm.land/fantasy"
)

// TestLoadV0FixtureUnchanged verifies that legacy session lines (no `v` /
// `msg` fields) load through the v1-aware reader exactly as they did before
// — the existing surface (Entries, MessagesFromEntries) must not regress.
func TestLoadV0FixtureUnchanged(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "v0.jsonl")

	// A hand-written v0 fixture: pre-Phase-3 shape, no `v` field, no `msg`
	// field. Spans user, assistant-with-tool-call, tool-result, assistant.
	fixture := strings.Join([]string{
		`{"ts":"2026-04-01T12:00:00Z","id":"1","role":"user","content":"read the readme"}`,
		`{"ts":"2026-04-01T12:00:01Z","id":"2","parent_id":"1","role":"assistant","content":"Reading.","tool_calls":[{"id":"call_a","type":"function","function":{"name":"read","arguments":"{\"path\":\"README.md\"}"}}]}`,
		`{"ts":"2026-04-01T12:00:02Z","id":"3","parent_id":"2","role":"tool","content":"# README","tool_call_id":"call_a"}`,
		`{"ts":"2026-04-01T12:00:03Z","id":"4","parent_id":"3","role":"assistant","content":"Done."}`,
	}, "\n") + "\n"

	if err := os.WriteFile(path, []byte(fixture), 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("load v0 fixture: %v", err)
	}
	if len(loaded.Entries) != 4 {
		t.Fatalf("expected 4 entries, got %d", len(loaded.Entries))
	}

	for i, e := range loaded.Entries {
		if e.V != 0 {
			t.Errorf("entry %d: expected V=0 for v0 fixture, got V=%d", i, e.V)
		}
		if e.Msg != nil {
			t.Errorf("entry %d: expected Msg=nil for v0 fixture, got %+v", i, e.Msg)
		}
		if e.LegacyLossy {
			t.Errorf("entry %d: expected LegacyLossy=false for v0 fixture", i)
		}
	}

	// Legacy MessagesFromEntries path must still work unchanged.
	msgs := MessagesFromEntries(loaded.Entries)
	if len(msgs) != 4 {
		t.Fatalf("expected 4 messages, got %d", len(msgs))
	}
	if msgs[1].Role != "assistant" || len(msgs[1].ToolCalls) != 1 {
		t.Fatalf("v0 assistant tool call did not survive: %+v", msgs[1])
	}
	if msgs[2].Role != "tool" || msgs[2].ToolCallID != "call_a" {
		t.Fatalf("v0 tool result did not survive: %+v", msgs[2])
	}

	// Forward-compat: v0 entries must promote into fantasy parts via the
	// new reader as well.
	fmsgs := FantasyFromEntries(loaded.Entries)
	if len(fmsgs) != 4 {
		t.Fatalf("FantasyFromEntries returned %d, want 4", len(fmsgs))
	}
	if fmsgs[0].Role != fantasy.MessageRoleUser {
		t.Errorf("expected first fantasy msg to be user, got %q", fmsgs[0].Role)
	}
	asst := fmsgs[1]
	if asst.Role != fantasy.MessageRoleAssistant {
		t.Fatalf("expected assistant role, got %q", asst.Role)
	}
	if len(asst.Content) != 2 {
		t.Fatalf("expected 2 parts (text+tool_call), got %d", len(asst.Content))
	}
	if _, ok := asst.Content[0].(fantasy.TextPart); !ok {
		t.Errorf("expected first part TextPart, got %T", asst.Content[0])
	}
	tc, ok := asst.Content[1].(fantasy.ToolCallPart)
	if !ok {
		t.Fatalf("expected second part ToolCallPart, got %T", asst.Content[1])
	}
	if tc.ToolCallID != "call_a" || tc.ToolName != "read" {
		t.Errorf("synthesized tool call wrong: %+v", tc)
	}
	tool := fmsgs[2]
	if tool.Role != fantasy.MessageRoleTool || len(tool.Content) != 1 {
		t.Fatalf("expected tool role with 1 part, got %+v", tool)
	}
	tr, ok := tool.Content[0].(fantasy.ToolResultPart)
	if !ok {
		t.Fatalf("expected ToolResultPart, got %T", tool.Content[0])
	}
	if tr.ToolCallID != "call_a" {
		t.Errorf("synthesized tool result id wrong: %q", tr.ToolCallID)
	}
}

// TestEntryFromFantasyTextOnlyIsNotLossy confirms a plain text message
// round-trips through Entry without the legacy_lossy flag — this is the
// dual-write happy path.
func TestEntryFromFantasyTextOnlyIsNotLossy(t *testing.T) {
	msg := fantasy.Message{
		Role:    fantasy.MessageRoleAssistant,
		Content: []fantasy.MessagePart{fantasy.TextPart{Text: "hello"}},
	}
	entry := EntryFromFantasy(msg)

	if entry.V != EntrySchemaVersion {
		t.Fatalf("expected V=%d, got %d", EntrySchemaVersion, entry.V)
	}
	if entry.Msg == nil {
		t.Fatal("expected Msg to be populated")
	}
	if entry.LegacyLossy {
		t.Error("text-only assistant message should not be lossy")
	}
	if entry.Role != "assistant" {
		t.Errorf("legacy role wrong: %q", entry.Role)
	}
	if entry.Content != "hello" {
		t.Errorf("legacy content wrong: %q", entry.Content)
	}
}

// TestEntryFromFantasyAssistantWithToolCall verifies the dual-write
// projection populates both Msg and the legacy ToolCalls slice and is
// considered lossless.
func TestEntryFromFantasyAssistantWithToolCall(t *testing.T) {
	msg := fantasy.Message{
		Role: fantasy.MessageRoleAssistant,
		Content: []fantasy.MessagePart{
			fantasy.TextPart{Text: "Reading file."},
			fantasy.ToolCallPart{
				ToolCallID: "call_42",
				ToolName:   "read",
				Input:      `{"path":"x"}`,
			},
		},
	}
	entry := EntryFromFantasy(msg)

	if entry.LegacyLossy {
		t.Error("text+tool_call should be losslessly representable")
	}
	if entry.Content != "Reading file." {
		t.Errorf("legacy content wrong: %q", entry.Content)
	}
	if len(entry.ToolCalls) != 1 || entry.ToolCalls[0].ID != "call_42" {
		t.Errorf("legacy tool calls wrong: %+v", entry.ToolCalls)
	}
}

// TestEntryFromFantasyMultiTextIsLossy covers a fantasy message with
// multiple TextParts — the legacy field collapses them, so the writer
// must flag legacy_lossy.
func TestEntryFromFantasyMultiTextIsLossy(t *testing.T) {
	msg := fantasy.Message{
		Role: fantasy.MessageRoleUser,
		Content: []fantasy.MessagePart{
			fantasy.TextPart{Text: "first paste"},
			fantasy.TextPart{Text: "second paste"},
		},
	}
	entry := EntryFromFantasy(msg)

	if !entry.LegacyLossy {
		t.Fatal("multi-text user message should be flagged legacy_lossy")
	}
	if !strings.Contains(entry.Content, "first paste") || !strings.Contains(entry.Content, "second paste") {
		t.Errorf("legacy content should include both text parts: %q", entry.Content)
	}
}

// TestEntryFromFantasyReasoningIsLossy verifies ReasoningPart triggers the
// lossy flag because legacy fields cannot represent reasoning.
func TestEntryFromFantasyReasoningIsLossy(t *testing.T) {
	msg := fantasy.Message{
		Role: fantasy.MessageRoleAssistant,
		Content: []fantasy.MessagePart{
			fantasy.ReasoningPart{Text: "chain of thought"},
			fantasy.TextPart{Text: "visible"},
		},
	}
	entry := EntryFromFantasy(msg)

	if !entry.LegacyLossy {
		t.Fatal("assistant message with reasoning must be lossy")
	}
	if entry.Content != "visible" {
		t.Errorf("legacy text projection should drop reasoning, got %q", entry.Content)
	}
}

// TestEntryFromFantasyMediaToolResultIsLossy verifies media tool-result
// outputs are flagged lossy (legacy can only carry a string).
func TestEntryFromFantasyMediaToolResultIsLossy(t *testing.T) {
	msg := fantasy.Message{
		Role: fantasy.MessageRoleTool,
		Content: []fantasy.MessagePart{fantasy.ToolResultPart{
			ToolCallID: "call_img",
			Output: fantasy.ToolResultOutputContentMedia{
				Data:      "ZmFrZS1iYXNlNjQ=",
				MediaType: "image/png",
				Text:      "preview",
			},
		}},
	}
	entry := EntryFromFantasy(msg)

	if !entry.LegacyLossy {
		t.Fatal("media tool result must be flagged legacy_lossy")
	}
	if entry.ToolCallID != "call_img" {
		t.Errorf("legacy tool_call_id wrong: %q", entry.ToolCallID)
	}
}

// TestEntryFromFantasyErrorToolResultIsLossy verifies error outputs trigger
// the lossy flag — legacy can't distinguish an error from a normal result
// whose body happens to look like one.
func TestEntryFromFantasyErrorToolResultIsLossy(t *testing.T) {
	msg := fantasy.Message{
		Role: fantasy.MessageRoleTool,
		Content: []fantasy.MessagePart{fantasy.ToolResultPart{
			ToolCallID: "call_err",
			Output:     fantasy.ToolResultOutputContentError{Error: errors.New("boom")},
		}},
	}
	entry := EntryFromFantasy(msg)

	if !entry.LegacyLossy {
		t.Fatal("error tool result must be flagged legacy_lossy")
	}
	if !strings.Contains(entry.Content, "boom") {
		t.Errorf("legacy content should include error text, got %q", entry.Content)
	}
}

// TestV1EntryRoundTripThroughJSON writes a fantasy message → Entry → JSON
// → Entry → fantasy.Message and asserts the canonical Msg survives the
// round trip with all parts intact.
func TestV1EntryRoundTripThroughJSON(t *testing.T) {
	original := fantasy.Message{
		Role: fantasy.MessageRoleAssistant,
		Content: []fantasy.MessagePart{
			fantasy.TextPart{Text: "Reading."},
			fantasy.ToolCallPart{
				ToolCallID: "call_99",
				ToolName:   "bash",
				Input:      `{"command":"ls"}`,
			},
		},
	}
	entry := EntryFromFantasy(original)

	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded Entry
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.V != EntrySchemaVersion {
		t.Fatalf("expected V=%d after round trip, got %d", EntrySchemaVersion, decoded.V)
	}
	if decoded.Msg == nil {
		t.Fatal("expected Msg to survive round trip")
	}
	got := FantasyFromEntries([]Entry{decoded})
	if len(got) != 1 {
		t.Fatalf("expected 1 message, got %d", len(got))
	}
	if got[0].Role != fantasy.MessageRoleAssistant {
		t.Errorf("role lost: %q", got[0].Role)
	}
	if len(got[0].Content) != 2 {
		t.Fatalf("expected 2 parts, got %d", len(got[0].Content))
	}
	if tp, ok := got[0].Content[0].(fantasy.TextPart); !ok || tp.Text != "Reading." {
		t.Errorf("text part lost: %T %+v", got[0].Content[0], got[0].Content[0])
	}
	tc, ok := got[0].Content[1].(fantasy.ToolCallPart)
	if !ok {
		t.Fatalf("expected ToolCallPart, got %T", got[0].Content[1])
	}
	if tc.ToolCallID != "call_99" || tc.ToolName != "bash" || tc.Input != `{"command":"ls"}` {
		t.Errorf("tool call lost data: %+v", tc)
	}
}

// TestV1EntryWithReasoningRoundTripPreservesPart verifies the v1 envelope
// keeps reasoning parts that the legacy projection would drop.
func TestV1EntryWithReasoningRoundTripPreservesPart(t *testing.T) {
	original := fantasy.Message{
		Role: fantasy.MessageRoleAssistant,
		Content: []fantasy.MessagePart{
			fantasy.ReasoningPart{Text: "step 1: think"},
			fantasy.TextPart{Text: "answer"},
		},
	}
	entry := EntryFromFantasy(original)
	if !entry.LegacyLossy {
		t.Fatal("expected legacy_lossy on reasoning entry")
	}

	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded Entry
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !decoded.LegacyLossy {
		t.Error("legacy_lossy flag lost in round trip")
	}

	got := FantasyFromEntries([]Entry{decoded})[0]
	if len(got.Content) != 2 {
		t.Fatalf("expected 2 parts (reasoning+text) preserved, got %d", len(got.Content))
	}
	rp, ok := got.Content[0].(fantasy.ReasoningPart)
	if !ok {
		t.Fatalf("expected ReasoningPart, got %T", got.Content[0])
	}
	if rp.Text != "step 1: think" {
		t.Errorf("reasoning text lost: %q", rp.Text)
	}
}

// TestMixedSessionFile writes a session file containing a mix of v0
// (hand-rolled JSONL) and v1 entries (via Append after EntryFromFantasy)
// and verifies the reader handles each entry on its own merits.
func TestMixedSessionFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mixed.jsonl")

	// Pre-seed with a v0 line so we can verify mid-file mixing — the path
	// then gets opened by Append in O_APPEND mode.
	v0 := `{"ts":"2026-04-01T12:00:00Z","id":"v0-1","role":"user","content":"legacy hi"}` + "\n"
	if err := os.WriteFile(path, []byte(v0), 0644); err != nil {
		t.Fatalf("seed v0: %v", err)
	}

	// Now construct a Session targeting that file. Load to populate
	// Entries so subsequent Appends chain parent IDs correctly.
	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("load seeded file: %v", err)
	}
	loaded.Path = path

	v1 := EntryFromFantasy(fantasy.Message{
		Role: fantasy.MessageRoleAssistant,
		Content: []fantasy.MessagePart{
			fantasy.TextPart{Text: "v1 reply"},
			fantasy.ToolCallPart{
				ToolCallID: "call_mix",
				ToolName:   "read",
				Input:      `{"path":"a"}`,
			},
		},
	})
	if err := loaded.Append(v1); err != nil {
		t.Fatalf("append v1: %v", err)
	}

	reread, err := Load(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if len(reread.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(reread.Entries))
	}
	if reread.Entries[0].V != 0 || reread.Entries[0].Msg != nil {
		t.Errorf("first entry should still be v0: V=%d Msg=%v", reread.Entries[0].V, reread.Entries[0].Msg)
	}
	if reread.Entries[1].V != EntrySchemaVersion || reread.Entries[1].Msg == nil {
		t.Errorf("second entry should be v1 with Msg: V=%d Msg=%v", reread.Entries[1].V, reread.Entries[1].Msg)
	}

	fmsgs := FantasyFromEntries(reread.Entries)
	if len(fmsgs) != 2 {
		t.Fatalf("expected 2 fantasy messages, got %d", len(fmsgs))
	}
	// v0 user → synthesized
	if fmsgs[0].Role != fantasy.MessageRoleUser {
		t.Errorf("first message wrong role: %q", fmsgs[0].Role)
	}
	if tp, ok := fmsgs[0].Content[0].(fantasy.TextPart); !ok || tp.Text != "legacy hi" {
		t.Errorf("v0 user content lost: %T %+v", fmsgs[0].Content[0], fmsgs[0].Content[0])
	}
	// v1 assistant → from Msg directly
	asst := fmsgs[1]
	if asst.Role != fantasy.MessageRoleAssistant || len(asst.Content) != 2 {
		t.Fatalf("v1 message parts lost: %+v", asst)
	}
	tc, ok := asst.Content[1].(fantasy.ToolCallPart)
	if !ok || tc.ToolCallID != "call_mix" {
		t.Errorf("v1 tool call lost: %T %+v", asst.Content[1], asst.Content[1])
	}

	// Legacy reader path must also work on the mixed file (the v1 entry's
	// dual-written legacy fields cover the downgrade case).
	lmsgs := MessagesFromEntries(reread.Entries)
	if len(lmsgs) != 2 {
		t.Fatalf("legacy reader returned %d messages from mixed file, want 2", len(lmsgs))
	}
	if lmsgs[1].Content != "v1 reply" || len(lmsgs[1].ToolCalls) != 1 {
		t.Errorf("v1 entry's legacy projection wrong: %+v", lmsgs[1])
	}
}

// TestFantasyFromEntriesSkipsLegacyToolWithoutID matches the existing
// MessagesFromEntries policy: a v0 tool entry missing tool_call_id can't
// replay through the provider API, so we drop it.
func TestFantasyFromEntriesSkipsLegacyToolWithoutID(t *testing.T) {
	msgs := FantasyFromEntries([]Entry{
		{Role: "assistant", Content: "done"},
		{Role: "tool", Content: "orphan"},
	})
	if len(msgs) != 1 {
		t.Fatalf("expected orphan tool entry to be skipped, got %d", len(msgs))
	}
	if msgs[0].Role != fantasy.MessageRoleAssistant {
		t.Errorf("expected assistant message to remain, got %q", msgs[0].Role)
	}
}

// TestEntryFromFantasyWithBootstrapPropagates checks the bootstrap flag is
// preserved through the new helper.
func TestEntryFromFantasyWithBootstrapPropagates(t *testing.T) {
	entry := EntryFromFantasyWithBootstrap(fantasy.Message{
		Role:    fantasy.MessageRoleSystem,
		Content: []fantasy.MessagePart{fantasy.TextPart{Text: "seed"}},
	}, true)
	if !entry.Bootstrap {
		t.Fatal("expected bootstrap=true to survive")
	}
	if entry.V != EntrySchemaVersion {
		t.Errorf("expected V=%d on bootstrap entry, got %d", EntrySchemaVersion, entry.V)
	}
}
