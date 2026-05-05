package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"charm.land/fantasy"

	"github.com/jstamagal/bitchtea/internal/session"
)

// genfixtures writes the canonical session-resume fixtures into
// internal/session/testdata. It's a one-shot generator — run it whenever
// you need to refresh the fixtures, then check the resulting .jsonl files
// into git. The binary itself is not shipped.
func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: genfixtures <out-dir>")
		os.Exit(2)
	}
	out := os.Args[1]
	must(os.MkdirAll(out, 0755))

	writeV1(filepath.Join(out, "v1.jsonl"))
	writeMixed(filepath.Join(out, "mixed_v0_v1.jsonl"))
	writeMultiContext(filepath.Join(out, "multi_context.jsonl"))

	// v0.jsonl and corrupted.jsonl are hand-written and live alongside the
	// generated ones — they're easier to keep stable as raw JSON than to
	// regenerate from typed structs (the corrupted file has invalid JSON
	// on purpose).
	fmt.Println("wrote fixtures to", out)
}

func writeV1(path string) {
	entries := []session.Entry{
		stamp(session.EntryFromFantasy(fantasy.Message{
			Role:    fantasy.MessageRoleUser,
			Content: []fantasy.MessagePart{fantasy.TextPart{Text: "scan the repo"}},
		}), "1", "", "2026-04-01T12:00:00Z"),
		stamp(session.EntryFromFantasy(fantasy.Message{
			Role: fantasy.MessageRoleAssistant,
			Content: []fantasy.MessagePart{
				fantasy.TextPart{Text: "Looking."},
				fantasy.ToolCallPart{ToolCallID: "call_v1", ToolName: "read", Input: `{"path":"main.go"}`},
			},
		}), "2", "1", "2026-04-01T12:00:01Z"),
		stamp(session.EntryFromFantasy(fantasy.Message{
			Role: fantasy.MessageRoleTool,
			Content: []fantasy.MessagePart{fantasy.ToolResultPart{
				ToolCallID: "call_v1",
				Output:     fantasy.ToolResultOutputContentText{Text: "package main"},
			}},
		}), "3", "2", "2026-04-01T12:00:02Z"),
		stamp(session.EntryFromFantasy(fantasy.Message{
			Role:    fantasy.MessageRoleAssistant,
			Content: []fantasy.MessagePart{fantasy.TextPart{Text: "Got it."}},
		}), "4", "3", "2026-04-01T12:00:03Z"),
	}
	writeJSONL(path, entries)
}

func writeMixed(path string) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	must(err)
	defer f.Close()
	// Two v0 lines hand-written.
	f.WriteString(`{"ts":"2026-04-01T12:00:00Z","id":"1","role":"user","content":"hello"}` + "\n")
	f.WriteString(`{"ts":"2026-04-01T12:00:01Z","id":"2","parent_id":"1","role":"assistant","content":"hi"}` + "\n")
	v1 := []session.Entry{
		stamp(session.EntryFromFantasy(fantasy.Message{
			Role:    fantasy.MessageRoleUser,
			Content: []fantasy.MessagePart{fantasy.TextPart{Text: "what's the time"}},
		}), "3", "2", "2026-04-01T12:00:02Z"),
		stamp(session.EntryFromFantasy(fantasy.Message{
			Role:    fantasy.MessageRoleAssistant,
			Content: []fantasy.MessagePart{fantasy.TextPart{Text: "noon"}},
		}), "4", "3", "2026-04-01T12:00:03Z"),
	}
	for _, e := range v1 {
		data, err := json.Marshal(e)
		must(err)
		f.Write(append(data, '\n'))
	}
}

func writeMultiContext(path string) {
	// A session whose entries are tagged with multiple IRC routing contexts.
	// session.Load currently treats Context as a passthrough field, but tests
	// can assert that the field survives load and that resume produces a
	// flattened agent history regardless of context tagging — the per-context
	// re-routing on resume is a separate concern (see docs/architecture.md).
	mk := func(role fantasy.MessageRole, text, ctx, id, parent, ts string) session.Entry {
		e := stamp(session.EntryFromFantasy(fantasy.Message{
			Role:    role,
			Content: []fantasy.MessagePart{fantasy.TextPart{Text: text}},
		}), id, parent, ts)
		e.Context = ctx
		if e.Msg != nil {
			// Mirror context onto the v1 envelope too — not required by
			// the schema, but keeps the fixture honest.
		}
		return e
	}
	entries := []session.Entry{
		mk(fantasy.MessageRoleUser, "deploy status?", "#ops", "1", "", "2026-04-01T12:00:00Z"),
		mk(fantasy.MessageRoleAssistant, "all green", "#ops", "2", "1", "2026-04-01T12:00:01Z"),
		mk(fantasy.MessageRoleUser, "review the diff", "#engineering", "3", "2", "2026-04-01T12:00:02Z"),
		mk(fantasy.MessageRoleAssistant, "looks fine", "#engineering", "4", "3", "2026-04-01T12:00:03Z"),
		mk(fantasy.MessageRoleUser, "ping", "alice", "5", "4", "2026-04-01T12:00:04Z"),
		mk(fantasy.MessageRoleAssistant, "pong", "alice", "6", "5", "2026-04-01T12:00:05Z"),
	}
	writeJSONL(path, entries)
}

func writeJSONL(path string, entries []session.Entry) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	must(err)
	defer f.Close()
	for _, e := range entries {
		data, err := json.Marshal(e)
		must(err)
		f.Write(append(data, '\n'))
	}
}

func stamp(e session.Entry, id, parent, ts string) session.Entry {
	e.ID = id
	e.ParentID = parent
	t, _ := time.Parse(time.RFC3339, ts)
	e.Timestamp = t
	return e
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
