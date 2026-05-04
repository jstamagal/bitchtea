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
(`docs/phase-7-process-model.md`) is future work. The current implementation
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
| `kind` | string | yes | Job type; known values: `session-checkpoint`, `memory-consolidate` |
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

Two kinds are registered (`jobs.go:77`):

| Kind constant | String value | Handler | File |
|--------------|--------------|---------|------|
| `KindSessionCheckpoint` | `session-checkpoint` | `handleSessionCheckpoint` | `checkpoint.go:49` |
| `KindMemoryConsolidate` | `memory-consolidate` | `handleMemoryConsolidate` | `memory_consolidate.go:75` |

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
