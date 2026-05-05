# bitchtea — Documentation Index

Maintainer-facing documentation. The user-facing entrypoint is the top-level
[README.md](../README.md).

**Start here:** [STATUS.md](STATUS.md) — current snapshot of where the project
is at (phase status, known loose ends, in-flight items).

## Architecture & Runtime

| Doc | What it covers |
|---|---|
| [architecture.md](architecture.md) | Package dependency graph, runtime state machine, foundational truth |
| [agent-loop.md](agent-loop.md) | Turn lifecycle, event flow, autonomous follow-ups, context switching |
| [streaming.md](streaming.md) | `Client.StreamChat`, fantasy / `llm.Message` conversion, cost tracking |
| [tools.md](tools.md) | Built-in tool surface, typed vs legacy adapters, schema generation |
| [memory.md](memory.md) | Scoped memory store (Root / Channel / Query), hot vs daily, search and write |
| [sessions.md](sessions.md) | JSONL session format, resume / fork / tree, checkpoints, membership |
| [daemon.md](daemon.md) | Daemon binary, mailbox IPC, registered jobs, lifecycle |
| [mcp.md](mcp.md) | MCP client, security gates, opt-in layers |

## Phase Contracts (design history)

These are design contracts written *before* the work landed. They stay in the
tree as the reference for the architectural decisions baked in. All eight have
**SHIPPED** banners as of 2026-05-04 (Phase 6 client-side only).

| Doc | What it locks down |
|---|---|
| [phase-3-message-contract.md](phase-3-message-contract.md) | Fantasy-native in-memory and on-disk message contract |
| [phase-4-preparestep.md](phase-4-preparestep.md) | `PrepareStep` ownership: cache, queue drain, tool refresh |
| [phase-5-catalog-audit.md](phase-5-catalog-audit.md) | Catwalk fetch, on-disk cache, ETag, offline behavior |
| [phase-6-mcp-contract.md](phase-6-mcp-contract.md) | MCP transport, config layout, security checklist |
| [phase-7-daemon-audit.md](phase-7-daemon-audit.md) | What the daemon may reuse from existing exports |
| [phase-7-process-model.md](phase-7-process-model.md) | Daemon lifecycle, IPC, locking, crash recovery |
| [phase-8-cancellation-state.md](phase-8-cancellation-state.md) | Per-tool cancellation state machine (Esc x1 vs x2 vs x3) |
| [phase-9-service-identity.md](phase-9-service-identity.md) | `Service` field separate from wire-format `Provider` |

## User-Facing References

| Doc | What it covers |
|---|---|
| [getting-started.md](getting-started.md) | First-run install / configure / launch |
| [user-guide.md](user-guide.md) | Day-to-day usage, all features end-to-end |
| [cli-flags.md](cli-flags.md) | Every command-line flag |
| [commands.md](commands.md) | Every slash command and its exact behavior |
| [signals-and-keys.md](signals-and-keys.md) | Esc / Ctrl+C ladders, picker keys, MP3 controls |
| [providers.md](providers.md) | Provider routing, env detection, built-in profiles |
| [catalog.md](catalog.md) | Model catalog system, refresh, embedded fallback |
| [ui-components.md](ui-components.md) | TUI components and styling |

## Maintainer References

| Doc | What it covers |
|---|---|
| [testing.md](testing.md) | Test conventions, fake streamer, integration patterns |
| [troubleshooting.md](troubleshooting.md) | Common failure modes and fixes |
| [glossary.md](glossary.md) | Project-specific terminology |
| [development-guide.md](development-guide.md) | Contributing notes and operational reality |

## Cross-References

- [CLAUDE.md](../CLAUDE.md) — developer workflow instructions injected at agent startup
- [AGENTS.md](../AGENTS.md) — runtime persona and interaction model injected at agent startup
- [CONTRIBUTING.md](../CONTRIBUTING.md) — quality gates, commit policy, dependency rules
