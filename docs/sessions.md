# Sessions, State, Context Scopes, and Management

This document is the source of truth for how this checkout serializes,
restores, displays, and manages session state. It covers the JSONL transcript,
the sidecar state files, IRC-style context scopes, channel membership, resume,
fork/list/tree commands, startup resume, headless resume, checkpoints, and the
tests that prove the behavior.

The session system is append-only for transcript entries. Focus, membership,
and checkpoint state are separate sidecar files in the session directory.

## Files On Disk

The default session directory is:

```text
~/.bitchtea/sessions
```

`config.MigrateDataPaths()` runs at startup before normal config resolution.
It moves old XDG data into `~/.bitchtea/` only if the old path exists and the
new path does not. For sessions, it moves:

```text
~/.local/share/bitchtea/sessions -> ~/.bitchtea/sessions
```

Migration errors are non-fatal. Startup prints this to stderr:

```text
bitchtea: data migration warning: <error>
```

The session subsystem writes these files:

```text
<SessionDir>/<YYYY-MM-DD_HHMMSS>.jsonl
<SessionDir>/<base>_fork_<HHMMSS>.jsonl
<SessionDir>/.bitchtea_focus.json
<SessionDir>/.bitchtea_membership.json
<SessionDir>/.bitchtea_checkpoint.json
```

Only `*.jsonl` files are listed as sessions. Hidden JSON sidecars are not
included by `/sessions`.

## Session Structs And JSON Shapes

### `Session`

`internal/session.Session` is:

```go
type Session struct {
    Path    string
    Entries []Entry
    mu      sync.Mutex
}
```

`Path` is the active JSONL file path. `Entries` is the in-memory append list.
`mu` serializes append calls inside one process. `Append` also takes an OS
`flock` on the JSONL file so concurrent bitchtea processes do not interleave
bytes in the same file.

### `Entry`

Each transcript line is one JSON object encoded from `session.Entry`.

```go
type Entry struct {
    Timestamp  time.Time      `json:"ts"`
    Role       string         `json:"role"`
    Content    string         `json:"content"`
    Context    string         `json:"context,omitempty"`
    Bootstrap  bool           `json:"bootstrap,omitempty"`
    ToolName   string         `json:"tool_name,omitempty"`
    ToolArgs   string         `json:"tool_args,omitempty"`
    ToolCallID string         `json:"tool_call_id,omitempty"`
    ToolCalls  []llm.ToolCall `json:"tool_calls,omitempty"`
    ParentID   string         `json:"parent_id,omitempty"`
    BranchTag  string         `json:"branch,omitempty"`
    ID         string         `json:"id"`
    V          int            `json:"v,omitempty"`
    Msg        *fantasy.Message `json:"msg,omitempty"`
    LegacyLossy bool          `json:"legacy_lossy,omitempty"`
}
```

`Role` is normally one of:

```text
system
user
assistant
tool
```

`Context` is the IRC-style route label stamped by the UI at turn completion:

```text
#main
#channel
#channel.sub
directTarget
```

`Bootstrap` marks startup-injected messages so replay display can hide them.
`ParentID` forms a linear parent chain for tree display and fork ancestry.
`BranchTag` exists in the schema but is not written by the live code today.
`ToolName` and `ToolArgs` exist in the schema, but the live v1 writer normally
fills `ToolCalls` and `ToolCallID` instead.

Current schema version:

```text
EntrySchemaVersion = 1
```

There are two supported entry generations:

```text
v0: no "v" field or v=0, no "msg"; legacy llm.Message-shaped fields only
v1: v=1, "msg" populated with canonical fantasy.Message, legacy fields also populated
```

Reader precedence is:

```text
if entry.V >= 1 and entry.Msg != nil:
    use entry.Msg as canonical
else:
    synthesize fantasy.Message from legacy fields
```

### `Checkpoint`

The checkpoint sidecar JSON shape is:

```go
type Checkpoint struct {
    TurnCount int            `json:"turn_count"`
    ToolCalls map[string]int `json:"tool_calls,omitempty"`
    Model     string         `json:"model,omitempty"`
    Timestamp time.Time      `json:"timestamp"`
}
```

`SaveCheckpoint` always writes:

```text
<SessionDir>/.bitchtea_checkpoint.json
```

using `json.MarshalIndent(checkpoint, "", "  ")`.

Error strings:

```text
create session dir: <error>
marshal checkpoint: <error>
write checkpoint: <error>
```

If `ToolCalls` is nil, `SaveCheckpoint` initializes it to an empty map before
marshaling. Because the field has `omitempty`, an empty map is still omitted
from the JSON. `tool_calls` appears only when at least one tool has a non-zero
count.

### `FocusState`

Focus sidecar shape:

```go
type FocusState struct {
    Contexts    []ContextRecord `json:"contexts"`
    ActiveIndex int             `json:"active"`
}
```

Context record shape:

```go
type ContextRecord struct {
    Kind    string `json:"kind"`
    Channel string `json:"channel,omitempty"`
    Sub     string `json:"sub,omitempty"`
    Target  string `json:"target,omitempty"`
}
```

Valid `Kind` values written by the UI are:

```text
channel
subchannel
direct
```

The focus file path is:

```text
<SessionDir>/.bitchtea_focus.json
```

