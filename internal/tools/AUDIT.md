# Tool Contract Audit — Phase 2 (bt-p2-audit)

Date: 2026-04-28
Scope: every tool defined in `internal/tools` + the `internal/llm/tools.go` adapter layer.
Rules: document params, output contract, error behavior, test gaps. No production changes.

---

## 1. `read`

| Aspect | Detail |
|---|---|
| **Params** | `path` (string, required), `offset` (int, optional, 1-indexed), `limit` (int, optional) |
| **Output shape** | Raw file content as string. Lines joined with `\n`. |
| **Truncation** | Hard cap at 50 KiB; `... (truncated)` suffix appended. |
| **Offset past EOF** | Returns empty string (no error). |
| **Offset 0 + limit 0** | Returns full content (neither applied). |
| **Error: parse** | Returns `"parse args: …"` wrapping json.Unmarshal. |
| **Error: file missing** | Returns `"read <path>: …"` wrapping os.ReadFile. |
| **Path resolution** | Relative → joined to WorkDir; absolute → used as-is. |
| **Tests** | `TestReadFile` – full read, offset+limit. |
| **Test gaps** | *(a)* absolute path; *(b)* offset past EOF returns empty; *(c)* truncation (write >50 KiB); *(d)* offset=0+limit=0 identity; *(e)* parse error (malformed JSON). |

---

## 2. `write`

| Aspect | Detail |
|---|---|
| **Params** | `path` (string, required), `content` (string, required) |
| **Output shape** | `"Wrote <N> bytes to <original path arg>"` |
| **Mkdir** | Calls `os.MkdirAll(filepath.Dir(resolved), 0755)`. |
| **Overwrite** | Yes — `os.WriteFile` with `0644` truncates. |
| **Error: parse** | `"parse args: …"`. |
| **Error: mkdir** | `"mkdir: …"`. |
| **Error: write** | `"write <path>: …"`. |
| **Tests** | `TestWriteFile` – creates subdir + writes. |
| **Test gaps** | *(a)* overwrite of existing file; *(b)* parse error; *(c)* mkdir on top of a file (EISDIR-like); *(d)* write to read-only dir. |

---

## 3. `edit`

| Aspect | Detail |
|---|---|
| **Params** | `path` (string, required), `edits` (array of `{oldText,newText}`, required) |
| **Uniqueness** | Each `oldText` must appear exactly once. Multi-match → error. |
| **Not found** | Error: `"oldText not found in <path>: <truncated>"`. |
| **Multi match** | Error: `"oldText matches <N> times in <path> (must be unique): <truncated>"`. |
| **Order** | Edits applied sequentially; later edits see earlier replacements. |
| **Output shape** | `"Applied <N> edit(s) to <path>"`. |
| **Error: read** | `"read <path>: …"`. |
| **Error: write** | `"write <path>: …"`. |
| **Tests** | `TestEditFile`, `TestEditFileNonUnique`. |
| **Test gaps** | *(a)* oldText not found (separate from non-unique); *(b)* parse error; *(c)* multi-replace sequential (edit swaps A→B then B→C); *(d)* read of missing file; *(e)* zero edits (should it error or return "Applied 0"? — currently returns 0 edits, no error). |

---

## 4. `search_memory`

| Aspect | Detail |
|---|---|
| **Params** | `query` (string, required), `limit` (int, optional; default 5 from memorypkg) |
| **Output shape (hits)** | `"Memory matches for <query>:"` + numbered list with Source/Heading/Snippet. |
| **Output shape (no hits)** | `"No memory matches found for query <query>."`. |
| **Scope** | Inherited from `Registry.Scope` (set via `SetScope`). Root scope → legacy `MEMORY.md`. Scoped → HOT.md + daily dirs, walking parent lineage. |
| **Error: parse** | `"parse args: …"`. |
| **Error: SearchInScope** | Propagated from memory package (e.g. empty query, read failure). |
| **Tests** | `TestSearchMemoryTool` – MEMORY.md in workDir, query matches. |
| **Test gaps** | *(a)* no-match output; *(b)* parse error; *(c)* scoped search (SetScope + HOT.md); *(d)* daily memory hits; *(e)* limit applied. |

---

## 5. `bash`

