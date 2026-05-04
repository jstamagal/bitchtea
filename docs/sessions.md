# Sessions

This document is the canonical contract for bitchtea session serialization,
session-adjacent state, IRC-style channel scopes, and session management
commands. It is written from the current implementation.

Primary files:

- `internal/session/session.go`: JSONL session entries, load/list/fork/tree,
  checkpoint sidecar, focus sidecar, fantasy v1 serialization.
- `internal/session/membership.go`: channel membership sidecar.
- `internal/ui/model.go`: session creation, resume replay, post-turn append,
  checkpoint/focus save.
- `internal/ui/context.go`: `IRCContext`, `FocusManager`, focus
  serialization.
- `internal/ui/membership.go`: in-memory membership manager and persistence.
- `internal/ui/commands.go`: `/sessions`, `/resume`, `/tree`, `/fork`,
  `/join`, `/part`, `/query`, `/channels`, `/msg`.
- `internal/ui/invite.go`: `/invite`, `/kick`, catch-up generation.
- `main.go`: startup `--resume`, headless resume.
- `internal/daemon/jobs/checkpoint.go`: daemon session-checkpoint job.

## Storage Roots

`config.DefaultConfig()` sets:

```text
SessionDir = ~/.bitchtea/sessions
LogDir     = ~/.bitchtea/logs
```

`config.BaseDir()` returns:

```text
~/.bitchtea
```

On startup, `config.MigrateDataPaths()` moves old directories only when the
old path exists and the new path does not:

```text
~/.local/share/bitchtea/sessions -> ~/.bitchtea/sessions
~/.local/share/bitchtea/logs     -> ~/.bitchtea/logs
~/.local/share/bitchtea/memory   -> ~/.bitchtea/memory
~/.config/bitchtea/profiles      -> ~/.bitchtea/profiles
```

Migration errors are printed to stderr by `main` as:

```text
bitchtea: data migration warning: <error>
```

The session subsystem writes these files under `SessionDir`:

- `<YYYY-MM-DD_HHMMSS>.jsonl`: append-only session transcript.
- `.bitchtea_focus.json`: ordered open contexts and active index.
- `.bitchtea_membership.json`: persona membership by channel key.
- `.bitchtea_checkpoint.json`: lightweight turn/tool/model checkpoint.

Only the `.jsonl` files are returned by `session.List`.

## Session Object

`session.Session` is:

```go
type Session struct {
    Path    string
    Entries []Entry
    mu      sync.Mutex
}
```

`Path` is the JSONL path. `Entries` is the in-memory copy loaded from disk or
appended during this process. `mu` serializes appends from goroutines inside
one process.

## Entry Schema

The current writer emits `EntrySchemaVersion = 1`.

`Entry` JSON fields:

```go
type Entry struct {
    Timestamp   time.Time      `json:"ts"`
    Role        string         `json:"role"`
    Content     string         `json:"content"`
    Context     string         `json:"context,omitempty"`
    Bootstrap   bool           `json:"bootstrap,omitempty"`
    ToolName    string         `json:"tool_name,omitempty"`
    ToolArgs    string         `json:"tool_args,omitempty"`
    ToolCallID  string         `json:"tool_call_id,omitempty"`
    ToolCalls   []llm.ToolCall `json:"tool_calls,omitempty"`
    ParentID    string         `json:"parent_id,omitempty"`
    BranchTag   string         `json:"branch,omitempty"`
    ID          string         `json:"id"`
    V           int            `json:"v,omitempty"`
    Msg         *fantasy.Message `json:"msg,omitempty"`
    LegacyLossy bool           `json:"legacy_lossy,omitempty"`
}
```

Meaning:

- `ts`: set by `Session.Append` to `time.Now()`; overwritten even if caller
  supplied a timestamp.
- `role`: legacy role projection: `system`, `user`, `assistant`, `tool`, or a
  preserved unknown role.
- `content`: legacy text projection.
- `context`: routing context label, for example `#main`, `#ops`, `buddy`, or
  `#hub.web`.
- `bootstrap`: true for startup-injected messages hidden from normal viewport
  replay.
- `tool_name`: optional display/helper name for tool-role entries.
- `tool_args`: legacy field present in the struct but not populated by the
  current v1 writer.
