# Project Map

`bitchtea` is a terminal coding assistant written in Go. It is a BitchX-inspired TUI that streams model output, exposes file and shell tools, persists sessions, and keeps a local memory layer on disk.

Before planning or refactors, read `ARCHITECTURE_CONVERSATION_LOG.md` first. It is the required first-pass architecture context for this repo.

## Runtime Flow

1. `main.go` builds the runtime config from env vars, CLI flags, and built-in profile selection.
2. The app can run in two modes: interactive TUI or `--headless`.
3. In TUI mode, `ui.NewModel` creates the Bubble Tea model, restores focus/membership/session state, and opens the main loop.
4. In headless mode, `runHeadless` sends one prompt through the agent and prints output to stdout.
5. Sessions, focus state, membership state, and transcript logs are written under the `~/.bitchtea` data tree.

## Major Subsystems

- `internal/config`
  Holds runtime config, built-in profiles, profile persistence, rc-file helpers, and provider detection from env vars.
- `internal/agent`
  Owns the turn loop, message history, tool execution, token/cost tracking, memory injection, and autonomous follow-up logic.
- `internal/llm`
  Wraps the chat transport, streaming events, provider-specific request shaping, retry handling, and cost accounting helpers.
- `internal/tools`
  Exposes the model tools: file read/write/edit, memory search, and bash execution.
- `internal/memory`
  Manages `MEMORY.md`, scoped HOT.md files, durable daily markdown memory, and search across memory layers.
- `internal/session`
  Stores JSONL transcripts plus persisted focus, membership, and checkpoint state.
- `internal/ui`
  Implements the TUI, slash commands, renderers, transcript logging, focus routing, membership/invite flow, tool panel, MP3 panel, and command dispatch.
- `internal/daemon`
  Runs the background heartbeat and janitor service for memory maintenance.

## Important Entry Points

- `main.go`
  Bootstraps config, profile loading, resume, headless mode, and the interactive TUI.
- `internal/ui/model.go`
  The main Bubble Tea model and event loop.
- `internal/ui/commands.go`
  Slash command registry and command handlers.
- `internal/agent/agent.go`
  The assistant turn loop and follow-up generation.
- `internal/llm/client.go`
  The streaming model client and provider request layer.
- `internal/session/session.go`
  Session file persistence and restore helpers.

## How The Pieces Fit

1. `config.DefaultConfig()` and `config.DetectProvider()` establish the base runtime settings.
2. `ui.NewModel()` builds the TUI state and constructs an `agent.Agent`.
3. `agent.NewAgent()` wires the LLM client, tool registry, context-file injection, and memory bootstrapping.
4. The agent streams model output through `llm.ChatStreamer` and tool calls through `internal/tools`.
5. The UI renders chat, tools, focus state, and side panels while persisting messages to `internal/session`.
6. `internal/memory` feeds both bootstrap memory and scoped recall/search.

## Behavior That Matters

- Chat is the primary interface.
- Slash commands control model settings, session state, and utility actions.
- `auto-next` and `auto-idea` can cause the agent to continue automatically after a completed turn.
- `focus` and `membership` make the TUI behave like IRC-style contexts and personas.
- `transcript` writes a daily human-readable log separate from JSONL session files.

## Current Risk Areas

- `internal/ui/commands.go` has a local unstaged edit that duplicates `handleInviteCommand` and `handleKickCommand`, which currently breaks `go build`.
- `/set` work is in progress and needs to stay aligned with the rc-file behavior in `internal/config/rc.go`.
- The rc-file helpers exist, but startup wiring still needs to be checked against `main.go`.
- `auto-next` follow-up logic can keep the agent moving after a turn ends, so it is a likely source of runaway spend if left uncontrolled.
- `internal/config/config.go` and `internal/llm/client.go` still carry the provider/service split TODOs for future refactoring.

## Mental Model For Resuming Work

- Read `ARCHITECTURE_CONVERSATION_LOG.md` first if you are about to plan or refactor anything.
- Read this file first.
- Then read `CURRENT_STATE.md` for the live snapshot.
- If the next move is unclear, write a tiny draft in `NEXT_STEPS.md` before touching code.
- Keep the conversation notes tentative until a decision is actually settled.
