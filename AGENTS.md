# Agent Instructions

This project uses **bd** (beads) for issue tracking. Run `bd prime` for full workflow context.

## Quick Reference

```bash
bd ready              # Find available work
bd show <id>          # View issue details
bd update <id> --claim  # Claim work atomically
bd close <id>         # Complete work
bd dolt push          # Push beads data to remote
```

## Non-Interactive Shell Commands

**ALWAYS use non-interactive flags** with file operations to avoid hanging on confirmation prompts.

```bash
cp -f source dest           # NOT: cp source dest
mv -f source dest           # NOT: mv source dest
rm -f file                  # NOT: rm file
rm -rf directory            # NOT: rm -r directory
```

## Project: bitchtea

**What it is:** A TUI-based agentic coding assistant in Go. Connects to OpenAI/Anthropic APIs, streams responses, executes tools (read/write/edit/bash), persists sessions as JSONL.

**Built with:** Go 1.24, charmbracelet stack (bubbletea, bubbles, glamour, lipgloss).

### Build & Test

```bash
go build ./...              # Build — must pass before committing
go test ./...               # Run all tests
go test -race ./...         # Race detector — should be clean
go vet ./...                # Static analysis — must pass before committing
```

All four must pass. If you change code, run all four before closing an issue.

### Module Map

```
main.go                     Entry point, CLI flags, TUI bootstrap
internal/
  config/config.go          Config struct, env detection, profiles
  agent/
    agent.go                Agent loop: LLM ↔ tool orchestration
    context.go              Context file discovery, @file expansion, MEMORY.md
  llm/
    client.go               OpenAI streaming client, types (Message, ToolCall, StreamEvent)
    anthropic.go            Anthropic Messages API adapter
    retry.go                Exponential backoff with jitter
    cost.go                 Model pricing, CostTracker
  session/session.go        JSONL session persistence, fork, tree
  tools/tools.go            Tool registry: read, write, edit, bash
  ui/
    model.go                Bubbletea Model — the big file. Update(), View(), slash commands
    message.go              ChatMessage types and Format()
    render.go               Markdown rendering, text wrapping
    styles.go               Lipgloss style definitions
    themes.go               Theme system (bitchx, nord, dracula, gruvbox, monokai)
    toolpanel.go            Collapsible tool sidebar
    art.go                  ANSI splash art
  sound/sound.go            Terminal bell notifications
```

### Dependency Flow (no circular deps)

```
main → config, session, ui
ui   → agent, config, session, sound
agent → config, llm, tools
session → llm (for Message/ToolCall types)
tools → llm (for ToolDef type)
llm, config, sound → (no internal deps)
```

### Key Patterns

- **Agent loop** (`agent.go:SendMessage`): Sends user message → streams LLM response → if tool calls, execute them and loop back for next LLM response. Events flow to UI via channel.
- **Fake streamer for testing** (`agent_loop_test.go`): `fakeStreamer` implements `llm.ChatStreamer` interface. Use `NewAgentWithStreamer(cfg, streamer)` for offline tests.
- **Session persistence**: JSONL files in `~/.local/share/bitchtea/sessions/`. Each entry has timestamp, role, content, tool metadata, parent ID for tree structure.
- **Bubbletea architecture**: `Model.Update()` receives messages, returns commands. `Model.View()` renders. State flows through the `tea.Model` interface. Never block in Update.

### What NOT to Do

- Don't add Python. TypeScript or Go only.
- Don't put files in `/tmp` — use `t.TempDir()` in tests.
- Don't modify files outside your issue's scope. Each issue lists its files.
- Don't add guardrails to the bash tool. Full system access is intentional.
- Don't create MEMORY.md files. Use `bd remember` for persistent knowledge.

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