- `tool_call_id`: legacy ID linking a tool result to an assistant tool call.
- `tool_calls`: legacy assistant tool-call projection.
- `parent_id`: linear parent pointer set by `Append` when absent.
- `branch`: branch label field; currently not populated by `Fork`.
- `id`: unique entry ID. `Append` sets it to `UnixNano()` as a decimal string
  when absent.
- `v`: schema version. Missing or zero means legacy v0. One means v1.
- `msg`: canonical fantasy-native message for v1 entries.
- `legacy_lossy`: true when the legacy fields cannot represent the full
  fantasy message.

Reader precedence:

1. If `v >= 1` and `msg != nil`, `msg` is the source of truth.
2. Otherwise the legacy fields are promoted into a fantasy message.

The legacy fields are still written on v1 entries so older binaries can load
the file with degraded fidelity.

## JSONL Append

`session.New(dir)`:

1. Creates `dir` with mode `0755`.
2. Names the file:

```text
<time.Now().Format("2006-01-02_150405")>.jsonl
```

3. Returns a `Session` with empty `Entries`.

It does not create the JSONL file immediately. The file appears on first
append.

`Session.Append(entry)`:

1. Locks `s.mu`.
2. Sets `entry.Timestamp = time.Now()`.
3. If `entry.ID == ""`, sets:

```text
<time.Now().UnixNano()>
```

4. If `entry.ParentID == ""` and `s.Entries` is non-empty, sets
   `ParentID` to the previous in-memory entry ID.
5. Appends the entry to `s.Entries`.
6. `json.Marshal(entry)`.
7. Opens `s.Path` with:

```go
os.O_APPEND | os.O_CREATE | os.O_WRONLY
```

mode `0644`.

8. Takes `syscall.Flock(..., LOCK_EX)`.
9. Writes the JSON bytes plus `\n`.
10. Unlocks by deferred `LOCK_UN`.

The exact disk output is one compact JSON object per line. Field order follows
Go's `encoding/json` struct field order. Example for a v0-style caller:

```json
{"ts":"2026-05-04T12:34:56.789Z","role":"user","content":"hello","parent_id":"123","id":"456"}
```

Actual timestamps and IDs are dynamic.

Append is append-only from the session API point of view. It never truncates
the active session JSONL.

Known limitation: `Append` does not call `fsync`; a process or OS crash after
write buffering can still lose recent data.

## Loading

`session.Load(path)`:

1. Reads the whole file with `os.ReadFile`.
2. Splits on newline bytes with `splitLines`.
3. Skips empty lines.
4. Attempts `json.Unmarshal` into `Entry`.
5. Silently skips malformed JSON lines.
6. Returns `Session{Path: path, Entries: parsedEntries}`.

Malformed-line skip is intentional in code but dangerous for diagnostics: a
corrupt line disappears from `Entries` with no surfaced warning.

`splitLines` preserves a final line without a trailing newline.

## Fantasy v1 Serialization

`EntryFromFantasy(msg)` calls `EntryFromFantasyWithBootstrap(msg, false)`.

`EntryFromFantasyWithBootstrap(msg, bootstrap)`:

1. Shallow-copies the fantasy message.
2. Clones the `Content` slice.
3. Projects the message into legacy `llm.Message` fields.
4. Sets:

```go
V = 1
Msg = &cloned
LegacyLossy = lossy
Bootstrap = bootstrap
Role = legacy.Role
Content = legacy.Content
ToolCallID = legacy.ToolCallID
ToolCalls = legacy.ToolCalls
```

It does not set `ToolName`, `ToolArgs`, `Context`, `ParentID`, `BranchTag`, or
`ID`; the caller or `Append` handles those.

`projectFantasyToLegacy` behavior:

- `fantasy.TextPart`: appended to legacy content.
- Multiple text parts are joined with `\n\n` and mark `LegacyLossy = true`.
- `fantasy.ReasoningPart`: dropped from legacy content and marks lossy.
- `fantasy.FilePart`: dropped from legacy content and marks lossy.
- `fantasy.ToolCallPart`: becomes one legacy `llm.ToolCall`.
- `fantasy.ToolResultPart`:
  - first tool call ID becomes legacy `ToolCallID`;
  - text output appends to legacy content;
  - media output writes only accompanying text and marks lossy;
  - error output writes the error string and marks lossy;
  - unknown output marks lossy.
- Unknown fantasy part types mark lossy.

