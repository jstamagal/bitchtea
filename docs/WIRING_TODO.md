# WIRING_TODO.md — Exhaustive Codebase-vs-Docs Audit

Generated 2026-05-04 by lead auditor pass. Companion files: `DOC_TODO.md`,
`TESTING_TODO.md`.

This document covers: files not wired in anywhere, commands that are cosmetic
(not doing what they claim), major discrepancies between docs and code, missing
documentation for existing features, and stale claims that contradict the
current codebase.

---

## 1. STALE / FALSE CLAIMS IN DOCS AND CLAUDE.md

### 1.1 CLAUDE.md "In flight" section claims per-context histories haven't landed

**File:** `CLAUDE.md` — "In flight" block, first bullet (`bt-x1o`, P0)

**Claim:** "/join #chan and /query nick only re-label the UI today; the agent
still streams against one shared messages slice."

**Reality:** `internal/agent/context_switch.go` implements full per-context
message isolation. The `Agent` struct (`agent.go:47-53`) has `contextMsgs
map[ContextKey][]fantasy.Message`, `contextSavedIdx`, and `currentContext`.
`SetContext()` swaps the active `messages` slice. `InitContext()` clones the
bootstrap prefix into new contexts. The UI calls `InitContext` + `SetContext`
+ `SetScope` at turn start (`model.go:933-938`).

**Fix:** Remove the "don't describe as done" warning for per-context histories.
Update the bullet to say it's landed but note any remaining gaps (e.g., `/join`
itself doesn't switch agent context — only the next `sendToAgent` does).

---

### 1.2 architecture.md says write_memory is "unimplemented"

**File:** `docs/architecture.md:36-37`

**Claim:** "Missing `write_memory` Tool: The prompt explicitly instructs the
LLM to call write_memory... but the tool is unimplemented."

**Reality:** `write_memory` is fully implemented:
- Registry: `internal/tools/tools.go` (Definitions + Execute)
- Typed wrapper: `internal/llm/typed_write_memory.go`
- Tests: `typed_write_memory_test.go`
- Documented correctly in `docs/memory.md`

**Fix:** Delete the "Missing write_memory" bullet from architecture.md. The
memory.md doc already has the correction note at its top.

---

### 1.3 architecture.md says CLAUDE.md claims "there is no daemon"

**File:** `docs/architecture.md:33`

**Claim:** 'Documentation (e.g., CLAUDE.md) explicitly claims "there is
currently no daemon binary, no cmd/daemon, no internal/daemon package."
This is factually incorrect.'

**Reality:** Both the architecture.md criticism AND the old CLAUDE.md claim are
now stale. CLAUDE.md has been updated to document the daemon as "rebuilt"
(`bt-p7, P3`), and `daemon_cli.go` + `cmd/daemon/main.go` +
`internal/daemon/` all exist and compile. Architecture.md is criticizing a
CLAUDE.md state that no longer exists.

**Fix:** Rewrite the "Phantom Daemon" bullet in architecture.md to document
what the daemon actually does today.

---

### 1.4 architecture.md says fantasy migration is "incomplete" without nuance

**File:** `docs/architecture.md:34`

**Claim:** "Fantasy Migration Incomplete: Tool execution remains largely
untyped."

**Reality:** 6 of 14 tools (read, write, edit, bash, search_memory,
write_memory) are now typed fantasy wrappers (`internal/llm/typed_*.go`).
MCP tools also have a typed `mcpAgentTool` adapter (`mcp_tools.go`). Only
the 7 terminal tools + preview_image remain on the legacy `bitchteaTool`
generic adapter. CLAUDE.md documents this accurately.

**Fix:** Update architecture.md to state the current typed/legacy split.

---

### 1.5 architecture.md context histories bullet is vague/stale

**File:** `docs/architecture.md:35`

**Claim:** "Context Histories Isolation... still blur the lines, relying on
shared pointers or bleeding memory scopes."

**Reality:** Per-context isolation is implemented (`context_switch.go`). The
agent holds a `contextMsgs` map with per-context slices. Session save is
per-context via `saveCurrentContextMessages` in `context_helpers.go`. Memory
scopes route correctly per context.

**Fix:** Replace with accurate description of what's wired and what gaps
remain (e.g., compaction operates on the active context only).

---

## 2. COMMANDS THAT ARE COSMETIC / MISLEADING

### 2.1 /join does NOT switch agent context immediately

**File:** `internal/ui/commands.go:693-706`