It is also indented JSON.

### `MembershipState`

Membership sidecar shape:

```go
type MembershipState struct {
    Channels map[string][]string `json:"channels"`
}
```

The membership file path is:

```text
<SessionDir>/.bitchtea_membership.json
```

`SaveMembership` also writes indented JSON. If `Channels` is nil, it writes an
empty object:

```json
{
  "channels": {}
}
```

## Creating A Session

`session.New(dir)`:

1. Creates `dir` with mode `0755`.
2. Builds a name from local time:

```text
2006-01-02_150405.jsonl
```

3. Returns a `Session` with `Path` set and `Entries` empty.
4. Does not create the file until the first `Append`.

If directory creation fails, the error is:

```text
create session dir: <error>
```

`ui.NewModel(cfg)` calls `session.New(cfg.SessionDir)` during startup. Failure
is non-fatal and prints to stderr:

```text
warning: session init failed: <error>
```

If session creation succeeds, the splash path later displays:

```text
Session: <session path>
```

## Appending Entries

`Session.Append(entry)` is the only live JSONL writer used for transcript
entries.

It performs these mutations before writing:

```text
entry.Timestamp = time.Now()
if entry.ID == "":
    entry.ID = fmt.Sprintf("%d", time.Now().UnixNano())
if entry.ParentID == "" and len(s.Entries) > 0:
    entry.ParentID = s.Entries[len(s.Entries)-1].ID
```

Then it appends the entry to `s.Entries`, marshals it as one JSON object, opens
the file with:

```text
O_APPEND | O_CREATE | O_WRONLY, mode 0644
```

takes an exclusive flock, writes the JSON plus a trailing newline, and unlocks
on close.

Error strings:

```text
marshal entry: <error>
open session file: <error>
flock session: <error>
```

The final write returns the raw `f.Write` error without wrapping.

Important behavior: `Append` mutates the in-memory `Entries` slice before
opening/writing the file. If file open or write fails, memory and disk can
diverge for that process.

## Loading Entries

`session.Load(path)` reads the whole file with `os.ReadFile`.

Read failure is wrapped as:

```text
read session: <error>
```

The reader splits on `\n` manually. Empty lines are skipped. Malformed JSON
lines are silently skipped:

```go
if err := json.Unmarshal(line, &entry); err != nil {
    continue
}
```

There is no visible warning for a skipped malformed line.

`Load` does not validate roles, parent IDs, context labels, timestamps, or
schema version. It returns whatever entries decode.

`splitLines(data)` is the private splitter used by `Load`. It splits only on
byte `\n`. It does not strip `\r`, so CRLF files pass a trailing carriage
return into `json.Unmarshal`; JSON permits surrounding whitespace, so normal
CRLF JSONL still decodes. A final line without a trailing newline is included.

`LastUserEntry()` scans `s.Entries` backward and returns the `Content` of the
last entry whose role is exactly:

```text
user
```

If none exists, it returns the empty string. `Info` uses this helper.

## v1 Fantasy Serialization

The agent's canonical history is `[]fantasy.Message`. Session v1 writes the
full fantasy message under `msg` and also writes legacy fields for downgrade
compatibility.

### `EntryFromFantasy`

`EntryFromFantasy(msg)` is:

```go
EntryFromFantasyWithBootstrap(msg, false)
```

### `EntryFromFantasyWithBootstrap`

This function:

1. Shallow-copies `msg`.
2. Copies the `Content` slice.
3. Projects `msg` into legacy `llm.Message` fields.
4. Returns an `Entry` with:

```text
v = 1
msg = &cloned
legacy_lossy = <projection lossy?>
bootstrap = <argument>
role = legacy.Role
content = legacy.Content
tool_call_id = legacy.ToolCallID
tool_calls = legacy.ToolCalls
```

It does not set `Timestamp`, `ID`, `ParentID`, `Context`, `ToolName`,
`ToolArgs`, or `BranchTag`. `Append` and the UI fill some of those later.

Provider options on the `fantasy.Message` are persisted inside `msg`. There is
no strip step.

### Legacy projection rules

`projectFantasyToLegacy` converts fantasy parts into legacy fields:

Text parts:

```text
fantasy.TextPart
*fantasy.TextPart
```

are concatenated into `content`. Multiple text parts are separated by exactly:

```text


```

and set `legacy_lossy=true` because part boundaries are collapsed.

Reasoning parts:

```text
fantasy.ReasoningPart
*fantasy.ReasoningPart
```

are dropped from `content` and set `legacy_lossy=true`.

File parts:

```text
fantasy.FilePart
*fantasy.FilePart
```

are dropped and set `legacy_lossy=true`.

Tool call parts:

```text
fantasy.ToolCallPart
*fantasy.ToolCallPart
```

append to legacy `tool_calls` with:

```json
{
  "id": "<ToolCallID>",
  "type": "function",
  "function": {
    "name": "<ToolName>",
    "arguments": "<Input>"
  }
}
```

Tool result parts set legacy `tool_call_id` from the first result part that
has an ID. Their output handling is:

```text
ToolResultOutputContentText:
    append text, lossless
ToolResultOutputContentMedia:
    append only output.Text, drop media data, lossy
ToolResultOutputContentError:
    append error.Error() if non-nil, lossy
unknown output type:
    append nothing, lossy
```

