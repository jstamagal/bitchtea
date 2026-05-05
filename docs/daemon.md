# Daemon

bitchtea ships a background daemon for work the TUI should not do inline:
background session checkpointing, periodic memory consolidation, and future
idle-time operations. It is **opt-in, additive, and per-workspace**. The TUI
remains fully usable without it — nothing the daemon does is on the critical
path of a turn.

## Entry Points

bitchtea has two daemon entry points with different use cases:

### `bitchtea daemon start` (in-process)

Entry point: `daemon_cli.go` (function `runDaemon` / `daemonStart`).

This runs the daemon code in-process within the main `bitchtea` binary. The
terminal blocks until SIGINT/SIGTERM. This is the user-facing path for v1 —
manual launch only, no detach. Users who want backgrounding use
`nohup bitchtea daemon start &` or wire systemd/launchd.

Subcommands:
- `start` — Run the daemon in the foreground. Acquires the lock, initializes
  the mailbox, and enters the poll loop. Ctrl-C stops it.
- `status` — Check the lock file and report pid. Prints `running (pid N)` or
  `not running`. Exits 0 either way (query, not probe).
- `stop` — Read the pid file and send SIGTERM. Reports `not running` if the
  pid file is missing or the process is dead, with exit 0 so scripted callers
  can chain `daemon stop && daemon start`.

Entry point: `daemon_cli.go:33` (`runDaemon`). The subcommand dispatcher at
`daemon_cli.go:38` handles `start`, `status`, `stop`, and `help`.

### `cmd/daemon/main.go` (standalone binary)

A thin shim around `internal/daemon.Run`. Builds as a separate binary
(`cmd/daemon`). This is a dedicated executable for init systems (systemd,
launchd) that want a daemon binary without dragging the TUI in.

The standalone binary produces a smaller binary and avoids importing any TUI
packages. It writes to the same lock file, pid file, and mailbox directories
as the in-process path — they are interchangeable.

### Relationship

Both entry points call the same `daemon.Run()` with the same `jobs.Handle`
dispatcher. The on-disk state (lock, pidfile, mailbox) is identical regardless
of which entry point was used. You can start the daemon via
`bitchtea daemon start`, kill it, and restart via `cmd/daemon` with no
difference in behavior.

The `cmd/daemon` binary is not shipped by default; it is an alternative for
packaging or init-system integration.

## Lifecycle

### Startup sequence

When `daemon.Run` is called (`internal/daemon/run.go:43`), the following steps
execute in order:

1. **Validate options** (`run.go:44-52`). `BaseDir` is required. `PollEvery`
   defaults to 5s. `DrainBudget` defaults to 30s.
2. **Acquire lock** (`run.go:60-63`). Exclusive non-blocking flock on
   `~/.bitchtea/daemon.lock`. If the lock is held by another process,
   `ErrLocked` is returned immediately.
3. **Init mailbox** (`run.go:66-69`). Creates `mail/`, `done/`, `failed/`
   directories under `~/.bitchtea/daemon/` with mode 0700.
4. **Write pidfile** (`run.go:71-73`). Writes `os.Getpid()` to
   `~/.bitchtea/daemon.pid` via atomic tmp+rename.
5. **Crash recovery scan** (`run.go:82-85`). Walks `mail/` and moves entries
   with mtime older than the daemon's start time to `failed/` with diagnostic
   `"previous daemon crashed mid-job"`. Non-fatal: errors are logged but the
   daemon continues.
6. **Signal handling** (`run.go:89-103`). SIGTERM and SIGINT are captured and
   translated to context cancellation for graceful drain.
7. **Initial process** (`run.go:114`). A synchronous `processOnce` call drains
   any mail entries that arrived during the window between the recovery scan
   and signal registration.
8. **Poll loop** (`run.go:116-128`). Ticks every `PollEvery` interval,
   calling `processOnce` each tick. Exits when the context is cancelled.

### Graceful stop

SIGTERM and SIGINT trigger the same sequence:

1. Context is cancelled (`run.go:99`).
2. The poll loop sees `<-loopCtx.Done()` (`run.go:118`).
3. `drainShutdown` is called (`run.go:121`) with `DrainBudget` timeout
   (default 30s). In the current scaffolding build with no long-running
   handlers, drain is a no-op. When bt-p7-session-jobs lands, in-flight
   handlers must finish within the budget or be moved to `failed/` with
   `"shutdown deadline"`.
4. The pid file is removed (deferred at `run.go:74`).
5. The lock is released (deferred at `run.go:64`). The kernel also releases
   the flock automatically on process exit.
6. `Run` returns nil.

