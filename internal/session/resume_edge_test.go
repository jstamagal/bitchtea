package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"charm.land/fantasy"

	"github.com/jstamagal/bitchtea/internal/llm"
)

// TestResumeCorruptedJSONL verifies Load gracefully degrades when the JSONL
// file contains truncated, partial, or otherwise malformed lines. Valid
// entries surrounding the corruption must still be recovered.
func TestResumeCorruptedJSONL(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "corrupted.jsonl")

	// Hand-crafted JSONL with three kinds of corruption:
	//   1. truncated line (unclosed brace)
	//   2. random non-JSON junk
	//   3. valid prefix with trailing garbage after the brace
	fixture := strings.Join([]string{
		`{"ts":"2026-05-03T12:00:00Z","id":"pre","role":"user","content":"before corruption"}`,
		`{"ts":"2026-05-03T12:00:01Z","id":"bad1","role":"assistant","content":"trunca`, // truncated
		`this is just noise`,
		`{"ts":"2026-05-03T12:00:03Z","id":"ok","role":"assistant","content":"survived"}`,
		`not-json-at-all`,
		`{"ts":"2026-05-03T12:00:04Z","id":"post","role":"user","content":"after corruption"}`,
	}, "\n") + "\n"

	if err := os.WriteFile(path, []byte(fixture), 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load should succeed (corrupt lines skipped): %v", err)
	}

	if len(loaded.Entries) != 3 {
		t.Fatalf("expected 3 valid entries after skipping corrupt lines, got %d", len(loaded.Entries))
	}

	if loaded.Entries[0].ID != "pre" || loaded.Entries[0].Content != "before corruption" {
		t.Errorf("first valid entry wrong: id=%q content=%q", loaded.Entries[0].ID, loaded.Entries[0].Content)
	}
	if loaded.Entries[1].ID != "ok" || loaded.Entries[1].Content != "survived" {
		t.Errorf("second valid entry wrong: id=%q content=%q", loaded.Entries[1].ID, loaded.Entries[1].Content)
	}
	if loaded.Entries[2].ID != "post" || loaded.Entries[2].Content != "after corruption" {
		t.Errorf("third valid entry wrong: id=%q content=%q", loaded.Entries[2].ID, loaded.Entries[2].Content)
	}
}

// TestResumeCorruptedJSONLEmptyFile verifies Load handles a completely empty
// file without error.
func TestResumeCorruptedJSONLEmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.jsonl")

	if err := os.WriteFile(path, []byte{}, 0644); err != nil {
		t.Fatalf("write empty file: %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load empty file: %v", err)
	}
	if len(loaded.Entries) != 0 {
		t.Fatalf("expected 0 entries in empty file, got %d", len(loaded.Entries))
	}
}

// TestResumeCorruptedJSONLAllCorrupt verifies Load returns an empty session
// when every line is unparseable.
func TestResumeCorruptedJSONLAllCorrupt(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "allcorrupt.jsonl")

	fixture := strings.Join([]string{
		`garbage line one`,
		`{still-not-json`,
		``, // empty line (should be skipped)
	}, "\n") + "\n"

	if err := os.WriteFile(path, []byte(fixture), 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load all-corrupt file: %v", err)
	}
	if len(loaded.Entries) != 0 {
		t.Fatalf("expected 0 entries in all-corrupt file, got %d", len(loaded.Entries))
	}
}

