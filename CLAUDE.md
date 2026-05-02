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

Build the main binary:

```bash
go build -o bitchtea .
```

`cmd/trace` is a developer scratchpad for exercising the tools registry against real filesystem/bash behavior — build it with `go build ./cmd/trace` and run only when investigating tool bugs. It is not shipped.

Run a single test or package:

```bash
go test ./internal/agent -run TestAgentLoop
go test ./internal/ui      # one package at a time while iterating
```

The project targets Go 1.26 (see `go.mod`). Keep shell commands non-interactive.

## High-Level Architecture

bitchtea is a BitchX-styled TUI coding assistant built on the Charm stack (Bubble Tea, Lipgloss, Glamour). The runtime splits across a small set of packages with a strict acyclic dependency graph:

```
main    -> agent, config, llm, session, tools, ui
ui      -> agent, config, llm, session, sound
agent   -> config, llm, memory, tools     (also agent/event subpkg)
session -> llm
llm     -> tools
tools   -> memory
```

Keep it acyclic. A change that adds an upward edge (e.g., `llm -> agent`, or `tools -> llm`) is wrong — `llm` already depends on `tools`, so the reverse direction would cycle.

### Runtime model

- `main.go` parses flags, runs `config.MigrateDataPaths()`, applies `~/.bitchtearc` via `config.ParseRC` + `ApplyRCSetCommands`, resolves a profile, optionally restores a session, then either runs `runHeadless` or boots Bubble Tea with `ui.NewModel(cfg)`.
- `internal/ui/model.go` is the Bubble Tea `Model`. It owns input handling, slash-command dispatch (`handleCommand`), tool-panel state, signal handling (`SignalMsg`), and routing of `agentEventMsg` events back into the viewport. **`Update()` must stay non-blocking** — long work belongs in `tea.Cmd`s/goroutines that send messages back.
- `internal/agent/agent.go` runs the LLM/tool loop: stream prompt → emit `event.Event`s (`text`, `tool_start`, `tool_result`, `state`, `error`, `done`) → execute tool calls → feed results back → repeat until the turn ends. The agent holds a single `messages []fantasy.Message` slice for the whole session and a current `MemoryScope` (root / `#channel` / `query`) used to route memory reads/writes — the message history itself is **not** yet isolated per IRC context (see "In flight" below). The agent bridges to `Client.StreamChat` (still `[]llm.Message`-typed) at the call site via `llm.FantasySliceToLLM` outgoing and `llm.LLMToFantasy` incoming. `Compact()` flushes older messages to per-day memory files via `internal/memory` before truncating; `bootstrapMsgCount` marks the boundary that compaction must not cross.
- `internal/llm` exposes a single `Client` (`client.go`) and a `ChatStreamer` interface; `stream.go` is the unified streaming loop and `providers.go` holds per-provider config (OpenAI-compatible vs Anthropic). `convert.go`, `cost.go`, `errors.go`, and `tools.go` handle message conversion, cost tracking, error classification, and tool-schema plumbing.
- `internal/tools/tools.go` defines and executes the built-in tool surface: `read`, `write`, `edit`, `bash`, `search_memory`, the `terminal_*` PTY family (`start`/`send`/`keys`/`snapshot`/`wait`/`resize`/`close`), and `preview_image`. Terminal state lives in `terminal.go`; image handling in `image.go`. Tool behavior is intentionally powerful — do not add artificial guardrails that break the coding-assistant workflow.
- `internal/session/session.go` writes one JSON line per `session.Entry` to `~/.bitchtea/sessions/`. Append-only matters: resume, list, fork, and tree all assume it. If you change the format, migrate it deliberately. Channel/membership state has its own file (`membership.go`) alongside the session log.
- `internal/memory` provides the scoped memory store (`RootScope`, `ChannelScope`, `QueryScope`) used by both `search_memory` and agent compaction. Files live under `~/.bitchtea/memory/` keyed by workspace + scope, with daily append files for compacted history.

### Provider detection and profiles

`config.DetectProvider` infers the provider from env vars (`OPENAI_API_KEY`, `ANTHROPIC_API_KEY`, etc.). Built-in profiles (`ollama`, `openrouter`, `zai-openai`, `zai-anthropic`) live in `internal/config` and are loadable via `--profile` or `/profile load`. The `ollama` profile is the only one that may have an empty API key (see `ProfileAllowsEmptyAPIKey`).

### Sessions and context discovery

- Sessions persist as JSONL under `~/.bitchtea/sessions/`; `--resume [path]` restores via `session.Load`. `latest` resolves to the most recent file.
- `AGENTS.md` and `CLAUDE.md` are discovered upward from the working directory and injected as agent context (see `internal/agent/context.go`).
- `@file` tokens inline file contents into the prompt.
- `MEMORY.md` (per-workspace) is gitignored and consumed via `/memory`.

