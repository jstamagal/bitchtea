# BITCHTEA: GETTING STARTED

Welcome to the canopy. Stop being a wimp and build the tool.

## Prerequisites

- **Go 1.26+** (see `go.mod` for the exact toolchain version)
- A terminal emulator with true-color and Unicode support
- An API key for at least one supported provider

## Install

**Via go install:**

```bash
go install github.com/jstamagal/bitchtea@latest
```

**Or build from source:**

```bash
git clone ssh://git@jelly.hedgehog-bortle.ts.net:2222/jstamagal/bitchtea.git
cd bitchtea
go build -o bitchtea .
```

## Provider Setup

Bitchtea auto-detects your provider from environment variables. Set at least one of the following:

| Provider | Environment variable |
|---|---|
| Anthropic | `ANTHROPIC_API_KEY` |
| OpenAI | `OPENAI_API_KEY` |
| OpenRouter | `OPENROUTER_API_KEY` |
| Z.ai | `ZAI_API_KEY` |

For OpenAI-compatible endpoints set `OPENAI_BASE_URL` as well:

```bash
export OPENAI_API_KEY="sk-..."
export OPENAI_BASE_URL="https://your-provider.example/v1"
```

Built-in profiles are available via `--profile`:

- `ollama` — targets `http://localhost:11434/v1` (no API key required)
- `openrouter` — reads `OPENROUTER_API_KEY`
- `zai-openai` — reads `ZAI_API_KEY`, points at Coding Plan endpoint
- `zai-anthropic` — reads `ZAI_API_KEY`, points at Anthropic-compatible endpoint

## First Run

```bash
./bitchtea
```

The TUI boots into a split-pane layout: a scrolling chat area on the left and a tool-activity panel on the right. Type a message and press **Enter** to start.

Try sending your first prompt:

```
hello, what files do you see in this project? @README.md
```

The `@file` syntax inlines the contents of a file into your prompt so the model can read it.

## Key Commands

| Command | Action |
|---|---|
| `/help` | Show help text |
| `/set <key> [value]` | Show or change settings (sound, model, provider, etc.) |
| `/quit` | Exit bitchtea |
| `/clear` | Clear the chat display |
| `/restart` | Reset the conversation without quitting |
| `/compact` | Compact conversation context to save tokens |
| `/sessions` | List saved sessions |
| `/fork` | Branch from the current session |

**Keyboard shortcuts:**

| Key | Action |
|---|---|
| `Enter` | Send message |
| `Ctrl+C` | Interrupt model output or quit |
| `Ctrl+Z` | Suspend the process |
| `Up/Down` | Cycle through input history |
| `PgUp/PgDn` | Scroll the chat viewport |
| `Tab` | Autocomplete commands and `@file` references |

## State and Data Locations

Data is stored under `~/.bitchtea/`:

- `~/.bitchtea/sessions/` — JSONL session files (one per conversation)
- `~/.bitchtea/memory/` — daily memory files for compaction
- `~/.bitchtea/catalog/` — cached model catalog
- `~/.bitchtea/mp3/` — music files for the MP3 player

Per-workspace memory is stored in a gitignored `MEMORY.md` file at the workspace root.

The startup config lives at `~/.bitchtearc` — see [commands.md](commands.md) for the RC file format.
