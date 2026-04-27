# 🦍 BITCHTEA ARCHITECTURE 🦍

This document is the source of truth for the `bitchtea` Go project. It maps the internal structure, dependency rules, and runtime flows for anyone returning to the canopy from the Cold Metal Rooms.

## 1. Package Map (`internal/`)

The core logic resides in `internal/`, strictly partitioned to maintain isolation.

- **`agent`**: Orchestrates the autonomous "fantasy" loop. It manages conversation history, state compaction, and tool execution. It is the high-level brain that coordinates between the LLM and the local environment. (See `internal/agent/agent.go`)
- **`config`**: Handles profile detection, environment variable loading, and data path migration. It ensures the application knows where to find its keys and where to store its poop. (See `internal/config/config.go`)
- **`llm`**: **[UNDER RECONSTRUCTION]** The interface layer for LLM providers (OpenAI, Anthropic, etc.). It defines types for messages, tool calls, and streaming events. It is currently being rebuilt to support more robust streaming and error handling.
- **`memory`**: Manages long-term and short-term "memory" scopes. It handles `MEMORY.md` at the root and `HOT.md` files in specific IRC-style contexts, as well as daily durable logs. (See `internal/memory/memory.go`)
- **`session`**: Responsible for the persistence of conversation history. It handles the JSONL format used to save, load, and resume sessions from disk. (See `internal/session/session.go`)
- **`sound`**: A small utility package for audio feedback. It handles MP3 playback for "bing" notifications when a turn completes or a tool runs. (See `internal/sound/sound.go`)
- **`tools`**: The registry and execution engine for agent capabilities (e.g., `bash`, `read_file`). It translates agent tool calls into local system actions. (See `internal/tools/tools.go`)
- **`ui`**: The Bubble Tea based terminal interface. It handles rendering, keyboard input, and the visual representation of the agent's internal state (transcript, tool panels). (See `internal/ui/model.go`)

## 2. Dependency Graph & Acyclic Law

The dependency graph is strictly unidirectional to prevent the "tangled vine" problem (circular dependencies) which makes testing and refactoring impossible.

**The Graph:**
- `main` -> `config`, `session`, `ui`
- `ui` -> `agent`, `config`, `session`, `sound`
- `agent` -> `config`, `llm`, `tools`, `memory`
- `session` -> `llm`
- `tools` -> `llm`

**Why Acyclic?**
If `agent` depended on `ui`, and `ui` depended on `agent`, neither could be initialized or tested in isolation. The core logic (`agent`) must never know about the presentation (`ui`). It emits events; the UI listens.

## 3. Runtime Flow

1. **Startup**: `main.go` calls `config.DefaultConfig()` and `config.DetectProvider()`. It then builds the startup model with `ui.NewModel(&cfg, sess, rcCommands)`.
2. **User Input**: The user types a message in the UI. `ui/model.go` catches this in `Update()` and triggers `m.sendToAgent(input)`.
3. **Agent Activation**: `m.sendToAgent` calls `agent.SendMessage(ctx, input, ch)`. This starts the "fantasy" loop in a background goroutine.
4. **LLM Streaming**: Inside `agent.SendMessage`, the agent prepares messages and calls `a.streamer.StreamChat(...)` (the LLM layer).
5. **Event Feedback**: As the LLM streams tokens or calls tools, the `agent` sends `Event` structs back through a channel.
6. **UI Update**: `ui/model.go` consumes these events in `handleAgentEvent(ev)`, updating the transcript and triggering re-renders until the turn is `Done`.

## 4. Session JSONL Format

Sessions are stored at `~/.bitchtea/sessions/` (or custom data dir).
- **Format**: Append-only JSONL (JSON Lines).
- **Structure**: Each line is a `session.Entry` struct (see `internal/session/session.go:17`).
- **Fields**: Includes `ts` (timestamp), `role`, `content`, `context` (IRC label), `tool_calls`, and `id`.
- **Usage**: Used for `--resume latest`, listing past conversations, forking branches, and visualizing the conversation tree.

## 5. IRC-Style Memory Scope

Bitchtea uses a hierarchical memory system (`internal/memory/memory.go`).
- **Root Scope**: `MEMORY.md` at the project root. This is the global "truth" for the repository.
- **Context Discovery**: When the agent runs, it discovers context by walking up from the current working directory, looking for `AGENTS.md` or `CLAUDE.md` (and `GEMINI.md` in this variant).
- **Scoped Memory**: In IRC-style modes (e.g. `#channel`), memory is stored in `HOT.md` files within `~/.bitchtea/memory/<scope-hash>/contexts/<channel>/HOT.md`.
- **Durable Daily**: Compaction flushes old messages to daily markdown files like `2026-04-27.md` to keep the context window lean while preserving history.

## 6. Signal & Key Handling

Per `REBUILD_NOTES.md`, signal handling is a first-class citizen for safety.
- **Ctrl+C**: Hard cancel. Triggers `ctx` cancellation. In-flight tool subprocesses receive `SIGKILL` via `exec.CommandContext`. A second `Ctrl+C` at the prompt quits the app.
- **Ctrl+Z**: Suspend. Sends `SIGTSTP` to the process, allowing the user to drop to shell and return with `fg`.
- **Esc (3-Stage)**:
  1. **Esc x1**: Close open panel OR cancel current tool call only (tool returns "user cancelled"). Turn continues.
  2. **Esc x2**: Full turn cancel (same as Ctrl+C).
  3. **Esc x3**: Full turn cancel AND clear the message queue (the "panic" button).

## 7. The Attic (`_attic/`)

Legacy code and reference material are quarantined here:
- **`cmd-daemon/`**: Old CLI for the daemon.
- **`daemon/`**: The previous background service implementation (janitor, etc.).
- **`crush-reference.md`**: Technical notes from the "Crush" era of the project.

APE STRONK TOGETHER. 🦍💪🤝