Any unknown fantasy message part sets `legacy_lossy=true` and contributes
nothing to legacy fields.

### `FantasyFromEntries`

`FantasyFromEntries(entries)` is the restore adapter.

For v1:

```text
if V >= 1 and Msg != nil:
    clone Msg and its Content slice
```

For v0:

```text
legacyEntryToFantasy(entry)
```

except orphan tool entries are skipped:

```text
if Role == "tool" and ToolCallID == "":
    skip entry
```

That skip exists because provider APIs reject tool results that cannot be tied
to a previous assistant tool call.

`legacyEntryToFantasy` maps roles:

```text
user      -> user message with one TextPart(Content)
system    -> system message with one TextPart(Content)
assistant -> assistant message with optional TextPart(Content), then ToolCallParts from ToolCalls
tool      -> tool message with one ToolResultPart(ToolCallID, text Content)
other     -> fantasy.MessageRole(Role), one TextPart(Content)
```

## Display Projection On Resume

`session.DisplayEntries(entries)` removes entries where:

```text
Bootstrap == true
```

Everything else is displayable.

`Model.ResumeSession(sess)` uses `DisplayEntries` to rebuild viewport messages.
For each display entry:

```text
role user      -> MsgUser, nick = cfg.UserNick
role assistant -> MsgAgent, nick = cfg.AgentNick
role tool      -> MsgTool, nick resolved as below
role system    -> MsgSystem
other role     -> MsgSystem
```

Tool nick resolution:

1. Build a map from every `ToolCalls[].ID` in all entries to
   `ToolCalls[].Function.Name`.
2. For a tool entry, use `entry.ToolName` if set.
3. Otherwise use the mapped tool name for `entry.ToolCallID`.
4. Otherwise use:

```text
tool
```

Display content is `entry.Content`. If it is longer than 500 bytes, it is
truncated to:

```text
<first 500 bytes>... (truncated from session)
```

The UI display path does not render the canonical `entry.Msg` directly. It
uses the legacy projection fields even for v1 entries. This means reasoning,
media data, and fantasy part boundaries can be preserved for the next LLM turn
but still absent or flattened in the viewport.

## Startup Resume

CLI resume parsing:

```text
--resume [path]
-r [path]
```

If `--resume` or `-r` is passed without a following path, it resolves to:

```text
latest
```

`latest` calls `session.Latest(cfg.SessionDir)`. `Latest` returns the first
path from `List`, or `""` if there are no sessions.

No sessions for `latest` prints to stderr and exits 1:

```text
bitchtea: no sessions to resume
```

Load failure prints to stderr and exits 1:

```text
bitchtea: failed to load session: <error>
```

Successful CLI resume prints to stderr:

```text
Resuming session: <path> (<N> entries)
```

In TUI mode, `buildStartupModel(cfg, sess, rcCommands)` calls:

```go
m := ui.NewModel(cfg)
if sess != nil {
    m.ResumeSession(sess)
}
```

`NewModel` always creates a fresh session first. If startup resume is used,
`ResumeSession` replaces `m.session` with the loaded session, so subsequent
turns append to the resumed JSONL file rather than the newly allocated file.

`NewModel` also loads `.bitchtea_focus.json` and `.bitchtea_membership.json`
from `cfg.SessionDir` before `ResumeSession` runs. Focus/membership sidecars
are therefore global for the configured session directory, not stored inside
one JSONL transcript.

`NewModel` sets the agent memory scope from the restored focus before any
resume file is applied. If that scope has HOT memory, `SetScope` can append a
synthetic memory exchange to the fresh agent. A later `ResumeSession` can then
replace the active message slice and discard that pre-resume injection. The
scope is applied again at `startAgentTurn`, so scoped memory may be re-injected
then if the injected path is not already marked. If the pre-resume injection
marked the path and `ResumeSession` discarded the message, later `SetScope`
will not re-inject that same scoped HOT file in the current process.

If startup resumes an arbitrary JSONL path outside `cfg.SessionDir`,
transcript appends go to that JSONL, but focus, membership, and normal UI
checkpoint sidecars still write to `cfg.SessionDir`.

In headless mode, `runHeadless(cfg, sess, prompt)` creates an `Agent`. If a
session was loaded, it calls:

```go
ag.RestoreMessages(session.FantasyFromEntries(sess.Entries))
```

Headless resume does not create a `ui.Model`, does not write session entries,
does not save focus, and does not save membership.

## `Model.ResumeSession`

`ResumeSession` replaces `m.session` and restores agent histories grouped by
`Entry.Context`.

Grouping rule:

```text
key = entry.Context
if key == "":
    key = agent.DefaultContextKey
```

`agent.DefaultContextKey` is:

```text
#main
```

Restore default group:

```go
msgs := session.FantasyFromEntries(defaultEntries)
m.agent.RestoreMessages(msgs)
m.agent.SetSavedIdx("#main", len(msgs))
m.lastSavedMsgIdx = len(msgs)
```

Restore non-default groups:

```go
msgs := session.FantasyFromEntries(groupEntries)
m.agent.RestoreContextMessages(key, msgs)
m.agent.SetSavedIdx(key, len(msgs))
```

