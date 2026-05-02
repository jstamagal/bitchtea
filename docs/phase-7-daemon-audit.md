# Phase 7: Daemon Integration Audit

Status: design only. Implementation tracked under epic `bt-p7` (`bt-p7-attic-audit` is this doc; `bt-p7-process-model` and `bt-p7-verify` build on it).

## Context

The previous `_attic` daemon was deleted during compaction (verified by `bt-76w`: no Go, mod, doc, or config refs remain). This audit treats the current TUI-only state as the baseline and maps what a future Phase 7 daemon would integrate with — *not* what the prior one did.

Cross-references in CLAUDE.md ("In flight" section) are authoritative: there is no `cmd/daemon`, no `internal/daemon`, no daemon binary, no IPC surface, no hardcoded compaction model. Anything that contradicts that is wrong about the current code.

## Reusable pieces (keep as-is, daemon builds on them)

These exports already do what a daemon would need; no rewrite required.

| Symbol | File | What a daemon uses it for |
|--------|------|---------------------------|
| `session.Append` | `internal/session/session.go:109` | Append-only JSONL with `sync.Mutex` + `flock` — already multi-process safe. |
| `session.Load`, `session.List`, `session.Latest` | `internal/session/session.go:84,231,280` | Read sessions the daemon did not author. |
| `session.MessagesFromEntries` | `internal/session/session.go:344` | Reconstruct `[]llm.Message` from JSONL for off-loop replay. |
| `session.SaveCheckpoint` | `internal/session/session.go:290` | Lightweight `.bitchtea_checkpoint.json` is exactly the shape a daemon would emit between turns. |
| `memory.AppendHot` | `internal/memory/memory.go:92` | flock-serialized append to scoped `HOT.md`. |
| `memory.AppendDailyForScope` | `internal/memory/memory.go:155` | flock-serialized append to scoped daily `YYYY-MM-DD.md`. |
| `memory.SearchInScope` | `internal/memory/memory.go:200` | Read path; daemon can run searches without coordinating with the TUI. |
| `memory.Scope` lineage | `internal/memory/memory.go:354` | Root / Channel / Query routing already keyed by workspace hash. |
| `config.MigrateDataPaths` | `internal/config/config.go:397` | Daemon must call this before touching `~/.bitchtea/` (or assume the TUI ran first). |

The flock discipline on session and memory writes is the load-bearing invariant. A second writer (the daemon) is already safe today because the locks are kernel-level, not in-process.

## Deleted assumptions (do NOT carry forward)

The prior daemon presumably assumed these. They are false now and must stay false unless explicitly re-litigated.

- **Hardcoded compaction model.** No model name appears in the current compaction path (`agent.Compact`, `agent.go:549`). It uses `a.streamer`, which is the same client the TUI uses. Phase 7 must not re-introduce a hardcoded "Opus-only compaction" lock — model choice belongs to config/profile.
- **Daemon-owned session writes.** `session.Append` is called from the agent goroutine inside the TUI process today. A daemon that *steals* session ownership breaks resume, fork, and tree. Daemon may write *new* JSONL files (its own background turns) but must not interleave into a TUI-owned session file.
- **Implicit always-on background work.** No periodic compaction, idle sweep, or memory consolidation runs today. Phase 7 must not assume code already exists to call into.
- **Per-context message isolation.** `bt-x1o` is open: the agent still has one shared `messages` slice. A daemon that assumes per-channel histories will produce wrong context. Wait on `bt-x1o` or scope around it explicitly.
- **`write_memory` tool.** `bt-vhs` is open. Daemon must not call a tool that does not exist; it must use `memory.AppendHot` / `memory.AppendDailyForScope` directly.
- **Fantasy-typed tools / messages.** Phases 2–4 incomplete. Daemon-side tool execution must go through the same untyped `tools.Registry.Execute(name, argsJSON)` boundary the TUI uses, or wait until those phases land.
- **In-memory IPC.** No daemon channels, no goroutine-shared state with the TUI. Daemon and TUI are separate processes; everything is files or sockets.

## Current runtime needs not yet served

What the daemon was *for*, that nothing does today:

1. **Background compaction.** `agent.Compact()` runs only when invoked from the active turn — there is no scheduler. Long-lived sessions grow until the user runs `/compact` or hits a context limit mid-turn.
2. **Periodic memory consolidation.** Daily files accumulate; nothing rolls them up into weekly/monthly summaries or prunes redundant entries.
3. **Idle-time work.** When the TUI is at the input prompt with no active turn, the process sits idle. There is no "while you're afk, summarize this" path.
4. **Cross-session knowledge stitching.** Each session is independent; no process reads multiple `~/.bitchtea/sessions/*.jsonl` files to extract cross-session patterns into root memory.
5. **Crash-resume scaffolding.** `session.SaveCheckpoint` exists but is never called from the TUI today. A daemon could be the writer.

None of these are P0 — the TUI works without them. Phase 7 is value-add, not gap-fill.

## Proposed package boundaries

