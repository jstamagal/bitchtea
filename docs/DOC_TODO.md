# DOC_TODO.md — Documentation Gaps & Inaccuracies

Generated 2026-05-04. Companion files: `WIRING_TODO.md`, `TESTING_TODO.md`.

This is the exhaustive list of what documentation is missing or incorrect from
an architectural and functional standpoint.

---

## A. DOCS CONTAINING FALSE OR STALE CLAIMS

### A.1 architecture.md — 4 false bullets in "Disconnected Wiring" section

**File:** `docs/architecture.md:32-36`

| Bullet | Claim | Reality |
|--------|-------|---------|
| "Phantom Daemon" | CLAUDE.md says daemon doesn't exist | CLAUDE.md was updated; daemon is documented as rebuilt. Architecture.md is criticizing a fixed doc. |
| "Fantasy Migration Incomplete" | "Tool execution remains largely untyped" | 6/14 tools have typed wrappers + MCP tools use `mcpAgentTool`. Only 8 remain on legacy adapter. |
| "Context Histories Isolation" | "still blur the lines, relying on shared pointers" | `context_switch.go` implements full per-context isolation with `contextMsgs` map. |
| "Missing write_memory" | "the tool is unimplemented" | `write_memory` is live in registry + has typed wrapper + has tests. |

**Action:** Rewrite the entire "Disconnected Wiring" section to match current state.

---

### A.2 commands.md — /join trace is wrong

**File:** `docs/commands.md:125-131`

**Claims:** "/join calls m.focus.SetFocus and m.agent.SetScope... triggers
agent.SetScope... If HOT.md exists for the channel, it is injected immediately."

**Reality:** `/join` only calls `m.focus.SetFocus(ctx)` and persists focus.
It does NOT call `agent.SetScope` or `agent.SetContext`. Those happen at
`sendToAgent` time (`model.go:933-938`). HOT.md injection is deferred.

**Action:** Rewrite the /join command trace.

---

### A.3 commands.md — duplicate REPL sections with wrong compaction order

**File:** `docs/commands.md:48-93` vs `docs/commands.md:95-133`

Two "TECHNICAL DEEP-DIVE" sections describe the same flows. The first says
compaction does "Summary Phase then Flush Phase" (summary first). The second
says "Memory Extraction then Summary" (extraction first). Code does extraction
first, then summary.

**Action:** Merge into one section with correct order.

---

### A.4 CLAUDE.md "In flight" per-context claim is stale

**File:** `CLAUDE.md`, "In flight" section, first bullet

**Claims:** "/join #chan and /query nick only re-label the UI today; the agent
still streams against one shared messages slice."

**Reality:** `context_switch.go` implements full per-context isolation.
`Agent.SetContext()` swaps the active `messages` slice from `contextMsgs` map.
UI calls `InitContext` + `SetContext` + `SetScope` at turn start.

**Action:** Remove "don't describe as done" warning. Update to reflect landed state.

---

### A.5 commands.md — /join verbatim map row is wrong

**File:** `docs/commands.md:89`

**Claims:** `/join #ch` → Agent Handshake: `a.SetScope(ChannelScope("#ch"))`

**Reality:** /join has no agent handshake. Scope is set at next sendToAgent.

**Action:** Fix the verbatim map entry.

---

## B. MISSING DOCUMENTATION — ENTIRE FEATURES

### B.1 MCP client system — zero user/maintainer docs

**Package:** `internal/mcp/` (client.go, manager.go, config.go, permission.go,
audit.go, redact.go)

**What exists:** `docs/phase-6-mcp-contract.md` (design contract).

**What's missing:**
- Architecture.md doesn't mention `internal/mcp` in the dependency graph
- tools.md doesn't describe MCP tool assembly or namespacing
- No user-facing doc explains `.bitchtea/mcp.json` config format
- No doc explains how MCP tools appear to the LLM (`mcp__<server>__<tool>`)
- No doc explains the Authorizer or AuditHook interfaces
- No doc explains server health tracking or timeout behavior

**Action:** Add MCP section to architecture.md. Add MCP tools to tools.md.
Create user-facing MCP setup guide or add to user-guide.md.

---

### B.2 Tool assembly pipeline — undocumented

**Files:** `internal/llm/tools.go` (TranslateTools), `internal/llm/mcp_tools.go`
(MCPTools, AssembleAgentTools)

**What's missing:** No doc describes the full pipeline from tool definitions
to what the LLM sees:
1. `Registry.Definitions()` → built-in schemas
2. `TranslateTools()` → typed fantasy wrappers or generic `bitchteaTool`
3. `MCPTools()` → MCP tools from Manager
4. `AssembleAgentTools()` → merged set (local wins on collision)

**Action:** Add "Tool Assembly" section to tools.md or agent-loop.md.

---

### B.3 Per-context message isolation — undocumented