`handleJoinCommand` calls `m.focus.SetFocus(ctx)` and saves focus to disk.
It does NOT call `m.agent.SetContext()` or `m.agent.SetScope()`. Those calls
happen at `sendToAgent` time (`model.go:933-938`).

**Impact:** If you `/join #dev` and then immediately `/memory`, the memory
shown is still from the previous context's scope. The agent context only
switches when you actually send a message.

**Docs claim (commands.md:126-131):** "/join calls m.focus.SetFocus and
m.agent.SetScope... triggers agent.SetScope... Context Injection: If HOT.md
exists for the channel, it is injected into the LLM as a context message
immediately."

**This is false.** HOT.md injection happens at the next sendToAgent, not at
/join time.

**Fix:** Update commands.md /join trace to be accurate. Consider whether /join
should eagerly call SetContext+SetScope (design decision, not just a doc fix).

---

### 2.2 /query does NOT switch agent context immediately (same issue as /join)

**File:** `internal/ui/commands.go:742-755`

Same pattern: `m.focus.SetFocus(ctx)` only. No `agent.SetContext` or
`agent.SetScope` call.

---

### 2.3 /part does NOT call agent.SetContext on the new focus

**File:** `internal/ui/commands.go:710-738`

`handlePartCommand` removes the context from FocusManager and saves. It does
not inform the agent that the active context changed. The agent will pick up
the new focus on next `sendToAgent`.

---

### 2.4 /invite does NOT wire the persona into the agent

**File:** `internal/ui/invite.go:13-58`

`handleInviteCommand` updates `m.membership` and displays a catch-up summary.
But the persona is not wired into the agent's system prompt, tool routing, or
any behavioral gate. The membership data is purely metadata — the LLM doesn't
know about invited personas unless told via a user message.

**Undocumented in:** commands.md, user-guide.md, ui-components.md. The /invite
and /kick commands are not mentioned in commands.md at all.

---

### 2.5 /kick does NOT remove the persona from agent awareness

**File:** `internal/ui/invite.go:60-85`

Same issue as /invite — purely metadata. No agent integration.

---

### 2.6 /activity is documented but behavior is opaque

**File:** `internal/ui/commands.go:339-363`

The `/activity` command shows "queued background activity" but this concept is
not documented anywhere in docs/. What generates background activity? How is
it queued? The docs don't explain.

---

## 3. UNDOCUMENTED FILES AND PACKAGES

### 3.1 internal/mcp/ — entire MCP client package

**Files:** `client.go`, `manager.go`, `config.go`, `permission.go`, `audit.go`,
`redact.go` + tests

The MCP client is a substantial subsystem with:
- Server lifecycle management (start/stop/health tracking)
- Tool namespacing (`mcp__<server>__<tool>`)
- Per-call authorization via `Authorizer` interface
- Audit hooks via `AuditHook` interface
- Secret redaction for session/transcript safety
- Config loading from `.bitchtea/mcp.json`
- Resource reading with size caps

**Documented in:** `docs/phase-6-mcp-contract.md` (the design contract).

**NOT documented in:** Any of the user-facing or maintainer docs. Not in
architecture.md's dependency graph. Not in tools.md. Not in user-guide.md.
The `phase-6` doc is a design spec, not operational documentation.

**Fix:** Add MCP section to architecture.md dependency graph. Add MCP tools
section to tools.md. Document `mcp.json` config format in a user-facing doc.

---

### 3.2 internal/llm/mcp_tools.go — MCP-to-fantasy bridge

**File:** `internal/llm/mcp_tools.go`

`mcpAgentTool` adapter and `MCPTools()` + `AssembleAgentTools()` functions.
This is the bridge that makes MCP tools appear as fantasy AgentTools alongside
built-in tools. Not documented in tools.md or agent-loop.md.

**Fix:** Document `AssembleAgentTools` in tools.md or agent-loop.md.

---

### 3.3 internal/llm/tool_context.go — per-tool cancellation

**File:** `internal/llm/tool_context.go`

`ToolContextManager` derives per-tool-call child contexts so individual tools
can be cancelled without killing the turn. Has `NewToolContext`, `CancelTool`,
`CancelAll`. Referenced in `docs/phase-8-cancellation-state.md` design doc but
not in any operational doc (agent-loop.md, tools.md, signals-and-keys.md).

**Fix:** Document per-tool cancellation in agent-loop.md and
signals-and-keys.md.

---

### 3.4 internal/llm/cache.go — Anthropic prompt caching

**File:** `internal/llm/cache.go`