| Aspect | Detail |
|---|---|
| **Params** | `command` (string, required), `timeout` (int, optional; default 30 s) |
| **Execution** | `bash -c <command>`; `cmd.Dir = WorkDir`. |
| **Output** | Combined stdout+stderr. Truncated at 50 KiB. |
| **Non-zero exit** | Returns output + `"\nExit code: <N>"` — **not** a Go error. |
| **Timeout** | Returns output + `"command timed out after <N>s"` — this IS a Go error. |
| **Context cancellation** | If parent ctx is done before exec, returns ctx.Err() as Go error. |
| **Error: parse** | `"parse args: …"`. |
| **Tests** | `TestBash`, `TestBashError` (exit 42). |
| **Test gaps** | *(a)* timeout; *(b)* context cancellation; *(c)* truncation (>50 KiB stdout); *(d)* parse error; *(e)* stdout+stderr mixing (verify order); *(f)* timeout=0 uses default 30. |

---

## 6. `terminal_start`

| Aspect | Detail |
|---|---|
| **Params** | `command` (string, required), `width` (int, default 100), `height` (int, default 30), `delay_ms` (int, default 200) |
| **Output shape** | `"terminal session <id> (WxH) running\n--- screen ---\n<screen_text>"`. |
| **Session ID** | Auto-generated: `"term-<N>"` (monotonic counter). |
| **Error: empty command** | `"command is required"`. |
| **Error: ctx cancelled** | Returns `ctx.Err()`. |
| **Error: pty create** | `"create pty: …"`. |
| **Error: start** | `"start terminal command: …"`. |
| **Tests** | `TestTerminalSessionEcho`, `TestTerminalSessionCommandExit`. |
| **Test gaps** | *(a)* empty command; *(b)* ctx cancelled before start; *(c)* width/height defaults applied (omit from JSON); *(d)* concurrent sessions get unique IDs; *(e)* command that immediately fails (e.g. `exit 1`). |

---

## 7. `terminal_send`

| Aspect | Detail |
|---|---|
| **Params** | `id` (string, required), `text` (string, required), `delay_ms` (int, default 100) |
| **Output shape** | Same snapshot format as `terminal_start`. |
| **Exited session** | Returns snapshot (with "exited" status), no error. |
| **Error: unknown id** | `"unknown terminal session: <id>"`. |
| **Error: parse** | `"parse args: …"`. |
| **Tests** | `TestTerminalSessionEcho` (sends to cat, verifies echo). |
| **Test gaps** | *(a)* unknown session; *(b)* exited session send; *(c)* empty text; *(d)* delay_ms default (omit from JSON); *(e)* parse error. |

---

## 8. `terminal_keys`

| Aspect | Detail |
|---|---|
| **Params** | `id` (string, required), `keys` ([]string, required), `delay_ms` (int, default 100) |
| **Output shape** | Snapshot. |
| **Empty keys** | `"keys is required"`. |
| **Exited session** | Returns snapshot, no error. |
| **Key mapping** | See `terminalKeyInput`: esc, enter, tab, backspace, delete, arrows, home, end, pageup, pagedown, ctrl-a..ctrl-z, space. Unknown strings passed as literal text. |
| **Tests** | `TestTerminalKeysSendNamedKeysAndLiteralText` (cat: sends text+enter+ctrl-d). |
| **Test gaps** | *(a)* unknown session; *(b)* exited session; *(c)* empty keys; *(d)* each named key mapping (esc, tab, backspace, delete, arrows, home, end, pageup, pagedown, space, ctrl-b..ctrl-y); *(e)* unknown key treated as literal; *(f)* parse error. |

---

## 9. `terminal_snapshot`

| Aspect | Detail |
|---|---|
| **Params** | `id` (string, required), `ansi` (bool, default false) |
| **Output shape** | Same snapshot format; ANSI=true uses `emu.Render()` else `emu.String()`. |
| **Error: unknown id** | `"unknown terminal session: <id>"`. |
| **Tests** | `TestTerminalSessionEcho` (plain snapshot), `TestTerminalSessionCommandExit` (exited snapshot). |
| **Test gaps** | *(a)* ANSI=true output; *(b)* unknown session; *(c)* parse error. |

---

## 10. `terminal_wait`

