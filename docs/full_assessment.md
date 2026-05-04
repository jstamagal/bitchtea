# Documentation Full Assessment

Comprehensive assessment of all documents in `docs/`, broken into discrete issues suitable for autonomous agent execution. Each issue is scoped to be completable in one session by a focused agent.

Assessed: 2026-05-04 against master at commit `66ad796`.

---

## Document Inventory

| File | Lines | Quality | Status |
|------|-------|---------|--------|
| `architecture.md` | 112 | Medium | Contains 2 WRONG claims, 4 GAPs |
| `development-guide.md` | 84 | Medium | Contains 2 WRONG claims, 4 GAPs |
| `agent-loop.md` | ~1000 | High | 6 GAPs, no wrong claims |
| `sessions.md` | ~1000 | High | 5 GAPs, self-aware of bugs |
| `memory.md` | ~1500 | High | 5 GAPs, corrects stale claims |
| `ui-components.md` | ~1000 | High | 7 GAPs, good self-documentation |
| `testing.md` | 234 | High | 5 minor GAPs |
| `tools.md` | 77 | Medium | 3 tools missing from table, 8 GAPs |
| `signals-and-keys.md` | 58 | Low | 2 WRONG, 8 GAPs, shallow |
| `cli-flags.md` | 60 | High | Complete for what it covers |
| `AUDIT_TODO.md` | 437 | Reference | Meta-audit, not user-facing |
| `TESTING_TODO.md` | ~300 | Reference | Meta-audit, not user-facing |

---

## PART 1: Fix Wrong/Stale Claims (Critical Priority)

These actively mislead agents and contributors. Fix before anything else.

### Issue 1: architecture.md — Remove phantom daemon claim

**File:** `docs/architecture.md` lines 33-36
**Problem:** States that CLAUDE.md claims the daemon is unimplemented. CLAUDE.md actually says the daemon was *rebuilt* and lists full file paths. This strawman confuses every agent that reads it.
**Fix:** Replace the "Phantom Daemon" bullet with a factual description of what `cmd/daemon/main.go` and `internal/daemon/` actually do today: session checkpoint and memory consolidation jobs, file-based IPC via mailbox, process locking.
**Verify:** Read `cmd/daemon/main.go`, `internal/daemon/run.go`, `internal/daemon/jobs/` to describe current behavior.

### Issue 2: architecture.md — Remove phantom write_memory claim

**File:** `docs/architecture.md` lines 36-37
**Problem:** Claims `write_memory` is unimplemented. It is live in `internal/llm/typed_write_memory.go` and the registry in `internal/tools/tools.go`. The `bt-vhs` issue is closed.
**Fix:** Strike the bullet entirely. Replace with a note that `write_memory` is a typed fantasy wrapper, same as the other 5 typed tools.
**Verify:** Confirm `write_memory` case exists in `tools.go` `Execute()` switch and `typed_write_memory.go` exists.

### Issue 3: architecture.md — Update dependency graph

**File:** `docs/architecture.md` lines 9-30
**Problem:** Graph omits `internal/mcp`, `internal/catalog`, `internal/daemon`, `internal/sound`, and `internal/agent/event`. The 2-level CLAUDE.md graph has all of these.
**Fix:** Replace the graph block with the full graph from CLAUDE.md (which is verified against imports). Add `cmd/daemon` and `cmd/trace` nodes.
**Verify:** Run `go list -deps ./...` or check `import` blocks in each package.

### Issue 4: development-guide.md — Remove both strawman claims

**File:** `docs/development-guide.md` lines 77-82
**Problem:** Same two false claims as architecture.md (daemon doesn't exist, write_memory doesn't exist).
**Fix:** Strike both bullets in section 5. Replace with accurate "current state" notes:
- Daemon: `cmd/daemon/main.go` exists, wired via `os.Args[1] == "daemon"` trap in `main.go`.
- `write_memory`: live, typed wrapper in `internal/llm/typed_write_memory.go`.

### Issue 5: development-guide.md — Fix "tools are still untyped" claim

**File:** `docs/development-guide.md` lines 43-45
**Problem:** States tools "are still untyped" and rely on `Registry.Execute(name, argsJSON)`. Six tools now have typed `fantasy.NewAgentTool` wrappers. Eight remain legacy.
**Fix:** Update section 3 to state: 6 typed (`read`, `write`, `edit`, `bash`, `search_memory`, `write_memory` in `internal/llm/typed_*.go`), 8 legacy (`terminal_*`, `preview_image` via `bitchteaTool` adapter). `translateTools` picks typed when available, falls back to generic.