`RestoreMessages` and `RestoreContextMessages` force the first message to be a
fresh system prompt. If the restored slice has no system message, a system
message is prepended. If it already has one, its content is replaced.

`RestoreMessages` resets agent-local counters:

```text
bootstrapMsgCount = 0
TurnCount = 0
ToolCalls = empty map
CostTracker = fresh tracker
StartTime = now
last turn/follow-up state = idle/none
```

The restored JSONL still contains the old transcript. Only in-memory runtime
stats are reset. Checkpoints written after resume therefore report post-resume
turn/tool counters from the current process, not totals recomputed from the
whole file.

Important current bug/gap: `ResumeSession` initializes the UI-side
`m.contextSavedIdx` map as:

```go
map[string]int{ircContextToKey(m.focus.Active()): 0}
```

but done-boundary persistence uses the agent's `SavedIdx`, not this map. The
UI map is stale/mostly unused except by `saveCurrentContextMessages`, which is
not called by the normal turn completion path. Junior models should not build
new logic on `m.contextSavedIdx` without reconciling it.

Second gap: `ResumeSession` groups by `entry.Context` exactly, but
`ircContextToKey(Subchannel("a","b"))` uses `#a.b`, and `Direct("x")` uses
`x`. That matches labels written by current code, but there is no validation
or migration for malformed context labels in old session files.

## Turn-End Persistence

The normal TUI persistence point is `agentDoneMsg` in `Model.Update`.

The UI freezes `m.turnContext` when the turn starts. At done time it stamps
every new session entry with:

```go
e.Context = m.turnContext.Label()
```

It reads the active agent history and per-context save watermark:

```go
ctxKey := ircContextToKey(m.turnContext)
msgs := m.agent.Messages()
savedIdx := m.agent.SavedIdx(ctxKey)
```

Then for every index from `savedIdx` to `len(msgs)-1`:

```go
e := session.EntryFromFantasyWithBootstrap(
    msgs[i],
    i < m.agent.BootstrapMessageCount(),
)
e.Context = ctxLabel
m.session.Append(e)
```

After append loop:

```go
m.agent.SetSavedIdx(ctxKey, len(msgs))
m.lastSavedMsgIdx = len(msgs)
```

Then focus is saved:

```go
m.focus.Save(m.config.SessionDir)
```

Failure adds this visible error message:

```text
focus save failed: <error>
```

Then a checkpoint sidecar is saved:

```go
session.SaveCheckpoint(m.config.SessionDir, session.Checkpoint{
    TurnCount: m.agent.TurnCount,
    ToolCalls: cloneToolStats(m.agent.ToolCalls),
    Model:     m.agent.Model(),
})
```

Failure adds:

```text
checkpoint save failed: <error>
```

Session append errors are ignored:

```go
_ = m.session.Append(e)
```

There is no visible error if a transcript entry fails to append.

Important context caveat: slash commands can change focus while a turn is
streaming. Persistence uses frozen `turnContext`, but visible `ChatMessage`
contexts are assigned from `m.focus.Active()` when each message is added.
Visible transcript context and persisted context can therefore drift.

## Context Keys, Labels, And Channel Scopes

The UI uses `IRCContext` for focus and display. The agent uses a string
`ContextKey` for per-context histories.

Context constructors:

```text
Channel(name):
    trim space, lowercase, strip leading "#"
    empty -> "main"
    Label() -> "#<channel>"

Subchannel(channel, sub):
    trim/lowercase both; strip leading "#"
    empty channel -> "main"
    Label() -> "#<channel>.<sub>"

Direct(target):
    trim space only, preserve case
    Label() -> "<target>"
```

`ircContextToKey` maps:

```text
KindChannel    -> "#<channel>"
KindSubchannel -> "#<channel>.<sub>"
KindDirect     -> "<target>"
default        -> "#main"
```

The memory scope sent to the agent at turn start is:

```text
KindChannel    -> agent.ChannelMemoryScope(channel, nil)
KindSubchannel -> agent.ChannelMemoryScope(sub, &ChannelMemoryScope(channel,nil))
KindDirect     -> agent.QueryMemoryScope(target, nil)
default        -> agent.RootMemoryScope()
```

Subchannel memory scope uses only the sub-name as the scope name with parent
set to the parent channel. The context key/label remains `#channel.sub`.

The membership key from a context is:

```text
KindChannel    -> channel
KindSubchannel -> channel + "." + sub
KindDirect     -> no key
```

Membership keys are normalized by lowercasing, trimming space, and stripping a
leading `#`.

## Focus Management

`FocusManager` maintains:

```go
contexts []IRCContext
active   int
```

There is always one active context. Fresh focus starts as:

```text
#main
```

`SetFocus(ctx)` switches to an existing equal context if present, otherwise
appends it and makes it active.

`Ensure(ctx)` appends a context if missing but does not change focus.

`Remove(ctx)`:

1. Refuses if there is only one context.
2. Removes the matching context if found.
3. If the active index is now past the end, clamps it to the last context.

Because removal only clamps the active index, removing a context before the
currently active index can shift active focus to the next context in the
underlying slice rather than preserving the same logical active context.
Tests cover the current shift/clamp behavior, not a stronger "same context"
guarantee.

`All()` returns a snapshot copy of the contexts slice.

`ToState()` serializes contexts in join order with active index.