// TestResumeV0SessionWithToolCalls verifies that a v0 session containing
// tool calls is correctly restored through FantasyFromEntries. The legacy
// tool_call shape (ToolCalls []llm.ToolCall on the assistant entry +
// tool_role with tool_call_id) must produce the same fantasy parts the
// in-flight conversion produces.
func TestResumeV0SessionWithToolCalls(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "v0_tools.jsonl")

	// Full v0 session spanning user, assistant with two tool calls, both tool
	// results, and a final assistant conclusion.
	fixture := strings.Join([]string{
		`{"ts":"2026-01-01T00:00:00Z","id":"u1","role":"user","content":"list files and read the README"}`,
		`{"ts":"2026-01-01T00:00:01Z","id":"a1","parent_id":"u1","role":"assistant","content":"Working.","tool_calls":[{"id":"call_bash","type":"function","function":{"name":"bash","arguments":"{\"command\":\"ls\"}"}},{"id":"call_read","type":"function","function":{"name":"read","arguments":"{\"path\":\"README.md\"}"}}]}`,
		`{"ts":"2026-01-01T00:00:02Z","id":"t1","parent_id":"a1","role":"tool","content":"file1\nfile2\nREADME.md","tool_call_id":"call_bash"}`,
		`{"ts":"2026-01-01T00:00:03Z","id":"t2","parent_id":"t1","role":"tool","content":"# Project Title\n\nDescription here.","tool_call_id":"call_read"}`,
		`{"ts":"2026-01-01T00:00:04Z","id":"a2","parent_id":"t2","role":"assistant","content":"Directory has 3 files. README describes the project."}`,
	}, "\n") + "\n"

	if err := os.WriteFile(path, []byte(fixture), 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("load v0 tool-call fixture: %v", err)
	}
	if len(loaded.Entries) != 5 {
		t.Fatalf("expected 5 entries, got %d", len(loaded.Entries))
	}

	// Verify all entries are recognised as v0 (no V / Msg fields).
	for i, e := range loaded.Entries {
		if e.V != 0 {
			t.Errorf("entry %d: expected V=0, got V=%d", i, e.V)
		}
		if e.Msg != nil {
			t.Errorf("entry %d: expected nil Msg, got %+v", i, e.Msg)
		}
	}

	// Now promote to fantasy — this is the path ResumeSession takes.
	fmsgs := FantasyFromEntries(loaded.Entries)
	if len(fmsgs) != 5 {
		t.Fatalf("FantasyFromEntries returned %d, want 5", len(fmsgs))
	}

	// Entry 0: user
	if fmsgs[0].Role != fantasy.MessageRoleUser {
		t.Fatalf("msg 0: expected user role, got %q", fmsgs[0].Role)
	}
	if tp, ok := fmsgs[0].Content[0].(fantasy.TextPart); !ok || tp.Text != "list files and read the README" {
		t.Errorf("msg 0 content wrong: %T %+v", fmsgs[0].Content[0], fmsgs[0].Content[0])
	}

	// Entry 1: assistant with two tool calls
	asst := fmsgs[1]
	if asst.Role != fantasy.MessageRoleAssistant {
		t.Fatalf("msg 1: expected assistant role, got %q", asst.Role)
	}
	if len(asst.Content) != 3 {
		t.Fatalf("msg 1: expected 3 parts (text + 2 tool_calls), got %d", len(asst.Content))
	}
	// Text part
	if tp, ok := asst.Content[0].(fantasy.TextPart); !ok || tp.Text != "Working." {
		t.Errorf("msg 1 text part wrong: %T %+v", asst.Content[0], asst.Content[0])
	}
	// First tool call
	tc1, ok := asst.Content[1].(fantasy.ToolCallPart)
	if !ok {
		t.Fatalf("msg 1 part 1: expected ToolCallPart, got %T", asst.Content[1])
	}
	if tc1.ToolCallID != "call_bash" || tc1.ToolName != "bash" || tc1.Input != `{"command":"ls"}` {
		t.Errorf("msg 1 bash tool call wrong: %+v", tc1)
	}
	// Second tool call
	tc2, ok := asst.Content[2].(fantasy.ToolCallPart)
	if !ok {
		t.Fatalf("msg 1 part 2: expected ToolCallPart, got %T", asst.Content[2])
	}
	if tc2.ToolCallID != "call_read" || tc2.ToolName != "read" || tc2.Input != `{"path":"README.md"}` {
		t.Errorf("msg 1 read tool call wrong: %+v", tc2)
	}

	// Entry 2: bash tool result
	tool1 := fmsgs[2]
	if tool1.Role != fantasy.MessageRoleTool {
		t.Fatalf("msg 2: expected tool role, got %q", tool1.Role)
	}
	tr1, ok := tool1.Content[0].(fantasy.ToolResultPart)
	if !ok {
		t.Fatalf("msg 2: expected ToolResultPart, got %T", tool1.Content[0])
	}
	if tr1.ToolCallID != "call_bash" {
		t.Errorf("msg 2 tool call id wrong: %q", tr1.ToolCallID)
	}
	if txt, ok := tr1.Output.(fantasy.ToolResultOutputContentText); !ok || !strings.Contains(txt.Text, "file1") {
		t.Errorf("msg 2 output wrong: %T %+v", tr1.Output, tr1.Output)
	}

	// Entry 3: read tool result
	tool2 := fmsgs[3]
	if tool2.Role != fantasy.MessageRoleTool {
		t.Fatalf("msg 3: expected tool role, got %q", tool2.Role)
	}
	tr2, ok := tool2.Content[0].(fantasy.ToolResultPart)
	if !ok {
		t.Fatalf("msg 3: expected ToolResultPart, got %T", tool2.Content[0])
	}
	if tr2.ToolCallID != "call_read" {
		t.Errorf("msg 3 tool call id wrong: %q", tr2.ToolCallID)
	}

	// Entry 4: final assistant
	if fmsgs[4].Role != fantasy.MessageRoleAssistant {
		t.Fatalf("msg 4: expected assistant role, got %q", fmsgs[4].Role)
	}
	if tp, ok := fmsgs[4].Content[0].(fantasy.TextPart); !ok || !strings.Contains(tp.Text, "README describes") {
		t.Errorf("msg 4 content wrong: %T %+v", fmsgs[4].Content[0], fmsgs[4].Content[0])
	}
}

