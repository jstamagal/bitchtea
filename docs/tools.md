# 🦍 TOOLS MECHANICS 🦍

This scroll documents the physical levers of `bitchtea`. These are the built-in capabilities registered in `internal/tools/tools.go` that allow the agent to touch the world.

## 1. Tool Registry (`internal/tools/tools.go:21`)

The `Registry` holds the `WorkDir` (project root) and `SessionDir`. It manages the bridge between LLM function calls and local execution.

## 2. The Toolset

Every tool follows a two-stage pattern:
1. **`Definitions()`**: Returns the JSON schema to the LLM.
2. **`Execute()`**: Dispatches the call to a specific `exec` function.

### `read`
- **Schema**: `path` (required), `offset` (optional), `limit` (optional).
- **Executor**: `execRead` (`tools.go:193`)
- **Behavior**: Reads file content. Supports line-based pagination. Truncates output to 50KB to protect the context window.

### `write`
- **Schema**: `path` (required), `content` (required).
- **Executor**: `execWrite` (`tools.go:237`)
- **Behavior**: Writes full content to a path. Automatically creates missing parent directories using `os.MkdirAll`.

### `edit`
- **Schema**: `path` (required), `edits` (required array of `oldText`/`newText`).
- **Executor**: `execEdit` (`tools.go:260`)
- **Behavior**: Performs surgical string replacement. **Strict Safety**: Fails if `oldText` is not unique or not found in the file to prevent corrupted state.

### `search_memory`
- **Schema**: `query` (required), `limit` (optional).
- **Executor**: `execSearchMemory` (`tools.go:300`)
- **Behavior**: Queries the hierarchical memory system (`MEMORY.md` and daily logs) for relevant snippets.

### `bash`
- **Schema**: `command` (required), `timeout` (optional, default 30s).
- **Executor**: `execBash` (`tools.go:317`)
- **Behavior**: Executes a command via `exec.CommandContext`. Combines stdout and stderr. Supports cancellation via the `turnCtx`.

## 3. Fantasy Adapter (`internal/llm/tools.go`)

Tool definitions are exposed to the model via fantasy's `AgentTool`
interface, not the raw `[]ToolDef` slice. `bitchteaTool` wraps the
`tools.Registry`:

- `Info()` returns a `fantasy.ToolInfo` built from
  `Registry.Definitions()` (split into `Parameters` map + `Required`
  slice via `splitSchema`, which handles both `[]string` and `[]any`
  shapes coming back from JSON unmarshalling).
- `Run(ctx, fantasy.ToolCall)` dispatches into `Registry.Execute(ctx,
  call.Name, call.Input)`. Errors return `fantasy.NewTextErrorResponse`
  so fantasy keeps streaming; returning a Go error would abort the
  whole agent loop.

`translateTools(reg)` builds the `[]fantasy.AgentTool` slice handed to
`fantasy.WithTools(...)` when the agent is constructed in `stream.go`.

## 4. Security Philosophy

Tools are intentionally powerful. `bitchtea` does not impose artificial guardrails (e.g., blocking `rm -rf`). The agent is trusted. Security resides in the user's visibility of the transcript and the ability to cancel turns (`Ctrl+C`).

APE STRONK TOGETHER. 🦍💪🤝