| Aspect | Detail |
|---|---|
| **Params** | `id` (string, required), `text` (string, required), `timeout_ms` (int, default 5000), `interval_ms` (int, default 100), `case_sensitive` (bool, default false) |
| **Output shape (match)** | `"matched terminal text \"<text>\"\n"` + snapshot. |
| **Output shape (exit)** | `"terminal exited before matching text \"<text>\"\n"` + snapshot. |
| **Output shape (timeout)** | `"timeout waiting for terminal text \"<text>\"\n"` + snapshot. |
| **Error: empty text** | `"text is required"`. |
| **Error: unknown id** | `"unknown terminal session: <id>"`. |
| **Tests** | `TestTerminalWaitMatchesScreenText` (case-insensitive match on "READY"). |
| **Test gaps** | *(a)* exited-before-match; *(b)* timeout; *(c)* case-sensitive match; *(d)* unknown session; *(e)* empty text. |

---

## 11. `terminal_resize`

| Aspect | Detail |
|---|---|
| **Params** | `id` (string, required), `width` (int, required), `height` (int, required), `delay_ms` (int, default 100) |
| **Output shape** | Snapshot with new dimensions. |
| **Error: zero width** | `"width must be positive"`. |
| **Error: zero height** | `"height must be positive"`. |
| **Error: resize pty** | `"resize pty: …"`. |
| **Tests** | `TestTerminalResizeUpdatesSnapshotDimensions`. |
| **Test gaps** | *(a)* zero width; *(b)* zero height; *(c)* unknown session; *(d)* parse error. |

---

## 12. `terminal_close`

| Aspect | Detail |
|---|---|
| **Params** | `id` (string, required) |
| **Output shape** | `"closed terminal session <id>"`. |
| **Error: unknown session** | `"unknown terminal session: <id>"`. |
| **Double close** | Idempotent — `close()` checks `s.closed` flag. |
| **Tests** | `TestTerminalSessionEcho` (closes at end). |
| **Test gaps** | *(a)* unknown session; *(b)* double close; *(c)* process still running at close (kill path); *(d)* parse error. |

---

## 13. `preview_image`

| Aspect | Detail |
|---|---|
| **Params** | `path` (string, required), `width` (int, default 80, max 160), `height` (int, optional, max 80, default preserves aspect ratio) |
| **Output shape** | `"image preview <path> (<format>, WxH)\n"` + ANSI block art via mosaic. |
| **Error: empty path** | `"path is required"`. |
| **Error: open** | `"open image <path>: …"`. |
| **Error: decode** | `"decode image <path>: …"`. |
| **Width clamp** | >160 → 160. |
| **Height clamp** | >80 → 80 (but 0 = no height limit). |
| **Tests** | `TestPreviewImage` – 2×2 PNG, width=4. |
| **Test gaps** | *(a)* empty path; *(b)* missing file; *(c)* invalid image (not PNG/JPEG/GIF); *(d)* width >160 clamping; *(e)* height >80 clamping; *(f)* height=0 (aspect-preserving); *(g)* parse error. |

---

## 14. Adapter layer (`internal/llm/tools.go`)

### `bitchteaTool.Run`

| Aspect | Detail |
|---|---|
| **Contract** | Never returns a Go error for tool-execution failures. All tool-level errors are wrapped in `fantasy.NewTextErrorResponse` and returned as `(fantasy.ToolResponse, nil)`. |
| **Go-error return** | Only if something fundamentally breaks (e.g. nil registry) — currently unreachable in practice. |
| **Tests** | `TestStreamChatSendsValidToolSchemaAndExecutesToolCall` (integration via StreamChat). |
| **Test gaps** | *(a)* tool execution error → NewTextErrorResponse, not Go error — the bt-vsh issue specifically demands this. |

### `translateTools`

| Aspect | Detail |
|---|---|
| **Contract** | Maps every `Registry.Definitions()` entry 1:1 to `fantasy.AgentTool` via `bitchteaTool`. `Parallel: false` for all tools. |
| **Tests** | `TestTranslateToolsExtractsFantasyParameterShape`, `TestTranslateToolsProducesValidSchemasForRealRegistryDefinitions`. |
| **Coverage** | 100%. |

### `splitSchema`

| Aspect | Detail |
|---|---|
| **Contract** | Strips OpenAI-style `{type:"object", properties:{…}, required:[…]}` outer envelope so fantasy gets bare `parameters` + `required` slice. Handles `[]string` and `[]any` required. |
| **Nil input** | Returns `nil, nil`. |
| **Already-split** | If no `"properties"` key, treats the map itself as properties. |
| **Malformed** | Drops non-map property values; filters required names to those in properties; handles non-object schema types. |
| **Tests** | `TestSplitSchemaAcceptsAlreadySplitPropertyMap`, `TestSplitSchemaEdgeCases` (11 cases). |
| **Coverage** | 88.2%. |