### In flight (don't describe these as done)

These are open architectural items tracked in `bd` — read the current state from the code, not from intuitions about what the IRC framing implies:

- **Per-context histories** (`bt-x1o`, P0): `/join #chan` and `/query nick` only re-label the UI today; the agent still streams against one shared `messages` slice. Don't write code or docs that assume isolated histories until this lands.
- **`write_memory` tool** (`bt-vhs`, P0): now implemented (added with bt-p2-switch and exposed as a typed wrapper). The bullet stays in "in flight" only until `bt-vhs` is closed; the surface itself is live.
- **Daemon removal** (`bt-76w`, P2): the old `_attic` daemon was deleted during compaction. There is currently **no daemon binary, no `cmd/daemon`, no `internal/daemon` package**, and no hardcoded compaction model. A future Phase 7 epic (`bt-p7`, P3) may rebuild it, but until then do not reintroduce daemon assumptions.
- **Fantasy migration** (Phases 2–4, epics `bt-p2`/`bt-p3`/`bt-p4`): partial. Phase 2 is in flight: 6 tools (`read`, `write`, `edit`, `bash`, `search_memory`, `write_memory`) are now typed `fantasy.NewAgentTool` wrappers in `internal/llm/typed_*.go`; the remaining 8 (`terminal_start`, `terminal_send`, `terminal_keys`, `terminal_snapshot`, `terminal_wait`, `terminal_resize`, `terminal_close`, `preview_image`) still flow through the legacy generic `bitchteaTool` adapter. `translateTools` picks the typed wrapper when one exists and falls back to the generic adapter; both bottom out in `Registry.Execute(name, argsJSON)`. **Phase 3 is complete**: the agent's in-memory history and session JSONL are both fantasy-native (`messages []fantasy.Message`; v1 entries written via `EntryFromFantasy`; restore via `FantasyFromEntries`). Legacy `EntryFromMessage` / `MessagesFromEntries` shims have been removed; the v0 → v1 fallback reader (`legacyEntryToFantasy`) stays so old session files keep loading. The remaining `llm.Message` usage is the necessary glue at the `Client.StreamChat` boundary (Phase 4-or-later: make `Client.StreamChat` take fantasy directly). `ProviderOptions` persistence on session entries was deferred. The `bt-p3-*` family is closed.

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

- `README.md` is the user-facing entrypoint.
- `docs/` holds longer-form maintainer docs (`architecture.md`, `agent-loop.md`, `streaming.md`, `tools.md`, `memory.md`, `sessions.md`, `commands.md`, `cli-flags.md`, `signals-and-keys.md`, `getting-started.md`, `user-guide.md`, `development.md`, `glossary.md`, `troubleshooting.md`). Prefer adding deeper detail there rather than expanding `README.md` or this file.
- `AGENTS.md` is read by agents at startup (alongside `CLAUDE.md`) — keep agent-facing instructions there if they're for *running* the assistant, not for working on the repo.


<!-- BEGIN BEADS INTEGRATION v:1 profile:minimal hash:ca08a54f -->
## Beads Issue Tracker

This project uses **bd (beads)** for issue tracking. Run `bd prime` to see full workflow context and commands.

### Quick Reference

```bash
bd ready              # Find available work
bd show <id>          # View issue details
bd update <id> --claim  # Claim work
bd close <id>         # Complete work
```

### Rules

- Use `bd` for ALL task tracking — do NOT use TodoWrite, TaskCreate, or markdown TODO lists
- Run `bd prime` for detailed command reference and session close protocol
- Use `bd remember` for persistent knowledge — do NOT use MEMORY.md files

## Session Completion

**When ending a work session**, you MUST complete ALL steps below. Work is NOT complete until `git push` succeeds.

**MANDATORY WORKFLOW:**

1. **File issues for remaining work** - Create issues for anything that needs follow-up
2. **Run quality gates** (if code changed) - Tests, linters, builds
3. **Update issue status** - Close finished work, update in-progress items
4. **PUSH TO REMOTE** - This is MANDATORY:
   ```bash
   git pull --rebase
   bd dolt push
   git push
   git status  # MUST show "up to date with origin"
   ```
5. **Clean up** - Clear stashes, prune remote branches
6. **Verify** - All changes committed AND pushed
7. **Hand off** - Provide context for next session

**CRITICAL RULES:**
- Work is NOT complete until `git push` succeeds
- NEVER stop before pushing - that leaves work stranded locally
- NEVER say "ready to push when you are" - YOU must push
- If push fails, resolve and retry until it succeeds
<!-- END BEADS INTEGRATION -->