Implements Anthropic-specific cache breakpoint marking for prompt caching.
Not documented in any doc. Not mentioned in streaming.md or architecture.md.

**Fix:** Add prompt caching section to streaming.md or a provider-specific doc.

---

### 3.5 internal/agent/context_switch.go — per-context message isolation

**File:** `internal/agent/context_switch.go`

Full per-context message isolation: `SetContext`, `InitContext`,
`InjectNoteInContext`, `RestoreContextMessages`, `SavedIdx`, `SetSavedIdx`.
This is the implementation of the "per-context histories" feature that
CLAUDE.md incorrectly calls "in flight."

**Documented partially in:** agent-loop.md, memory.md, sessions.md reference
`SetContext`/`InitContext`. But no doc describes the full context-switch
mechanics, bootstrap duplication, or the context map lifecycle.

**Fix:** Add "Per-Context Message Isolation" section to agent-loop.md.

---

### 3.6 internal/ui/context.go — FocusManager and IRC context model

**File:** `internal/ui/context.go`

Full IRC-style context model: `IRCContext`, `ContextKind` (Channel,
Subchannel, Direct), `FocusManager` with ordered context list, persistence
via `session.FocusState`. Serialization to/from JSON sidecar files.

**Documented partially in:** sessions.md, ui-components.md. But no doc
describes the `FocusManager` lifecycle, the ordered context list, or how
`contextToRecord`/`recordToContext` work.

---

### 3.7 internal/ui/context_helpers.go — context-to-key bridge

**File:** `internal/ui/context_helpers.go`

`ircContextToKey` converts UI contexts to agent ContextKey strings.
`saveCurrentContextMessages` persists per-context messages to session JSONL
with context labels. Not documented.

---

### 3.8 internal/catalog/ — model catalog system

**Files:** `load.go`, `cache.go`, `refresh.go` + tests

Full catalog subsystem: on-disk cache at `~/.bitchtea/catalog/providers.json`,
catwalk embedded fallback, background refresh with TTL, ETag support.
`docs/model-catalog.md` and `docs/phase-5-catalog-audit.md` exist but are
design docs, not operational.

**Not documented in:** architecture.md dependency graph omits `internal/catalog`.
Not in getting-started.md or troubleshooting.md.

---

### 3.9 internal/ui/mp3.go — full MP3 player

**File:** `internal/ui/mp3.go` (537 lines)

Full-featured MP3 player: library scanning from `~/.bitchtea/mp3/`, playback
via mpv/ffplay/mpg123, pause/resume via SIGSTOP/SIGCONT, playlist navigation,
progress bar, visualizer, side panel rendering.

**Documented in:** commands.md mentions `/mp3 [cmd]` briefly. Not documented
at all in ui-components.md despite being a major UI feature. The MP3 panel
rendering, key bindings (space=pause, j/k=prev/next), library directory, and
player fallback chain are all undocumented.

**Fix:** Add MP3 player section to ui-components.md. Document the library dir,
player requirements, and key bindings.

---

### 3.10 internal/ui/model_picker.go — fuzzy model picker

**Files:** `model_picker.go`, `model_picker_keys.go`

Interactive fuzzy-find overlay for model selection. Key bindings, filtering,
pagination. Documented in commands.md for `/models` but the picker's own key
bindings and rendering are not in ui-components.md or signals-and-keys.md.

---

### 3.11 daemon_cli.go — daemon CLI dispatch (root package)

**File:** `daemon_cli.go`

`runDaemon` dispatches `start|status|stop` subcommands. Includes log file
setup, lock checking, pid file management, SIGTERM sending. Documented in
`--help` output and phase-7 docs but not in cli-flags.md or user-guide.md as
a user-facing feature.

---

### 3.12 internal/llm/debug.go — HTTP debug logging

**File:** `internal/llm/debug.go`

Debug hook for HTTP request/response logging when `/debug on` is active.
Not documented in any operational doc.

---

### 3.13 internal/ui/themes.go — theme system

**File:** `internal/ui/themes.go`

Theme definitions and `/theme` command support. The `/theme` command is
registered in `commands.go` but not documented in commands.md help text
or commands.md doc file.

**Fix:** Add `/theme` to commands.md.

---

### 3.14 internal/ui/art.go — ASCII art / splash

**File:** `internal/ui/art.go`

Startup ASCII art and splash screen. Not documented.

---

### 3.15 internal/sound/sound.go — sound notification system

**File:** `internal/sound/sound.go`