Provider options on `fantasy.Message` are persisted inside `Msg`. The legacy
projection does not carry them.

`FantasyFromEntries(entries)`:

1. For v1 entries with `Msg`, clone and return `Msg`.
2. For v0 tool entries without `tool_call_id`, skip the entry entirely.
3. For every other v0 entry, call `legacyEntryToFantasy`.

`legacyEntryToFantasy`:

- `user`: one user text part from `Content`.
- `assistant`: optional text part plus one `ToolCallPart` per legacy tool call.
- `tool`: one `ToolResultPart` using `ToolCallID` and text output.
- `system`: one system text part.
- unknown role: preserved role string with one text part.

## Display Replay

`DisplayEntries(entries)` filters out entries where `Bootstrap == true` and
returns all others unchanged.

`Model.ResumeSession(sess)` does two separate jobs:

1. Restore agent histories grouped by `Entry.Context`.
2. Replay display entries into the visible chat buffer.

Context grouping:

- Empty `Entry.Context` becomes `agent.DefaultContextKey`, which is `#main`.
- Entries are grouped by their raw context label.
- The `#main` group is restored through `Agent.RestoreMessages`.
- Non-`#main` groups are restored through `Agent.RestoreContextMessages`.

For display replay, `ResumeSession` first builds a map:

```go
toolNames[toolCallID] = toolFunctionName
```

from every entry's legacy `ToolCalls`.

Then for every non-bootstrap entry:

- `role=user`: visible type `MsgUser`, nick `config.UserNick`.
- `role=assistant`: visible type `MsgAgent`, nick `config.AgentNick`.
- `role=tool`: visible type `MsgTool`, nick is:
  1. `Entry.ToolName`;
  2. `toolNames[Entry.ToolCallID]`;
  3. `"tool"`.
- `role=system`: visible type `MsgSystem`.
- unknown role: visible type `MsgSystem`.

Visible replay truncates content longer than 500 bytes:

```text
... (truncated from session)
```

Replay appends `ChatMessage{Time: e.Timestamp, Type: ..., Nick: ..., Content:
...}`. `addMessage` is not used, so replay does not re-log old messages to the
transcript logger.

`ResumeSession` does not clear `m.messages` before replaying entries. The
`/resume` command therefore appends the resumed viewport history below whatever
was already visible. Startup resume normally begins from a fresh model, so that
path does not show duplication; repeated in-app `/resume` calls can show old
visible history above the newly replayed session.

Important: resume does not replay tool execution. A resumed tool result is a
viewport message and an agent-history message only; it does not create live
tool panel running state.

Default-context resume calls `Agent.RestoreMessages`. That function forces a
fresh system prompt at the front of the restored slice and then sets
`bootstrapMsgCount = 0`. Non-default contexts use `Agent.RestoreContextMessages`,
which also forces a fresh system prompt for that stored context but does not
switch to it. Saved indexes are set by `ResumeSession` after conversion, not by
the session package.

## Post-Turn Session Save

The TUI saves session entries at the `agentDoneMsg` boundary, after a turn is
finished and the agent transcript has been updated.

When `m.session != nil`:

1. `ctxKey := ircContextToKey(m.turnContext)`.
2. `msgs := m.agent.Messages()`.
3. `savedIdx := m.agent.SavedIdx(ctxKey)`.
4. `ctxLabel := m.turnContext.Label()`.
5. For `i := savedIdx; i < len(msgs); i++`:
   - convert `msgs[i]` with `EntryFromFantasyWithBootstrap`;
   - mark bootstrap when `i < m.agent.BootstrapMessageCount()`;
   - set `e.Context = ctxLabel`;
   - append to the session file.
6. Set `m.agent.SetSavedIdx(ctxKey, len(msgs))`.
7. Set `m.lastSavedMsgIdx = len(msgs)`.

The save watermark that matters is `Agent.contextSavedIdx`, not
`Model.contextSavedIdx`.

`Model.saveCurrentContextMessages` exists and uses `Model.contextSavedIdx`, but
no current code path calls it. Treat it as stale helper code unless wired in.

After session append, the TUI also saves focus and checkpoint sidecars.

## Context Labels And Scopes

`IRCContext` has three kinds:

- `KindChannel`: label `#channel`.
- `KindSubchannel`: label `#channel.sub`.
- `KindDirect`: label `target`.