### Ungraceful stop (SIGKILL)

- The flock is released by the kernel on process exit.
- The pid file is **not** cleaned up — it becomes stale.
- Mail entries that were in-flight stay in `mail/` and are moved to `failed/`
  on the next daemon start by the crash recovery scan.
- No data loss: session files and memory files are append-only with flock
  discipline, so partial writes during a SIGKILL do not corrupt them. Result
  files use atomic tmp+rename and are either fully written or not present.

### Restart

`bitchtea daemon restart` is `stop` then `start`, both via the lock.
Idempotent: a restart issued while no daemon is running is identical to
`start`. The mailbox is the source of truth across restarts — pending requests
survive.

### Auto-restart

Out of scope. The user's init system (or their fingers) is responsible. A
daemon that is down means the TUI behaves as it does today, which is fine.

## Data Paths & File Layout

All paths are relative to `~/.bitchtea/`, resolved through
`internal/daemon/paths.go:20` (`Layout`).

```
~/.bitchtea/
  daemon.pid                       written on start, removed on graceful stop
  daemon.lock                      flock target, never read
  daemon.log                       stdout + stderr (combined), append-only
  daemon/
    mail/<ulid>.json               job requests, TUI-written
    done/<ulid>.json               successful results, daemon-written
    failed/<ulid>.json             rejected/failed jobs, daemon-written
  sessions/
    daemon_<ts>.jsonl              daemon-authored background turns
```

| Path | Code constant | Purpose |
|------|--------------|---------|
| `daemon.lock` | `paths.LockPath` | Exclusive flock, authoritative liveness check |
| `daemon.pid` | `paths.PidPath` | Informational pid, read by `status`/`stop` |
| `daemon.log` | `paths.LogPath` | Combined log, opened for append via `OpenLog()` |
| `daemon/mail/` | `paths.MailDir` | Incoming job envelopes, written by requestors |
| `daemon/done/` | `paths.DoneDir` | Completed job results, written by daemon |
| `daemon/failed/` | `paths.FailedDir` | Rejected/timed-out jobs, written by daemon |

## Lock Semantics

### The lock file

The daemon uses a **separate lock file** (`daemon.lock`) from the pid file.
The lock is authoritative; the pid file is informational and may go stale.

Implementation: `internal/daemon/lock.go:31` (`Acquire`).

```go
f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
```

- Creates the file with mode 0600 if it does not exist.
- Uses `flock(LOCK_EX|LOCK_NB)` for exclusive, non-blocking access.
- If `EWOULDBLOCK` is returned, another daemon holds the lock and `ErrLocked`
  is returned.
- The kernel releases the flock when the file descriptor is closed, which
  happens automatically on process exit — even SIGKILL. No panic-unwind
  needed for correctness.

### Stale pid recovery

The pid file may outlive the daemon (e.g., SIGKILL, filesystem left behind).
On startup, the daemon:

1. Tries to acquire the lock — if successful, no other daemon is running.
2. Overwrites the pid file unconditionally (no "is the pid alive" probe).
3. Proceeds normally.

The flock check subsumes any stale-pid probe. A dead process cannot hold a
flock, so acquiring the lock is sufficient proof that no daemon is live.

### Status probe

The `IsLocked` function (`lock.go:64`) probes liveness without starting a
daemon:

```go
locked, err := daemon.IsLocked(paths.LockPath)
```

It attempts `flock(LOCK_EX|LOCK_NB)` and immediately releases it. If the lock
is held by another process (`EWOULDBLOCK`), it returns `true`. Used by
`bitchtea daemon status` (`daemon_cli.go:92`).

### Locking and session/memory safety

The daemon respects the same flock discipline that `internal/session` and
`internal/memory` already enforce:

- **`session.Append`** holds `flock(LOCK_EX)` on the JSONL file. The daemon
  never calls `Append` on a TUI-owned session; it writes its own
  `daemon_<ts>.jsonl` files. If it ever did, kernel-level serialization would
  make it safe.
- **`memory.AppendHot` / `memory.AppendDailyForScope`** hold `flock(LOCK_EX)`
  on the target file. The daemon calls these directly. Concurrent TUI writes
  interleave at the entry boundary, never mid-line.
- **Daemon's own writes** to `done/` and `failed/` use atomic tmp+rename, so
  no flock is needed — there is one daemon writer per file.

Cardinal rule: the daemon must never append to an active TUI session JSONL.
Reading is fine (via `session.Load`, `FantasyFromEntries`), and is in fact
how `session-checkpoint` works.

## Pidfile Management

Implementation: `internal/daemon/pidfile.go`.

