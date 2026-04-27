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

- **Definitions**: `Registry.Definitions()` returns OpenAI-compatible JSON schemas (`tools.go:42`).
- **Execution**: `Registry.Execute()` is the entry point for all tool calls. It unmarshals JSON args and routes to private `exec*` methods.

🦍💪🤝 APES STRONK TOGETHER 🦍💪🤝
