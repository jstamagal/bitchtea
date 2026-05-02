# 🦍 THE BITCHTEA SCROLLS: TOOLS

Bitchtea's hands are the tools. They live in `internal/tools/tools.go`.

## 🛠️ THE REGISTRY

The `Registry` is the master of all capabilities. It manages:
- **WorkDir**: The root of the project.
- **SessionDir**: Where session-local data is stored.
- **terminals**: A manager for persistent PTY sessions.

## 📜 BUILT-IN TOOLS

### 1. `read`
Reads file contents with optional pagination.
- **Args**: `path` (string), `offset` (int, 1-indexed), `limit` (int).
- **Limit**: Max 50KB per read to avoid context blowup.

### 2. `write`
Writes full content to a file.
- **Args**: `path` (string), `content` (string).
- **Behavior**: Automatically creates parent directories (`MkdirAll`).

### 3. `edit`
Surgical text replacement.
- **Args**: `path` (string), `edits` ([]object{oldText, newText}).
- **Rule**: `oldText` must be unique in the file to prevent ambiguous patches.

### 4. `bash`
Executes arbitrary shell commands.
- **Args**: `command` (string), `timeout` (int, default 30s).
- **Environment**: Runs in `WorkDir`. Combines stdout and stderr.

### 5. `terminal_*` (Start, Send, Snapshot, Close)
Persistent interactive terminal sessions.
- **Start**: Launches a `bash -lc` command attached to a PTY.
- **Snapshot**: Captures the current screen state via a terminal emulator (`charmbracelet/x/vt`).
- **Send**: Injects raw input (including ANSI escapes) into the PTY.

### 6. `search_memory`
Recalls durable facts from the long-term archive.
- **Args**: `query` (string), `limit` (int).
- **Scope**: Filtered by the active IRC context (channel/query).

### 7. `preview_image`
Renders images (PNG/JPEG/GIF) into ANSI block art for the terminal.

## 🧬 DISPATCHER & EXECUTOR

- **Definitions**: `Registry.Definitions()` returns OpenAI-compatible JSON schemas (`tools.go:42`). These are still the canonical schema source for the system prompt.
- **Execution**: `Registry.Execute()` is the underlying executor. All tools dispatch into it eventually.
- **Provider-facing registration** (Phase 2 / `bt-p2`): `internal/llm/translateTools` now prefers a **typed `fantasy.NewAgentTool` wrapper** for each tool that has been ported. As of bt-p2-verify the typed roster is `read`, `write`, `edit`, `bash`, `search_memory`, `write_memory` (see `internal/llm/typed_*.go`). The remaining tools — `terminal_start`, `terminal_send`, `terminal_keys`, `terminal_snapshot`, `terminal_wait`, `terminal_resize`, `terminal_close`, and `preview_image` — still flow through the legacy generic adapter (`bitchteaTool`). Both paths bottom out in `Registry.Execute`, so behavior is identical; the typed path just gives fantasy a real Go type for the input instead of a JSON string.

🦍💪🤝 APES STRONK TOGETHER 🦍💪🤝

## 🧱 TECHNICAL DEEP-DIVE: THE METAL

### 1. `read`
- **Executor**: `os.ReadFile(path)` (`internal/tools/tools.go:197`).
- **Logic**: Slurps the entire file into memory, then slices by `offset`/`limit` if provided via `strings.Split(content, "\n")`.
- **LLM Verbatim**: Returns the raw string content of the requested slice.
- **Constraints**: Forcefully truncated to 50KB (`maxSize`) at line 222 to prevent context saturation.

### 2. `write`
- **Executor**: `os.MkdirAll` followed by `os.WriteFile(path, data, 0644)` (`internal/tools/tools.go:238-241`).
- **Logic**: Ensures parent directory existence before commit.
- **LLM Verbatim**: `"Wrote %d bytes to %s"`.

### 3. `edit`
- **Executor**: Read-Modify-Write cycle. `os.ReadFile` -> `strings.Replace` -> `os.WriteFile` (`internal/tools/tools.go:267-285`).
- **Gears**: Checks for uniqueness using `strings.Count`. If `count > 1`, it refuses to patch to avoid ambiguity (`line 279`).
- **LLM Verbatim**: `"Applied %d edit(s) to %s"`.

### 4. `bash`
- **Executor**: `exec.CommandContext(ctx, "bash", "-c", command)` (`internal/tools/tools.go:325`).
- **Signals/Timeout**: 
  - Uses `context.WithTimeout` (default 30s). 
  - Context cancellation (via `Ctrl+C` or `Esc`) triggers a `SIGKILL` to the process group.
- **LLM Verbatim**: Combined stdout and stderr. If the command fails with a non-zero exit code, it appends `"\nExit code: X"` (`line 343`). Output is capped at 50KB.

### 5. `search_memory`
- **Executor**: `memorypkg.SearchInScope` (`internal/tools/tools.go:299`).
- **Logic**: Walks the `MemoryScope` hierarchy, querying the vector-like markdown archives.
- **LLM Verbatim**: A rendered markdown list of hits, including file source and snippet.

### 6. `terminal_*` (PTY Suite)
- **Executor**: `xpty.NewPty` and `exec.CommandContext(ctx, "bash", "-lc", command)` (`internal/tools/terminal.go:73-74`).
- **Internal State Management**:
  - `terminalManager` maintains a `map[string]*terminalSession` protected by a `sync.Mutex`.
  - `terminalSession` tracks the PTY, a `vt.SafeEmulator` for screen state, and a `done` channel for exit tracking.
  - Background `io.Copy` routines sync the PTY output into the virtual terminal emulator (`line 94`).