- **WritePid** (`pidfile.go:13`): Writes `pid\n` to the target path via
  atomic tmp+rename. Creates parent directories with mode 0700.
- **ReadPid** (`pidfile.go:32`): Returns the pid, or `(0, os.ErrNotExist)` if
  missing. Validates the file is a positive integer.
- **RemovePid** (`pidfile.go:53`): Removes the pid file if present. Missing
  file is not an error — idempotent cleanup.

The pid file is strictly informational. The TUI reads it for
`/daemon status` output (`daemon_cli.go:103`). The `stop` subcommand reads it
to know which pid to signal (`daemon_cli.go:113`).

If `ReadPid` on stop fails with `os.ErrNotExist`, the response is
`not running` (exit 0). If the signal fails with `ESRCH` (process gone but
pidfile present), the pid file is cleaned up and `not running` is reported.
This makes scripted `daemon stop && daemon start` chains safe.

## Mailbox Protocol

The file mailbox is the IPC mechanism between the TUI and the daemon. All
state is exchanged as JSON files in three directories, with atomic rename for
crash safety.

### Directory roles

| Dir | Writer | Reader | Lifecycle |
|-----|--------|--------|-----------|
| `mail/` | TUI (and any future requestor) | Daemon | File created, daemon picks it up, file moves out |
| `done/` | Daemon | TUI (poll) | Kept indefinitely; manual or scripted cleanup |
| `failed/` | Daemon | TUI (poll) + human | Stays indefinitely; operator grooms by hand |

### Filename convention

`<ulid>.json`. ULIDs sort by creation time and embed enough randomness to
avoid collisions across requestors. The daemon generates ULIDs via
`NewULID()` (`internal/daemon/ulid.go:18`), which produces 26-character
Crockford base32 identifiers (10 timestamp chars + 16 random chars).

The result file uses the **same** ULID as the request:
`mail/01H7ABC…json` becomes `done/01H7ABC…json`. Trivial correlation.

### Write atomicity

All daemon mailbox writes follow the same pattern
(`internal/daemon/mailbox.go:167`, `atomicWrite`):

1. Write to `<dir>/<ulid>.json.tmp`.
2. `fsync` the file (best-effort for crash safety).
3. `rename` to `<dir>/<ulid>.json`.

POSIX `rename` is atomic within a filesystem, which `~/.bitchtea/` always is.
The reader (daemon for `mail/`, TUI for `done/`) only ever sees fully-written
files.

The daemon moves a completed job from `mail/` to `done/` (or `failed/`) by:
1. Writing the result atomically to `done/` (or `failed/`).
2. Removing the `mail/` entry.

These are separate syscalls — there is no cross-directory rename (which could
clobber an existing same-name file in the target dir).

### Polling cadence

Default: **5s poll** (`run.go:48`). The daemon runs a synchronous
`processOnce` at startup to drain any entries that arrived between the
recovery scan and the signal handler setup, then polls every `PollEvery`
interval.

The combined `fsnotify`+poll approach from the design doc
(`docs/archive/phase-7-process-model.md`) is future work. The current implementation
is poll-only, which is safe and simple. There is no correctness concern with
a slow poll — the daemon is doing background work, and a few seconds of delay
is unnoticeable.

The `PollEvery` option is configurable via `RunOptions.PollEvery`
(`run.go:25`). Tests override it (e.g., 50ms in
`internal/daemon/jobs/dispatch_test.go:64`).

### Job envelope format

Jobs are written as JSON to `mail/<ulid>.json`. Schema
(`internal/daemon/envelope.go:28`):

