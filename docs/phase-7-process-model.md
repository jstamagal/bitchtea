# Phase 7: Daemon Process & IPC Model

Status: design only. Builds directly on `docs/phase-7-daemon-audit.md` (the keep/delete/rewrite map). Implementation is split across `bt-p7-cli` (binary + flag plumbing) and `bt-p7-session-jobs` (compaction/consolidation jobs). This doc nails down lifecycle, IPC, locking, and crash recovery — the contract those tasks must satisfy.

## Overview

The daemon is the home for work the TUI can't or shouldn't do inline: background compaction, periodic memory consolidation, idle-time summarization, and cross-session knowledge stitching (audit §"Current runtime needs not yet served"). It is **opt-in, additive, and per-workspace**. The TUI must remain fully usable without it; nothing the daemon does is on the critical path of a turn.

The daemon and TUI are separate processes. They share state only via files in `~/.bitchtea/`, with the same flock discipline `internal/session` and `internal/memory` already use (audit §"Reusable pieces"). There is no Go API surface, no shared goroutines, no in-process channel — discovery is on-disk.

## Single-instance behavior

One daemon per user, system-wide. Per-workspace daemons would multiply pid-file bookkeeping for no real benefit; the daemon already keys all per-workspace work via the `WorkDir` argument inside each job envelope.