**File:** `internal/agent/context_switch.go`

**What's missing:** No doc describes:
- `contextMsgs` map structure
- `SetContext()` message slice swapping
- `InitContext()` bootstrap cloning
- `InjectNoteInContext()` for cross-context notes
- `RestoreContextMessages()` for resume
- How bootstrap messages are duplicated per context
- Context save watermarks (`contextSavedIdx`)

**Action:** Add "Per-Context Message Isolation" section to agent-loop.md.

---

### B.4 MP3 player — undocumented in ui-components.md

**File:** `internal/ui/mp3.go` (537 lines)

**What's missing from ui-components.md:**
- Library directory (`~/.bitchtea/mp3/`)
- Player detection chain (mpv → ffplay → mpg123)
- Pause/resume via SIGSTOP/SIGCONT
- Panel rendering and layout
- Key bindings (space=pause, j/k or arrows=prev/next)
- Track scanning, playlist navigation
- Progress bar and visualizer rendering
- Auto-advance on track completion

**What's in commands.md:** One line: "/mp3 [cmd]: Control the built-in MP3 player."

**Action:** Add full MP3 section to ui-components.md.

---

### B.5 Model picker — partially documented

**Files:** `internal/ui/model_picker.go`, `model_picker_keys.go`

commands.md documents the `/models` command well. But ui-components.md and
signals-and-keys.md don't describe the picker overlay, its key bindings, or
how it integrates with the catalog system.

**Action:** Add model picker section to ui-components.md and its keys to
signals-and-keys.md.

---

### B.6 Per-tool cancellation — undocumented in operational docs

**File:** `internal/llm/tool_context.go`

**What exists:** `docs/phase-8-cancellation-state.md` (design doc).

**What's missing from operational docs:**
- How `ToolContextManager` derives child contexts per tool call
- How Esc-press-1 cancels active tool via `CancelTool`
- How turn cancellation cancels all tools via `CancelAll`
- How this integrates with the Esc/Ctrl-C ladder in the UI

**Action:** Add to agent-loop.md and signals-and-keys.md.

---

### B.7 Prompt caching — undocumented

**File:** `internal/llm/cache.go`

Anthropic-specific prompt cache breakpoint marking. No docs mention this.

**Action:** Add to streaming.md or a new provider-specific doc.

---

### B.8 Cost tracking pipeline — undocumented

**Files:** `internal/llm/cost.go`, catalog integration in main.go

The `CostTracker`, `PriceSource` interface, `CatalogPriceSource` bridge,
and `SetDefaultPriceSource` are not documented beyond one line in
streaming.md.

**Action:** Add cost tracking section to streaming.md or a new doc.

---

### B.9 Debug logging — undocumented

**File:** `internal/llm/debug.go`

HTTP debug hook for `/debug on`. Not documented in any operational doc.

**Action:** Add debug hook description to agent-loop.md or streaming.md.

---

### B.10 Theme system — undocumented

**File:** `internal/ui/themes.go`

Theme definitions. `/theme` command is registered but currently reports
"switching is disabled." Not documented in commands.md.

**Action:** Add `/theme` to commands.md. Document theme system in ui-components.md.

---

### B.11 Sound system — undocumented

**File:** `internal/sound/sound.go`

Sound notifications. `/set sound` exists. UI imports the package. No doc
explains what sounds play, when, or how to configure.

**Action:** Document in user-guide.md or ui-components.md.

---

### B.12 Splash / ASCII art — undocumented

**File:** `internal/ui/art.go`

Startup ASCII art rendering. Not documented.

**Action:** Mention in ui-components.md or getting-started.md.

---

### B.13 /invite and /kick — undocumented

**File:** `internal/ui/invite.go`

Fully implemented membership commands. Not in commands.md, user-guide.md,
or any doc. The membership system (`internal/ui/membership.go`,
`internal/session/membership.go`) is partially documented in sessions.md
but the commands themselves are missing.

**Action:** Add to commands.md.

---

### B.14 /activity — undocumented

**File:** `internal/ui/commands.go:339-363`

Shows/clears background activity queue. Not explained in any doc. What
generates background activity? How is it queued?

**Action:** Add to commands.md and explain the activity model.

---

### B.15 .bitchtearc — not in user-facing docs

**File:** `internal/config/rc.go`

`~/.bitchtearc` is parsed at startup. `/set` lines are applied before the
TUI boots. Mentioned in CLAUDE.md but not in getting-started.md,
user-guide.md, or cli-flags.md.

**Action:** Add to getting-started.md and user-guide.md.

---

### B.16 Headless mode — partially documented

**File:** `main.go:245-322`

`--headless` mode with `--prompt` and stdin piping. Documented in cli-flags.md
but the output format (stdout for text, stderr for tool/status events) and
follow-up behavior in headless mode are not in user-guide.md.