```
cmd/daemon/                       new binary, separate from `bitchtea`
  main.go                         flag parsing, ~/.bitchtea/daemon.pid, signal handling
internal/daemon/                  new package
  daemon.go                       run loop: read mailbox, dispatch jobs
  job.go                          Job type: Kind, Scope, Args, deadline
  mailbox.go                      file-watched job intake (see IPC below)
  log.go                          ~/.bitchtea/daemon.log writer
internal/daemon/jobs/             one file per job kind
  compact.go                      reads session JSONL, calls memory.AppendDaily
  consolidate.go                  rolls daily files into summaries
```

Dependency direction (additive, no cycles):

```
cmd/daemon -> internal/daemon -> internal/{session, memory, llm, config, tools}
```

`internal/daemon` is the *only* new internal package. It depends on the same packages the agent depends on, in the same direction, so the existing acyclic graph stays valid.

The TUI does **not** import `internal/daemon`. Discovery is via the on-disk mailbox path, not a Go API.

### IPC surface

Three options, ranked by simplicity:

1. **File-watched mailbox** (preferred for v1). TUI drops `~/.bitchtea/daemon/mail/<ulid>.json` describing a job. Daemon polls the directory every N seconds (or uses `fsnotify`). Result lands in `~/.bitchtea/daemon/done/<ulid>.json`. Pros: zero protocol code, survives daemon restart, debuggable with `cat`. Cons: latency floor of the poll interval.
2. **Unix socket**. Daemon listens on `~/.bitchtea/daemon.sock`. TUI dials, sends a single JSON-RPC request, reads response. Pros: low latency, request/response semantics. Cons: requires connection lifecycle, retry logic, daemon-down handling.
3. **Local HTTP**. Daemon binds `127.0.0.1:<port>`. Pros: trivially testable with `curl`. Cons: port allocation, firewall noise, overkill.

Pick (1) for v1. Re-evaluate when latency matters. (2) is the obvious upgrade path; (3) is rejected.

### State ownership split

| State | Owner | Notes |
|-------|-------|-------|
| Active session JSONL (`<ts>.jsonl`) | TUI | Daemon may *read* but never append. |
| `.bitchtea_checkpoint.json` | TUI writes; daemon reads | Existing helper, no contract change. |
| `.bitchtea_focus.json` | TUI | Daemon does not need it. |
| Per-workspace `MEMORY.md` | TUI writes; daemon may append via `memory.AppendHot` | flock makes this safe. |
| Scoped `HOT.md` and daily files | Either, via `memory.AppendHot` / `memory.AppendDailyForScope` | flock-serialized. |
| `~/.bitchtea/daemon/mail/`, `~/.bitchtea/daemon/done/` | Daemon | TUI writes job requests, daemon writes results. |
| `~/.bitchtea/daemon.pid`, `~/.bitchtea/daemon.log` | Daemon | TUI may read for `/daemon status`. |
| Daemon-authored sessions (background turn output) | Daemon | New `~/.bitchtea/sessions/daemon_<ts>.jsonl` files; named so they sort distinctly. |

## Out of scope

- **Model selection for daemon jobs.** Out by user instruction. Daemon reads model from its own config file or a per-job arg, no hardcoding.
- **Compaction algorithm changes.** This phase wires *where* compaction runs; the algorithm in `agent.Compact` is reused as-is.
- **Session ownership transfer.** Daemon never takes ownership of a TUI-owned session.
- **Distributed / multi-host daemon.** Local single-host only.
- **Auth.** Single-user system; mailbox files inherit `0700` on `~/.bitchtea/daemon/`.
- **Tool execution from background jobs that mutate the workspace** (`write`, `edit`, `bash`). Defer until a clear use case appears; v1 daemon jobs are read-only over `~/.bitchtea/`.

## Open questions

- Mailbox poll interval: 1s feels right for "user just typed /compact-bg" UX, but burns wakeups on idle laptops. Maybe `fsnotify` with a 5s fallback poll for portability.
- Does the daemon need its own copy of `config.Config`, or does it read whatever the most recent TUI session wrote? Leaning: daemon has a tiny `~/.bitchtea/daemon.json` for its own model/profile so it can run when no TUI has ever started in the workspace.
- Job cancellation: if the TUI drops `cancel/<ulid>.json` mid-job, does the daemon honor it? Probably yes; cheap to implement, painful to add later.
- Result delivery to a *running* TUI: does the daemon re-use the background-activity status-line surface (`m.backgroundActivity` in `internal/ui/model.go:107`)? That requires the TUI to poll `done/`. Acceptable; defer the polling cadence to `bt-p7-process-model`.
- Should the daemon be opt-in per-workspace via a marker file (`<WorkDir>/.bitchtea/daemon-enabled`) the way Phase 6 gates MCP? Strongly leaning yes — silent background processes surprise users.
- Crash-resume: if the daemon dies mid-job, is the mail file requeued or moved to `failed/`? `failed/` keeps the queue moving; requeue can loop on bad input.