- **Pid file**: `~/.bitchtea/daemon.pid`. Contains the daemon PID as a single decimal line.
- **Lock**: `flock(LOCK_EX|LOCK_NB)` on `~/.bitchtea/daemon.lock` (separate file, held for the daemon's lifetime). The pid file is informational; the flock is authoritative. If acquiring the lock fails with `EWOULDBLOCK`, another daemon is live — exit 0 with `daemon already running (pid N)` printed to stderr.
- **Stale pid**: pid file exists but no flock holder. On startup the daemon takes the lock, overwrites the pid file, and proceeds. No "is the pid still alive" probe — flock subsumes the check (a dead process releases its kernel locks).
- **TUI discovery**: the TUI reads `daemon.pid` and (optionally) `kill(pid, 0)` to render `/daemon status`. It never tries to acquire the lock; that is daemon-only.

The pid file lives at `~/.bitchtea/daemon.pid` rather than `~/.bitchtea/daemon/daemon.pid` so the TUI can find it without knowing the mailbox layout.

## Lifecycle

### Start

Manual launch only for v1: `bitchtea daemon start` (handled by `bt-p7-cli`). No auto-spawn from the TUI; users opt in explicitly. No systemd unit shipped — users who want auto-start wire their own. Auto-restart on crash: out of scope.

Startup sequence:

1. Acquire `~/.bitchtea/daemon.lock` (single-instance check).
2. Call `config.MigrateDataPaths()` — daemon may be the first writer in a fresh install.
3. Create `~/.bitchtea/daemon/{mail,done,failed}/` with mode `0700`.
4. Write pid file.
5. Crash recovery scan (see below).
6. Enter run loop.

### Stop

`SIGTERM` (and `SIGINT`, identical handling) triggers graceful drain:

1. Stop accepting new mailbox entries (mark loop done).
2. Let any in-flight job finish; budget 30s. Unfinished jobs at deadline get moved to `failed/` with `error: "shutdown deadline"`.
3. Remove pid file. Release flock (implicit on exit).
4. Exit 0.

`SIGKILL` leaves the pid file behind; the next start treats it as stale per above. Mail entries that were mid-process stay in `mail/` and are picked up on the next start (see Crash recovery).

### Restart

`bitchtea daemon restart` is `stop` then `start`, both via the lock. Idempotent: a restart issued while no daemon is running is identical to `start`. The mailbox is the source of truth across restarts — pending requests survive.

### Auto-restart

Out of scope. The user's init system (or their fingers) is responsible. The audit's "value-add, not gap-fill" framing applies: a daemon that's down means the TUI behaves as it does today, which is fine.

## Logs & data paths

Layout, anchored to `~/.bitchtea/`:

```
~/.bitchtea/
  daemon.pid                       written on start, removed on graceful stop
  daemon.lock                      flock target, never read
  daemon.log                       stdout + stderr (combined), append-only
  daemon.json                      daemon's own profile/model config (audit OQ)
  daemon/
    mail/<ulid>.json               jobs, TUI-written
    done/<ulid>.json               results, daemon-written
    failed/<ulid>.json             jobs the daemon refused or timed out
  sessions/
    daemon_<ts>.jsonl              daemon-authored background turns (audit §"State ownership split")
```

- **Logs**: stdout and stderr are redirected into `daemon.log` (line-buffered). No rotation in v1. Users who care wire `logrotate` themselves; the file is plain text and append-only so rotation is trivial.
- **Job records**: `mail/`, `done/`, `failed/` are the queue. Each is one JSON file per job, never edited in place, only renamed across directories. There is no separate journal.
- **`daemon.json`**: the daemon's own model/profile, so it can run before any TUI session has populated `~/.bitchtearc`. Schema follows whatever `bt-p7-cli` lands on; not specified here.

## IPC contract — file mailbox v1

Per audit §"IPC surface", v1 is a file-watched mailbox. JSON files, atomic rename, three directories.

### Directory roles

| Dir | Writer | Reader | Lifecycle |
|-----|--------|--------|-----------|
| `mail/` | TUI (and any future requestor) | Daemon | File created, daemon picks it up, file moves out. |
| `done/` | Daemon | TUI (poll) | Stays until requestor reads it; `bt-p7-session-jobs` decides retention. Default: keep last 100, prune older on daemon start. |
| `failed/` | Daemon | TUI (poll) + human | Stays indefinitely; user grooms by hand. |

### Filename convention

`<ulid>.json`. ULIDs sort by creation time and embed enough randomness to avoid collisions across requestors. The TUI uses one source of ULIDs (`internal/agent` already has a clock for IDs); the daemon does not generate ULIDs for inbound work, only for `done/` results matching the inbound ULID.

Result file uses the **same** ULID as the request: `mail/01H7…json` → `done/01H7…json`. Trivial correlation, no envelope-side ID needed.

### Write atomicity

All writes go to `<dir>/<ulid>.json.tmp`, `fsync`, then `rename` to `<dir>/<ulid>.json`. POSIX `rename` is atomic within a filesystem, which `~/.bitchtea/` always is. The reader (daemon for `mail/`, TUI for `done/`) only ever sees fully-written files.

The daemon moves a completed job from `mail/` to `done/` (or `failed/`) with a single `rename` across the two directories — same-filesystem guarantee again.

### Polling cadence vs fsnotify

Default: **`fsnotify` watch on `mail/` with a 5s fallback poll**. `fsnotify` gives sub-100ms latency on Linux without burning wakeups, and the 5s poll covers (a) NFS or other backends where `fsnotify` is unreliable, (b) events lost during a watch reset, (c) portability if anyone ever runs the daemon on macOS. This pair lands well below the audit's 1s "feels right" target without the wakeup cost.

If `fsnotify` setup fails outright (no inotify, exhausted user limit), the daemon logs once and falls back to a 1s pure poll.

### Job envelope

```json
{
  "kind": "compact",
  "args": { "...": "..." },
  "work_dir": "/abs/path/to/checkout",
  "session_path": "/abs/path/to/sessions/2026-05-01_120000.jsonl",
  "scope": { "kind": "channel", "name": "main" },
  "requestor_pid": 12345,
  "submitted_at": "2026-05-01T12:00:00Z",
  "deadline": "2026-05-01T12:10:00Z"
}
```

- `kind`: job type; `bt-p7-session-jobs` registers handlers (`compact`, `consolidate`, ...). Unknown kinds → `failed/` with `error: "unknown kind"`.
- `args`: opaque to the daemon shell; passed through to the handler.
- `work_dir`, `session_path`, `scope`: the same triple `internal/session` and `internal/memory` already key on. Required so the daemon doesn't have to guess workspace.
- `requestor_pid`: informational, used for `kill(pid, 0)` checks if the daemon ever needs to know whether the requestor still cares (see Crash recovery).
- `deadline`: hard timeout for the handler; the daemon enforces it with `context.WithDeadline`. Missing/zero deadline → no enforcement (handler runs to completion).

Envelope unmarshals with `DisallowUnknownFields` — typo'd keys fail loudly, same convention as `mcp.json` in phase 6.

### Result envelope

```json
{
  "success": true,
  "kind": "compact",
  "output": "...handler-defined...",
  "error": "",
  "started_at": "2026-05-01T12:00:01Z",
  "finished_at": "2026-05-01T12:00:42Z"
}
```

`success: false` files still go to `done/` (the job ran to completion with an error); `failed/` is reserved for jobs the daemon refused to run (unknown kind, malformed envelope, deadline-at-shutdown). This split lets the TUI surface "tried and failed" differently from "couldn't even start".

## Locking

The daemon respects the same flock the TUI uses:

- **`session.Append`** holds `flock(LOCK_EX)` on the JSONL file. The daemon never calls `Append` on a TUI-owned session (audit §"Deleted assumptions"); it writes its own `daemon_<ts>.jsonl` files. The flock invariant means *if* it ever did, kernel-level serialization would already make it safe.
- **`memory.AppendHot` / `memory.AppendDailyForScope`** hold `flock(LOCK_EX)` on the target file. The daemon calls these directly (no `write_memory` tool yet — `bt-vhs` open). Concurrent TUI writes interleave at the entry boundary, never mid-line.
- **Daemon's own writes** to `done/` and `failed/` are atomic-rename, so no flock needed — there is one daemon writer per file.

Single rule restated: **the daemon must never append to an active TUI session JSONL.** Reading is fine (`Load`, `FantasyFromEntries`), and is in fact how `compact` works.

## Crash recovery

Three failure shapes, three policies.

### Daemon crash with jobs in flight

On the next start, the recovery scan walks `mail/`:

- Files older than the daemon's start time: assume the previous instance was processing them when it died. Move to `failed/` with `error: "previous daemon crashed mid-job"`. **Do not requeue** — per audit OQ, requeuing can loop on input that crashes the daemon.
- Files newer than start time: leave for the run loop to pick up normally.

The "older than start time" check uses file mtime, not envelope `submitted_at` — mtime is harder to spoof and correctly survives clock skew. Mtime is set by `rename` at write time, so it represents the moment the file became visible.

### TUI restart while daemon is running

The TUI does not "own" any in-flight job; it only knows the ULIDs it submitted. On restart, it reads `done/` for any ULIDs it remembers from its session log (the requestor records the ULID on submit) and surfaces results into the transcript. ULIDs it does not remember are ignored — another bitchtea instance may have submitted them.

No duplicate-work risk: the daemon doesn't care who reads `done/`, and the TUI doesn't resubmit jobs whose ULIDs it can match against `done/` or `failed/`.

### Both crash

`mail/` retains pending requests, `done/` retains completed results, sessions and memory remain intact (their writers are append-only with flock). Nothing in flight survives the daemon-side handler — same as the daemon-only crash case. Any TUI that comes back up replays from session JSONL the same way it does today; the daemon picks up where the queue left off.

## TUI behavior when daemon absent

The daemon is opt-in and additive. If it's not running:

- **All existing functionality works unchanged.** No fallback compaction path, no degraded-mode warnings on startup, no nag.
- **No in-process compaction stand-in.** The audit notes that `agent.Compact()` already runs in-process when invoked from a turn; that path is the "no daemon" answer for now. Background/idle compaction is daemon-only and silently unavailable. Explicit out-of-scope: a future "spawn a one-shot job in this process if no daemon" mode.
- **Status surface**: when the user has *opted in* (per-workspace marker per audit OQ — leaning `<WorkDir>/.bitchtea/daemon-enabled`) and the daemon is absent, the TUI's background-activity status line shows `(no daemon)`. Without the marker, no status line entry — silent.
- **`/daemon status` slash command** (lands with `bt-p7-cli`) is the explicit query: prints pid, uptime, last job, mailbox depth. With no daemon: `not running`. No further action.

If the user submits a job (e.g. via a future `/compact-bg`) and no daemon is up: the TUI writes to `mail/` anyway and prints `queued for daemon (not currently running)`. Next daemon start drains the queue. This is the intended behavior — the mailbox is durable.

## Open questions

- **Mail retention in `done/`**: 100 entries is a guess. Real number depends on how chatty `bt-p7-session-jobs` makes the daemon. Revisit after first measurement.
- **`failed/` review UX**: human-grooms-by-hand is fine for v1, but a `/daemon failed` slash command that lists and prunes would be cheap to add. Defer to `bt-p7-cli`.
- **Per-workspace opt-in marker** (carried from audit OQ): leaning `<WorkDir>/.bitchtea/daemon-enabled`, mirroring phase 6's `mcp.json` opt-in. Final placement decided when `bt-p7-cli` lands the marker check.
- **Job cancellation** (carried from audit OQ): `cancel/<ulid>.json` written by the requestor, polled by the daemon. Cheap to add later if needed; not v1.
- **fsnotify on macOS**: design assumes Linux/inotify. macOS users get the 1s poll fallback. Whether we ship a daemon binary for macOS at all is a `bt-p7-cli` question.
- **Result delivery to a *running* TUI** (carried from audit OQ): this doc commits the TUI to polling `done/`. Cadence is a UI decision — proposed default 2s while a known ULID is outstanding, no polling otherwise. Final answer in `bt-p7-cli`.
