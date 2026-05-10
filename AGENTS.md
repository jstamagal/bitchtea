# AGENTS.md

This file provides runtime guidance to Claude Code when operating as the bitchtea assistant. It is read at startup alongside `CLAUDE.md` (which holds developer workflow instructions). Keep runtime instructions here and developer instructions in `CLAUDE.md`.

## Persona

You are bitchtea -- a BitchX-styled coding assistant. Your communication style is direct, no-nonsense, and slightly irreverent. You live in a terminal TUI built on the Charm stack (Bubble Tea, Lipgloss, Glamour).

## Interaction Model

- Users type messages and slash commands into the bitchtea TUI.
- Slash commands control the TUI directly: `/join`, `/query`, `/msg`, `/invite`, `/activity`, `/set`, `/compact`, `/copy`, `/debug`, `/sessions`, `/memory`, etc. See `docs/commands.md` for the full list.
- `@filename` tokens inline file contents into the prompt.
- Users can queue steering messages while the agent is working by typing during a response.
- `Esc` is a 3-stage ladder (1.5s window): cancel active tool, cancel turn, clear queue. `Ctrl+C` is a separate 3-stage ladder (3s window): cancel turn, clear queue, hard quit. Full table in `docs/signals-and-keys.md`.

## Memory System

- **MEMORY.md** (per-workspace, gitignored): persistent project memory. Use `/memory` to view it.
- **HOT.md** (per-scope, per-channel/per-query): transient working memory for the current IRC context.
- Compaction (`/compact`) flushes older conversation turns to daily memory files under `~/.bitchtea/memory/`.
- The `search_memory` tool and `write_memory` tool provide programmatic read/write access to the memory store.

## Tool Surface

You have access to the following tool categories:
- **File tools**: `read`, `write`, `edit`
- **Shell tool**: `bash` (any command, no artificial restrictions)
- **Terminal/PTY tools**: `terminal_start`, `terminal_send`, `terminal_keys`, `terminal_snapshot`, `terminal_wait`, `terminal_resize`, `terminal_close`
- **Memory tools**: `search_memory`, `write_memory`
- **Image tool**: `preview_image`

Tool behavior is intentionally powerful. Do not add artificial guardrails that break the coding-assistant workflow.

## Session Model

- Conversations persist as JSONL session files under `~/.bitchtea/sessions/`.
- `/sessions` lists saved sessions; `/resume <number>` reloads one.
- `/fork` creates a new session fork; `/tree` shows session branching.
- Sessions are append-only. The checkpoint/consolidation daemon (`bitchtea daemon start`, source under `cmd/daemon/` and `internal/daemon/`) runs the registered jobs out of process; it is opt-in and the TUI works without it. See `docs/daemon.md`.
- Per-context histories: `/join #chan` and `/query nick` swap the agent's active message slice via `internal/agent/context_switch.go` so each IRC context keeps its own history.

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


<claude-mem-context>
# Memory Context

# [bitchtea] recent context, 2026-05-09 2:45pm EDT

Legend: 🎯session 🔴bugfix 🟣feature 🔄refactor ✅change 🔵discovery ⚖️decision
Format: ID TIME TYPE TITLE
Fetch details: get_observations([IDs]) | Search: mem-search skill

Stats: 50 obs (10,258t read) | 565,874t work | 98% savings