`RestoreState(state)`:

1. No-ops for empty state.
2. Drops invalid records:

```text
subchannel with empty channel or sub
direct with empty target
unknown kind
```

3. If no valid contexts remain, no-ops.
4. Replaces the context list.
5. Sets `active = state.ActiveIndex`.
6. If active is out of range, clamps to last context.

There is no lower-bound clamp for negative active indexes in `RestoreState`.
Current serialized state should not contain negative values, but a hand-edited
sidecar could panic later when `Active()` indexes the slice.

`Save(dir)` delegates to `session.SaveFocus`.

`session.SaveFocus(dir, state)` creates `dir` with mode `0755`, marshals
`state` using `json.MarshalIndent(state, "", "  ")`, and writes:

```text
<dir>/.bitchtea_focus.json
```

with mode `0644`.

Error strings:

```text
create session dir: <error>
marshal focus: <error>
write focus: <error>
```

`session.LoadFocus(dir)` reads the same hidden file. If the file is missing,
it returns zero-value `FocusState{}` and nil error. Other errors are:

```text
read focus: <error>
unmarshal focus: <error>
```

`LoadFocusManager(dir)` starts with a fresh `#main` manager. If `LoadFocus`
succeeds and has contexts, it restores them. If load returns an error, it
silently keeps default focus.

## Membership Management

`MembershipManager` stores:

```go
channels map[string]map[string]struct{}
```

where the channel key is normalized without a leading `#`.

`Invite(channelKey, persona)`:

1. Normalizes channel key.
2. Trims persona.
3. Returns false if either is empty.
4. Creates the channel set if needed.
5. Returns false if persona already exists.
6. Adds persona and returns true.

`Part(channelKey, persona)` removes a persona. If the channel becomes empty,
the channel key is removed from the map.

`Members(channelKey)` returns sorted persona names or nil.

`IsJoined(channelKey, persona)` checks normalized channel and trimmed persona.

`ToState()` serializes sorted member lists.

`RestoreState(state)` replaces the whole membership map. It skips channels
with empty persona slices. It does not normalize keys or trim names on restore.

`Save(dir)` delegates to `session.SaveMembership`.

`session.SaveMembership(dir, state)` creates `dir` with mode `0755`, replaces a
nil `state.Channels` with an empty map, marshals indented JSON, and writes:

```text
<dir>/.bitchtea_membership.json
```

with mode `0644`.

Error strings:

```text
create session dir: <error>
marshal membership: <error>
write membership: <error>
```

`session.LoadMembership(dir)` reads the same hidden file. If the file is
missing, it returns zero-value `MembershipState{}` and nil error. Other errors
are:

```text
read membership: <error>
unmarshal membership: <error>
```

`LoadMembershipManager(dir)` falls back to an empty manager if loading fails.
Errors are swallowed.

## Session Management Commands

All command output below is the raw `Content` string added to the viewport.
System/error styling may add prefixes/colors outside the content.

### `/sessions` and `/ls`

Aliases:

```text
/sessions
/ls
```

Handler calls `session.List(m.config.SessionDir)`.

If listing errors or returns no sessions:

```text
No saved sessions.
```

Pagination size is 20. The optional page argument is parsed with `fmt.Sscanf`
as an integer; invalid or less than 1 becomes page 1. If requested page is past
the end, it is clamped to the last page.

Single-page output:

```text
Sessions:
  1. <session.Info(path)>
  2. <session.Info(path)>
  Resume: /resume <number>
```

Multi-page output:

```text
Sessions (page <page>/<totalPages>):
  <N>. <session.Info(path)>
  ... use /sessions <nextPage> for next page
  Resume: /resume <number>
```

The next-page hint appears only when there is a later page.

`session.Info(path)` returns:

```text
<basename> (<entryCount> entries, <userMsgs> user msgs) <lastUserContent>
```

`entryCount`, `userMsgs`, and `lastUserContent` are computed over all loaded
entries. They do not filter out `Bootstrap` entries.

If load fails:

```text
<basename> (error loading)
```

`lastUserContent` is the last entry whose `Role == "user"`. If longer than 50
bytes, it is truncated to:

```text
<first 50 bytes>...
```

If there is no user entry, the final content suffix is empty but the format
still includes the trailing space after the closing parenthesis.

`session.List` sorts paths lexicographically descending, not by mtime. The file
name timestamp convention makes that behave like newest-first for normal
session names.

`/sessions` and `/resume <number>` only operate on files under
`m.config.SessionDir`. Startup `--resume <path>` can load an arbitrary path.

### `/resume <number>`

Missing argument:

```text
Usage: /resume <number>  (use /sessions to list)
```

Invalid number:

```text
Invalid session number: <arg>
```

No sessions or list error:

```text
No saved sessions.
```

Out of range:

```text
Session <N> not found. <available> sessions available.
```

If currently streaming, it cancels the active turn with:

```text
Session resume
```

and clears queued messages.

Load failure:

```text
Error loading session: <error>
```

Success:

```text
Resumed session <N>: <basename>
```

After success, `m.session` points to the loaded JSONL. Future turn-end appends
write into that resumed file.

### `/tree`

If there is no active session:

```text
No active session.
```

Otherwise it displays `m.session.Tree()` as raw cyan ANSI text:

```text
\033[1;36m<tree>\033[0m
```

For an empty session, `Tree()` returns:

```text
(empty session)
```

For a non-empty session:

```text
Session: <basename>
Entries: <N>

├── [HH:MM:SS] <role>: <content>
├── [HH:MM:SS] <role>: <content>
└── [HH:MM:SS] <role>: <content>
```

For every entry except the last, the prefix is:

```text
├── 
```

For the last:

```text
└── 
```

`content` is `Entry.Content`, truncated to 60 bytes with:

```text
...
```

and all newlines replaced with spaces.

If `Entry.ToolName != ""`, role is:

```text
tool:<ToolName>
```

otherwise role is `Entry.Role`.

Current limitation: `Tree()` does not render an actual branching tree from
`ParentID`; it renders entries in slice order with tree glyphs. The tests only
assert non-empty output and basic content, not structural branch correctness.

### `/fork`

If no active session or the session has no entries:

```text
No session to fork from.
```

It always forks from the current last entry:

```go
lastID := m.session.Entries[len(m.session.Entries)-1].ID
```

Success:

```text
Forked to new session: <new path>
```

Failure:

```text
Fork failed: <error>
```

`Session.Fork(fromID)` builds:

```text
<same dir>/<old basename without .jsonl>_fork_<HHMMSS>.jsonl
```

It copies entries from the start through and including the first entry whose
`ID == fromID`. If `fromID` is not found, it copies all entries. It writes the
new file with:

```text
O_CREATE | O_WRONLY | O_TRUNC, mode 0644
```

Unlike `Append`, the fork writer does not take a flock. It also ignores marshal
and write errors inside the loop:

```go
data, err := json.Marshal(e)
if err != nil {
    continue
}
f.Write(append(data, '\n'))
```

Only open and close errors are returned:

```text
write fork: <error>
close fork file: <error>
```

After `/fork`, `m.session` points to the fork. The original file remains
unchanged.

## Context And Membership Commands

These commands manage focus/membership state. They are session-adjacent because
they determine `Entry.Context`, focus sidecar contents, membership sidecar
contents, and memory scope used on future turns.

### `/join <#channel>`

Missing arg:

```text
Usage: /join <#channel>
```

Success:

```text
Joined #<channel>
```

Focus save failure:

```text
focus save: <error>
```

The channel name is lowercased and leading `#` is stripped before storage.

### `/part [#channel|persona]`

No arg targets the current focus. An arg starting with `#` targets a channel;
otherwise it targets a direct context.

If removing fails because only one context exists:

```text
Can't part the last context.
```

If target is not open:

```text
Not in context <label>.
```

Focus save failure:

```text
focus save: <error>
```

Success:

```text
Parted <oldLabel> — now in <activeLabel>
```

### `/query <persona>`

Missing arg:

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

Direct target casing is preserved.

### `/channels` and `/ch`

Aliases:

```text
/channels
/ch
```

Output:

```text
Open contexts:
* #active
  #other [persona1, persona2]
  directTarget
```

The active context line starts with:

```text
* 
```

Inactive lines start with two spaces:

```text
  
```

For channel/subchannel contexts with members, a sorted member list is appended:

```text
 [<member1>, <member2>]
```

Direct contexts never show members.

### `/msg <nick> <text>`

This is a one-shot routed message. It does not change focus or membership.

Missing nick/text or empty text:

```text
Usage: /msg <nick> <text>
```

If streaming, it queues a post-turn prompt with this internal text:

```text
[to:<nick>] <text>
```

and shows:

```text
Queued /msg to <nick> (agent busy).
```

If not streaming, it displays the user message as:

```text
→<nick>: <text>
```

and sends this exact prompt to the agent/LLM:

```text
[to:<nick>] <text>
```

The prompt is appended to the active context history like any normal user
message. There is no separate direct context unless the user opened one with
`/query`.

### `/invite <persona> [#channel]`

Missing persona:

```text
Usage: /invite <persona> [#channel]
```

If no channel arg is supplied and current focus is a direct context:

```text
Cannot /invite in a DM context. Switch to a channel first.
```

The optional channel argument is honored only when it starts with `#`. A third
argument without `#` is ignored and the current channel/subchannel is used.

If already joined:

```text
<persona> is already in #<channelKey>
```

Success writes membership state and displays:

```text
*** <persona> joined #<channelKey>
```

Then it displays a catch-up block from `buildChannelCatchup`.

If there is no session:

```text
Catch-up: no session history available.
```

If no prior non-tool messages exist for that channel:

```text
Catch-up for #<channelKey>: no prior conversation found.
```

If history exists:

```text
Catch-up for #<channelKey> (<N> messages):
  [<role>] <content>
  [<role>] <content>
```

Catch-up filtering:

```text
entry.Context must equal "#"+channelKey
entry.Role must not be "tool"
entry.ToolCallID must be empty
```

It takes the last 50 matching entries. It does not summarize with the LLM and
does not inject the catch-up into the agent history. It is visible UI output
only.

Membership save errors are ignored:

```go
_ = m.membership.Save(m.config.SessionDir)
```

### `/kick <persona>`

Missing persona:

```text
Usage: /kick <persona>
```

If current focus is not a channel/subchannel, it uses channel key:

```text
main
```

If persona is not joined:

```text
<persona> is not in #<channelKey>
```

Success:

```text
*** <persona> has been kicked from #<channelKey>
```

Membership save errors are ignored.

## Restart And Session State

`/restart` cancels a streaming turn if needed, calls `m.agent.Reset()`, clears
visible messages and queued input, and starts a fresh session file:

```go
if newSess, err := session.New(m.config.SessionDir); err == nil {
    m.session = newSess
}
```

If `session.New` fails here, the error is ignored. The visible restart message
still appears, and `m.session` remains whatever it was before restart.

It resets:

```text
m.lastSavedMsgIdx = 0
m.contextSavedIdx = map[string]int{ircContextToKey(m.focus.Active()): 0}
```

Visible output after the clear:

```text
*** Conversation restarted. Fresh context.
```

It does not clear focus state or membership state. The active context remains
whatever `FocusManager` held before restart, while `Agent.Reset` resets agent
context storage internally to `#main`. The next turn's `startAgentTurn` will
initialize/switch the agent back to the current UI focus.

## Compaction And Session State

`/compact` is not a session command, but it mutates the same agent message
slice that turn-end persistence reads.

If a turn is streaming:

```text
Can't compact while agent is working. Be patient.
```

Otherwise the handler records `before := m.agent.EstimateTokens()`, calls:

```go
m.agent.Compact(context.Background())
```

and then displays either:

```text
Compaction failed: <error>
```

or:

```text
Compacted: ~<before> -> ~<after> tokens
```

`formatTokens` controls the token strings:

```text
0..999 -> <N>
1000+  -> <N/1000 with one decimal>k
```

Under the hood, `Agent.Compact(ctx)` does nothing if the active message count
is below 6. Otherwise it sets:

```text
end = len(messages) - 4
```

and sends the middle slice:

```text
messages[1:end]
```

through two LLM calls with no tools.

First LLM call, memory extraction:

```text
Role: user
Content:
Extract durable memory from this conversation slice before it is compacted.
Return concise markdown bullets covering only lasting facts: user preferences, decisions, completed work, relevant files, and open follow-ups.
Skip transient chatter and tool noise. If nothing deserves durable memory, reply with exactly NONE.

[<role>]: <messageText truncated to 700 bytes>
[<role>]: <messageText truncated to 700 bytes>
...
```

Only `text` stream events are accumulated. If the trimmed result is empty or
case-insensitive `none`, nothing is written. Otherwise the text is appended to
scoped daily memory with the active memory scope.

Second LLM call, summary:

```text
Role: user
Content:
Summarize the following conversation concisely, preserving all important technical details, decisions made, files modified, and current state:

[<role>]: <messageText truncated to 500 bytes>
[<role>]: <messageText truncated to 500 bytes>
...
```

Only `text` stream events are accumulated. The active in-memory message slice
is then replaced with:

```text
system message
user message: [Previous conversation summary]:\n<summary>
assistant message: Got it, I have the context from the summary.
last 4 original messages
```

No session JSONL entries are written during compaction.

Important current bug/gap: `Agent.Compact` does not adjust
`contextSavedIdx`. If the current context was already saved with a watermark
larger than the compacted message length, the next done-boundary loop:

```go
for i := savedIdx; i < len(msgs); i++ { ... }
```

can skip new post-compaction messages until the compacted history grows past
the old watermark. Junior models should treat compaction/session persistence
as not fully wired until the save watermark is reconciled after compaction.

## Daemon Session Checkpoint Job

The daemon has an opt-in job handler:

```text
session-checkpoint
```

The job args shape is:

```json
{
  "session_path": "<path>",
  "model": "<model>"
}
```

If `args.session_path` is empty, the handler falls back to the envelope's
top-level `SessionPath`.

Failures:

```text
session-checkpoint: parse args: <error>
session-checkpoint: session_path is required
session-checkpoint: load <path>: <error>
session-checkpoint: save: <error>
session-checkpoint: <context error>
```

The handler loads the JSONL read-only with `session.Load`. It never writes the
active JSONL. It writes only the sibling checkpoint sidecar in
`filepath.Dir(session_path)`.

Turn count is recomputed as:

```text
count of entries where Bootstrap == false and Role == "user"
```

Tool call counts are recomputed from every entry's legacy `ToolCalls`, grouped
by `ToolCalls[].Function.Name`. This means v1 entries whose legacy projection
does not include tool calls would not count here, even if `Msg` contained some
future richer call representation.

Successful daemon result output JSON is:

```json
{
  "checkpoint_path": "<dir>/.bitchtea_checkpoint.json",
  "turn_count": <turns>,
  "tool_call_count": <total tool calls>
}
```

The daemon handler is bounded by a 30 second context and checks cancellation
before/after major I/O.

## What Goes To The LLM On Resume

TUI resume restores `agent.messages` through `session.FantasyFromEntries`.
On the next user turn, `Agent.SendMessage` converts the active context history
to `[]llm.Message` with `llm.FantasySliceToLLM`, then `Client.StreamChat`
splits it for fantasy/provider input.

Therefore:

```text
v1 Msg fields -> fantasy.Message history -> llm.Message projection -> fantasy prompt/prior
v0 legacy fields -> synthesized fantasy.Message history -> llm.Message projection -> fantasy prompt/prior
```