Constructors:

- `Channel(name)` trims spaces, strips one leading `#`, lowercases, and
  defaults empty input to `main`.
- `Subchannel(channel, sub)` strips/lowercases channel and lowercases sub. Empty
  channel defaults to `main`.
- `Direct(target)` trims spaces and preserves case.

`ircContextToKey` returns the same strings used for session `Context`:

- channel: `#` + channel.
- subchannel: `#` + channel + `.` + sub.
- direct: target.
- unknown: `#main`.

`ircContextToMemoryScope` maps UI context to agent memory scope:

- channel -> channel memory scope with that channel name.
- subchannel -> child channel memory scope with parent channel.
- direct -> query memory scope.
- default -> root memory scope.

Memory scope is not serialized in the JSONL entry. It is re-derived from focus
when a turn starts.

## Focus State

`FocusManager` holds:

```go
contexts []IRCContext
active   int
```

There is always at least one context in a new manager: `#main`.

`SetFocus(ctx)` switches to an existing context or appends a new one and makes
it active.

`Ensure(ctx)` appends a context without changing focus.

`Remove(ctx)` refuses to remove the last context. If it removes the active
context and the active index is now out of range, focus moves to the last
remaining context.

`FocusState` sidecar JSON:

```go
type FocusState struct {
    Contexts    []ContextRecord `json:"contexts"`
    ActiveIndex int             `json:"active"`
}
```

`ContextRecord` JSON:

```go
type ContextRecord struct {
    Kind    string `json:"kind"`
    Channel string `json:"channel,omitempty"`
    Sub     string `json:"sub,omitempty"`
    Target  string `json:"target,omitempty"`
}
```

`SaveFocus(dir, state)` writes pretty JSON to:

```text
<SessionDir>/.bitchtea_focus.json
```

with mode `0644`, creating the session dir with `0755`.

Example:

```json
{
  "contexts": [
    {
      "kind": "channel",
      "channel": "main"
    },
    {
      "kind": "direct",
      "target": "buddy"
    },
    {
      "kind": "subchannel",
      "channel": "hub",
      "sub": "web"
    }
  ],
  "active": 2
}
```

`LoadFocus(dir)`:

- returns zero-value `FocusState{}` with no error if the file is missing;
- errors on read failure;
- errors on invalid JSON.

`LoadFocusManager(dir)` creates a default manager first, then restores state if
loading succeeded and the file had at least one context.

If `.bitchtea_focus.json` is invalid JSON or unreadable, `LoadFocusManager`
silently keeps the default `#main` manager. The lower-level `session.LoadFocus`
returns the error, but the UI manager loader does not surface it.

`RestoreState` drops invalid records:

- unknown kind is dropped;
- subchannel with empty channel or sub is dropped;
- direct with empty target is dropped.

If `ActiveIndex` is out of bounds, it is clamped to the last context. Negative
active indexes are not clamped by current code and would panic if later used;
the file format should never write a negative index.

## Membership State

Membership is separate from focus and session JSONL. It says which personas are
present in channels.

`MembershipState` sidecar JSON:

```go
type MembershipState struct {
    Channels map[string][]string `json:"channels"`
}
```

It writes to:

```text
<SessionDir>/.bitchtea_membership.json
```

Example:

```json
{
  "channels": {
    "main": [
      "debugger",
      "reviewer"
    ],
    "ops": [
      "oncall"
    ]
  }
}
```

`MembershipManager` stores an in-memory map:

```go
map[channelKey]set(persona)
```

Channel keys do not include `#`. `normalizeMembershipKey` trims spaces, strips
one leading `#`, and lowercases.

`channelKeyFromCtx`:

- channel -> `channel`.
- subchannel -> `channel.sub`.
- direct -> false.

`Invite(channelKey, persona)`:

- normalizes channel key;
- trims persona;
- returns false if either is empty;
- creates the channel set if needed;
- returns false if already present;
- otherwise inserts and returns true.

`Part(channelKey, persona)`:

- returns false if channel or persona is absent;
- removes persona;
- deletes the channel key when the member set becomes empty.

`Members(channelKey)` returns sorted persona names.

`ToState()` sorts every channel's member list before writing.

`LoadMembership(dir)` returns empty state with no error if the sidecar does not
exist.

