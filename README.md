# bitchtea

A terminal coding assistant that doesn't hold your hand. BitchX-inspired TUI for LLM-powered coding sessions, built in Go with the [Charm](https://charm.sh) stack.

```
┌─ bitchtea — anthropic/claude-sonnet-4-20250514 ─────────[3:42pm]─┐
│──────────────────────────────────────────────────────────────────│
│ [11:36] <you> fix this broken migration                          │
│ [11:37] <bitchtea> Looking at the schema...                      │
│ [11:37]   → read: db/migrations/003_add_users.sql                │
│ [11:38] <bitchtea> Found it. Column type mismatch:               │
│         ```sql                                                   │
│         ALTER TABLE users ADD email VARCHAR(255);           ╭────│
│         ```                                                 │Tool│
│ [11:38]   → edit: applied 1 edit                            │ ───│
│ [11:39] <bitchtea> Fixed. Running migration...              │read│
│ [11:39]   → bash: go run cmd/migrate/main.go up             │edit│
│                                                             │bash│
│──────────────────────────────────────────────────────────────╰────│
│ [bitchtea] ◉ running tools...   read(3) bash(2) | ~4k tokens    │
│──────────────────────────────────────────────────────────────────│
│ >> _                                                             │
└──────────────────────────────────────────────────────────────────┘
```

## Install

```bash
go install github.com/jstamagal/bitchtea@latest
```

Or build from source:

```bash
git clone ssh://git@jelly.hedgehog-bortle.ts.net:2222/jstamagal/bitchtea.git
cd bitchtea
go build -o bitchtea .
```

## Quick Start

```bash
export ANTHROPIC_API_KEY=sk-ant-...   # or OPENAI_API_KEY=sk-...
bitchtea
```

It auto-detects which provider you've configured. Start typing.

## Usage

```
bitchtea [flags]

Flags:
  -m, --model <name>     Model to use (default: auto-detected)
  -p, --profile <name>   Load a saved connection profile
  -r, --resume [path]    Resume a session (latest if no path)
  --auto-next-steps      Keep the agent working after each turn
  --auto-next-idea       Brainstorm improvements after auto-next completes
  -h, --help             Show help

Environment:
  OPENAI_API_KEY         OpenAI API key
  OPENAI_BASE_URL        OpenAI-compatible base URL
  ANTHROPIC_API_KEY      Anthropic API key
  BITCHTEA_MODEL         Default model override
  BITCHTEA_PROVIDER      Force provider (openai, anthropic)
```

## What It Does

bitchtea connects to an LLM, streams responses into a terminal viewport, and gives the model four tools to work with: **read**, **write**, **edit**, and **bash**. The agent loop detects tool calls, executes them, feeds results back, and keeps going until the work is done. You steer it with natural language.

### Providers

Supports **OpenAI** (and any OpenAI-compatible API) and **Anthropic** natively. Both stream token-by-token via SSE. Switch at runtime with `/provider` and `/model`, or save combos as profiles with `/profile save`.

### Steering

Type while the agent is working. Messages queue up and get injected after the current turn finishes. Like IRC -- the channel keeps going but you can jump in any time.

### Sessions

Every conversation persists as a JSONL file with tree structure. Resume with `--resume` or `/sessions`. Fork from any point with `/fork`. Visualize the tree with `/tree`.

### Context

Auto-discovers `AGENTS.md` and `CLAUDE.md` walking up the directory tree. Supports `@file` references that expand inline. Compacts context automatically when approaching token limits, or manually with `/compact`.

## Commands

| Command | What it does |
|---|---|
| `/model <name>` | Switch model |
| `/provider <name>` | Switch provider (openai, anthropic) |
| `/baseurl <url>` | Set API base URL |
| `/apikey <key>` | Set API key |
| `/profile save\|load\|delete <n>` | Manage connection profiles |
| `/compact` | Compact conversation context |
| `/clear` | Clear chat display |
| `/diff` | Show git diff |
| `/status` | Git status |
| `/undo` | Revert unstaged changes |
| `/commit [msg]` | Git commit |
| `/tokens` | Token usage |
| `/memory` | Show MEMORY.md |
| `/sessions` | List sessions |
| `/tree` | Session tree |
| `/fork` | Fork session |
| `/auto-next` | Toggle auto-next-steps |
| `/auto-idea` | Toggle auto-next-idea |
| `/theme <name>` | Switch theme (bitchx, nord, dracula, gruvbox, monokai) |
| `/sound` | Toggle completion bell |
| `/help` | Help |
| `/quit` | Exit |

## Keybindings

| Key | Action |
|---|---|
| `Enter` | Send message |
| `Ctrl+C` | Interrupt / quit |
| `Ctrl+Z` | Suspend |
| `Ctrl+T` | Toggle tool panel |
| `Up/Down` | Input history |
| `PgUp/PgDn` | Scroll viewport |
| `Mouse wheel` | Scroll viewport |
| `Tab` | Complete commands and @file refs |

## Architecture

```
main.go                   CLI entry, flag parsing, signal handling
internal/
  config/                 Config struct, env detection, profiles
  agent/                  Agent loop (LLM <-> tool orchestration), context discovery
  llm/                    Streaming clients (OpenAI + Anthropic), retry, cost tracking
  session/                JSONL persistence, fork, tree
  tools/                  Tool registry: read, write, edit, bash
  ui/                     Bubbletea model, rendering, themes, tool panel, ANSI art
  sound/                  Terminal bell
```

Dependency flow: `main -> config, session, ui -> agent -> llm, tools`. No circular deps.

## Built With

- [bubbletea](https://github.com/charmbracelet/bubbletea) -- Elm-architecture TUI framework
- [lipgloss](https://github.com/charmbracelet/lipgloss) -- Terminal styling
- [bubbles](https://github.com/charmbracelet/bubbles) -- Viewport, textarea, spinner
- [glamour](https://github.com/charmbracelet/glamour) -- Markdown rendering

## Testing

```bash
go test ./...           # unit tests (includes offline agent loop via fake streamer)
go vet ./...            # static analysis
go build ./...          # build check
```

## Heritage

Named after [BitchX](https://en.wikipedia.org/wiki/BitchX), the IRC client that didn't care about your feelings. Six randomized ANSI splash screens carry on the tradition.