```json
{
  "kind": "session-checkpoint",
  "args": { "session_path": "/path/to/session.jsonl" },
  "work_dir": "/abs/path/to/checkout",
  "session_path": "/abs/path/to/sessions/2026-05-01_120000.jsonl",
  "scope": { "kind": "root" },
  "requestor_pid": 12345,
  "submitted_at": "2026-05-01T12:00:00Z",
  "deadline": "2026-05-01T12:10:00Z"
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `kind` | string | yes | Job type; known values: `session-checkpoint`, `memory-consolidate`, `stale-cleanup` |
| `args` | object | no | Kind-specific arguments, opaque to the daemon shell |
| `work_dir` | string | no | Absolute workspace path; required by handlers |
| `session_path` | string | no | Path to session JSONL; used by checkpoint handler |
| `scope` | object | no | Memory scope: `{"kind":"root"}` or `{"kind":"channel","name":"main"}` |
| `requestor_pid` | number | no | PID of the submitting process (informational) |
| `submitted_at` | string | no | RFC3339 timestamp of submission |
| `deadline` | string | no | RFC3339 deadline; zero = no enforcement |

Envelope unmarshaling uses `DisallowUnknownFields` (`envelope.go:66`) —
typo'd keys fail loudly, same convention as `mcp.json`.

### Result envelope format

Results are written to `done/<ulid>.json` or `failed/<ulid>.json`. Schema
(`internal/daemon/envelope.go:43`):

```json
{
  "success": true,
  "kind": "session-checkpoint",
  "output": { "checkpoint_path": "...", "turn_count": 12, "tool_call_count": 45 },
  "error": "",
  "started_at": "2026-05-01T12:00:01Z",
  "finished_at": "2026-05-01T12:00:42Z"
}
```

| Field | Type | Description |
|-------|------|-------------|
| `success` | bool | `true` when the handler completed normally |
| `kind` | string | Mirrors the job kind from the request |
| `output` | object | Handler-defined structured output |
| `error` | string | Diagnostic on failure; empty on success |
| `started_at` | string | RFC3339 when the handler started |
| `finished_at` | string | RFC3339 when the handler finished |

`success: false` results go to `done/` (the job ran to completion with an
error). `failed/` is reserved for jobs the daemon refused to run (unknown
kind, malformed envelope, deadline-at-shutdown, shutdown deadline exceeded).
This split lets consumers distinguish "tried and failed" from "could not even
start".

## Job Dispatch

### Dispatcher signature

The daemon main loop is agnostic to job kinds. It calls a `Dispatcher`
function (`run.go:20`):

```go
type Dispatcher func(ctx context.Context, job Job) Result
```

The real dispatcher is `jobs.Handle` (`internal/daemon/jobs/jobs.go:50`),
wired in by both `cmd/daemon/main.go:46` and `daemon_cli.go:70`.

When `Dispatch` is nil (older tests or scaffolding callers), `processOnce`
moves every job to `failed/` with `"no handler registered"`.

### Registered job kinds

Three kinds are registered (`jobs.go:77`):

| Kind constant | String value | Handler | File |
|--------------|--------------|---------|------|
| `KindSessionCheckpoint` | `session-checkpoint` | `handleSessionCheckpoint` | `checkpoint.go:49` |
| `KindMemoryConsolidate` | `memory-consolidate` | `handleMemoryConsolidate` | `memory_consolidate.go:75` |
| `KindStaleCleanup` | `stale-cleanup` | `handleStaleCleanup` | `stale_cleanup.go` |

Adding a new kind requires:
1. Define the kind constant in `jobs.go`.
2. Implement the `Handler` function in a sibling file.
3. Register it in the `registry` map (`jobs.go:77`).

### session-checkpoint

Parses a session JSONL, builds a `session.Checkpoint` summary, and writes
it to `.bitchtea_checkpoint.json` in the session's directory.

Args shape (`checkpoint.go:23`):
```json
{
  "session_path": "/abs/path/to/session.jsonl",
  "model": "claude-opus-4"
}
```

Output shape (`checkpoint.go:31`):
```json
{
  "checkpoint_path": "/abs/path/to/.bitchtea_checkpoint.json",
  "turn_count": 12,
  "tool_call_count": 45
}
```

Idempotency: `SaveCheckpoint` truncates and rewrites the same fixed path on
every call. Two runs against the same session produce a checkpoint with
identical counts; only the embedded timestamp differs.

Bound: 30s context timeout (`checkpoint.go:53`).

### memory-consolidate

Scans daily memory files for a scope and folds new entries into the hot file
(HOT.md). Uses dedupe markers to skip entries that a previous run already
folded in.

Args shape (`memory_consolidate.go:27`):
```json
{
  "session_dir": "/abs/path/to/sessions",
  "work_dir": "/abs/path/to/checkout",
  "scope_kind": "channel",
  "scope_name": "main",
  "scope_parent_kind": "",
  "scope_parent_name": "",
  "since": "2026-04-01"
}
```

Output shape (`memory_consolidate.go:40`):
```json
{
  "hot_path": "/abs/path/to/channels/main/HOT.md",
  "dailies_seen": 15,
  "entries_added": 42,
  "entries_skipped": 120
}
```

Dedupe mechanism: Each consolidated block embeds an HTML comment marker
(`<!-- bitchtea-consolidated:<dailyFile>|<timestamp> -->`) at the start.
The next run scans `HOT.md` for existing markers and skips entries whose
markers are already present (`memory_consolidate.go:259-284`).

Bound: 60s context timeout (`memory_consolidate.go:79`).

### stale-cleanup

Archives session JSONL files older than a configured cutoff into a sibling
archive directory. Designed to run on a slow cadence (daily / on-shutdown)
to keep `~/.bitchtea/sessions/` from growing without bound.

Args shape (`stale_cleanup.go`):
```json
{
  "session_dir": "/abs/path/to/sessions",
  "archive_dir": "/abs/path/to/archive",
  "max_age_days": 30
}
```

Output shape (`stale_cleanup.go`):
```json
{
  "scanned": 14,
  "archived": 9,
  "skipped": 1,
  "conflicts": ["2026-01-02_080000.jsonl"]
}
```

Behavior:
- Cutoff is `now - max_age_days * 24h`. Any `*.jsonl` directly under
  `session_dir` whose mtime is strictly before the cutoff is moved to
  `archive_dir/<basename>`. Sidecar files (e.g.
  `.bitchtea_checkpoint.json`) and non-`.jsonl` entries are left in place.
- `archive_dir` is created on demand (so a fresh host doesn't need to
  pre-provision it) but must NOT live inside `session_dir` — that
  configuration is rejected to avoid the next run rescanning the archive.
- `max_age_days >= 0` is required; negative values fail with a clear
  diagnostic.
- Cross-filesystem moves fall back to copy + remove; mtime is preserved on
  the destination so a downstream consumer can still reason about the
  session's original age.

Idempotency: archival uses `os.Rename` (or copy+remove on EXDEV), so after
a successful run the source no longer exists. A re-run with the same cutoff
finds nothing new, leaving the archive layout identical. Destination
collisions (a file with the same basename already in the archive) are
never overwritten — the source is left in place and reported in
`conflicts` so an operator can resolve manually.

Bound: 60s context timeout. Cancellation is checked before the directory
listing and between every per-file move so a SIGTERM-driven shutdown
aborts cleanly without a half-archived state.

### Dispatch flow

When `processOnce` receives a batch of jobs from `List()`:

1. Each job is passed to the dispatcher in ULID order (`mailbox.go:95`, sorted
   `sort.Strings(names)`).
2. If the dispatcher returns `success=true`, `mailbox.Complete()` writes to
   `done/` and removes the `mail/` entry.
3. If the dispatcher returns `success=false`, `mailbox.Fail()` writes to
   `failed/` with the `Result.Error` diagnostic and removes the `mail/` entry.
4. If the dispatcher is nil (no handler registered), every job goes to
   `failed/` with a sentinel error.

Malformed envelopes (parse errors from `List()`) are logged but left in
`mail/`. The operator must remove them manually — auto-deletion would
silently lose data (`run.go:147-152`).

## Logging

The daemon logs to `~/.bitchtea/daemon.log` and to stderr (tee'd via
`io.MultiWriter`). The log format is:

```
daemon: 2026/05/01 12:00:00 started (pid 12345, poll 5s, base /home/user/.bitchtea)
daemon: 2026/05/01 12:00:05 job 01H7ABC... (kind=session-checkpoint) succeeded
daemon: 2026/05/01 12:00:07 received SIGTERM, beginning 30s graceful drain
daemon: 2026/05/01 12:00:07 drain complete (no in-flight handlers in scaffold build)
daemon: 2026/05/01 12:00:07 stopped
```

Log file is opened via `OpenLog()` (`run.go:236`), which opens the path with
`O_WRONLY|O_CREATE|O_APPEND` (mode 0600). In `cmd/daemon/main.go:40`,
stderr is also sent to the log file via `MultiWriter` so fatal startup errors
appear both on the terminal and in the log.

There is no log rotation in v1. The file is plain text and append-only;
users who care wire `logrotate` themselves.

### Why `daemon.log` vs `daemon/mail` layout

The log file lives at `~/.bitchtea/daemon.log` (not inside
`~/.bitchtea/daemon/`) so the TUI can tail it without knowing the mailbox
directory structure.

## Submitting a Job

The TUI (or any future requestor) submits a job by writing an envelope to
`mail/`. The `Mailbox.Submit` method (`mailbox.go:52`) handles this:

```go
mb := daemon.New(config.BaseDir())
id, err := mb.Submit(daemon.Job{
    Kind:        "session-checkpoint",
    Args:        argsJSON,
    WorkDir:     workDir,
    Scope:       daemon.Scope{Kind: "root"},
    SubmittedAt: time.Now().UTC(),
})
```

- If `job.ID` is empty, `Submit` generates one via `NewULID()`.
- `EnsureMailDir()` creates `mail/` if it does not exist (the TUI can submit
  even if no daemon has ever run on this machine).
- The write is atomic (tmp+rename).

The submitter records the returned `id` to later match against `done/` or
`failed/`.

## Crash Recovery

### Daemon crash with jobs in flight

On the next start, `recoverCrashedJobs()` (`run.go:197`) walks `mail/`:

- Files with mtime **before** start time: assumed to be from a crashed
  instance. Moved to `failed/` with `"previous daemon crashed mid-job"`.
- Files with mtime **after** start time: left in `mail/` for the normal poll
  loop to pick up.

The check uses file mtime (not envelope `submitted_at`) because mtime is
harder to spoof and correctly survives clock skew. Mtime is set by `rename`
at write time, so it represents the moment the file became visible.

**Do not requeue**: moving to `failed/` prevents loops on input that crashes
the daemon.

### TUI restart while daemon is running

The TUI does not own any in-flight job; it only knows the ULIDs it submitted.
On restart, it reads `done/` for any ULIDs it remembers from its session log
and surfaces results into the transcript. ULIDs it does not remember are
ignored — another bitchtea instance may have submitted them.

No duplicate-work risk: the daemon does not care who reads `done/`, and the
TUI does not resubmit jobs whose ULIDs it can match against `done/` or
`failed/`.

### Both crash

`mail/` retains pending requests, `done/` retains completed results, sessions
and memory remain intact (their writers are append-only with flock). Nothing
in flight survives the daemon-side handler — same as the daemon-only crash
case. Any TUI that comes back up replays from session JSONL the same way it
does today; the daemon picks up where the queue left off.

## Source Files Reference

| File | Purpose |
|------|---------|
| `cmd/daemon/main.go` | Standalone daemon binary |
| `daemon_cli.go` | `bitchtea daemon <subcommand>` CLI |
| `internal/daemon/run.go` | Main loop: lock, signal handling, poll, dispatch |
| `internal/daemon/paths.go` | Well-known path layout |
| `internal/daemon/lock.go` | Exclusive flock acquisition, release, probe |
| `internal/daemon/pidfile.go` | Pid file read/write/remove |
| `internal/daemon/mailbox.go` | File mailbox: Submit, List, Complete, Fail |
| `internal/daemon/envelope.go` | Job and Result JSON schema + marshal/unmarshal |
| `internal/daemon/ulid.go` | ULID generation for envelope filenames |
| `internal/daemon/io.go` | Internal helpers (bytes reader shim) |
| `internal/daemon/jobs/jobs.go` | Dispatcher registry, kind constants, result helpers |
| `internal/daemon/jobs/checkpoint.go` | session-checkpoint handler |
| `internal/daemon/jobs/memory_consolidate.go` | memory-consolidate handler |
| `internal/daemon/jobs/stale_cleanup.go` | stale-cleanup handler |
| `internal/daemon/e2e_test.go` | End-to-end smoke test (real binary, real round-trip) |
| `internal/daemon/integration_test.go` | Failure-mode tests (stale lock, recovery, etc.) |
| `internal/daemon/jobs/dispatch_test.go` | Dispatch integration tests with full daemon run |

## Troubleshooting

### Daemon won't start: "another daemon is already running"

A prior instance is still alive. Check with `bitchtea daemon status`. If the
process is dead but the lock file persists (impossible on Linux — flock is
kernel-automatically released), remove the lock file manually:

```bash
rm -f ~/.bitchtea/daemon.lock
```

### Daemon won't start: permission errors

The daemon creates directories under `~/.bitchtea/` with mode 0700. If the
parent directory has wrong ownership or permissions, start will fail with an
`EACCES` error. Fix with:

```bash
chmod 700 ~/.bitchtea
```

### Daemon won't stop gracefully

Send SIGKILL if SIGTERM does not work within the drain budget:

```bash
kill -9 $(cat ~/.bitchtea/daemon.pid)
```

Then clean up the stale pid file:

```bash
rm -f ~/.bitchtea/daemon.pid
```

On the next start, the crash recovery scan will handle any in-flight mail
entries.

### Jobs stuck in mail/

If a job envelope remains in `mail/` for more than one poll interval (default
5s), check:

1. Is the daemon running? `bitchtea daemon status`.
2. Is the envelope valid? `cat ~/.bitchtea/daemon/mail/<ulid>.json`. A
   malformed envelope (parse error) is logged and left in place — the
   operator must remove it manually.
3. Check `daemon.log` for error messages about the job.

### Malformed envelopes in mail/

The daemon logs the parse error and skips the file. To remove:

```bash
rm ~/.bitchtea/daemon/mail/<ulid>.json
```

Auto-deletion was deliberately **not** implemented — silently removing data
that the operator might want to inspect is worse than leaving a broken file
in place.

### E2E smoke test

The daemon ships with a full end-to-end test:

```bash
go test -run TestDaemonE2E ./internal/daemon/
```

This builds `cmd/daemon`, spawns it against an isolated `HOME`, submits a
real `session-checkpoint` job, polls `done/` for the result, then SIGTERMs
and asserts clean shutdown. Wall clock: roughly 5-7s.

Faster in-process failure-mode tests (stale lock, lock contention, crash
recovery, dispatcher hook) run as part of the default test suite:

```bash
go test ./internal/daemon/
```

## Daemon Job Round-Trip

Every daemon job follows the same path from TUI submission to result storage.
The IPC contract is a file-based mailbox (three directories under
`~/.bitchtea/daemon/`):

```text
mail/     ← TUI writes new jobs here as mail/<ulid>.json
done/     ← daemon writes completed results here as done/<ulid>.json
failed/   ← daemon writes failed jobs here as failed/<ulid>.json
```

### Submission → Execution → Result

```text
TUI / CLI caller
  │
  │  daemon.New(config.BaseDir())        ① create mailbox handle
  │  mailbox.Submit(job{Kind, Args, ...})
  │    → EnsureMailDir()                  create mail/ if absent
  │    → MarshalJob(job)                  JSON with indent
  │    → atomicWrite(mail/<ulid>.json)    tmp+rename for crash safety
  │    → return id
  ▼