Sound notification support. Referenced in config (`sound` setting, `/set
sound`). The UI package imports it. Not documented in any doc.

---

## 4. DOCS THAT CONTAIN DUPLICATED / CONTRADICTORY SECTIONS

### 4.1 commands.md has TWO "REPL-TO-AGENT ORCHESTRATION" sections

**File:** `docs/commands.md:48-93` and `docs/commands.md:95-133`

Two separate "TECHNICAL DEEP-DIVE" sections that describe the same flows with
slightly different details. The first describes `/compact` as "Summary Phase
then Flush Phase" (summary first); the second describes it as "Churn Phase 1
(Memory Extraction) then Churn Phase 2 (Summary)" (extraction first).

**Which is correct?** The code (`agent.go` Compact) does extraction FIRST,
then summary. So the second section is correct and the first is wrong.

**Fix:** Merge into one section. Fix the compaction order.

---

### 4.2 commands.md /join trace is wrong

See item 2.1. The trace claims `/join` calls `agent.SetScope` directly. It
doesn't. The scope switch is deferred to `sendToAgent`.

---

## 5. MISSING DATA FLOW DOCUMENTATION

### 5.1 The full tool assembly pipeline is undocumented

The path from tool definitions to what the LLM sees:
1. `tools.Registry.Definitions()` returns built-in schemas
2. `llm.TranslateTools()` converts some to typed fantasy wrappers, rest to
   generic `bitchteaTool` adapter
3. `llm.MCPTools()` adds MCP tools from Manager
4. `llm.AssembleAgentTools()` merges both sets (local wins on collision)
5. Result goes to `fantasy.Agent` for streaming

This pipeline is not documented end-to-end anywhere. `tools.md` covers the
registry. `agent-loop.md` covers the agent. Neither covers the assembly.

---

### 5.2 The session resume data flow has gaps

Resume path:
1. `session.Load(path)` reads JSONL → `[]Entry`
2. `session.FantasyFromEntries(entries)` converts to `[]fantasy.Message`
   (uses v1 format; falls back to `legacyEntryToFantasy` for v0)
3. `agent.RestoreMessages(msgs)` replaces in-memory history
4. Per-context restore via `RestoreContextMessages` for multi-context sessions

**Gap:** Step 4 is not documented. Sessions.md documents per-context JSONL
entries but not how multi-context restore works in practice. How does the UI
know which entries belong to which context? (Answer: `Entry.Context` field.)

---

### 5.3 The queued prompt / mid-turn steering flow

When a user types while the agent is streaming:
1. UI calls `m.agent.QueuePrompt(text)`
2. Agent's `PrepareStep` (called by fantasy between tool calls) drains queue
3. Queued prompts are injected as `[queued prompt N]: text` user messages

This is partially documented in agent-loop.md and architecture.md but the
exact injection format and `PrepareStep` mechanics aren't in user-guide.md.
Users can steer mid-turn but the user-guide doesn't explain how.

---

### 5.4 The follow-up / auto-next pipeline

`MaybeQueueFollowUp()` checks `--auto-next-steps` / `--auto-next-idea` flags
and the `AUTONEXT_DONE` / `AUTOIDEA_DONE` magic tokens. If the assistant
didn't signal done, a follow-up prompt is injected automatically.

**Headless mode** supports this via `runHeadlessLoop` (main.go:262-313).
**TUI mode** support is through `handleAgentDone` → `MaybeQueueFollowUp`.

This pipeline is mentioned in architecture.md but not in user-guide.md or
cli-flags.md in enough detail for users to understand the behavior.

---

### 5.5 The cost tracking pipeline

`CostTracker` accumulates tokens per turn. It can use either the embedded
catwalk snapshot or the cached catalog for pricing. `CatalogPriceSource`
bridges the catalog `Envelope` to the price lookup interface. main.go wires
this at startup (`llm.SetDefaultPriceSource`).

Not documented in any operational doc. streaming.md mentions it in one line.

---

## 6. FILES THAT EXIST BUT AREN'T WIRED IN

### 6.1 cmd/trace/main.go — developer scratchpad

**Status:** Documented in CLAUDE.md as a developer scratchpad. Not in any
docs/ file. Not in `--help`. Correct — it's intentionally not shipped.

### 6.2 cmd/daemon/main.go — separate daemon binary entry

**Status:** This is a second binary entry point (`go build ./cmd/daemon`).
`daemon_cli.go` dispatches `bitchtea daemon start` which runs the daemon
in-process. `cmd/daemon/main.go` is a standalone entry that goes directly
to `daemon.Run`. The relationship between these two entry points is not
documented. Are they both intended? Which should users use?

