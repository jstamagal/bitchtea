# Phase 2 — Duplication Report

**Date:** 2026-05-10
**Confidence:** HIGH — flowcharts give concrete `file:line` evidence for all claims.
**Known gaps:** Daemon job handlers (memory_consolidate, checkpoint) were only partially traced; additional duplications may exist in error paths not covered by the happy-path flowcharts.

---

## Cross-Feature Duplications

### D1: Atomic File Write (tmp + fsync + rename) — 3 locations

| Location | File | Line | Context |
|---|---|---|---|
| Session append | `internal/session/session.go` | 144 | `os.OpenFile` with `O_APPEND` |
| Daemon mailbox complete | `internal/daemon/mailbox.go` | 167 | `atomicWrite()` tmp+fsync+rename |
| Daemon mailbox fail | `internal/daemon/mailbox.go` | 138 | `atomicWrite()` tmp+fsync+rename |
| Catalog cache write | `internal/catalog/cache.go` | 66 | `CreateTemp` + `Rename` + `Sync` |
| Memory append hot | `internal/memory/memory.go` | 103 | `os.OpenFile` with `O_APPEND` + `flock` |
| Memory append daily | `internal/memory/memory.go` | 160 | `os.OpenFile` with `O_APPEND` + `flock` |

**Why they diverged:** Session and Memory use `O_APPEND` for append-only logs (no truncate step). Daemon and Catalog use truncate-w-then-rename because they overwrite the same path. Memory uses `flock` in addition because concurrent compaction may touch the same file.

**Verdict: LEGITIMATE SPECIALIZATION.** Append-only and replace-in-place have different semantics. Do not unify.

---

### D2: Inter-Process File Lock (syscall.Flock) — 3 locations

| Location | File | Line | Function |
|---|---|---|---|
| Session append | `internal/session/session.go` | 147–150 | `syscall.Flock(LOCK_EX/LOCK_UN)` |
| Memory hot/daily append | `internal/memory/memory.go` | 103 | `syscall.Flock(LOCK_EX)` in `AppendHot`/`AppendDailyForScope` |
| Daemon lock | `internal/daemon/lock.go` | 31 | `Acquire()` uses same pattern for daemon singleton |

**Why they diverged:** Three independent locking needs, each for different files. No shared abstraction existed at the time.

**Verdict: CANDIDATE FOR CONSOLIDATION.** A single `internal/lockfile/lock.go` package exporting `LockFile(path)` and `LockAppend(path)` would serve all three. However, the divergence is not harmful — each call site is its own two-line pattern and the stdlib syscall is stable.

---

### D3: Message Format Conversion (fantasy ↔ llm) — 2 locations, bidirectional

| Location | File | Line | Direction |
|---|---|---|---|
| `splitForFantasy` | `internal/llm/convert.go` | 16–80 | `[]Message` → fantasy types |
| `fantasyToLLM` | `internal/llm/convert.go` | 150–157 | fantasy result → `[]Message` |
| `fantasyToLLM` in session load | `internal/session/session.go` | 376–384 | fantasy → legacy llm for v0 entries |

**Why they diverged:** Two separate conversion needs: (a) streaming boundary at LLM client, and (b) session persistence. The session layer needs a lossy legacy fallback that the streaming layer doesn't need.

**Verdict: LEGITIMATE SPECIALIZATION.** Session persistence requires a lossy fallback for v0 entries; streaming does not. Different conversion functions are correct.

---

### D4: Per-Tool Timeout via context.WithTimeout — 2 locations

| Location | File | Line | Pattern |
|---|---|---|---|
| Tool registry Execute | `internal/tools/tools.go` | 504 | `toolCtx, toolCancel := context.WithTimeout(ctx, r.ToolTimeout)` |
| Bash executor | `internal/tools/tools.go` | 796 | `ctx, cancel = context.WithTimeout(ctx, timeout*time.Second)` — bash bypasses tool-level timeout |

**Why it diverged:** Bash tool manages its own timeout field; all other tools rely on `r.ToolTimeout` from the registry.

**Verdict: LEGITIMATE SPECIALIZATION.** Bash has a per-call timeout field; others use the registry default. The bypass is intentional.

---

### D5: Tool Namespacing Convention — 2 locations

| Location | File | Line | Convention |
|---|---|---|---|
| MCP tools | `internal/mcp/manager.go` | 525–526 | `mcp__<server>__<tool>` |
| Legacy typed tools | `internal/llm/typed_*.go` | various | `bdsh__<tool>` (bdsh prefix) |

**Why they diverged:** Two separate tool-naming systems: MCP uses `mcp__` for external servers; the fantasy typed wrappers use `bdsh__` for built-in shell tools. Different trust domains.

**Verdict: LEGITIMATE SPECIALIZATION.** External MCP servers vs. internal built-in tools are different trust domains. The prefix is correct.

---

### D6: Error Wrapping with Self-Correction Prompt — 1 location, reuse potential

| Location | File | Line | Pattern |
|---|---|---|---|
| Tool errors | `internal/tools/tools.go` | 31 | `wrapToolError(err)` → `<tool_call_error><cause>...<reflection>...</reflection></tool_call_error>` |

**Reuse potential:** The same pattern could be applied to MCP tool call failures and daemon job failures — both surface errors to the LLM that could benefit from a structured XML envelope with reflection prompt.

**Verdict: CANDIDATE FOR EXTRACTION.** `internal/tools/toolerror/wrap.go` with `func Wrap(err error) string` used by both tools and MCP would consolidate this. The `reflectionPrompt` constant is currently tools-internal.

