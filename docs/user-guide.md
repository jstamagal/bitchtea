# 🦍 BITCHTEA USER GUIDE

Welcome to bitchtea — a BitchX-styled TUI coding assistant. This guide covers the
full feature set from a user's perspective. For command reference see
[commands.md](commands.md), and for deep-dives follow the cross-links.

## Quick Start

```bash
# Set your API key
export ANTHROPIC_API_KEY="sk-ant-..."
# Or: export OPENAI_API_KEY="sk-..."

# Run
go build -o bitchtea . && ./bitchtea
```

Type a message and press **Enter** to start a conversation. Use `@file` to inline
file contents into your prompt:

```
what files are here? @README.md
```

For full setup see [getting-started.md](getting-started.md).

## Slash Commands

All slash commands are documented in [commands.md](commands.md). Here are the
main categories:

### Context & Navigation

| Command | Action |
|---------|--------|
| `/join #channel` | Switch to or create a channel context |
| `/query <nick>` | Open a direct-message conversation with a persona |
| `/part [#channel]` | Leave the current or named context |
| `/channels` (or `/ch`) | List all open contexts |
| `/msg <nick> <text>` | Send a one-shot message without changing focus |

### Configuration

| Command | Action |
|---------|--------|
| `/set <key> [value]` | Show or change settings |
| `/profile [load\|save\|show\|delete] <name>` | Manage connection profiles |
| `/models` | Open a fuzzy-find model picker |

### Session & Memory

| Command | Action |
|---------|--------|
| `/sessions` (or `/ls`) | List saved sessions |
| `/fork` | Branch a new session from current state |
| `/compact` | Summarize history to save tokens |
| `/memory` | View MEMORY.md and scoped HOT.md contents |
| `/tokens` | Show estimated token usage and cost |
| `/tree` | Show session branch structure |

### Utilities

| Command | Action |
|---------|--------|
| `/mp3 [cmd]` | Control the built-in MP3 player |
| `/debug [on\|off]` | Toggle verbose HTTP logging |
| `/copy [n]` | Copy the nth assistant response to clipboard |
| `/activity [clear]` | View daemon background activity |
| `/clear` | Clear scrollback |
| `/help` (or `/h`) | Show quick help |
| `/quit` (or `/q`) | Exit |

