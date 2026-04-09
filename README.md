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

For non-interactive runs:

```bash
bitchtea --headless --prompt "summarize this repo"
git diff --stat | bitchtea --headless --prompt "review these changes"
```

`--headless` runs a single prompt without starting the TUI. It requires `--prompt`, piped stdin, or both. If both are present, bitchtea concatenates them before sending the request.

Built-in provider profiles are available too:

```bash
bitchtea --profile ollama
bitchtea --profile openrouter
bitchtea --profile zai-openai
bitchtea --profile zai-anthropic
```

`ollama` targets `http://localhost:11434/v1` with no API key. `openrouter` reads `OPENROUTER_API_KEY`. The `zai-*` profiles read `ZAI_API_KEY`, with `zai-openai` pointed at the Coding Plan endpoint `https://api.z.ai/api/coding/paas/v4`.

## Usage

```text
bitchtea [flags]

Flags:
  -m, --model <name>     Model to use
  -p, --profile <name>   Load a saved or built-in profile
  -r, --resume [path]    Resume a session (latest if no path)
  -H, --headless         Run once without the TUI
  --prompt <text>        Prompt to send in headless mode
  --auto-next-steps      Keep the agent working after each turn
  --auto-next-idea       Brainstorm improvements after auto-next completes
  -h, --help             Show help
```

Environment:

```text
OPENAI_API_KEY         OpenAI API key
OPENAI_BASE_URL        OpenAI-compatible base URL
ANTHROPIC_API_KEY      Anthropic API key
OPENROUTER_API_KEY     OpenRouter API key for the openrouter profile
ZAI_API_KEY            Z.ai API key for the zai-* profiles
BITCHTEA_MODEL         Default model override
BITCHTEA_PROVIDER      Force provider (openai, anthropic)
```

## What You Get

- Natural-language coding workflow with tool execution for reading files, editing code, writing files, and running shell commands.
- Streaming responses in a single scrolling terminal buffer while tool activity stays visible in the side panel.
- Live steering: you can type while the agent is still working and your next message gets queued for the next turn.
- Session persistence with resume and fork support.
- Runtime switches for provider, model, profiles, and sound without leaving the UI.

## Commands

| Command | Behavior | Risk profile |
|---|---|---|
| `/model <name>` | Switch model | Safe |
| `/provider <name>` | Switch provider | Safe |
| `/baseurl <url>` | Set API base URL | Safe |
| `/apikey <key>` | Set API key | Safe |
| `/profile save\|load\|delete <name>` | Manage saved profiles and load built-ins like `ollama`, `openrouter`, `zai-openai`, and `zai-anthropic` | Safe |
| `/compact` | Compact conversation context | Moderate: rewrites in-memory context |
| `/clear` | Clear chat display | Safe |
| `/diff` | Show git diff | Safe |
| `/status` | Show git status | Safe |
| `/undo` | Preview tracked-file revert; use `/undo confirm` to restore all unstaged tracked files or `/undo <file>` to restore one tracked file | Destructive after preview |
| `/commit [msg]` | Preview git state with no message, or commit tracked changes only with `git add -u` when a message is provided | Persistent: writes a git commit |
| `/copy` | Copy the last assistant message | Safe |
| `/tokens` | Show token usage estimate | Safe |
| `/memory` | Show `MEMORY.md` from the current workspace | Safe |
| `/sessions` | List saved sessions | Safe |
| `/tree` | Show session tree | Safe |
| `/fork` | Fork the current session | Safe |
| `/auto-next` | Toggle auto-next-steps | Moderate: changes turn behavior |
| `/auto-idea` | Toggle auto-next-idea | Moderate: changes turn behavior |
| `/sound` | Toggle completion bell | Safe |
| `/mp3 [rescan\|play\|pause\|next\|prev]` | Toggle the MP3 panel and control playback from `~/.bitchtea/mp3` | Safe |
| `/help` | Show help | Safe |
| `/quit` | Exit | Safe |

## Keybindings

| Key | Action |
|---|---|
| `Enter` | Send message |
| `Ctrl+C` | Interrupt or quit |
| `Ctrl+Z` | Suspend |
| `Ctrl+T` | Toggle tool panel |
| `Space` | Pause or resume music when the MP3 panel is open and the input is empty |
| `Left/Right` or `j/k` | Previous or next track when the MP3 panel is open and the input is empty |
| `Up/Down` | Input history |
| `PgUp/PgDn` | Scroll viewport |
| `Mouse wheel` | Scroll viewport |
| `Tab` | Complete commands and `@file` references |

## Sessions And Context

- Sessions are stored as JSONL under `~/.bitchtea/sessions/`.
- Use `--resume` or `/sessions` to continue prior work.
- Use `/fork` when you want to branch from the current conversation state.
- `AGENTS.md` and `CLAUDE.md` are discovered upward from the working directory and injected as context.
- `@file` references inline file contents into your prompt.

## Theme

The TUI currently ships with one built-in BitchX-style theme. Theme switching is intentionally disabled until the styling system is redesigned.

## Building From Source

```bash
go build -o bitchtea .
./bitchtea --help
```

Contributor and architecture notes live in [CONTRIBUTING.md](CONTRIBUTING.md).

## Heritage

Named after [BitchX](https://en.wikipedia.org/wiki/BitchX), the IRC client that did not care about your feelings. The ANSI splash art keeps the tone honest.