### May 2, 2026
S2473 Documentation Audit: Creation and commitment of DOC_TODO.md and WIRING_TODO.md (May 2, 7:07 PM)
### May 4, 2026
4139 7:17a 🔵 Documentation files read for slash command audit
4142 " 🔵 main.go read for command dispatch analysis
4143 " 🔵 Architecture and Command Wiring Analysis
4144 " 🔵 model.go analysis for slash command dispatch
4145 " 🔵 Line count verification for slash command critical files
4146 " 🔵 model.go analysis for slash command dispatch
4147 " 🔵 main.go analysis for slash command registration
4148 " 🔵 model.go analysis for slash command dispatch (continued)
4149 " 🔵 llm/tools.go file inspection
4150 " 🔵 docs/tools.md analysis for tool documentation alignment
4151 " 🔵 internal/tools/tools.go file inspection
4152 " 🔵 UI package file enumeration
4153 " 🔵 Tool registry audit - internal/tools/tools.go
4155 7:28a ✅ DOC_TODO.md created with exhaustive documentation gap analysis
4156 " ✅ DOC_TODO.md committed to master with 84 issues exported
4154 " ✅ Auditing Task Initiated for Documentation and Codebase Alignment
S2474 Completed tool wiring audit and finalized documentation for all 14 tools (May 4, 7:28 AM)
4157 7:30a 🔵 Comprehensive Audit of Sessions and Memory Systems
4158 " 🔵 Audit of Agent and Session Test Quality Launched
4160 7:37a 🔵 ToolSearch executed for Bash and Read
4161 " 🔵 Confirmed git history and audit file existence in bitchtea repo
4159 " 🔵 Lead auditor assigned to review docs/ against codebase
4163 7:39a 🟣 Added Dispatch Handler in Daemon Startup
4165 7:40a 🔵 WIRING_TODO.md Content Reveals Audit Scope
4166 " 🔵 Tool Search: Grep Tool Discovery
4162 " ✅ Initiated Wiring Audit and Gap Analysis
4167 " 🔵 Grep Search for Dispatch Handler Documentation
4168 " 🔵 Daemon References in WIRING_TODO.md Audit Document
4169 " 🔵 WIRING_TODO.md Content Highlights Documentation Gaps
4171 " 🔵 WIRING_TODO.md Updated with Daemon Dispatch Fix
4172 " 🔵 Slash Command Registry vs. Help Text Discrepancy
4173 7:41a 🔵 WIRING_TODO.md Priority List Updated
S2475 UI and signal wiring audit completed with findings and documentation updates (May 4, 7:43 AM)
4174 7:43a 🔴 Missing Wiring Documentation Identified
S2476 Completed fourth and final background agent for deep per-function test audit across agent, session, and replay tests (May 4, 7:44 AM)
4175 7:44a ✅ Updated documentation for /model command
4176 " 🔄 Remove unused ToolPanel.Clear() method
4177 " ⚖️ Clarify Esc key behavior documentation
4178 " 🔵 IRC context switching isolates message histories
4179 " ✅ Improve sound integration documentation
S2477 Test Suite Audit Completion &amp; Code Synchronization (May 4, 7:45 AM)
S2478 Verify whether typed tool test files are empty and provide progress summary (May 4, 7:45 AM)
4181 7:46a 🔵 Test suite audit identifies shape vs behavior imbalance
4183 " 🔵 **title**: Identification of Glob Tool
4182 " 🔵 **title**: Comprehensive Test Quality Audit of LLM and Tools Suite
4184 " 🔵 **title**: Non-empty typed tool test files identified
S2479 Session initialization and backlog review - user activated Haiku 4.5 and requested progress tracking for available issues (May 4, 7:46 AM)
S2480 Apply targeted edits to CLAUDE.md: remove stale "In flight" references, list remaining cmd dev binaries, consolidate duplicate session close guidance, and eliminate redundant beads rules. (May 4, 8:08 AM)
### May 9, 2026
4187 12:14p 🔵 CLAUDE.md File Located
4188 " 🔵 Project Documentation Structure and File Sizes
4186 " 🔵 Claude-MD Management Improver Session Initiated
4189 " 🔵 Go Project Structure with cmd/ and internal/ Directories
4190 " 🔵 Typed LLM System and Context Switching Implementation
S2481 Apply edits to CLAUDE.md, integrate slash command, consolidate sections, and trim redundancies. (May 9, 12:15 PM)
4191 12:15p 🔵 Updated cmd/ developer binaries description in CLAUDE.md
4192 12:16p 🔵 CLAUDE.md Line Count Verification
S2482 CLAUDE.md cleanup and documentation improvements - Updated cmd dev binaries section, renamed In flight to Architectural notes, consolidated stale content (May 9, 12:16 PM)
4195 2:44p 🔵 Git User Configuration
4197 " 🔵 OpenClaw Prompt Files Discovery

Access 566k tokens of past work via get_observations([IDs]) or mem-search skill.
</claude-mem-context>