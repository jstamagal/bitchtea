# bitchtea Tools & Terminal Architecture

This document details the tool registry, terminal PTY family, and execution architecture that enables `bitchtea` to interact with the host system.

## Tool Execution Architecture

The tool system follows a strict **Research -> Strategy -> Execution** lifecycle. Tools are exposed to the LLM via JSON schemas and executed through a centralized `Registry`.

### The Registry (`internal/tools/tools.go`)

The `Registry` is the orchestrator for all built-in tools. It manages:
- **WorkingDirectory**: All relative paths are resolved against this.
- **SessionDirectory**: Used for session-specific persistence (like memory).
- **MemoryScope**: Tracks the current IRC-style context (#channel, query) for scoped memory operations.
- **TerminalManager**: Manages the lifecycle of persistent PTY sessions.

### Execution Flow

1. **LLM Dispatch**: The LLM sends a tool call with a name and a JSON string of arguments.
2. **Registry.Execute**:
   - Checks for context cancellation (allows `CancelTool` signals to stop execution).
   - Dispatches to the specific `exec<ToolName>` handler.
3. **Argument Parsing**: Handlers unmarshal the JSON into typed structs.
4. **Path Resolution**: File-based tools use `resolvePath` to ensure operations stay within the expected workspace (unless absolute paths are provided).
5. **Output Truncation**: Tool results are capped at 50KB to prevent context window flooding.

## Terminal PTY Family (`internal/tools/terminal.go`)

The terminal system provides a way to run interactive CLI applications, REPLs, and TUIs while maintaining state between turns. It uses the Charm `xpty` and `vt` packages.

### Persistent Sessions

Unlike the `bash` tool which is fire-and-forget, `terminal_start` creates a persistent session identified by an ID (e.g., `term-1`).

- **Bash Isolation**: Commands are run via `bash -c` (not `-lc`) to prevent host-specific `.bashrc` aliases or prompts from breaking reproducibility.
- **PTY/Emulator Pair**: Each session pairs a real Pseudo-Terminal (PTY) with a virtual terminal emulator.
- **Goroutine Synchronization**:
  - One goroutine copies PTY output into the emulator's write buffer.
  - Another goroutine handles input feedback from the emulator back to the PTY.
  - A `wait` goroutine monitors the process lifecycle.

### Terminal Tools

| Tool | Purpose | Key Behavior |
| :--- | :--- | :--- |
| `terminal_start` | Boot a new PTY session | Returns initial screen snapshot after a configurable delay. |
| `terminal_send` | Send raw text | Used for simple string input. |
| `terminal_keys` | Send control keys | Handles Esc, Enter, Ctrl-C, arrows, etc. (e.g. `["esc", ":q!", "enter"]` to quit vim). |
| `terminal_snapshot`| View the screen | Can return plain text or ANSI-styled output. |
| `terminal_wait` | Block until text appears | Polls the screen buffer. Prevents the LLM from "guessing" if a command finished. |
| `terminal_resize` | Adjust PTY dimensions | Resizes both the PTY and the virtual emulator. |
| `terminal_close` | Teardown session | Kills the process and cleans up resources. |

### Concurrency & Race Protection

The terminal manager implements sophisticated locking to prevent races:
- **`emuMu`**: Serializes all access to the emulator (Write, Read, String, Close).
- **`closing` atomic**: Signals I/O goroutines to exit before teardown.
- **WaitGroups**: Ensure all buffers are drained before the emulator is closed.

## Complete Tool Reference

All 14 tools from `internal/tools/tools.go` `Definitions()`. Typed Wrapper indicates
whether the tool has a dedicated `internal/llm/typed_*.go` fantasy wrapper or falls
through to the generic `bitchteaTool` adapter in `internal/llm/tools.go`.

| Tool | Category | Description | Typed Wrapper |
| :--- | :--- | :--- | :--- |
| `read` | File System | Read file contents with offset/limit for large files. | Yes (`typed_read.go`) |
| `write` | File System | Overwrite or create a file; auto-creates parent directories. | Yes (`typed_write.go`) |
| `edit` | File System | Unique-string replacement; fails if `oldText` is ambiguous or missing. | Yes (`typed_edit.go`) |
| `bash` | System | Shell execution with 30s timeout; captures combined stdout/stderr. | Yes (`typed_bash.go`) |
| `search_memory` | Memory | Query hot (`MEMORY.md`/`HOT.md`) and daily archive files in current scope. | Yes (`typed_search_memory.go`) |
| `write_memory` | Memory | Persist notes into current or specified scope; optional daily mode. | Yes (`typed_write_memory.go`) |
| `terminal_start` | Terminal | Boot a new persistent PTY session; returns initial snapshot. | No (legacy adapter) |
| `terminal_send` | Terminal | Send raw text to a running PTY session. | No (legacy adapter) |
| `terminal_keys` | Terminal | Send control key sequences (Esc, Enter, Ctrl-C, arrows, etc.). | No (legacy adapter) |
| `terminal_snapshot` | Terminal | Capture current terminal screen as plain text or ANSI. | No (legacy adapter) |
| `terminal_wait` | Terminal | Poll screen buffer and block until target text appears. | No (legacy adapter) |
| `terminal_resize` | Terminal | Resize PTY and virtual emulator dimensions. | No (legacy adapter) |
| `terminal_close` | Terminal | Kill the process and clean up PTY resources. | No (legacy adapter) |
| `preview_image` | Visual | Render PNG/JPEG/GIF into terminal-friendly ANSI block art for the LLM to "see". | No (legacy adapter) |

Tool counts: 6 typed wrappers, 8 legacy adapter (7 terminal + 1 preview_image).
Total: 14, matching `tools.Definitions()`.

## Tool Assembly Pipeline

Tools are assembled at stream time by `translateTools` in `internal/llm/tools.go`,
then merged with MCP tools by `AssembleAgentTools` in `internal/llm/mcp_tools.go`.
The assembly order and dispatch logic:

```
Registry.Definitions() (14 tool defs)
    |
    v
translateTools()                          internal/llm/tools.go:48
    |
    +-- typedToolFor(name, reg)           internal/llm/tools.go:99
    |       |
    |       +-- match: edit   -> typed_edit.go
    |       +-- match: read   -> typed_read.go
    |       +-- match: write  -> typed_write.go
    |       +-- match: bash   -> typed_bash.go
    |       +-- match: search_memory -> typed_search_memory.go
    |       +-- match: write_memory  -> typed_write_memory.go
    |       +-- no match: fall through to legacy bitchteaTool adapter
    |
    v
[]fantasy.AgentTool (14 tools, 6 typed + 8 legacy)
    |
    v
MCPTools() + AssembleAgentTools()         internal/llm/mcp_tools.go
    |
    v
final []fantasy.AgentTool (local + MCP, local-wins on collision)
```

### Typed Wrappers (fantasy.NewAgentTool)

Six tools dispatch through dedicated typed wrappers that validate args with
`json.Unmarshal` into per-tool Go structs, check cancellation, and delegate
to the Registry. Each wrapper file follows the same pattern:

```go
// internal/llm/typed_read.go (representative)
func readTool(reg *tools.Registry) fantasy.AgentTool {
    return fantasy.NewAgentTool(fantasy.ToolInfo{...},
        func(ctx context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
            var args readArgs
            json.Unmarshal(call.Input, &args)
            // ... execute via reg.Execute or direct helper ...
        },
    )
}
```

Typed wrappers return tool errors as `NewTextErrorResponse` (Go error is nil)
to keep the fantasy stream alive on tool failures.

### Legacy Adapter (bitchteaTool)

Eight tools without typed wrappers use the generic `bitchteaTool` adapter
(`internal/llm/tools.go:16`). The adapter passes the raw JSON input directly
to `Registry.Execute(name, json.RawMessage)`, which dispatches to the
per-tool `exec<ToolName>` handler in `internal/tools/tools.go`.

The legacy path is compatibility code. New tools added to the Registry must
also add a typed wrapper and wire it into `typedToolFor`.

### MCP Integration

`AssembleAgentTools(local, mcpTools)` merges the local tool surface with MCP
tools from `internal/mcp`. Local tools always win on name collision. MCP tool
names use the `mcp__<server>__<tool>` namespace prefix, guaranteeing no
collision with local tool names (none start with `mcp__`).

### Schema Flattening

`translateTools` and `AssembleAgentTools` both need to convert OpenAI-style tool
schemas (with `type`, `properties`, `required` nested under a root object) into
the flat parameters + required pair that `fantasy.ToolInfo` expects. Without
this conversion, fantasy would include `"type"`, `"properties"`, and
`"required"` as bogus parameter names in the schema sent to the provider —
which Anthropic rejects as invalid.

Four helpers in `internal/llm/tools.go` handle the translation:

| Function | Line | Purpose |
| :--- | :--- | :--- |
| `splitSchema` | `tools.go:124` | Entry point. Takes the full OpenAI-style `parameters` map and returns `(properties, required)`. Detects whether the input is already a flat map or a structured schema with `"type":"object"`. |
| `sanitizeProperties` | `tools.go:157` | Unwraps the `"properties"` value from the schema object. Drops malformed entries so a single corrupt property doesn't break the tool surface. |
| `parseRequired` | `tools.go:179` | Extracts `"required"` from the schema. Handles both `[]string` (Go-native) and `[]any` (JSON-decoded). |
| `filterRequired` | `tools.go:198` | Filters required names to only those present in properties. Returns `nil` (not `[]string{}`) when nothing survives — some providers reject `"required": []`. |

The same `splitSchema` is reused by `splitMCPSchema` (`mcp_tools.go:182`) for
MCP input schemas.

Tests in `internal/llm/tools_test.go` cover edge cases:
- `TestSplitSchemaEdgeCases` (line 122): 13 sub-cases (missing properties, malformed required, mixed-type lists, nil-after-filter, non-object schemas, already-flat inputs).
- `TestFilterRequiredEmptyAfterFilterMarshalsWithoutRequiredKey` (line 303): regression guard (bt-qna) proving that nil required omits the `"required"` JSON key.
- `TestTranslateToolsExtractsFantasyParameterShape` (line 17): end-to-end assertion that `translateTools` output never carries bogus top-level schema keys.
- `TestTranslateToolsProducesValidSchemasForRealRegistryDefinitions` (line 376): every registered tool's schema round-trips through flattening safely.

## Tool-Call Round-Trip

Every tool call from the model follows the same path through fantasy, the tool
wrapper layer, and the registry back to the streaming event loop:

```text
fantasy.Agent.Stream
  │
  │  model generates "tool_use" content block
  ▼
stream.go:OnToolCall (fantasy callback)
  │  ① fantasy invokes registered OnToolCall with call (name, input, id).
  │     StreamEvent{tool_call, ToolName, ToolArgs, ToolCallID} sent to agent.
  ▼
fantasy.AgentTool.Run(ctx, call)
  │  ② fantasy dispatches to the typed or generic tool wrapper.
  │
  ├─ typed wrapper (typed_*.go)
  │    json.Unmarshal(call.Input, &args)
  │    check ctx.Err() for cancellation
  │    ↓
  │    reg.Execute(ctx, call.Name, call.Input)
  │       or direct helper (typed wrappers bottom out in Registry)
  │    ↓
  │    fantasy.NewTextResponse(out)  or  NewTextErrorResponse(err)  on error
  │    return (response, nil)  ← nil Go error keeps stream alive
  │
  └─ legacy bitchteaTool adapter (tools.go:26)
       t.reg.Execute(ctx, call.Name, call.Input)
       ↓
       fantasy.NewTextResponse(out)  or  NewTextErrorResponse(err)  on error
       return (response, nil)
  │
  ▼
internal/tools/tools.go:Registry.Execute(ctx, name, argsJSON)
  │  ③ dispatches by name:
  │     execRead    → resolvePath → os.ReadFile
  │     execWrite   → resolvePath → os.WriteFile
  │     execEdit    → resolvePath → string replacement
  │     execBash    → cmd.Run with 30s timeout
  │     execSearchMemory → filepath.Walk over scoped memory dirs
  │     execWriteMemory → append or overwrite memory file
  │     execTerminalStart → create PTY session via xpty
  │     execTerminalSend  → pty.Write(input)
  │     ... (14 tools total)
  │
  ▼
result string returned up the call stack
  │
  ▼
stream.go:OnToolResult (fantasy callback)
  │  ④ fantasy invokes registered OnToolResult with result.
  │     StreamEvent{tool_result, ToolCallID, ToolName, Text} sent to agent.
  ▼
agent/agent.go:sendMessage select loop
  │  ⑤ agent maps to agent.Event{tool_result, ToolName, ToolCallID, ToolResult}
  │     → TUI displays truncated result, clears activeToolName
  ▼
fantasy continues streaming
  │  model can issue more text or another tool_use
  ▼
next OnTextDelta or next OnToolCall (repeat cycle)
```

### Cancellation path (per-tool)

When Esc x1 is pressed during a tool call:

```text
UI handleEscKey
  → agent.CancelTool(toolCallID)           agent/agent.go:225
    → client.ToolContextManager()
      → toolCtxMgr.CancelTool(toolCallID)  llm/tool_context.go
        → cancel() on the child context
          → typed wrapper's ctx.Err() check
            → NewTextErrorResponse("Error: context canceled")
              → fantasy OnToolResult receives cancellation text
                → model sees "Error: context canceled" and can proceed
```

The parent turn context is NOT cancelled. The model receives the cancellation
text as a normal tool result and can continue streaming — it may re-issue the
same tool call or move on.

### File reference

| Hop | File | Function |
|-----|------|----------|
| ① | `internal/llm/stream.go:152` | `OnToolCall` callback |
| ② | `internal/llm/typed_read.go:50` | typed wrapper `Run` (6 tools) |
| ② | `internal/llm/tools.go:26` | legacy `bitchteaTool.Run` (8 tools) |
| ③ | `internal/tools/tools.go:73` | `Registry.Execute` dispatcher |
| ④ | `internal/llm/stream.go:161` | `OnToolResult` callback |
| ⑤ | `internal/agent/agent.go:351` | agent `tool_call` → `tool_start` mapping |
| ⑤ | `internal/agent/agent.go:361` | agent `tool_result` → `Event{tool_result}` |