### Issue 6: signals-and-keys.md — Re-derive from code

**File:** `docs/signals-and-keys.md` (entire file, 58 lines)
**Problem:** Multiple wrong/imprecise claims about Esc ladder behavior, Ctrl+C graduation, and missing coverage of tool cancellation propagation, picker key overrides, suspend lifecycle, and textarea shortcuts.
**Fix:** Complete rewrite. Source of truth is `internal/ui/model.go` functions `handleEscKey()`, `handleCtrlCKey()`, `cancelActiveTurn()`, `cancelActiveTurnWithQueueArm()`. Quote exact method names and exact output strings from `ui-components.md` (which is already verified).
**Sections needed:**
1. Signal table (SIGINT, SIGTERM, SIGWINCH, SIGTSTP)
2. Esc ladder (3 stages with exact messages, 1.5s window, panel-close priority)
3. Ctrl+C ladder (3 stages with exact messages, 3s window)
4. Tool cancellation chain (ToolContextManager -> executor ctx -> synthetic result)
5. Picker-mode key overrides (model picker, MP3 panel steal input)
6. Suspend/resume lifecycle (`tea.SuspendMsg`)
7. Textarea line-edit keys (whatever `textarea.Model` exposes)

---

## PART 2: Complete Existing Docs (High Priority)

These docs are good but have documented gaps that need filling.

### Issue 7: tools.md — Complete tool reference table

**File:** `docs/tools.md` lines 44-53
**Problem:** Table lists 11 tools but registry has 14. Missing: `terminal_snapshot`, `terminal_resize`, `terminal_close`.
**Fix:** Add missing rows. Add a "Typed Wrapper?" column showing which tools have `internal/llm/typed_*.go` wrappers vs legacy `bitchteaTool` adapter.
**Verify:** Check `internal/tools/tools.go` `Definitions()` for canonical list of 14.

### Issue 8: tools.md — Add MCP tools section

**File:** `docs/tools.md` (new section after line 77)
**Problem:** No mention of MCP-contributed tools at all.
**Fix:** Add section covering:
- Namespacing: `mcp__<server>__<tool>`
- Registration timing (startup via `internal/mcp/manager.go`)
- Collision handling: local tools win on name conflict
- First-wins dedup within a single MCP server
- Failure mode: server crash returns text error, not Go error
**Source:** `internal/llm/mcp_tools_test.go`, `internal/mcp/manager.go`

### Issue 9: tools.md — Add schema flattening section

**File:** `docs/tools.md` (new section)
**Problem:** `splitSchema`, `sanitizeProperties`, `parseRequired`, `filterRequired` in `internal/llm/tools.go` are load-bearing for provider compatibility but undocumented.
**Fix:** Add section explaining:
- Why flattening exists (Anthropic rejects nested `{type, properties, required}` wrappers)
- What `splitSchema` does (extracts properties map + required array from object schema)
- What `sanitizeProperties` does (removes invalid entries)
- What `filterRequired` does (drops required fields not in properties)
**Source:** `internal/llm/tools.go`, `internal/llm/tools_test.go`

### Issue 10: tools.md — Add ToolContextManager cancellation section

**File:** `docs/tools.md` (new section)
**Problem:** Per-tool cancellation is a key feature with no doc coverage.
**Fix:** Add section explaining:
- Each `streamOnce` creates a `ToolContextManager`
- Each tool invocation gets a child context keyed by tool call ID
- `Agent.CancelTool(id)` cancels one tool without killing the turn
- `CancelAll()` at turn end cleans up
- Error text when no active tool: `no active tool with id <id>`
**Source:** `internal/llm/tool_context.go`, `internal/llm/tool_context_test.go`

### Issue 11: agent-loop.md — Add compaction flow section

**File:** `docs/agent-loop.md` (new section after line ~888)
**Problem:** Compaction is critical to long sessions but only documented in `memory.md`. The agent-loop doc should show how `Compact()` interacts with the message slice and bootstrap boundary.
**Fix:** Add section covering:
- Threshold: `len(messages) < 6` is no-op
- Window: `messages[1:len-4]` gets compacted
- Memory extraction prompt (exact text)
- Summary prompt (exact text)
- Rebuild: system + summary user msg + "Got it" + last 4
- `bootstrapMsgCount` reset behavior
- Session save watermark interaction (gap: not adjusted)
**Source:** `internal/agent/compact.go`, `internal/agent/compact_test.go`

