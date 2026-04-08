# Contributing

This file is for people changing `bitchtea` itself. User-facing usage belongs in `README.md`.

## Development Setup

```bash
git clone ssh://git@jelly.hedgehog-bortle.ts.net:2222/jstamagal/bitchtea.git
cd bitchtea
go build -o bitchtea .
./bitchtea --help
```

The project targets Go 1.24. Keep changes inside this repo and keep shell commands non-interactive.

## Required Checks

If you change code, all four of these must pass before closing the issue:

```bash
go build ./...
go test ./...
go test -race ./...
go vet ./...
```

When you need the actual binary, also build it directly:

```bash
go build -o bitchtea .
```

## Repo Layout

```text
main.go                     Entry point, CLI flags, TUI bootstrap
internal/
  agent/                    Agent loop and context discovery
  config/                   Config defaults, env detection, profiles
  llm/                      OpenAI/Anthropic clients, retry, cost tracking
  session/                  JSONL persistence, resume, fork, tree
  sound/                    Terminal bell notifications
  tools/                    Tool registry and execution
  ui/                       Bubble Tea model, rendering, styles, themes
charmbracelet/              Local Charm-related material kept with the repo
```

Dependency flow:

```text
main -> config, session, ui
ui -> agent, config, session, sound
agent -> config, llm, tools
session -> llm
tools -> llm
```

Keep it acyclic.

## Runtime Model

The main loop is split across a few packages:

- `main.go` parses flags, detects the provider, restores a session, and starts Bubble Tea.
- `internal/ui/model.go` owns input handling, slash commands, tool-panel state, and event routing.
- `internal/agent/agent.go` runs the LLM/tool loop: send prompt, stream output, execute tool calls, feed tool results back, repeat until the turn ends.
- `internal/tools/tools.go` defines the built-in tool surface: `read`, `write`, `edit`, and `bash`.
- `internal/session/session.go` persists each event as one JSON line and handles resume, list, and fork behavior.

`Update()` must stay non-blocking. Long-running work belongs in commands/goroutines that send messages back into the model.

## Session Format

Sessions live in `~/.local/share/bitchtea/sessions/` as JSONL. Each line is a `session.Entry` with:

- `ts`
- `role`
- `content`
- `tool_name`
- `tool_args`
- `tool_call_id`
- `tool_calls`
- `parent_id`
- `branch`
- `id`

Append-only writes matter here. If you change the format, keep resume, tree, and fork behavior compatible or migrate it deliberately.

## Adding Or Changing Commands

Slash commands are handled in `internal/ui/model.go` inside `handleCommand`.

When adding a command:

1. Update command parsing and state changes in `handleCommand`.
2. Add tests in `internal/ui/` for the new behavior.
3. Update `README.md` if the command is user-facing.
4. Update `printUsage()` in [main.go](main.go) if the CLI help should mention it.

Do not block inside command handlers. Return Bubble Tea commands instead.

## Adding Or Changing Tools

Built-in tool definitions and execution live in `internal/tools/tools.go`.

When adding a tool:

1. Extend `Definitions()` with the schema exposed to the LLM.
2. Extend `Execute()` with the new dispatcher case.
3. Implement the executor with path handling and useful error messages.
4. Add tests in `internal/tools/tools_test.go`.

Tool behavior is intentionally powerful. Do not add artificial guardrails that break the coding-assistant workflow.

## UI And Theme Work

- `internal/ui/render.go` handles markdown and wrapping.
- `internal/ui/styles.go` and `internal/ui/themes.go` define look-and-feel.
- `internal/ui/toolpanel.go` owns the collapsible tool sidebar.
- `internal/ui/art.go` holds splash art.

If you add a theme, wire it into `themes.go` and make sure it shows up in `/theme`.

## Testing Notes

- Use the fake streamer in `internal/agent/agent_loop_test.go` for agent-loop tests instead of making network calls.
- Use `t.TempDir()` in tests. Do not write to `/tmp` manually.
- Prefer focused package tests while iterating, then run the full required check set before issue close.

## Docs Split Rule

- `README.md` is for users.
- `CONTRIBUTING.md` is for maintainers and contributors.

If documentation drifts, move implementation detail out of the README instead of duplicating it.