---

## 7. DOCS THAT REFERENCE NON-EXISTENT OR RENAMED THINGS

### 7.1 commands.md references handleCommand

**File:** `docs/commands.md:54`

References "handleCommand" as the dispatch function. The actual dispatcher is
`lookupSlashCommand` from the `slashCommandRegistry` map in `commands.go`.
There is no function literally named `handleCommand`.

---

### 7.2 streaming.md is a stub

**File:** `docs/streaming.md` (36 lines)

Barely documents the streaming system. Mentions `ChatStreamer` interface and
`StreamEvent` types but doesn't cover: the fantasy shim details, provider
differences, error handling, retry logic, the `done` event contract, how
messages are accumulated, or how streaming integrates with the agent loop.

**Fix:** Expand substantially or merge content into agent-loop.md.

---

### 7.3 architecture.md dependency graph is incomplete

**File:** `docs/architecture.md:9-29`

Missing from the graph:
- `internal/catalog` (imported by `internal/llm` and `main`)
- `internal/mcp` (imported by `internal/llm` and `internal/agent`)
- `internal/agent/event` (subpackage)
- `internal/daemon/jobs` (subpackage)
- `charm.land/fantasy` (critical external dep)
- `charm.land/catwalk` (catalog dep)

---

## 8. UNDOCUMENTED CONFIG / SETTINGS

### 8.1 /set sound — not documented

The `sound` setting is in the slash command registry and help text but not
explained in commands.md, user-guide.md, or cli-flags.md. What does it do?
What sounds does it play? When?

### 8.2 /set auto-next and /set auto-idea — partially documented

Mentioned in cli-flags.md as `--auto-next-steps` and `--auto-next-idea` flags.
Can also be toggled via `/set auto-next` and `/set auto-idea`. The /set
variants are in help text but the runtime behavior (magic tokens, follow-up
injection) is not in user-guide.md.

### 8.3 /set nick — not documented

The `nick` setting controls `config.UserNick` which is used for display in
chat messages. Not documented in commands.md beyond the help text listing.

### 8.4 /set service — partially documented

Documented in commands.md's "Provider vs Service" section. But the
interaction between service and provider when using `/set baseurl` (which
clobbers service to "custom") is only in commands.md, not user-guide.md.

### 8.5 .bitchtearc — partially documented

Mentioned in CLAUDE.md and main.go. `config.ParseRC` reads `~/.bitchtearc`.
`config.ApplyRCSetCommands` applies `/set` lines from it. Not documented in
user-guide.md or getting-started.md.

### 8.6 BITCHTEA_CATWALK_URL and BITCHTEA_CATWALK_AUTOUPDATE

Documented in `--help` output. Not in any docs/ file.

---

## 9. PHASE DOCS THAT CONTRADICT CURRENT STATE

### 9.1 phase-3-message-contract.md

Should be marked as COMPLETED. Phase 3 is done per CLAUDE.md. The contract
doc is accurate as a historical record but should have a status banner.

### 9.2 phase-4-preparestep.md

PrepareStep is implemented. The doc should note completion status.

### 9.3 phase-5-catalog-audit.md

Catalog is implemented. The doc should note completion status.

### 9.4 phase-6-mcp-contract.md

MCP client is implemented. The doc is the design contract and is still
accurate, but it should note which items shipped and which are future (e.g.,
resources, prompts).

### 9.5 phase-7 docs

Daemon is rebuilt. The audit doc (`phase-7-daemon-audit.md`) and process
model doc (`phase-7-process-model.md`) should note current implementation
status.

### 9.6 phase-8-cancellation-state.md

`ToolContextManager` is implemented. Doc should note completion.

### 9.7 phase-9-service-identity.md

Service field is implemented. Doc should note completion.

---

## 10. BUGS / SEMANTIC ISSUES FOUND DURING AUDIT

### 10.1 /join doesn't switch agent scope — /memory shows wrong context

If you `/join #dev` then immediately `/memory`, the memory command reads the
scope from `m.focus.Active()` to compute the memory scope
(`commands.go:409-450`), so it DOES read the right context. But the AGENT's
scope (for `search_memory` / `write_memory`) is still the previous context
until the next message send. This means an autonomous `write_memory` call
that fires between `/join` and the next user message writes to the wrong
scope.

**Severity:** Low (autonomous writes between /join and next message are rare).

### 10.2 Subchannel context is undocumented and untestable via commands