`LoadMembershipManager(dir)` silently falls back to an empty manager if
`.bitchtea_membership.json` is invalid JSON or unreadable. The lower-level
`session.LoadMembership` returns the error, but the UI manager loader ignores
it.

## Checkpoint State

`session.Checkpoint` JSON:

```go
type Checkpoint struct {
    TurnCount int            `json:"turn_count"`
    ToolCalls map[string]int `json:"tool_calls,omitempty"`
    Model     string         `json:"model,omitempty"`
    Timestamp time.Time      `json:"timestamp"`
}
```

`SaveCheckpoint(dir, checkpoint)`:

1. Creates `dir` with mode `0755`.
2. Sets `checkpoint.Timestamp = time.Now()`.
3. If `ToolCalls == nil`, replaces it with an empty map.
4. Writes pretty JSON to:

```text
<SessionDir>/.bitchtea_checkpoint.json
```

mode `0644`.

Example:

```json
{
  "turn_count": 7,
  "tool_calls": {
    "bash": 1,
    "read": 2
  },
  "model": "gpt-test",
  "timestamp": "2026-05-04T12:34:56.789Z"
}
```

The TUI writes checkpoint after every completed `agentDoneMsg` using the live
agent `TurnCount`, tool call stats, and model.

Current wiring gap: normal startup and `/resume` do not load
`.bitchtea_checkpoint.json` back into the agent. The sidecar is written, and
the daemon can write it too, but it is not currently restoring counters in the
normal resume path.

## Daemon Checkpoint Job

`internal/daemon/jobs/checkpoint.go` handles kind:

```text
session-checkpoint
```

Input args:

```json
{
  "session_path": "/path/to/session.jsonl",
  "model": "optional-model"
}
```

If `args.session_path` is empty, the handler falls back to the envelope
`Job.SessionPath`.

Failure output uses daemon `Result{Success:false, Error:<message>}`. Missing
session path error contains exactly:

```text
session-checkpoint: session_path is required
```

Behavior:

1. Wraps the handler in a `30s` timeout.
2. Checks cancellation before I/O.
3. Loads the session JSONL read-only.
4. Counts non-bootstrap user entries as turns.
5. Counts every assistant legacy `tool_calls` function name.
6. Writes sibling `.bitchtea_checkpoint.json`.
7. Returns structured output:

```json
{
  "checkpoint_path": "/path/to/.bitchtea_checkpoint.json",
  "turn_count": 2,
  "tool_call_count": 1
}
```

The daemon job never writes into the active JSONL.

## Session Listing

`session.List(dir)`:

- returns nil, nil if `dir` does not exist;
- reads directory entries;
- includes only files whose extension is `.jsonl`;
- sorts by path string descending.

Because filenames start with `YYYY-MM-DD_HHMMSS`, descending string sort is
newest-first for normal session names.

`session.Latest(dir)` returns the first item from `List`, or empty string if no
sessions exist or listing fails.

`session.Info(path)`:

1. Loads the session.
2. Counts entries where `Role == "user"`.
3. Finds the last user entry content.
4. Truncates that last user content to 50 bytes plus `...` if longer.
5. Returns:

```text
<basename> (<entry count> entries, <user count> user msgs) <last user content>
```

If loading fails:

```text
<basename> (error loading)
```

## Session Tree And Forking

`Session.Tree()` returns:

```text
(empty session)
```

when no entries exist.

Otherwise it returns:

```text
Session: <basename>
Entries: <N>

├── [HH:MM:SS] <role>: <content>
...
└── [HH:MM:SS] <role>: <content>
```

Formatting details:

- Last row uses `└── `.
- Earlier rows use `├── `.
- `content` is `Entry.Content`.
- Content longer than 60 bytes becomes first 60 bytes plus `...`.
- Newlines in content are replaced with spaces.
- `role` is `Entry.Role`, except if `ToolName != ""`, role becomes
  `tool:<ToolName>`.

`Tree()` does not currently render nested branches from `ParentID`; it renders
the linear `Entries` slice with tree glyphs.

`Session.Fork(fromID)`:

1. Builds new path:

```text
<original base>_fork_<time.Now().Format("150405")>.jsonl
```

2. Copies entries from the original `Entries` slice through the first entry
   whose `ID == fromID`.