Daemon Run(ctx, opts)
  │  ② Acquire(lock)                     exclusive process lock
  │     Init()                            create mail/ + done/ + failed/
  │     WritePid(pid)
  │     recoverCrashedJobs()             pre-start mail/ entries → failed/
  ▼
Poll loop (every opts.PollEvery = 5s)
  │
  ▼
processOnce(ctx, mailbox, logger, dispatch)   ③ dequeue + dispatch
  │
  ├─ mailbox.List()
  │     → ReadDir(mail/)                        scan for *.json
  │     → UnmarshalJob for each entry           strict JSON decoder
  │     → return []Job sorted by ULID
  │
  ├─ for each job:
  │     dispatch(ctx, job)                      ④ e.g. jobs.Handle
  │       │
  │       ├─ switch job.Kind                    ⑤ kind dispatch
  │       │    "session-checkpoint" → handleSessionCheckpoint
  │       │    "memory-consolidate" → handleMemoryConsolidate
  │       │    "stale-cleanup"      → handleStaleCleanup
  │       │    (future kinds)       → Result{Success: false, Error: "no handler"}
  │       │
  │       └─ return Result{Success, Output, StartedAt, FinishedAt}
  │
  ├─ result.Success == true
  │     mailbox.Complete(id, result)            ⑥ write done/<id>.json
  │       → MarshalResult(result)
  │       → atomicWrite(done/<id>.json)
  │       → os.Remove(mail/<id>.json)           remove from queue
  │
  └─ result.Success == false
        mailbox.Fail(id, reason)                ⑦ write failed/<id>.json
          → MarshalResult(result)
          → atomicWrite(failed/<id>.json)
          → os.Remove(mail/<id>.json)
