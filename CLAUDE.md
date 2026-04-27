# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build and Test

Required checks before closing any code-changing issue (all four must pass):

```bash
go build ./...
go test ./...
go test -race ./...
go vet ./...
```

Build the actual binaries:

```bash
go build -o bitchtea .
go build -o daemon ./cmd/daemon
```

Run a single test or package:

```bash
go test ./internal/agent -run TestAgentLoop
go test ./internal/ui      # one package at a time while iterating
```

The project targets Go 1.26 (see `go.mod`). Keep shell commands non-interactive.

## High-Level Architecture

bitchtea is a BitchX-styled TUI coding assistant built on the Charm stack (Bubble Tea, Lipgloss, Glamour). The runtime splits across a small set of packages with a strict acyclic dependency graph:

```
main -> config, session, ui
ui   -> agent, config, session, sound
agent-> config, llm, tools, memory
session -> llm
tools -> llm
```

Keep it acyclic. A change that adds an upward edge (e.g., `llm -> agent`) is wrong.

### Runtime model

- `main.go` parses flags, runs `config.MigrateDataPaths()`, applies `~/.bitchtearc` via `config.ParseRC` + `ApplyRCSetCommands`, resolves a profile, optionally restores a session, then either runs `runHeadless` or boots Bubble Tea with `ui.NewModel(cfg)`.
- `internal/ui/model.go` is the Bubble Tea `Model`. It owns input handling, slash-command dispatch (`handleCommand`), tool-panel state, signal handling (`SignalMsg`), and routing of `agentEventMsg` events back into the viewport. **`Update()` must stay non-blocking** — long work belongs in `tea.Cmd`s/goroutines that send messages back.
- `internal/agent/agent.go` runs the LLM/tool loop: stream prompt → emit `Event`s (`text`, `tool_start`, `tool_result`, `state`, `error`, `done`) → execute tool calls → feed results back → repeat until the turn ends. The agent also tracks IRC-style `MemoryScope` and a `bootstrapMsgCount` for compaction.
- `internal/llm` has a single `Client` plus separate codepaths for OpenAI (`client.go`) and Anthropic (`anthropic.go`) streaming, behind the `ChatStreamer` interface. Retry, error hints, and cost tracking live alongside.
- `internal/tools/tools.go` defines and executes the built-in tool surface (`read`, `write`, `edit`, `bash`). Tool behavior is intentionally powerful — do not add artificial guardrails that break the coding-assistant workflow.
- `internal/session/session.go` writes one JSON line per `session.Entry` to `~/.bitchtea/sessions/`. Append-only matters: resume, list, fork, and tree all assume it. If you change the format, migrate it deliberately.
- `internal/daemon` is a separate binary (`cmd/daemon`) that runs heartbeat and janitor loops. **`compactModel` is hardcoded to `claude-opus-4-6` / `anthropic` and must NOT be downgraded** — see the comment in `daemon.go`.

### Provider detection and profiles

`config.DetectProvider` infers the provider from env vars (`OPENAI_API_KEY`, `ANTHROPIC_API_KEY`, etc.). Built-in profiles (`ollama`, `openrouter`, `zai-openai`, `zai-anthropic`) live in `internal/config` and are loadable via `--profile` or `/profile load`. The `ollama` profile is the only one that may have an empty API key (see `ProfileAllowsEmptyAPIKey`).

### Sessions and context discovery

- Sessions persist as JSONL under `~/.bitchtea/sessions/`; `--resume [path]` restores via `session.Load`. `latest` resolves to the most recent file.
- `AGENTS.md` and `CLAUDE.md` are discovered upward from the working directory and injected as agent context (see `internal/agent/context.go`).
- `@file` tokens inline file contents into the prompt.
- `MEMORY.md` (per-workspace) is gitignored and consumed via `/memory`.

## Adding a Slash Command

1. Update parsing and state changes in `handleCommand` (`internal/ui/model.go`).
2. Add tests in `internal/ui/`.
3. Update `README.md` if the command is user-facing.
4. Update `printUsage()` in `main.go` if it should show in `--help`.

Do not block inside command handlers — return `tea.Cmd`s.

## Adding a Tool

1. Extend `Definitions()` in `internal/tools/tools.go` with the schema exposed to the LLM.
2. Extend `Execute()` with the new dispatcher case.
3. Implement the executor with path handling and useful error messages.
4. Add tests in `internal/tools/tools_test.go`.

## Testing Notes

- Use the fake streamer in `internal/agent/agent_loop_test.go` for agent-loop tests instead of making network calls.
- Use `t.TempDir()` in tests; never write to `/tmp` manually.
- Iterate on focused package tests, then run the full required-check set before closing the issue.

## Docs Split

- `README.md` is for users.
- `CONTRIBUTING.md` is for maintainers (deeper detail than this file).
- If docs drift, move implementation detail out of `README.md` rather than duplicating it.