`internal/ui/context.go` defines `KindSubchannel` and `Subchannel()`. But no
slash command creates a subchannel context. `/join #dev.build` creates a
channel named "dev.build", not a subchannel. The subchannel type exists in
code but is unreachable from the UI.

**Severity:** Medium (dead code path for a feature that may be intended).

### 10.3 Daemon consolidation uses flat scopes, losing parentage

`docs/memory.md:1312-1313`: "Daemon-built scopes are flat. They do not
preserve a parent channel for query scopes."

This means `search_memory` in a query-under-channel context sees
parent+root, but daemon consolidation for that query's daily files only
consolidates into the flat query's HOT, not the nested one.

### 10.4 Root MEMORY.md can be injected twice into LLM context

`docs/memory.md:793-803`: Root memory is injected at bootstrap as "session
memory from previous work" AND can be re-injected via `SetScope(root)` as
"Context memory for root". This wastes tokens on duplication.

### 10.5 Pre-resume scoped injection can be lost

`docs/memory.md:805-809`: `NewModel` calls `SetScope` before `ResumeSession`.
If that marks a HOT path as injected, then `ResumeSession` replaces the
message slice, the injection is lost but the path stays marked. Later turns
never re-inject.

### 10.6 Daily memory heading always says "pre-compaction flush"

`docs/memory.md:463-469`: Even `write_memory` daily writes use
`AppendDailyForScope` which stamps the heading as "pre-compaction flush".
This is misleading for explicit tool writes.

---

## 11. printUsage() vs ACTUAL COMMANDS

Commands in `printUsage()` (main.go:405-421):
```
/set, /profile, /models, /compact, /clear, /restart, /tokens,
/sessions, /tree, /fork, /mp3, /help, /quit
```

Commands registered in `slashCommandRegistry` (commands.go:33-60):
```
/quit, /q, /exit, /help, /h, /set, /clear, /restart, /compact,
/copy, /tokens, /debug, /activity, /mp3, /theme, /memory,
/sessions, /ls, /resume, /tree, /fork, /profile, /models,
/join, /part, /query, /channels, /ch, /msg, /invite, /kick
```

**Missing from printUsage():**
- `/copy` — user-facing, should be in --help
- `/debug` — user-facing, should be in --help
- `/activity` — user-facing, should be in --help
- `/theme` — user-facing, should be in --help
- `/memory` — user-facing, should be in --help
- `/resume` — user-facing, should be in --help
- `/join`, `/part`, `/query`, `/msg`, `/channels` — core IRC commands
- `/invite`, `/kick` — IRC membership commands

**Missing from commands.md:**
- `/invite` — not mentioned at all
- `/kick` — not mentioned at all
- `/theme` — not mentioned at all
- `/activity` — briefly mentioned but not explained
- `/resume` — mentioned in help text but not in the command reference section

---

## 12. SUMMARY OF HIGHEST-PRIORITY FIXES

| # | Priority | Item |
|---|----------|------|
| 1 | **P0** | CLAUDE.md "In flight" per-context claim is false — remove/update |
| 2 | **P0** | architecture.md has 4 stale/false bullets (write_memory, daemon, fantasy, context) |
| 3 | **P1** | commands.md /join trace is wrong (claims immediate SetScope) |
| 4 | **P1** | commands.md has duplicate REPL sections with contradicting compaction order |
| 5 | **P1** | MCP package has zero user-facing or maintainer documentation |
| 6 | **P1** | streaming.md is a 36-line stub |
| 7 | **P1** | MP3 player (537 lines) has no docs in ui-components.md |
| 8 | **P2** | Tool assembly pipeline (Registry → typed/generic → MCP → AssembleAgentTools) undocumented |
| 9 | **P2** | /invite and /kick not in any docs |
| 10 | **P2** | /theme not in any docs |
| 11 | **P2** | Subchannel context type exists but is unreachable from UI |
| 12 | **P2** | printUsage() missing 13 commands |
| 13 | **P2** | architecture.md dependency graph missing 6 packages |
| 14 | **P3** | Phase docs need completion status banners |
| 15 | **P3** | .bitchtearc not documented in user-facing docs |
| 16 | **P3** | sound system not documented |
| 17 | **P3** | Per-tool cancellation (ToolContextManager) not in operational docs |
| 18 | **P3** | Prompt caching (cache.go) not documented |
| 19 | **P3** | Cost tracking pipeline not documented |
| 20 | **P3** | cmd/daemon/main.go vs daemon_cli.go relationship undocumented |