**Action:** Expand headless documentation.

---

## C. DOCS THAT ARE STUBS OR TOO THIN

### C.1 streaming.md — 36-line stub

**File:** `docs/streaming.md`

Covers: `ChatStreamer` interface, `StreamEvent` types, fantasy shim mention,
one-line cost tracking mention.

**Doesn't cover:** Provider differences (OpenAI vs Anthropic streaming),
error handling in streams, retry logic, the `done` event contract, how
messages are accumulated during streaming, cache breakpoint injection,
debug hook integration, how `StreamChat` bridges to fantasy, usage event
reporting.

**Action:** Expand to at least 150 lines or merge into agent-loop.md.

---

### C.2 user-guide.md — check for completeness

Needs review against the full feature set. Should cover: IRC commands,
memory workflow, MP3 player, profiles, headless mode, .bitchtearc,
@file tokens, mid-turn steering, Esc/Ctrl-C ladder, MCP setup.

---

### C.3 getting-started.md — check for completeness

Should cover: API key setup, first run, .bitchtearc, profiles, env vars.
Currently uses `getting_started.md` (underscore) — inconsistent with other
docs using hyphens.

**Action:** Also rename to `getting-started.md` for consistency.

---

### C.4 glossary.md — check for completeness

Should include: fantasy, catwalk, ContextKey, MemoryScope, FocusManager,
IRCContext, ToolContextManager, bitchteaTool, mcpAgentTool, HOT.md.

---

## D. DEPENDENCY GRAPH GAPS

### D.1 architecture.md graph is incomplete

**File:** `docs/architecture.md:9-29`

**Missing from graph:**
- `internal/catalog` (imported by `internal/llm/cost.go` and `main.go`)
- `internal/mcp` (imported by `internal/llm/mcp_tools.go` and `internal/agent/agent.go`)
- `internal/agent/event` (subpackage, imported by `internal/llm`)
- `internal/daemon/jobs` (subpackage)
- `charm.land/fantasy` (critical external dep)
- `charm.land/catwalk` (catalog dep)

**Action:** Add all missing nodes and edges.

---

## E. PHASE DOCS NEED STATUS BANNERS

All phase docs are design contracts written before implementation. They are
accurate as historical records but need a status line at the top indicating
whether the phase shipped.

| Doc | Status |
|-----|--------|
| `phase-3-message-contract.md` | SHIPPED (Phase 3 complete) |
| `phase-4-preparestep.md` | SHIPPED (PrepareStep implemented) |
| `phase-5-catalog-audit.md` | SHIPPED (Catalog implemented) |
| `phase-6-mcp-contract.md` | SHIPPED (MCP client implemented; resources/prompts/sampling future) |
| `phase-7-daemon-audit.md` | SHIPPED (Daemon rebuilt) |
| `phase-7-process-model.md` | SHIPPED (Daemon rebuilt) |
| `phase-8-cancellation-state.md` | SHIPPED (ToolContextManager implemented) |
| `phase-9-service-identity.md` | SHIPPED (Service field implemented) |

**Action:** Add `> Status: SHIPPED` banner to each.

---

## F. MISSING FROM printUsage()

`main.go:377-423` `printUsage()` lists 13 commands. The actual registry has
25 command groups (30 with aliases).

**Missing (user-facing, should be in --help):**
`/copy`, `/debug`, `/activity`, `/theme`, `/memory`, `/resume`,
`/join`, `/part`, `/query`, `/msg`, `/channels`, `/invite`, `/kick`

**Action:** Add at least the core commands. Consider grouping by category.

---

## G. CONFIG SETTINGS NOT DOCUMENTED

| Setting | Where it's mentioned | What's missing |
|---------|---------------------|----------------|
| `sound` | help text, /set handler | What it does, what sounds, when |
| `nick` | help text, /set handler | What it controls |
| `auto-next` | cli-flags.md, help text | Runtime behavior (magic tokens, follow-up injection) |
| `auto-idea` | cli-flags.md, help text | Same as auto-next |
| `service` | commands.md "Provider vs Service" | Not in user-guide.md |

---

## H. ENV VARS NOT IN DOCS

| Var | In --help | In docs/ |
|-----|-----------|----------|
| `BITCHTEA_CATWALK_URL` | Yes | No |
| `BITCHTEA_CATWALK_AUTOUPDATE` | Yes | No |
| `BITCHTEA_MODEL` | Yes | No |
| `BITCHTEA_PROVIDER` | Yes | No |

**Action:** Add to cli-flags.md or a new env-vars section.

---

## I. FILE NAMING INCONSISTENCY

`docs/getting_started.md` uses underscores. All other docs use hyphens
(`agent-loop.md`, `cli-flags.md`, etc.).

**Action:** Rename to `getting-started.md` and update any cross-references.