3. If `fromID` is not found, copies the entire session and returns success.
4. Opens the fork file with `O_CREATE | O_WRONLY | O_TRUNC`, mode `0644`.
5. Marshals each copied entry and writes it plus `\n`.
6. Closes the file and returns the new session.

Known limitations:

- Fork does not use `flock`.
- Fork ignores per-entry marshal/write errors inside the loop.
- Fork does not set `BranchTag`.
- `/fork` always passes the current last entry ID, so the TUI fork command
  currently copies the whole active session into a new file.

## Startup Resume

CLI parsing:

- `--resume <path>` or `-r <path>` sets `opts.resumePath = <path>`.
- Bare `--resume` or `-r` sets `opts.resumePath = "latest"`.

If `latest` resolves to no file, stderr output is exactly:

```text
bitchtea: no sessions to resume
```

and the process exits with code 1.

If loading fails, stderr output is:

```text
bitchtea: failed to load session: <error>
```

and the process exits with code 1.

On successful load, stderr output is:

```text
Resuming session: <path> (<N> entries)
```

TUI startup then calls `m.ResumeSession(sess)`.

Headless startup calls:

```go
ag.RestoreMessages(session.FantasyFromEntries(sess.Entries))
```

Headless resume does not replay visible UI messages, focus state, membership
state, or checkpoint state. It restores only the agent message history.

## TUI Session Commands

All visible command outputs below are `MsgSystem` unless noted as errors.
Actual rendered viewport lines add timestamp and style prefixes.

### `/sessions` and `/ls`

If listing errors or no sessions exist:

```text
No saved sessions.
```

Page size is exactly 20. Invalid page input defaults to page 1. Page numbers
less than 1 default to page 1. Page numbers beyond the last page clamp to the
last page.

One-page output:

```text
Sessions:
  1. <session.Info(path)>
  2. <session.Info(path)>
  Resume: /resume <number>
```

Multi-page output:

```text
Sessions (page <page>/<totalPages>):
  <n>. <session.Info(path)>
  ... use /sessions <nextPage> for next page
  Resume: /resume <number>
```

The next-page hint appears only when there is another page.

### `/resume <number>`

Missing argument:

```text
Usage: /resume <number>  (use /sessions to list)
```

Invalid number:

```text
Invalid session number: <arg>
```

No sessions:

```text
No saved sessions.
```

Out of range:

```text
Session <num> not found. <len> sessions available.
```

If a turn is streaming, `/resume` first cancels it with visible system text:

```text
Session resume
```

Load failure:

```text
Error loading session: <error>
```

Success:

```text
Resumed session <num>: <basename>
```

`/resume` does not create a new session file. It points `m.session` at the
loaded file, so future appends continue in that JSONL.

### `/tree`

No active session:

```text
No active session.
```

Otherwise it appends raw cyan ANSI content:

```text
\033[1;36m<session.Tree()>\033[0m
```

### `/fork`

No session or empty session:

```text
No session to fork from.
```

Fork failure is `MsgError`:

```text
Fork failed: <error>
```

Success:

```text
Forked to new session: <newSess.Path>
```

The model's active session is switched to the fork.

### `/join <#channel>`

Missing argument is `MsgError`:

```text
Usage: /join <#channel>
```

Success:

```text
Joined <ctx.Label()>
```

Examples:

```text
Joined #code
Joined #general
```

`/join general` and `/join #general` both focus `#general`.

### `/part [target]`

With no argument, removes the active context. With an argument beginning with
`#`, removes that channel. Otherwise removes a direct context with that name.

If it would remove the last context:

```text
Can't part the last context.
```

If target is not open:

```text
Not in context <target.Label()>.
```

Focus save failure:

```text
focus save: <error>
```

Success:

```text
Parted <target.Label()> — now in <activeLabel>
```

That string contains a Unicode em dash.

### `/query <persona>`

Missing argument is `MsgError`:

```text
Usage: /query <persona>
```

Focus save failure:

```text
focus save: <error>
```

Success:

```text
Query open: <persona>
```

Direct context labels preserve persona case.

### `/channels` and `/ch`

Output starts:

```text
Open contexts:
```

Each context line is:

```text
* <label>
```

for the active context, or:

```text
  <label>
```

for inactive contexts.

If a channel or subchannel has members, the members are appended sorted:

```text
* #main [debugger, reviewer]
```

Direct contexts do not show membership.

### `/msg <nick> <text>`