// TestResumeV0SessionJSONRoundTrip verifies that a v0 session entry survives a
// full JSON marshal→unmarshal cycle and still produces correct fantasy parts
// from its legacy fields. This covers the case where a session file is read
// and re-serialized (e.g., by a migration or repair tool).
func TestResumeV0SessionJSONRoundTrip(t *testing.T) {
	v0User := Entry{
		ID:      "u99",
		Role:    "user",
		Content: "what is in config.yaml?",
	}
	v0Assistant := Entry{
		ID:      "a99",
		Role:    "assistant",
		Content: "Let me check.",
		ToolCalls: []llm.ToolCall{
			{
				ID:   "call_x",
				Type: "function",
				Function: llm.FunctionCall{
					Name:      "read",
					Arguments: `{"path":"config.yaml"}`,
				},
			},
		},
	}
	v0Tool := Entry{
		ID:         "t99",
		Role:       "tool",
		Content:    "port: 8080\ndebug: true",
		ToolCallID: "call_x",
	}
	v0Summary := Entry{
		ID:      "a100",
		Role:    "assistant",
		Content: "The config has port 8080 and debug enabled.",
	}

	// Marshal each to JSON and unmarshal — simulates full file round-trip.
	var decoded []Entry
	for _, e := range []Entry{v0User, v0Assistant, v0Tool, v0Summary} {
		data, err := json.Marshal(e)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		var d Entry
		if err := json.Unmarshal(data, &d); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		decoded = append(decoded, d)
	}

	// Verify nothing got V or Msg from the JSON round-trip.
	for i, d := range decoded {
		if d.V != 0 {
			t.Errorf("entry %d: expected V=0 after JSON round-trip, got V=%d", i, d.V)
		}
		if d.Msg != nil {
			t.Errorf("entry %d: expected nil Msg after JSON round-trip, got %+v", i, d.Msg)
		}
	}

	fmsgs := FantasyFromEntries(decoded)
	if len(fmsgs) != 4 {
		t.Fatalf("expected 4 messages, got %d", len(fmsgs))
	}

	// Msg 0: user
	if fmsgs[0].Role != fantasy.MessageRoleUser {
		t.Fatalf("msg 0 role: got %q, want user", fmsgs[0].Role)
	}

	// Msg 1: assistant with text + tool call
	asst := fmsgs[1]
	if len(asst.Content) != 2 {
		t.Fatalf("msg 1: expected 2 parts (text + tool_call), got %d", len(asst.Content))
	}
	if tp, ok := asst.Content[0].(fantasy.TextPart); !ok || tp.Text != "Let me check." {
		t.Errorf("msg 1 text part wrong: %T %+v", asst.Content[0], asst.Content[0])
	}
	tc, ok := asst.Content[1].(fantasy.ToolCallPart)
	if !ok {
		t.Fatalf("msg 1 part 1: expected ToolCallPart, got %T", asst.Content[1])
	}
	if tc.ToolCallID != "call_x" || tc.ToolName != "read" || tc.Input != `{"path":"config.yaml"}` {
		t.Errorf("msg 1 tool call wrong: %+v", tc)
	}

	// Msg 2: tool result
	tool := fmsgs[2]
	if tool.Role != fantasy.MessageRoleTool {
		t.Fatalf("msg 2 role: got %q, want tool", tool.Role)
	}
	tr, ok := tool.Content[0].(fantasy.ToolResultPart)
	if !ok {
		t.Fatalf("msg 2: expected ToolResultPart, got %T", tool.Content[0])
	}
	if tr.ToolCallID != "call_x" {
		t.Errorf("msg 2 tool call id wrong: %q", tr.ToolCallID)
	}

	// Msg 3: final assistant
	if fmsgs[3].Role != fantasy.MessageRoleAssistant {
		t.Fatalf("msg 3 role: got %q, want assistant", fmsgs[3].Role)
	}
	if tp, ok := fmsgs[3].Content[0].(fantasy.TextPart); !ok || !strings.Contains(tp.Text, "port 8080") {
		t.Errorf("msg 3 content wrong: %T %+v", fmsgs[3].Content[0], fmsgs[3].Content[0])
	}
}