---

### D7: Scope-Based Routing (Memory vs Daemon) — 2 locations

| Location | File | Line | Usage |
|---|---|---|---|
| Memory scopes | `internal/memory/memory.go` | 44–52 | `RootScope()`, `ChannelScope()`, `QueryScope()` constructors |
| Daemon memory_consolidate | `internal/daemon/jobs/memory_consolidate.go` | 111 | `buildScope(kind, name, parentKind, parentName)` |

**Why they diverged:** The memory package defines the authoritative scope types. The daemon's `buildScope` is a thin wrapper that calls the memory package constructors. This is a thin adapter, not real duplication.

**Verdict: NOT DUPLICATION.** The daemon correctly delegates to the memory package scope constructors. No action needed.

---

### D8: Config + Profile Merge — scattered

| Location | File | Line | Pattern |
|---|---|---|---|
| Config build | `internal/config/rc.go` | 193 | `applySetToConfig` for rc-set commands |
| Profile apply | `internal/config/config.go` | 480 | `ApplyProfile` merges profile into config |
| /set command | `internal/ui/commands.go` | 129 | `handleSetCommand` dispatches to both |

**Why it diverged:** Three separate code paths handle config mutation: (a) rc file parsing, (b) profile loading, (c) runtime /set commands. Each has its own field-level update logic.

**Verdict: CANDIDATE FOR CONSOLIDATION.** The rc.go `applySetToConfig` (lines 196–305) is a 110-line switch statement with near-identical structure to what `/set key value` does at runtime. A shared `MutateConfig(cfg, key, value) error` function could serve both. However, the two paths have different side-effect profiles (rc: no agent re-config; runtime: agent re-config), so some branching is unavoidable.

---

## Within-Feature Duplications

### W1: Agent Loop — 1666-line monolith

| File | Line | Concern |
|---|---|---|
| `internal/agent/agent.go` | 1–1666 | Contains: NewAgent, SendMessage, sendMessage, event loop, context switching, compaction, prompt draining, turn state machine, cost tracking. No internal separation. |

**Verdict: ARCHITECTURAL SMELL, NOT DUPLICATION.** The file is long but the concerns are distinct. The real issue is lack of internal file separation, not repeated code. Compaction, context switching, and the main loop could be split into `compact.go`, `context_switch.go`, and `event_loop.go` within the agent package.

---

### W2: Tool Registry — 4 named patterns in one file

The tools.go file defines 4 identifiable patterns that are well-documented but all in the same file:

1. **Pattern 1** (line 21): Structured error with reflection prompt — only used in tools.go
2. **Pattern 2** (line 46): Read-before-edit guard — only in tools.go
3. **Pattern 3** (line 54): Per-tool timeout — only in tools.go
4. **Pattern 4** (line 59): File-read tracking map — only in tools.go

**Verdict: NOT DUPLICATION.** These are intentional design patterns within one feature. Pattern 2 is the only one worth extracting for cross-feature reuse (e.g., session file overwrites).

---

### W3: TUI Model — 3 separate event-type switches

| Location | File | Line |
|---|---|---|
| Agent event switch | `internal/ui/model.go` | 831 |
| /set key dispatch | `internal/ui/commands.go` | 196–305 |
| StreamEvent type switch | `internal/llm/stream.go` | 283–336 |

**Verdict: NOT DUPLICATION.** These are different dispatch domains with no common root. Each is correct as-is.

---

## Summary Table

| ID | Type | Concern | Locations | Verdict |
|---|---|---|---|---|
| D1 | Cross | Atomic write (tmp+fsync+rename) | session:144, daemon:167, catalog:66, memory:103,160 | LEGITIMATE SPECIALIZATION |
| D2 | Cross | Flock (inter-process lock) | session:147, memory:103, daemon:lock.go | CANDIDATE — shared lockfile pkg |
| D3 | Cross | fantasy↔llm message conversion | convert.go:16, session:376 | LEGITIMATE SPECIALIZATION |
| D4 | Cross | Per-tool context.WithTimeout | tools.go:504,796 | LEGITIMATE SPECIALIZATION |
| D5 | Cross | Tool namespacing | mcp:525, typed_*.go | LEGITIMATE SPECIALIZATION |
| D6 | Cross | Error wrap + reflection prompt | tools.go:31 | CANDIDATE — extract to toolerror pkg |
| D7 | Cross | Scope-based routing | memory:44, daemon:111 | NOT DUPLICATION |
| D8 | Cross | Config field mutation | rc.go:193, config.go:480, commands.go:129 | CANDIDATE — shared MutateConfig |
| W1 | Within | 1666-line agent monolith | agent.go:1–1666 | ARCHITECTURAL SMELL (not duplication) |
| W2 | Within | 4 design patterns in tools.go | tools.go | NOT DUPLICATION |
| W3 | Within | 3 separate type switches | model.go, commands.go, stream.go | NOT DUPLICATION |

## Proposed Priority

1. **D6 (tool error wrap extraction)** — Low effort, high utility. `toolerror.Wrap(err)` used by tools AND MCP.
2. **D8 (MutateConfig consolidation)** — Medium effort, eliminates rc vs runtime dispatch divergence.
3. **D2 (shared lockfile package)** — Low effort but low urgency; current duplication is not harmful.
4. **W1 (agent internals split)** — Refactor only if further work touches agent.go; splitting a 1666-line file that isn't duplicated is not worth it proactively.