Missing nick or text is `MsgError`:

```text
Usage: /msg <nick> <text>
```

If the agent is streaming, it queues:

```text
[to:<nick>] <text>
```

and shows:

```text
Queued /msg to <nick> (agent busy).
```

If idle, it appends visible user content:

```text
→<nick>: <text>
```

and sends this exact text to the agent/LLM as the user turn:

```text
[to:<nick>] <text>
```

`/msg` does not change focus.

### `/invite <persona> [#channel]`

Missing persona is `MsgError`:

```text
Usage: /invite <persona> [#channel]
```

If no channel argument is supplied and focus is direct:

```text
Cannot /invite in a DM context. Switch to a channel first.
```

If already joined:

```text
<persona> is already in #<channelKey>
```

Success writes membership state and appends:

```text
*** <persona> joined #<channelKey>
```

Then it appends a catch-up system message from session history.

Catch-up with no active session:

```text
Catch-up: no session history available.
```

Catch-up with no prior non-tool entries in that channel:

```text
Catch-up for #<channel>: no prior conversation found.
```

Catch-up with history:

```text
Catch-up for #<channel> (<N> messages):
  [<role>] <content>
  [<role>] <content>
```

It filters by exact `Entry.Context == "#"+channelKey`, excludes `Role=="tool"`
and entries with `ToolCallID != ""`, and takes the last 50 matching entries.

Current wiring gap: `/invite` only updates membership and displays catch-up.
It does not inject the catch-up into agent context. `Agent.InjectNoteInContext`
exists, but `/invite` does not call it.

### `/kick <persona>`

Missing persona is `MsgError`:

```text
Usage: /kick <persona>
```

If current focus is direct, it falls back to channel key `main`.

If persona is not joined:

```text
<persona> is not in #<channelKey>
```

Success:

```text
*** <persona> has been kicked from #<channelKey>
```

## Restart And Clear Interaction

`/clear` clears only the visible chat display. It does not clear the agent
history or session file.

`/restart`:

- cancels an active turn with visible `Restart`;
- calls `agent.Reset`;
- clears visible messages;
- clears stream buffer and queued messages;
- creates a fresh session file with `session.New` if possible;
- resets `lastSavedMsgIdx = 0`;
- resets `Model.contextSavedIdx` for the active context;
- shows:

```text
*** Conversation restarted. Fresh context.
```

Known gap: `/restart` resets `Model.contextSavedIdx`, but post-turn save uses
`Agent.contextSavedIdx`. `agent.Reset` resets the agent map to `#main`; if the
UI focus is another context, the next `startAgentTurn` initializes that context
with saved index 0, so normal operation still starts cleanly.

## State Not In Session JSONL

The JSONL does not contain:

- active focus index;
- list of open contexts;
- channel membership;
- checkpoint counters;
- transcript log state;
- MP3 state;
- queued input;
- active cancellation ladder state;
- current provider/API key/base URL/model unless those appear in message text.

Those are sidecars, runtime-only fields, or config/profile state.

## Fully Wired Behavior

These paths are wired end to end:

- New TUI startup creates a new session object.
- Post-turn `agentDoneMsg` appends new fantasy-native entries to JSONL.
- v1 entries preserve canonical fantasy messages through JSON round trip.
- v0 entries still load and promote to fantasy messages.
- Mixed v0/v1 files load.
- Bootstrap entries are hidden from display replay.
- Tool result nick on resume resolves from legacy `tool_calls`.
- Focus sidecar saves and loads channel/direct/subchannel focus.
- Membership sidecar saves and loads sorted channel members.
- `/sessions`, `/resume`, `/tree`, `/fork` operate on session JSONL files.
- CLI `--resume` restores agent history for both TUI and headless modes.
- Daemon `session-checkpoint` writes a checkpoint sidecar without mutating JSONL.

## Not Fully Wired Or Risky

Checkpoint restore is not wired. `.bitchtea_checkpoint.json` is written by the
TUI and daemon, but normal startup/resume does not read it back into the agent.

`saveCurrentContextMessages` is unused and uses `Model.contextSavedIdx`, while
the real post-turn save path uses `Agent.contextSavedIdx`.