For the complete `/set` key reference see [commands.md#set-key-reference](commands.md).

## @file Token Expansion

Typing `@filename` in your message inlines the file's contents into your prompt.
The file path is resolved relative to the working directory. This is useful for
providing context from source files, logs, or configuration:

```
Explain this error: @logs/server.log
Refactor this function: @internal/worker/processor.go
```

Tab completion works for `@file` references — press **Tab** to autocomplete paths.

## Context System (Channels & Queries)

bitchtea organizes conversations into **contexts**, similar to IRC channels and
direct messages:

- **Channel** (`#name`) — a named topic context. Switch with `/join #dev`.
- **Query** (`@persona`) — a direct conversation with a persona. Open with
  `/query alice`.
- **Root** — the default context, used when no channel or query is active.

Each context has its own message history and memory scope. Switching contexts
preserves the history of the previous context — you can `/join #docs` to work
on documentation, then `/join #backend` to continue backend work without losing
either thread.

See [commands.md](commands.md) for detailed command traces, and
[memory.md](memory.md) for how memory scopes work per context.

## Memory Workflow

bitchtea has a two-tier memory system:

### Memory Files

| Scope | Hot file (always injected) | Daily archive (searchable) |
|-------|---------------------------|---------------------------|
| Root | `<workdir>/MEMORY.md` | `~/.bitchtea/memory/<ws>/<date>.md` |
| Channel | `.../HOT.md` | `.../daily/<date>.md` |
| Query | `.../HOT.md` | `.../daily/<date>.md` |

Hot memory is loaded into the LLM context at the start of each turn in that
scope. Daily archives accumulate compaction flushes and tool writes and are
retrievable via search.

### Memory Tools

The LLM has two tools for working with memory:

- **`search_memory`** — keyword search across hot and daily files for the active
  scope and its parents. The model is prompted to call this before substantive
  work or when prior context matters.
- **`write_memory`** — persist a note into hot memory (durable knowledge) or
  the daily archive (ephemeral session notes). Supports scope overrides to write
  into a different channel or query.

### `/memory` Command

Shows the current MEMORY.md and scoped HOT.md contents in the viewport. This is
read-only — the output is not sent to the LLM.

See [memory.md](memory.md) for the full reference including compaction, daemon
consolidation, and scope semantics.

## Profiles

Profiles bundle provider, model, base URL, and API key into a named, repeatable
configuration. bitchtea ships 15 built-in profiles and supports saved custom
profiles.

### Built-in Profiles

Use `--profile <name>` on the command line or `/profile load <name>` at runtime:

| Profile | Wire Format | Service |
|---------|-------------|---------|
| `ollama` | OpenAI | ollama (local, no API key required) |
| `openrouter` | OpenAI | openrouter |
| `zai-openai` | OpenAI | zai-openai |
| `zai-anthropic` | Anthropic | zai-anthropic |
| `aihubmix` | OpenAI | aihubmix |
| ... and 10 more | | |

### Custom Profiles

Save your current connection settings as a named profile:

```
/profile save my-config
```

Profiles are stored as JSON files under `~/.bitchtea/profiles/`.

### Manual Overrides

Setting a connection parameter directly (e.g. `/set model gpt-4o`) clears the
active profile tag. The settings persist but are no longer associated with a
profile.

See [providers.md](providers.md) for full provider routing, detection, and
profile reference.

## Startup Configuration

bitchtea reads `~/.bitchtea/bitchtearc` on startup to set defaults and run
initial commands. Lines starting with `#` are comments; blank lines are ignored.

```
# bitchtearc — startup commands
set provider anthropic
set model claude-sonnet-4-20250514
join #dev
query alice
```

### `set` Lines

| Key | Value | Effect |
|-----|-------|--------|
| `provider` | `openai` or `anthropic` | Set LLM provider |
| `model` | model name | Set model |
| `apikey` | API key | Set API key |
| `baseurl` | URL | Set API base URL |
| `nick` | name | Set user nick |
| `profile` | profile name | Load a profile |
| `sound` | `on`/`off` | Toggle notification sounds |
| `auto-next` | `on`/`off` | Toggle auto next-step prompts |
| `auto-idea` | `on`/`off` | Toggle auto improvement ideas |

### Non-set Lines

Any other line is treated as a slash command without the leading `/`. These are
executed silently after startup:

```
join #code
query buddy
```

### Startup Ordering

1. RC file is read from `~/.bitchtea/bitchtearc`
2. `--profile` flag is applied (overrides RC)
3. CLI flags (`--model`, `-m`, etc.) are applied (override everything)
4. Session restore (if `--resume`)
5. RC non-set commands are executed silently

## Headless Mode

Run bitchtea without the TUI for scripting and pipes:

```bash
bitchtea --headless --prompt "list all Go files"
echo "check the build" | bitchtea --headless
bitchtea --headless --resume latest --prompt "continue where we left off"
```

**stdout** — model text output only.
**stderr** — structured tool and status events (`[tool]`, `[status]`, `[auto]`).

Headless mode supports follow-up loops: after a turn completes, any queued
auto-next or auto-idea prompts are processed automatically.

See [cli-flags.md](cli-flags.md) for all available flags.

## Mid-Turn Steering (Queue)

While the LLM is generating a response, you can type additional messages. They
are **queued** and processed when the current turn finishes:

- Type a message while the agent is busy → it's queued with a
  "Queued message (agent is busy): ..." confirmation in the viewport.
- When the turn finishes, queued messages are sent to the LLM in order.
- Messages older than 2 minutes are considered stale and are not automatically
  drained (you can re-send them).

This allows you to steer the conversation without waiting for the current turn
to complete — type corrections, follow-ups, or clarifications ahead of time.

## Autonomous Modes

bitchtea can automatically generate follow-up prompts after each turn:

- **`/set auto-next on`** — after each turn, the LLM is prompted to suggest
  "next steps" and briefly explain why. This keeps the conversation moving
  forward without manual input.
- **`/set auto-idea on`** — after each turn, the LLM generates an improvement
  idea, useful for code review or document editing sessions.

Both can be enabled independently. When the agent finishes a turn and one of
these is active, a follow-up prompt is queued automatically. In headless mode
these follow-ups are processed until no more remain.

## MP3 Player

bitchtea includes a built-in MP3 player for background music:

```
/mp3           Toggle the MP3 panel
/mp3 rescan    Scan ~/.bitchtea/mp3/ for music files
/mp3 play      Play the current track
/mp3 pause     Toggle pause
/mp3 next      Skip to next track
/mp3 prev      Go to previous track
```

MP3 files go in `~/.bitchtea/mp3/`. See [ui-components.md](ui-components.md) for
the player UI details.

## Esc / Ctrl+C Ladder

bitchtea uses a progressive Esc ladder for safe cancellation:

| Stage | When | Action |
|-------|------|--------|
| 1 | Streaming, no active tool | "Press Esc again to cancel the turn." |
| 2 | Streaming | Cancels the turn (queued messages preserved) |
| — | Not streaming | No effect (resets the ladder) |

- **Ctrl+C** during streaming cancels the entire turn immediately (equivalent to
  Esc stage 2 in one press).
- **Ctrl+C** while idle quits the application.
- While a tool is actively running, Esc cancels just that tool first; the next
  Esc cancels the turn.

See [signals-and-keys.md](signals-and-keys.md) for the full ladder specification.

## MCP Integration

bitchtea supports the Model Context Protocol (MCP) for connecting external
tool servers. Configure MCP servers via `<workdir>/.bitchtea/mcp.json`:

```json
{
  "enabled": true,
  "servers": {
    "my-server": {
      "command": "npx",
      "args": ["-y", "@my/mcp-server"],
      "env": {
        "MY_API_KEY": "sk-..."
      }
    }
  }
}
```

MCP tools are namespaced as `mcp__<server>__<tool>` in the tool registry.
Local built-in tools take precedence over MCP tools with the same name.

See [mcp.md](mcp.md) for the full configuration reference.

## Sound System

Bitchtea can play terminal bell sounds at the end of an agent turn:

```
/set sound on    Enable notification sounds
/set sound off   Disable
```

When enabled, one BEL character is sent to the terminal after each completed
agent turn (three BELs on errors). Whether this produces an audible beep or
visual flash depends on your terminal emulator's bell configuration.

## Data Locations

All data is stored under `~/.bitchtea/`:

| Path | Contents |
|------|----------|
| `~/.bitchtea/sessions/` | JSONL session files (one per conversation) |
| `~/.bitchtea/memory/` | Daily memory archives (scoped per workspace) |
| `~/.bitchtea/catalog/` | Cached model catalog |
| `~/.bitchtea/mp3/` | MP3 music files |
| `~/.bitchtea/profiles/` | Saved connection profiles |
| `~/.bitchtea/bitchtearc` | Startup command file |
| `<workdir>/MEMORY.md` | Per-workspace root hot memory (gitignored) |
| `<workdir>/.bitchtea/mcp.json` | MCP server configuration |

🦍💪🤝 APES STRONK TOGETHER 🦍💪🤝
