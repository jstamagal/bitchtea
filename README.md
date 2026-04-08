# bitchtea

A terminal coding assistant that does the work instead of performing concern about it. BitchX-inspired TUI for LLM-powered coding sessions, built in Go with the Charm stack.

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
export ANTHROPIC_API_KEY=sk-ant-...
bitchtea
```

OpenAI-compatible endpoints work too:

```bash
export OPENAI_API_KEY=sk-...
export OPENAI_BASE_URL=https://your-provider.example/v1
bitchtea
```

`bitchtea` auto-detects the configured provider, opens the TUI, and starts streaming once you send a prompt.

## Usage

```text
bitchtea [flags]

Flags:
  -m, --model <name>     Model to use
  -p, --profile <name>   Load a saved connection profile
  -r, --resume [path]    Resume a session (latest if no path)
  --auto-next-steps      Keep the agent working after each turn
  --auto-next-idea       Brainstorm improvements after auto-next completes
  -h, --help             Show help
```

Environment:

```text
OPENAI_API_KEY         OpenAI API key
OPENAI_BASE_URL        OpenAI-compatible base URL
ANTHROPIC_API_KEY      Anthropic API key
BITCHTEA_MODEL         Default model override
BITCHTEA_PROVIDER      Force provider (openai, anthropic)
```

## What You Get

- Natural-language coding workflow with tool execution for reading files, editing code, writing files, and running shell commands.
- Streaming responses in a single scrolling terminal buffer while tool activity stays visible in the side panel.
- Live steering: you can type while the agent is still working and your next message gets queued for the next turn.
- Session persistence with resume and fork support.
- Runtime switches for provider, model, profiles, theme, and sound without leaving the UI.

## Commands

| Command | What it does |
|---|---|
| `/model <name>` | Switch model |
| `/provider <name>` | Switch provider |
| `/baseurl <url>` | Set API base URL |
| `/apikey <key>` | Set API key |
| `/profile save\|load\|delete <name>` | Manage connection profiles |
| `/compact` | Compact conversation context |
| `/clear` | Clear chat display |
| `/diff` | Show git diff |
| `/status` | Git status |
| `/undo` | Revert unstaged changes |
| `/commit [msg]` | Git commit |
| `/copy` | Copy the last assistant message |
| `/tokens` | Show token usage estimate |
| `/memory` | Show `MEMORY.md` from the current workspace |
| `/sessions` | List saved sessions |
| `/tree` | Show session tree |
| `/fork` | Fork the current session |
| `/auto-next` | Toggle auto-next-steps |
| `/auto-idea` | Toggle auto-next-idea |
| `/theme <name>` | Switch theme |
| `/sound` | Toggle completion bell |
| `/help` | Show help |
| `/quit` | Exit |

## Keybindings

| Key | Action |
|---|---|
| `Enter` | Send message |
| `Ctrl+C` | Interrupt or quit |
| `Ctrl+Z` | Suspend |
| `Ctrl+T` | Toggle tool panel |
| `Up/Down` | Input history |
| `PgUp/PgDn` | Scroll viewport |
| `Mouse wheel` | Scroll viewport |
| `Tab` | Complete commands and `@file` references |

## Sessions And Context

- Sessions are stored as JSONL under `~/.local/share/bitchtea/sessions/`.
- Use `--resume` or `/sessions` to continue prior work.
- Use `/fork` when you want to branch from the current conversation state.
- `AGENTS.md` and `CLAUDE.md` are discovered upward from the working directory and injected as context.
- `@file` references inline file contents into your prompt.

## Themes

Built-in themes:

- `bitchx`
- `nord`
- `dracula`
- `gruvbox`
- `monokai`

## Building From Source

```bash
go build -o bitchtea .
./bitchtea --help
```

Contributor and architecture notes live in [CONTRIBUTING.md](CONTRIBUTING.md).

## Heritage

Named after [BitchX](https://en.wikipedia.org/wiki/BitchX), the IRC client that did not care about your feelings. The ANSI splash art keeps the tone honest.