Default-context resume can set a stale saved watermark. `ResumeSession` calls
`FantasyFromEntries`, then `Agent.RestoreMessages`, which may prepend a fresh
system prompt when the restored default group did not start with a system
message. The saved index is then set to the pre-prepend length. On the next
post-turn save, that can duplicate the final restored message. Tests currently
assert the pre-prepend saved index shape instead of this failure mode.

`Session.Load` silently skips malformed JSON lines. That protects against one
bad line killing resume, but it also hides corruption.

Repeated in-app `/resume` calls append replayed entries to the visible chat
buffer instead of replacing it. This is display-only duplication; the agent
history is restored from the selected session, but the viewport can contain
older messages above the replay.

`Session.Fork` ignores marshal/write errors for individual entries, does not
flock, does not set `BranchTag`, and treats missing `fromID` as "copy all".

`Session.Tree` renders a linear list and does not actually build a nested tree
from `ParentID`.

`/fork` always forks from the current last entry, so it is effectively "copy
whole current session into a new file" today. There is no command to fork from
an arbitrary earlier entry ID.

`/invite` catch-up is visible only. It is not sent to the LLM and not persisted
as an agent note.

Negative `FocusState.ActiveIndex` is not clamped in `RestoreState`.

The v1 legacy projection marks lossy content, but the UI display path still
uses legacy `Entry.Content`, so reasoning/media/provider-option fidelity is
kept for agent restore but not surfaced in the chat viewport.

`Entry.ToolArgs` and `Entry.BranchTag` exist but are not populated by the
current live writer.

Session append uses `flock`, but transcript replay and focus/membership
sidecar writes do not coordinate with JSONL appends beyond normal file
replacement/write behavior.

## Test Coverage Reality

Strong tests:

- `TestV1EntryRoundTripThroughJSON` verifies fantasy v1 `Msg` survives JSON
  round trip with text and tool-call parts.
- `TestV1EntryWithReasoningRoundTripPreservesPart` verifies reasoning survives
  in canonical `Msg` while legacy projection is lossy.
- `TestMixedSessionFile` verifies mixed v0/v1 JSONL loading and fantasy
  reconstruction.
- `TestResumeFromV0FixtureFile` exercises disk JSONL -> `session.Load` ->
  `ResumeSession` -> agent history and viewport.
- `TestResumeFromV1Fixture` exercises v1 fixture resume through the same UI
  path.
- `TestForkV1Session` verifies forked v1 files remain resumable.
- `TestResumeV1ToolCallPopulatesPanelStats` verifies resumed tool nicks and
  tool-call parts survive in agent history.
- Focus and membership round-trip tests verify sidecar serialization.
- Daemon checkpoint dispatch tests exercise a real mailbox envelope through the
  daemon run loop and confirm checkpoint sidecar creation.

Shape-heavy or weaker tests:

- `TestNewAndAppend` mostly verifies append creates a file and grows the slice;
  it does not assert JSONL bytes, parent IDs, lock behavior, or concurrent
  append behavior.
- `TestLoadSession` checks happy-path content but not malformed-line reporting
  or mixed corruption.
- `TestListSessions` checks extension filtering, not sort order details beyond
  count.
- `TestTree` checks non-empty output and substrings, not exact tree rendering.
- `TestInfo` checks two substrings, not exact output or truncation.
- `TestResumeSessionRestoresAgentMessagesAndToolNick` uses an in-memory
  session and does not cover disk parsing.
- Routing command tests mostly assert focus/message shape, not that future
  post-turn saves use the intended context.
- `/invite` tests assert membership and visible catch-up, not LLM context
  injection.
- Checkpoint tests verify writes and daemon generation, but there is no test
  proving checkpoint restore because restore is not implemented.

Missing high-value tests:

- Resume a default-context session without a system entry, complete a new turn,
  and assert no restored entry is duplicated in JSONL.
- Exact JSONL append test for v1 entries including `context`, `bootstrap`,
  parent ID, and newline framing.
- Concurrent append test across goroutines and, if practical, two processes.
- Corrupt JSONL test that asserts skipped-line behavior is either logged or
  deliberately silent.
- `/fork` from arbitrary historical ID, if the UI ever exposes it.
- Negative `FocusState.ActiveIndex` load behavior.
- `/invite` should either inject catch-up into agent context and test it, or
  explicitly remain UI-only with tests pinning that.
- Session sidecars should have tests for partial/invalid JSON handling at the
  UI load-manager layer, not only the session package layer.
