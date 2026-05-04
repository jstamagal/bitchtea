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

## Tool Surface Reference

### File System
- **`read`**: Supports `offset` and `limit` for surgical reading of large files.
- **`write`**: Overwrites files and automatically creates parent directories.
- **`edit`**: Implements unique-string replacement. Fails if `oldText` is ambiguous or missing.

### System
- **`bash`**: Standard shell execution with a default 30s timeout. Captures combined stdout/stderr.

### Memory
- **`search_memory`**: Queries hot (`MEMORY.md`) and daily archive files.
- **`write_memory`**: Persists notes into the current or specified scope.

### Visuals
- **`preview_image`**: (`image.go`) Renders PNG/JPEG/GIF into terminal-friendly ANSI block art for the LLM to "see" screenshots.
