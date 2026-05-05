# Project Status — Snapshot

**Date:** 2026-05-04

This is the "where are we right now" file. It is a manually maintained snapshot,
not auto-generated. The authoritative live state lives in `bd` (the beads issue
tracker — `bd ready`, `bd list`, `bd stats`) and the phase docs in this
directory. Use this file to orient before reading deeper.

## Headline

The audit-driven cleanup wave is complete. Phases 3–9 are shipped per their own
status banners. The codebase is hybrid `fantasy` + legacy `llm.Message` only at
the `Client.StreamChat` boundary; everywhere else (agent history, session log,
tool dispatch for the typed tools) is fantasy-native.

## Phase status

| Phase | Doc | Status |
|---|---|---|
| 3 — Fantasy message contract | `archive/phase-3-message-contract.md` | SHIPPED with residuals (StreamChat boundary still `[]llm.Message`; ProviderOptions persistence deferred) |
| 4 — PrepareStep responsibilities | `archive/phase-4-preparestep.md` | SHIPPED |
| 5 — Catwalk catalog audit | `archive/phase-5-catalog-audit.md` | SHIPPED with residual (background catalog refresh via daemon NOT wired) |
| 6 — MCP transport / config / security | `archive/phase-6-mcp-contract.md` | SHIPPED for client; resources/prompts/sampling future |
| 7 — Daemon audit + process model | `archive/phase-7-daemon-audit.md`, `archive/phase-7-process-model.md` | SHIPPED |
| 8 — Per-tool cancellation state machine | `archive/phase-8-cancellation-state.md` | SHIPPED |
| 9 — Service identity field | `archive/phase-9-service-identity.md` | SHIPPED |

Phase docs are *design contracts*, not changelogs. They describe what the
architecture must satisfy. Their rationale has been ported into the live
maintainer docs above; the contracts themselves now live under `archive/`
as the reference for the design decisions baked in.

## Known loose ends

These are the items CLAUDE.md still flags in its "In flight" section. Read the
code, not the framing, for current state.

- **Fantasy migration — Phase 2 (tools), partial.** Six tools have typed
  `fantasy.NewAgentTool` wrappers under `internal/llm/typed_*.go`: `read`,
  `write`, `edit`, `bash`, `search_memory`, `write_memory`. Eight still flow
  through the legacy generic `bitchteaTool` adapter: `terminal_start`,
  `terminal_send`, `terminal_keys`, `terminal_snapshot`, `terminal_wait`,
  `terminal_resize`, `terminal_close`, `preview_image`. `translateTools`
  selects the typed wrapper when one exists; both paths bottom out in
  `Registry.Execute(name, argsJSON)`.
- **`Client.StreamChat` boundary.** Still takes `[]llm.Message`. The agent
  bridges across via `llm.FantasySliceToLLM` outgoing and `llm.LLMToFantasy`
  incoming. Removing this glue is the last fantasy-migration item.
- **`ProviderOptions` persistence on session entries.** Deferred — not on the
  current sprint.
- **Per-context histories — residual gaps.** The agent slice swap is shipped
  (`internal/agent/context_switch.go`), but: (a) `/join` / `/query` themselves
  don't switch agent context — the swap happens lazily on the next
  `sendToAgent`; (b) the TUI prompt queue (`m.queued`) is a single global
  slice, not per-context; (c) `Compact()` operates on the active context only;
  (d) save watermarks are per-context but the daemon `session-checkpoint` job
  is still session-global.
- **`bt-p6-verify`, `bt-test.15`, `bt-test.16`, `bt-wire.10`, `bt-wire.4` —
  in_progress.** See `bd show <id>` for details.

## Architectural anchors (don't violate)

- **Acyclic package dependency graph.** See `architecture.md` for the
  authoritative diagram. Adding an upward edge (e.g. `tools -> llm`) is wrong.
- **`Update()` must stay non-blocking.** Long work goes in `tea.Cmd`s /
  goroutines that send messages back to the Bubble Tea loop.
- **No artificial guardrails on tools.** `bash`, `terminal_*`, and the file
  tools are intentionally powerful. The user owns the risk.
- **Append-only session log.** Resume, list, fork, and tree all assume it.
  Schema changes need a deliberate migration.
- **flock discipline on session and memory writes.** Both the TUI and the
  daemon are valid writers. The kernel-level locks are the load-bearing
  invariant that makes that safe.

## Where to read next

- `architecture.md` — package map and dependency graph
- `agent-loop.md` — turn lifecycle, event flow, follow-up logic
- `streaming.md` — `Client.StreamChat`, conversion shims, cost tracking
- `tools.md` — built-in tool surface and the typed/legacy adapter split
- `daemon.md` — daemon binary, mailbox IPC, registered jobs
- `memory.md` — scoped memory store, hot vs daily, search/write
- `sessions.md` — JSONL format, resume/fork/tree, checkpoints
- `mcp.md` — MCP client, security gates, opt-in layers
- `commands.md` — every slash command and its exact behavior
- `signals-and-keys.md` — Esc / Ctrl+C ladders, picker keys, MP3 controls
- `cli-flags.md`, `getting-started.md`, `user-guide.md` — user-facing
- `testing.md`, `troubleshooting.md`, `glossary.md`, `development-guide.md` — maintainer
- `providers.md`, `catalog.md` — model registry and provider routing
- `ui-components.md` — TUI components and styling