- **LLM Verbatim**: A text-based "snapshot" of the current virtual screen, including terminal dimensions and exit status (`line 186`).

### 7. `preview_image`
- **Executor**: `image.Decode` and `mosaic.New().Render(img)` (`internal/tools/image.go:44-51`).
- **Logic**: Decodes PNG/JPEG/GIF and converts pixels to Unicode block characters with ANSI color codes.
- **LLM Verbatim**: Metadata string (`path (format, WxH)`) followed by the ANSI block art.

🦍💪🤝 APES STRONK TOGETHER 🦍💪🤝

## 🦴 INTERNAL GEARS: VERBATIM TRACE & STATE

This section peels back the skin of the executor (`internal/tools/Registry.Execute`) and tracing the raw byte flow between the LLM and the OS.

### 🧬 THE DATA FLOW HANDSHAKE
1.  **LLM** sends a `tool_call` event with a JSON payload of `arguments`.
2.  **fantasy** dispatches the call into the registered tool. For typed-ported tools (read/write/edit/bash/search_memory/write_memory) it lands in the typed `Run` callback in `internal/llm/typed_*.go`, which re-marshals to JSON and calls `Registry.Execute`. For unported tools (terminal_*, preview_image) it lands in `bitchteaTool.Run` which forwards directly to `Registry.Execute`. Either way the agent sees a `tool_start` event when execution begins.
3.  **Registry** unmarshals the JSON into a local struct and runs the private `exec*` method.
4.  **REPL/TUI** receives a `tool_start` event and prints `calling [tool]...` to the viewport.
5.  **Registry** returns a `string` (success) or `error`.
6.  **TUI** prints the result (truncated to 20 lines) to the viewport.
7.  **LLM** receives the *untruncated* string (up to 50KB safety limit) as a `tool` role message to continue its thought loop.

---

### 🔨 TOOL-BY-TOOL EXPOSURE

#### 1. `read`
- **Executor**: `execRead(argsJSON)` (`internal/tools/tools.go:185`)
- **Under the Hood**: Slurps the entire file via `os.ReadFile` (`line 197`), converts to string, then uses `strings.Split(content, "\n")` to paginate if `offset` or `limit` are set.
- **REPL Output**: Shows the file content (capped at 20 lines in TUI).
- **LLM Verbatim Out**: The raw text of the file slice. If the file is > 50KB, it is truncated at `line 222` and appends `\n... (truncated)`.

#### 2. `write`
- **Executor**: `execWrite(argsJSON)` (`internal/tools/tools.go:228`)
- **Under the Hood**: Calls `os.MkdirAll(filepath.Dir(path), 0755)` (`line 238`) before `os.WriteFile`. This ensures the ape can create deep directory structures in one move.
- **REPL Output**: `Wrote X bytes to path/to/file`.
- **LLM Verbatim Out**: Same as REPL output.

#### 3. `edit`
- **Executor**: `execEdit(argsJSON)` (`internal/tools/tools.go:250`)
- **Under the Hood**: A non-atomic Read-Replace-Write. It uses `strings.Count(content, edit.OldText)` (`line 279`) as a safety fuse. If the count != 1, it returns an error to the LLM: `oldText matches %d times... (must be unique)`.
- **REPL Output**: `Applied X edit(s) to path/to/file`.
- **LLM Verbatim Out**: Same as REPL output.

#### 4. `bash`
- **Executor**: `execBash(ctx, argsJSON)` (`internal/tools/tools.go:312`)
- **Under the Hood**: Uses `exec.CommandContext(ctx, "bash", "-c", command)` (`line 325`). 
- **Timeout/Signals**: Hardcoded 30s timeout if not provided. The `ctx` is derived from the **Turn Context**. If KING hits `Ctrl+C` or `Esc`, the context is cancelled, and Go sends `SIGKILL` to the bash process.
- **REPL Output**: The combined stdout/stderr of the command.
- **LLM Verbatim Out**: The full output (up to 50KB). If a non-zero exit occurs, it appends `\nExit code: %d` (`line 343`).

#### 5. `terminal_start`
- **Executor**: `terminalManager.Start(ctx, argsJSON)` (`internal/tools/terminal.go:48`)
- **Under the Hood**: 
  1. Creates a PTY via `xpty.NewPty` (`line 73`).
  2. Spawns `bash -lc` inside that PTY.
  3. Spawns two background goroutines: `io.Copy(session.emu, pty)` and `io.Copy(pty, session.emu)` (`line 94-95`).
  4. The `vt.SafeEmulator` (`emu`) acts as the virtual screen buffer.
- **Internal State**: The `terminalManager` struct (`line 13`) holds a `map[string]*terminalSession` and a `sync.Mutex` to prevent race conditions when multiple tools (or models) try to touch the same PTY.
- **REPL Output**: A text snapshot of the terminal screen.
- **LLM Verbatim Out**: The same screen snapshot string.

#### 6. `search_memory`
- **Executor**: `execSearchMemory(argsJSON)` (`internal/tools/tools.go:289`)
- **Under the Hood**: Triggers a crawl of the `MEMORY.md` (root) and `HOT.md` (scoped) files via `memorypkg.SearchInScope`. It performs a keyword match across the hierarchical lineage of the current channel.
- **REPL Output**: A list of memory hits.
- **LLM Verbatim Out**: A formatted markdown block: `Memory matches for "query":\n1. Source: path\nHeading: text\nsnippet...`

🦍💪🤝 APES STRONK TOGETHER 🦍💪🤝