### Issue 12: agent-loop.md — Add persona prompt section

**File:** `docs/agent-loop.md` (new section)
**Problem:** The persona injection ("I AM CODE APE" block) and rehearsal token are completely undocumented.
**Fix:** Add section documenting:
- Where injected (system prompt via `personaPrompt` in `agent.go`)
- Rehearsal exchange appended after persona
- Whether it crosses compaction (it's within bootstrap boundary, so preserved)
- How to disable (set persona to empty string)
**Source:** `internal/agent/agent.go` (search for `personaPrompt`)

### Issue 13: agent-loop.md — Add auto-next/auto-idea section

**File:** `docs/agent-loop.md` (new section)
**Problem:** `AUTONEXT_DONE`/`AUTOIDEA_DONE` tokens, the re-prompt loop, and kill switch are only partially described.
**Fix:** Add section covering:
- Enabled via `--auto-next-steps` / `--auto-next-idea` flags
- After turn completion, `MaybeQueueFollowUp()` checks last assistant text
- If no done token present, re-prompts with exact text
- `followUpStreamSanitizer` strips done tokens from visible output
- Loop terminates when model emits the done token
- No max-iteration cap (can loop forever if model never signals done)
**Source:** `internal/agent/agent.go` (search for `AUTONEXT_DONE`, `MaybeQueueFollowUp`)

### Issue 14: agent-loop.md — Add Anthropic cache markers section

**File:** `docs/agent-loop.md` (new section)
**Problem:** When and how cache markers are placed is undocumented.
**Fix:** Add section covering:
- Only fires when `Client.Service == "anthropic"`
- Stamps `cache_control: {type:"ephemeral"}` on last bootstrap message
- Index is `BootstrapMsgCount - 1`
- Tested in `internal/llm/cache_test.go`
- `zai-anthropic` explicitly excluded until verified
**Source:** `internal/llm/stream.go` (PrepareStep), `internal/llm/cache_test.go`

### Issue 15: sessions.md — Document fork/tree mechanics

**File:** `docs/sessions.md` (new section after the fork mention)
**Problem:** ParentID chain and fork behavior are mentioned but not explained.
**Fix:** Add section covering:
- `/fork` command creates `<base>_fork_<HHMMSS>.jsonl`
- Each entry gets `ParentID` pointing to previous entry's ID
- `session.Tree()` reconstructs the tree from parent chains
- Fork from middle: new entries branch from that point
- `/tree` display format
**Source:** `internal/session/session.go` (Fork function), `internal/session/session_test.go`

### Issue 16: sessions.md — Document daemon session-checkpoint job

**File:** `docs/sessions.md` (new section)
**Problem:** The daemon's `session-checkpoint` job writes sibling files but this interaction is undocumented.
**Fix:** Add section covering:
- Job kind: `session-checkpoint`
- What it writes (checkpoint sidecar JSON)
- Envelope args shape
- Conflict with live process writes (both write same file, last-write-wins)
- Test coverage: `internal/daemon/jobs/checkpoint_test.go`
**Source:** `internal/daemon/jobs/checkpoint.go`

### Issue 17: memory.md — Add filesystem layout example

**File:** `docs/memory.md` (after line ~100)
**Problem:** The paths are documented individually but there's no concrete tree showing what a real workspace looks like.
**Fix:** Add a `tree` output example showing:
```
~/.bitchtea/
  memory/
    bitchtea-a1b2c3d4/
      2026-05-03.md
      2026-05-04.md
      contexts/
        channels/
          dev/
            HOT.md
            daily/
              2026-05-04.md
        queries/
          alice/
            HOT.md
```
Plus `<WorkDir>/MEMORY.md` for root hot.

### Issue 18: memory.md — Document daemon memory-consolidate interaction

**File:** `docs/memory.md` (after line ~1393)
**Problem:** The daemon job section exists but doesn't explain timing, conflict with live writes, or the flock gap.
**Fix:** Add a callout section:
- Daemon uses direct append without flock (documented in code comment)
- Simultaneous `write_memory` from foreground process uses flock
- Potential torn writes if both run at same instant
- Consolidation markers prevent duplicate entries
- No notification to the live agent that HOT changed
**Source:** `internal/daemon/jobs/memory_consolidate.go`

### Issue 19: ui-components.md — Add startup RC execution flow

**File:** `docs/ui-components.md` (after `ExecuteStartupCommand` section ~line 220)
**Problem:** The wiring from `~/.bitchtearc` to execution and timing relative to session restore is missing.
**Fix:** Add section covering:
- `main.go` calls `config.ParseRC()` to extract set commands
- `buildStartupModel()` calls `m.ExecuteStartupCommand(line)` for each
- Timing: after `NewModel()`, after `ResumeSession()`, before `Init()`
- `suppressMessages = true` during execution (output dropped)
- Profile selection in RC clears manual connection settings
**Source:** `main.go` (search for `rcCommands`, `ExecuteStartupCommand`)

### Issue 20: ui-components.md — Add model picker callback wiring

**File:** `docs/ui-components.md` (new section)
**Problem:** `pickerOnSelect` callback is mentioned but the installation path isn't traced.
**Fix:** Add section covering:
- `/models` command calls `handleModelsCommand()`
- Handler loads catalog via `loadModelCatalog(service)`
- Creates picker overlay with model list
- Installs `pickerOnSelect` callback that calls `agent.SetModel()`
- Picker keys override normal input until closed
- Esc or Ctrl+C closes without selection
**Source:** `internal/ui/models_command.go` (or equivalent)

### Issue 21: testing.md — Add required gates prominently

**File:** `docs/testing.md` (beginning, after line 8)
**Problem:** Required checks are listed but not prominently. CLAUDE.md mandates them before closing any code-changing issue.
**Fix:** Add a prominent callout box at the top:
```
MANDATORY before closing any code-changing issue:
  go build ./...
  go test ./...
  go test -race ./...
  go vet ./...
```
Also note: `go build -o bitchtea .` for the main binary.

### Issue 22: testing.md — Add daemon test coverage section

**File:** `docs/testing.md` (after line 193)
**Problem:** No mention of daemon tests in the package inventory.
**Fix:** The daemon test section IS present (lines 186-193) but needs expansion noting:
- `e2e_test.go` is the only subprocess test in the whole repo
- Linux-only, skipped under `-short`
- Tests real lock, real pidfile, real signal delivery
- Job handlers tested in isolation via `jobs/*_test.go`

---

## PART 3: Create Missing Referenced Docs (Medium Priority)

CLAUDE.md's "Docs Split" and README reference these files that don't exist. Each is a standalone creation task.

### Issue 23: Create docs/commands.md

**Scope:** Full slash command reference.
**Content needed:**
- Table: command name, aliases, args, category, handler location
- Per-command detail sections with examples
- Hidden vs listed commands
- Subcommand parsing convention
- Commands that mutate agent state vs display-only
**Source:** `internal/ui/commands.go`, `internal/ui/model.go` (`handleCommand`), test files in `internal/ui/commands_test.go`, `routing_commands_test.go`
**Estimated size:** 200-400 lines

### Issue 24: Create docs/streaming.md

**Scope:** LLM streaming architecture reference.
**Content needed:**
- `ChatStreamer` interface contract
- `stream.go` unified loop walkthrough
- `streamOnce` → fantasy → provider flow
- Provider-specific quirks (OpenAI SSE vs Anthropic)
- Retry logic (delays, retryable error detection, max retries)
- Backpressure (channel buffer size 100, safeSend)
- Error classification (`errors.go`, `ErrorHint`)
- Cost accounting hook points
**Source:** `internal/llm/stream.go`, `internal/llm/client.go`, `internal/llm/errors.go`, `internal/llm/cost.go`
**Estimated size:** 300-500 lines

### Issue 25: Create docs/getting-started.md

**Scope:** New user quickstart.
**Content needed:**
- Prerequisites (Go 1.26, API key for at least one provider)
- Install (`go install` or `go build -o bitchtea .`)
- Environment setup (which env vars for which provider)
- First run (what the splash screen shows)
- First prompt (type something, see response)
- Key commands to know (`/help`, `/set`, `/quit`, Esc, Ctrl+C)
- Where state lands (`~/.bitchtea/`, `MEMORY.md`)
**Source:** `main.go` (flags), `internal/config/` (env vars, profiles), splash in `ui-components.md`
**Estimated size:** 100-150 lines

### Issue 26: Create docs/user-guide.md

**Scope:** Day-in-the-life usage guide.
**Content needed:**
- Prompting (plain text, `@file` references)
- Tool interaction (what happens when agent calls tools)
- Queueing (typing while busy, queue drain, staleness)
- Focus/contexts (`/join`, `/query`, `/part`, `/channels`)
- Memory workflow (when to search, when to write, scopes)
- Session management (`/sessions`, `--resume`, `/fork`)
- Autonomous modes (`/set auto-next on`, `--auto-next-steps`)
- Headless mode (`--headless --prompt "..."`)
- Configuration (`~/.bitchtearc`, `/set`, `/profile`)
**Source:** Various docs already written, `main.go`, `internal/ui/` commands
**Estimated size:** 300-500 lines

### Issue 27: Create docs/glossary.md

**Scope:** Term definitions for the codebase.
**Terms to define:**
- bootstrap, compaction, context (IRC), context key, scope (memory)
- persona, rehearsal token, ladder (cancel), adapter (typed/legacy)
- HOT.md, daily archive, mailbox, envelope, sidecar (file)
- focus, membership, turn, stream, event, fantasy (the library)
- provider, service, profile, catalog, catwalk
- checkpoint, watermark, drain, sanitizer
**Estimated size:** 80-120 lines

### Issue 28: Create docs/troubleshooting.md

**Scope:** Common failure modes and remediation.
**Content needed:**
- Missing API key (error message, how to fix)
- Wrong provider detected (env var precedence)
- Daemon not running (how to start, how to check)
- MCP server crash (symptoms, recovery)
- Terminal tool hang (cancel chain, force kill)
- Session corruption (partial JSONL, how to recover)
- Memory not being recalled (scope mismatch, search behavior)
- Model picker empty (catalog not loaded, service not set)
- Build failures (Go version, dependency issues)
**Source:** `internal/llm/errors.go` (ErrorHint), `internal/config/` (detection), daemon CLI
**Estimated size:** 150-250 lines

### Issue 29: Create docs/daemon.md (combines phase-7 references)

**Scope:** Full daemon reference (replaces missing `phase-7-daemon-audit.md` and `phase-7-process-model.md`).
**Content needed:**
- Process model: standalone binary (`cmd/daemon/main.go`), lock-file exclusion
- Startup: `bitchtea daemon start` (foreground), logging to stdout + file
- Management: `status`, `stop` subcommands
- Mailbox protocol: `mail/` directory, envelope files, polling
- Envelope format: JSON with kind, args, metadata
- Job dispatch: `session-checkpoint`, `memory-consolidate`
- Job lifecycle: `mail/` -> handler -> `done/` or `failed/`
- Lock semantics: exclusive flock, stale lock recovery
- PID file: write on start, remove on shutdown, stale detection
- IPC with TUI: TUI writes envelope files, daemon processes them
- Failure recovery: orphan mail, partial writes, crash quarantine
**Source:** `cmd/daemon/main.go`, `internal/daemon/run.go`, `internal/daemon/mailbox.go`, `internal/daemon/envelope.go`, `internal/daemon/lock.go`, `internal/daemon/pidfile.go`, `internal/daemon/jobs/`
**Estimated size:** 400-600 lines

### Issue 30: Create docs/mcp.md

**Scope:** MCP integration reference.
**Content needed:**
- What MCP is (Model Context Protocol, external tool servers)
- Configuration (`internal/mcp/config.go` schema)
- Server lifecycle: discovery, handshake, tool registration, teardown
- Tool namespacing: `mcp__<server>__<tool>`
- Collision rules: local tools win, first-wins within server
- Resource and prompt discovery (recent commit `777093d`)
- Failure modes: server crash, slow start, authorization
- Permission model (`internal/mcp/permission.go`)
- Testing approach (fake servers, no real subprocess)
**Source:** `internal/mcp/`, `internal/llm/mcp_tools.go`, `internal/llm/mcp_tools_test.go`
**Estimated size:** 200-350 lines

### Issue 31: Create docs/providers.md

**Scope:** Provider routing, profiles, and detection.
**Content needed:**
- `DetectProvider` env var precedence
- Built-in profiles: `ollama`, `openrouter`, `zai-openai`, `zai-anthropic`
- `ProfileAllowsEmptyAPIKey` (only ollama)
- Profile load/save/delete (`/profile` commands)
- Service identity and routing (`routeByService`, `routeOpenAICompatible`)
- URL handling (`hostOf`, `stripV1Suffix`)
- Cost tracking: per-provider pricing, catalog vs embedded, cache hits
- `/set provider`, `/set service`, `/set baseurl`, `/set apikey` effects
**Source:** `internal/config/profiles.go`, `internal/config/config.go`, `internal/llm/providers.go`, `internal/llm/cost.go`
**Estimated size:** 200-300 lines

### Issue 32: Create docs/catalog.md

**Scope:** Model catalog system.
**Content needed:**
- What's catalogued (available models per service)
- Cache location: `~/.bitchtea/catalog/providers.json`
- Refresh: background async on startup if `BITCHTEA_CATWALK_AUTOUPDATE` set
- Source URL: `BITCHTEA_CATWALK_URL`
- ETag handling, 304 behavior, stale fallback
- Embedded snapshot fallback when cache missing/corrupt
- `/models` command interaction
**Source:** `internal/catalog/`, `internal/catalog/refresh.go`, `internal/catalog/load.go`
**Estimated size:** 100-150 lines

### Issue 33: Create CONTRIBUTING.md

**Scope:** Contributor guide (referenced from README).
**Content needed:**
- Required quality gates (4 commands)
- `bd` workflow: claim -> in_progress -> close -> dolt push -> git push
- Dependency graph rules (no upward edges, no cycles)
- Tool addition procedure (from CLAUDE.md)
- Slash command addition procedure (from CLAUDE.md)
- Testing philosophy (assert state, not just no-panic)
- Commit policy (liberal, short messages, no ceremony)
- Session completion protocol
**Source:** CLAUDE.md rules, `docs/testing.md`, `docs/development-guide.md`
**Estimated size:** 100-150 lines

---

## PART 4: Cross-Cutting Reconciliation (Lower Priority)

### Issue 34: Reconcile AGENTS.md vs CLAUDE.md

**Problem:** `AGENTS.md` is nearly byte-identical to `CLAUDE.md` and literally starts with `# CLAUDE.md`. They should diverge: AGENTS.md is agent-facing runtime instructions (for the assistant being run), CLAUDE.md is contributor-facing development instructions.
**Fix:** Either:
- (a) Make AGENTS.md a symlink to CLAUDE.md (simplest), or
- (b) Strip AGENTS.md down to only what a running agent needs (persona anchors, tool surface, memory workflow), removing build/test/contribution instructions

### Issue 35: Update CLAUDE.md "in flight" section

**Problem:** CLAUDE.md says per-context histories are "in flight" but code has `contextMsgs`/`SetContext`/`InitContext` working. The prompt-queue is NOT per-context. This partial state confuses agents.
**Fix:** Update the `bt-x1o` bullet to say: "History isolation is shipped (messages are per-context, `SetContext`/`InitContext` wired). Remaining gaps: TUI prompt-queue is global, compaction doesn't respect context boundaries, session save watermarks are per-context but checkpoint is global."

### Issue 36: Fix README.md dead links

**Problem:** README references `docs/commands.md`, `docs/phase-9-service-identity.md`, `CONTRIBUTING.md` which don't exist.
**Fix:** After creating those docs (issues 23, 31, 33), verify all README links resolve. If any referenced doc is deliberately not created, remove the link from README.

### Issue 37: Remove or document cmd/cpm/

**Problem:** `cmd/cpm/` directory exists but is empty (no source files). Unclear if it's a placeholder or abandoned.
**Fix:** Either add a README explaining its purpose, or `git rm -r cmd/cpm/` if it's dead code.

---

## PART 5: Documentation Infrastructure

### Issue 38: Add doc linting

**Problem:** No automated check that doc claims match code. Stale claims persist for months.
**Fix:** Add a `make doc-check` target or CI step that:
- Verifies all `docs/*.md` internal links resolve
- Verifies all file paths mentioned in docs exist in the repo
- Flags any doc referencing `write_memory` as "unimplemented" or daemon as "missing"
- Checks that the tool count in docs matches `len(Definitions())`

### Issue 39: Add doc generation for tool schemas

**Problem:** Tool reference table drifts from code (currently 3 tools missing).
**Fix:** Add a `cmd/gendocs/` or build step that reads `tools.Definitions()` and outputs a markdown table. The table in `tools.md` should be generated, not hand-maintained.

---

## PART 6: Deep Reference Additions (Backlog)

These are valuable but not blocking. Each expands an existing doc with a detailed flow.

### Issue 40: Add REPL turn data-flow diagram to agent-loop.md

Trace: keystroke -> `Update()` -> `tea.Cmd` -> agent goroutine -> `StreamChat` -> provider -> `event.Event` -> `agentEventMsg` -> viewport append -> next keystroke.

### Issue 41: Add tool-call round-trip diagram to tools.md

Trace: model emits tool_call -> adapter (typed or legacy) -> `Registry.Execute` -> executor -> result -> message append -> next stream step.

### Issue 42: Add focus/context switch flow to agent-loop.md

Trace: `/join` -> `SetContext` -> `contextMsgs` swap -> memory scope update -> tool panel reset -> viewport replay from new context history.

### Issue 43: Add session resume flow to sessions.md

Trace: `--resume` -> `session.Load` -> JSONL parse -> `FantasyFromEntries` -> groupBy context -> `RestoreMessages` -> focus restore -> RC commands.

### Issue 44: Add daemon job round-trip flow to daemon.md

Trace: TUI emits envelope -> mailbox file write -> daemon polls -> job dispatch -> handler -> result envelope -> `done/` directory.

### Issue 45: Document @file token expansion

**Location:** New section in `agent-loop.md` or standalone doc.
**Content:** Path resolution (relative to WorkDir), 30KB truncation, error handling, multiple @-tokens in one prompt, whitespace normalization.
**Source:** `internal/agent/file_refs.go` (or equivalent)

### Issue 46: Document ~/.bitchtearc format

**Location:** `docs/cli-flags.md` or new `docs/rc-file.md`
**Content:** Grammar, valid commands (only `set` today), ordering vs session restore, error reporting, profile selection clearing manual settings.
**Source:** `internal/config/rc.go`, `internal/config/rc_test.go`

### Issue 47: Document /set command surface

**Location:** `docs/commands.md` (once created)
**Content:** Every key the user can `/set`, accepted values, effect, persistence. Keys include: `provider`, `baseurl`, `apikey`, `model`, `service`, `auto-next`, `auto-idea`, `sound`, `debug`.
**Source:** `internal/ui/command_validation_test.go`, `internal/ui/model.go` (handleSetCommand)

### Issue 48: Document sound system

**Location:** `docs/commands.md` or standalone.
**Content:** Bell patterns (bell, done, success, error), `/set sound` toggle, MP3 panel (track discovery, controls, key bindings when panel focused).
**Source:** `internal/sound/sound.go`, `internal/ui/mp3.go`

### Issue 49: Document clipboard system

**Location:** `docs/commands.md` or standalone.
**Content:** `/copy` command, OSC52 vs xclip fallback, numbered selection, error cases.
**Source:** `internal/ui/clipboard.go`, `internal/ui/clipboard_test.go`

### Issue 50: Document history recall

**Location:** `docs/ui-components.md` (new section)
**Content:** Up/Down navigation, Ctrl+P/Ctrl+N explicit history, storage (in-memory only, not persisted), dedup behavior, interaction with queue unqueue.
**Source:** `internal/ui/model.go` (history-related fields and key handlers)

---

## Execution Priority Summary

| Priority | Issues | Effort | Impact |
|----------|--------|--------|--------|
| P0 - Fix wrong claims | 1-6 | 1 session | Stops misinformation propagation |
| P1 - Complete existing docs | 7-22 | 2-3 sessions | Makes existing docs trustworthy |
| P2 - Create missing docs | 23-33 | 4-5 sessions | Eliminates 404 references |
| P3 - Cross-cutting reconciliation | 34-37 | 1 session | Consistency across all docs |
| P4 - Infrastructure | 38-39 | 1 session | Prevents future drift |
| P5 - Deep flows (backlog) | 40-50 | 3-4 sessions | Polish, not critical |

**Total: 50 issues, approximately 12-15 agent sessions to complete all.**

---

## Agent Execution Notes

For each issue above, an autonomous agent should:

1. **Read the source files** listed under "Source" before writing
2. **Run quality gates** if any code changes are made (`go build ./...`, `go test ./...`)
3. **Cross-reference** other docs to avoid re-introducing contradictions
4. **Quote exact strings** from code for any user-visible output
5. **Mark stale claims** explicitly rather than silently removing context
6. **Keep the acyclic principle** — never describe upward dependencies
7. **Verify against CLAUDE.md** — it is the most frequently updated authority