```

### Result polling (TUI side)

After submission, the TUI can poll for results:

```text
model.go:submitDaemonCheckpoint
  │
  ├─ mailbox.Submit(job) → id
  ├─ NotifyBackgroundActivity("submitted: <id>")
  │
  └─ (later) poll done/<id>.json or failed/<id>.json
       ├─ exists + Success → result processed
       ├─ exists + !Success→ log failure
       └─ neither → still pending in mail/
```

### Crash recovery path

```text
Daemon start (Run)
  │
  ▼
recoverCrashedJobs(mailbox, startTime, logger)
  │  ⑧ scan mail/ by mtime
  │
  ├─ mtime < startTime
  │     → mailbox.Fail(id, "previous daemon crashed mid-job")
  │     → moved to failed/
  │
  └─ mtime >= startTime
       → left in mail/ for normal processing
```

### File references

| Hop | File | Function |
|-----|------|----------|
| ① | `internal/daemon/mailbox.go:52` | `Mailbox.Submit` |
| ② | `internal/daemon/run.go:43` | `Run` — lock, init, poll loop |
| ③ | `internal/daemon/run.go:141` | `processOnce` — list + dispatch |
| ④ | `internal/daemon/jobs/jobs.go:39` | `Handle` — kind dispatch |
| ⑤ | `internal/daemon/jobs/checkpoint.go` | `handleSessionCheckpoint` |
| ⑥ | `internal/daemon/mailbox.go:121` | `Mailbox.Complete` |
| ⑦ | `internal/daemon/mailbox.go:138` | `Mailbox.Fail` |
| ⑧ | `internal/daemon/run.go:197` | `recoverCrashedJobs` |
| TUI | `internal/ui/model.go:1318` | daemon checkpoint submission |

## Design rationale

Originally documented in `archive/phase-7-daemon-audit.md` and
`archive/phase-7-process-model.md` (both archived).

**Opt-in, additive, per-workspace.** The TUI must remain fully usable
without it; nothing the daemon does is on the critical path of a turn. The
audit's "value-add, not gap-fill" framing applies — a daemon that's down
means the TUI behaves as it does today, which is fine. No fallback
compaction path, no degraded-mode warnings, no nag.

**Single instance per user, system-wide.** Per-workspace daemons would
multiply pidfile bookkeeping for no real benefit; the daemon already keys
all per-workspace work via the `WorkDir` field inside each job envelope.
One process, one lock, one mailbox, regardless of how many checkouts the
user has.

**Pidfile informational, flock authoritative.** The kernel releases flock
on process exit even on SIGKILL, so a stale pidfile cannot fool the
liveness check. There is no "is the pid alive" probe — flock subsumes it.
A dead process cannot hold a flock, so acquiring the lock is sufficient
proof that no daemon is live. The pidfile lives at `~/.bitchtea/daemon.pid`
rather than `~/.bitchtea/daemon/daemon.pid` so the TUI can find it without
knowing the mailbox layout.

**File mailbox over unix socket or HTTP.** Three options were ranked: file
mailbox (chosen), unix socket, local HTTP. The mailbox wins on simplicity:
zero protocol code, survives daemon restart, debuggable with `cat`,
durable across both daemon and TUI crashes. Unix socket is the obvious
upgrade if latency ever matters. Local HTTP was rejected as overkill (port
allocation, firewall noise). The latency floor of the poll interval is
acceptable because the daemon is doing background work — a few seconds of
delay is unnoticeable.

**ULID filenames.** ULIDs sort by creation time and embed enough
randomness to avoid collisions across requestors. The result file uses the
same ULID as the request — trivial correlation, no envelope-side ID
needed.

**Atomic write via tmp+rename.** POSIX `rename` is atomic within a
filesystem, which `~/.bitchtea/` always is. The reader (daemon for `mail/`,
TUI for `done/`) only ever sees fully-written files. The daemon moves a
completed job by writing the result atomically to `done/` (or `failed/`)
then removing the `mail/` entry — separate syscalls, no cross-directory
rename that could clobber an existing same-name file in the target dir.

**`success: false` goes to `done/`, not `failed/`.** The split
distinguishes "job ran to completion with an error" (`done/`) from "daemon
refused to run the job at all" (`failed/`). Unknown kinds, malformed
envelopes, and shutdown-deadline misses go to `failed/`; handler returns
with `success: false` go to `done/`. This lets consumers surface "tried
and failed" differently from "couldn't even start".

**Crash recovery uses mtime, not envelope `submitted_at`.** Mtime is
harder to spoof and correctly survives clock skew. It is set by `rename`
at write time, so it represents the moment the file became visible to the
daemon. Files with mtime before the daemon's start time are assumed to be
from a crashed instance and moved to `failed/`.

**Do not requeue crashed jobs.** Moving to `failed/` prevents a loop on
input that crashes the daemon. The cost is that a one-time transient
failure (e.g. host OOM during the job) needs operator action to re-submit;
the alternative — an infinite crash loop on a poison-pill envelope — is
worse.

**Malformed envelopes are left in `mail/`, not auto-deleted.** Silently
removing data the operator might want to inspect is worse than leaving a
broken file in place. The parse error is logged; the operator removes the
file by hand.

**State ownership split.** The daemon never appends to a TUI-owned session
JSONL; it writes its own `daemon_<ts>.jsonl` files. Reading is fine
(`session.Load`, `FantasyFromEntries`). For memory, both processes write
the same files (`HOT.md`, daily files) via flock-serialized helpers
(`memory.AppendHot`, `memory.AppendDailyForScope`); concurrent writes
interleave at the entry boundary, never mid-line. The flock discipline on
session and memory writes is the load-bearing invariant — a second writer
(the daemon) is already safe today because the locks are kernel-level, not
in-process.

**No hardcoded compaction model.** The prior (deleted) daemon presumably
locked compaction to a single model; the current design takes model choice
from job args or daemon config. Hardcoding the model would force every
user onto whatever was current at build time and ignore profile/provider
choices the user already made.