**Remaining `splitSchema` gap:**

```go
// In splitSchema, after "no properties but type exists" branch:
} else if schemaType, ok := params["type"].(string); ok {
    if schemaType != "object" {
        return map[string]any{}, nil  // ← tested (non-object schema)
    }
    return map[string]any{}, nil      // ← UNTESTED: type="object" but no properties
}
```

**Remaining `filterRequired` gap:**

```go
// When required is non-empty but properties is nil:
if properties == nil {
    return required  // ← UNTESTED
}
```

---

## Race Condition (Found During Audit)

`go test -race` detected data races in the terminal close path. `terminalSession.close()` calls `s.emu.Close()` while the background `io.Copy(s.emu, pty)` goroutine still reads from `emu.Read()`. Also races exist between `emu.String()` (snapshot) and `emu.Write()` (background copy from pty). These are pre-existing, not introduced by this audit.

Affected tests: `TestTerminalSessionEcho`, `TestTerminalSessionCommandExit`, `TestTerminalKeysSendNamedKeysAndLiteralText`, `TestTerminalWaitMatchesScreenText`, `TestTerminalResizeUpdatesSnapshotDimensions`.

Root cause: `close()` needs to cancel the context and wait for the `done` channel (which signals `wait()` returned) before calling `emu.Close()`. Currently it does `cancel()`, kills process, then immediately calls `emu.Close()` and `pty.Close()` without synchronizing with the io.Copy goroutines.

---

## Summary: Test Gap Inventory

| # | Tool/Function | Gap | Severity |
|---|---|---|---|
| 1 | `read` | absolute path | low |
| 2 | `read` | offset past EOF | low |
| 3 | `read` | truncation boundary | medium |
| 4 | `read` | parse error | low |
| 5 | `write` | overwrite existing | low |
| 6 | `write` | parse error | low |
| 7 | `write` | mkdir failure (not dir) | low |
| 8 | `edit` | oldText not found (single match) | medium |
| 9 | `edit` | parse error | low |
| 10 | `edit` | sequential edits interaction | low |
| 11 | `edit` | zero edits | low |
| 12 | `search_memory` | no-match output | medium |
| 13 | `search_memory` | parse error | low |
| 14 | `search_memory` | scoped search (SetScope) | medium |
| 15 | `search_memory` | daily memory hits | medium |
| 16 | `bash` | timeout | medium |
| 17 | `bash` | context cancellation | medium |
| 18 | `bash` | truncation | low |
| 19 | `bash` | parse error | low |
| 20 | `terminal_start` | empty command | medium |
| 21 | `terminal_start` | ctx cancelled | medium |
| 22 | `terminal_start` | defaults applied | low |
| 23 | `terminal_start` | concurrent unique IDs | low |
| 24 | `terminal_send` | unknown session | medium |
| 25 | `terminal_send` | exited session send | low |
| 26 | `terminal_send` | parse error | low |
| 27 | `terminal_keys` | unknown session | medium |
| 28 | `terminal_keys` | empty keys | medium |
| 29 | `terminal_keys` | each named key mapping | high |
| 30 | `terminal_keys` | parse error | low |
| 31 | `terminal_snapshot` | ANSI=true | medium |
| 32 | `terminal_snapshot` | unknown session | medium |
| 33 | `terminal_wait` | exited-before-match | medium |
| 34 | `terminal_wait` | timeout | medium |
| 35 | `terminal_wait` | case-sensitive | low |
| 36 | `terminal_wait` | unknown session | medium |
| 37 | `terminal_resize` | zero width/height | medium |
| 38 | `terminal_resize` | unknown session | medium |
| 39 | `terminal_close` | unknown session | medium |
| 40 | `terminal_close` | double close | low |
| 41 | `preview_image` | empty path | medium |
| 42 | `preview_image` | missing/invalid file | medium |
| 43 | `preview_image` | width/height clamping | low |
| 44 | `bitchteaTool.Run` | error → NewTextErrorResponse | high (bt-vsh) |
| 45 | `splitSchema` | type=object no properties | low |
| 46 | `filterRequired` | nil properties path | low |
| 47 | `terminalKeyInput` | ~15 named keys untested | high |
| 48 | `containsTerminalText` | case-sensitive=true | low |
| 49 | `truncate` | short string (no-overflow) | low |
| 50 | `SetScope` | setter invocation | low |
