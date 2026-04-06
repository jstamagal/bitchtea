### Attention Agent
**For the love of God organize your directory**
**Create some better docs**
**Keep up the rich git history**

# bitchtea

An agentic coding harness that puts the BITCH back in your terminal. BitchX-inspired TUI for LLM-powered coding sessions.

## Architecture

```
bitchtea/
├── main.go                     # CLI entry point, flag parsing
├── internal/
│   ├── ui/                     # bubbletea TUI layer
│   │   ├── model.go            # top-level bubbletea Model (Update/View/Init)
│   │   ├── message.go          # ChatMessage types and formatting
│   │   ├── styles.go           # lipgloss color palette and styles
│   │   ├── art.go              # ANSI art splash screens (6 variants)
│   │   ├── render.go           # glamour markdown rendering, word wrap
│   │   ├── toolpanel.go        # collapsible tool sidebar component
│   │   ├── message_test.go
│   │   ├── render_test.go
│   │   └── toolpanel_test.go
│   ├── agent/                  # agent loop and context management
│   │   ├── agent.go            # SendMessage loop, tool dispatch, auto-next
│   │   ├── context.go          # AGENTS.md discovery, MEMORY.md, @file expansion
│   │   └── context_test.go
│   ├── llm/                    # LLM API clients
│   │   ├── client.go           # OpenAI-compatible streaming (SSE)
│   │   ├── anthropic.go        # Native Anthropic Messages API streaming
│   │   ├── client_test.go
│   │   └── anthropic_test.go
│   ├── session/                # JSONL session persistence
│   │   ├── session.go          # New/Load/Append/Fork/Tree/List
│   │   └── session_test.go
│   ├── tools/                  # tool implementations
│   │   ├── tools.go            # read, write, edit, bash
│   │   └── tools_test.go
│   └── config/                 # configuration and profiles
│       ├── config.go           # Config struct, profiles, env detection
│       └── config_test.go
├── go.mod
└── go.sum
```

## Features

### Dual Provider Support
- **OpenAI** — OpenAI-compatible API (GPT-4o, etc). Set `OPENAI_API_KEY`.
- **Anthropic** — Native Messages API streaming. Set `ANTHROPIC_API_KEY`.
- Switch at runtime with `/provider` and `/model`.
- Profiles let you save/load provider+model+key combos.

### UI
- BitchX-style top/bottom bars with model, provider, token count, elapsed time
- Scrollable viewport with mouse wheel support
- **Markdown rendering** via glamour (code blocks, bold, lists, headings)
- **Word wrapping** for long lines
- **Multi-line input** via textarea (Enter sends, Ctrl+P/N for history)
- **Tool panel** — collapsible sidebar showing tool call stats and recent activity (Ctrl+T to toggle)
- 6 randomized ANSI art splash screens
- Queue indicator when steering messages are pending

### Agent Loop
- Streams LLM responses token by token into the viewport
- Detects tool calls, executes them, feeds results back, loops
- Tools: `read`, `write`, `edit`, `bash` (same as Claude Code)
- Auto-next-steps: keeps the agent working after each turn
- Auto-next-idea: brainstorms improvements after auto-next completes
- Context compaction when approaching token limits

### Session Management
- JSONL session files with tree structure
- `/resume` to pick up old sessions
- `/fork` to branch from any point
- `/tree` to visualize session structure
- `/sessions` to list all saved sessions

### Context
- Auto-discovers `AGENTS.md`, `CLAUDE.md` walking up the directory tree
- Loads `MEMORY.md` from working directory
- Auto-saves `MEMORY.md` with session summaries every 3 turns
- `@file` references expand to file contents in messages

### Steering
Type while the agent is working. Messages get queued and injected after the current turn completes. Like IRC — the channel keeps going but you can jump in.

## Usage

```bash
# Basic
export OPENAI_API_KEY=sk-...
./bitchtea

# With Anthropic
export ANTHROPIC_API_KEY=sk-ant-...
./bitchtea

# Flags
./bitchtea --model claude-sonnet-4-20250514
./bitchtea --resume                    # resume latest session
./bitchtea --resume path/to/session.jsonl
./bitchtea --profile myprofile
./bitchtea --auto-next-steps
./bitchtea --auto-next-idea
```

## Commands

| Command | Description |
|---------|-------------|
| `/model <name>` | Switch LLM model |
| `/provider <name>` | Set provider (openai, anthropic) |
| `/baseurl <url>` | Set API base URL |
| `/apikey <key>` | Set API key |
| `/profile save/load/delete <name>` | Manage connection profiles |
| `/compact` | Compact conversation context |
| `/clear` | Clear chat display |
| `/diff` | Show git diff |
| `/status` | Git status |
| `/undo` | Revert unstaged changes |
| `/commit [msg]` | Git commit |
| `/tokens` | Token usage estimate |
| `/memory` | Show MEMORY.md contents |
| `/sessions` | List saved sessions |
| `/tree` | Show session tree |
| `/fork` | Fork session from current point |
| `/auto-next` | Toggle auto-next-steps |
| `/auto-idea` | Toggle auto-next-idea |
| `/help` | Show help |
| `/quit` | Exit |

## Keybindings

| Key | Action |
|-----|--------|
| `Enter` | Send message |
| `Ctrl+C` | Interrupt agent / Quit |
| `Ctrl+P` | Input history up |
| `Ctrl+N` | Input history down |
| `Ctrl+T` | Toggle tool panel |
| `PgUp/PgDn` | Scroll viewport |
| Mouse wheel | Scroll viewport |
| `Tab` | Complete slash commands and @file references |

## Tech Stack

- [bubbletea](https://github.com/charmbracelet/bubbletea) — Elm-architecture TUI framework
- [lipgloss](https://github.com/charmbracelet/lipgloss) — Terminal styling
- [bubbles](https://github.com/charmbracelet/bubbles) — Pre-built components (viewport, textarea, spinner)
- [glamour](https://github.com/charmbracelet/glamour) — Markdown rendering

## UI Layout

```
┌─ bitchtea — openai/gpt-4o [auto] ──────────[3:42pm]─┐
│──────────────────────────────────────────────────────│
│ [11:36] <jstamagal> how to fix this npm error        │
│ [11:37] <bitchtea> Let me look at that...            │
│ [11:37]   → read: reading package.json               │
│ [11:38] <bitchtea> Found the issue. Here's the fix:  │
│         ```json                                      │
│         { "dependencies": { ... } }             ╭────│
│         ```                                     │Tool│
│ [11:38]   → edit: Applied 1 edit(s)             │ ───│
│ [11:39] <bitchtea> Fixed. Running tests now...  │read│
│ [11:39]   → bash: npm test                      │edit│
│                                                 │bash│
│──────────────────────────────────────────────────╰────│
│ [bitchtea] ◉ running tools...   read(3) bash(2) | ~4k│
│──────────────────────────────────────────────────────│
│ >> fix the npm dependency issue and run tests_       │
│                                                      │
└──────────────────────────────────────────────────────┘
```

## References
- https://en.wikipedia.org/wiki/BitchX
- UI Screenshots: ~/Pictures/bx
- Charmbracelet repos: ./charmbracelet/repos
- Modern BitchX port: ./prime001/BitchX

## What's Next

- [x] Tab completion for slash commands and @file references
- [x] Retry with exponential backoff on rate limits
- [x] Cost tracking per provider/model
- [x] Theme system (color schemes)
- [x] Notification sounds on completion
- [ ] VHS tape recordings for GIF demos
- [ ] Multi-agent dispatch via acpx