The next LLM call receives:

```text
system prompt rebuilt from current binary/config/tool definitions
prior restored non-tail messages for the active context
new user prompt as Prompt
tool schemas from current Registry/MCP state
```

Restored bootstrap entries marked `bootstrap=true` are hidden from the viewport
but are not automatically removed from LLM context if they are present in the
restored group. `RestoreMessages` sets `bootstrapMsgCount = 0`, so a restored
session has no hidden bootstrap window from the agent's perspective.

Current caveat: If the resumed file contains entries from multiple contexts,
only the active context loaded by `FocusManager` is used for the next turn.
Other context histories are restored into `Agent.contextMsgs` and become active
only after focus switches and `startAgentTurn` calls `Agent.SetContext`.

If the active focus sidecar points at a context that has no entries in the
resumed JSONL, the next turn does not use another context's restored
transcript.

Important current bug/gap: `RestoreMessages` sets `bootstrapMsgCount = 0`.
After resume, `Agent.InitContext` creates new contexts from:

```go
a.messages[:a.bootstrapMsgCount]
```

which becomes an empty slice. That means a newly opened or sidecar-restored
context with no JSONL entries can start without the system prompt. `SetScope`
may then append scoped memory before the user prompt, but it does not rebuild
the system bootstrap.

## Tests: What They Prove And What They Do Not

Strong tests:

- `TestLoadV0FixtureUnchanged` proves v0 JSONL still loads and promotes user,
  assistant tool-call, tool-result, and assistant entries into fantasy parts.
- `TestV1EntryRoundTripThroughJSON` proves v1 `Msg` survives JSON marshal and
  unmarshal with text and tool-call parts intact.
- `TestV1EntryWithReasoningRoundTripPreservesPart` proves reasoning survives
  in v1 `Msg` even though legacy projection drops it.
- `TestMixedSessionFile` proves one JSONL can mix v0 and v1 lines.
- `TestFantasyFromEntriesSkipsLegacyToolWithoutID` proves orphan v0 tool
  results are dropped before reaching provider context.
- `TestForkV1Session` proves forked v1 entries keep `v` and `msg` fields and
  can be resumed.
- `TestResumeFromV0FixtureFile`, `TestResumeFromV1Fixture`, and
  `TestResumeFromMixedV0V1Fixture` exercise disk-to-viewport resume paths.
- `TestResumeV1ToolCallPopulatesPanelStats` proves v1 assistant tool calls can
  resolve the tool result nick in the viewport and survive in agent history.
- Focus and membership round-trip tests cover sidecar serialization and default
  missing-file behavior.
- Daemon dispatch tests prove a real `session-checkpoint` job writes the
  checkpoint sidecar through the daemon mailbox path.

Shape-heavy or partial tests:

- `TestTree` only checks that tree output contains role/content. It does not
  prove `ParentID` is rendered as an actual tree; current code does not do
  that.
- Fork tests cover parent chain preservation and truncation, but not the
  ignored write errors inside `Fork`'s copy loop.
- `TestInfo` checks count substrings, not exact spacing/trailing content.
- `TestEntryFromFantasy*IsLossy` tests projection flags and text fallback; it
  does not verify a downgraded binary's full behavior.
- Resume viewport tests intentionally use legacy projection content; they do
  not prove the viewport can display richer fantasy-only parts.
- Focus tests cover active-index clamping above the end, not negative active
  indexes from a corrupt sidecar.
- Membership tests cover save/load and nil channel maps, not ignored save
  failures from `/invite` and `/kick`.

Known gaps that junior models should not paper over:

- `Session.Load` silently skips malformed JSONL lines.
- `Session.Append` can diverge in-memory state from disk if append fails after
  `s.Entries` is mutated.
- Normal turn persistence ignores `Session.Append` errors.
- `Session.Fork` ignores marshal/write errors for individual entries and does
  not flock the fork file.
- `/tree` is linear display, not true branch rendering.
- `BranchTag` is schema-only today; no live command writes it.
- `ToolName` and `ToolArgs` are mostly legacy/schema fields today; v1 writer
  relies on `ToolCalls` and `ToolCallID`.
- The UI `contextSavedIdx` map is stale in normal flow; the agent's
  `contextSavedIdx` is the real done-boundary watermark.
- `ResumeSession` restores display from legacy projection, not canonical
  fantasy `Msg`, so rich v1 data can be preserved for the LLM while absent
  from viewport replay.
- After resume, `bootstrapMsgCount = 0`; newly initialized contexts can start
  without a system prompt.
- Pre-resume scoped HOT memory injection can be discarded by `ResumeSession`
  while its path remains marked as injected, preventing reinjection later in
  the same process.
- `/compact` does not reconcile `contextSavedIdx`, so post-compaction turns
  can fail to append to the JSONL until message length exceeds the old saved
  watermark.
- Focus and membership load errors are swallowed by UI loader helpers.
- A corrupt negative `FocusState.ActiveIndex` is not clamped.
- `/invite` catch-up is UI-only. It does not send catch-up text to the invited
  persona's LLM context.
- Focus can change mid-turn; persisted context uses frozen turn context while
  visible messages may carry current focus.
- Headless resume restores messages but does not write back to session files.
