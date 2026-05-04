# Memory Scopes, Retrieval, Append Files, and Storage

This document is the source of truth for memory in this checkout. It covers
the `internal/memory` store, agent wrappers, tool behavior, UI display,
LLM-facing prompts, compaction flushes, daemon consolidation, and known gaps.

Important correction: older docs in this tree still say `write_memory` is not
implemented. That is stale. `write_memory` is live in the tool registry and has
a typed fantasy wrapper.

## Files On Disk

The legacy root hot-memory file is:

```text
<WorkDir>/MEMORY.md
```

The scoped memory base directory is derived from `SessionDir`, not from
`WorkDir` directly:

```text
memoryBaseDir = filepath.Join(filepath.Dir(SessionDir), "memory", scopeName(WorkDir))
```

With default config:

```text
SessionDir = ~/.bitchtea/sessions
memoryBaseDir = ~/.bitchtea/memory/<workspace-scope-name>
```

`scopeName(workDir)` is:

```text
clean = filepath.Clean(workDir)
base = lowercase(filepath.Base(clean))
base = replace every [^a-z0-9]+ with "-"
base = trim leading/trailing "-"
if base == "" or base == ".":
    base = "root"
hash = FNV-1a 32-bit of clean
scopeName = fmt.Sprintf("%s-%08x", base, hash)
```

The hash uses the cleaned path bytes, preserving their case. The display base
is lowercased, but the hash is path-sensitive.

Root scope paths:

```text
hot:   <WorkDir>/MEMORY.md
daily: <memoryBaseDir>/<YYYY-MM-DD>.md
```

Channel scope paths:

```text
hot:   <memoryBaseDir>/contexts/channels/<channel>/HOT.md
daily: <memoryBaseDir>/contexts/channels/<channel>/daily/<YYYY-MM-DD>.md
```

Query scope paths:

```text
hot:   <memoryBaseDir>/contexts/queries/<query>/HOT.md
daily: <memoryBaseDir>/contexts/queries/<query>/daily/<YYYY-MM-DD>.md
```

Nested scopes include every ancestor after root:

```text
<memoryBaseDir>/contexts/channels/<channel>/queries/<query>/HOT.md
<memoryBaseDir>/contexts/channels/<channel>/queries/<query>/daily/<YYYY-MM-DD>.md
```

Scope path segments are sanitized with:

```text
name = lowercase(strings.TrimSpace(name))
name = replace every [^a-z0-9]+ with "-"
name = trim leading/trailing "-"
if name == "":
    name = "unnamed"
```

Examples:

```text
"#Dev" -> "dev"
"coding buddy" -> "coding-buddy"
"!!!" -> "unnamed"
```

Startup runs `config.MigrateDataPaths()` before normal config resolution. For
memory, it moves:

```text
~/.local/share/bitchtea/memory -> ~/.bitchtea/memory
```

only when the old path exists and the new path does not. Migration errors are
non-fatal and print to stderr as:

```text
bitchtea: data migration warning: <error>
```

## Scope Model

The memory package owns this type:

```go
type ScopeKind string

const (
    ScopeRoot    ScopeKind = "root"
    ScopeChannel ScopeKind = "channel"
    ScopeQuery   ScopeKind = "query"
)

type Scope struct {
    Kind   ScopeKind
    Name   string
    Parent *Scope
}
```

Constructors:

```go
RootScope() Scope
ChannelScope(name string, parent *Scope) Scope
QueryScope(name string, parent *Scope) Scope
```

The constructors do not sanitize or normalize `Name`. Sanitization happens
when a path is built. The UI usually passes normalized channel names, but
`write_memory` overrides can pass names like `#dev`; the path layer normalizes
that to `dev`.

Root scope is also represented by a zero-value scope:

```go
Scope{}
```

`HotPath` and `DailyPathForScope` treat `Kind == ""` as root.

`Scope.lineage()` returns the current scope first, then parents outward. If no
root parent appears, it appends root at the end.

For a query under a channel:

```text
Query("buddy", parent=Channel("dev", parent=Root))
```

the lineage searched is:

```text
query buddy
channel dev
root
```

For a flat daemon-built channel scope with no parent:

```text
Channel("dev", nil)
```

the lineage searched is:

```text
channel dev
root
```

## UI Context To Memory Scope

`internal/ui/model.go` maps IRC focus to memory scope with
`ircContextToMemoryScope`.

```text
KindChannel:
    agent.ChannelMemoryScope(ctx.Channel, nil)

KindSubchannel:
    parent = agent.ChannelMemoryScope(ctx.Channel, nil)
    agent.ChannelMemoryScope(ctx.Sub, &parent)

KindDirect:
    agent.QueryMemoryScope(ctx.Target, nil)

default:
    agent.RootMemoryScope()
```

Important subchannel behavior: subchannels are stored as a channel scope whose
name is the sub-name, with the parent channel as a parent scope. A UI context
`#dev.build` therefore stores scoped memory under:

```text
contexts/channels/dev/channels/build/
```

not under a `subchannels` directory.

Direct contexts are query scopes. A UI `/query alice` context stores under:

```text
contexts/queries/alice/
```

The agent calls `SetScope` at the start of every turn after freezing the turn
context:

```go
m.turnContext = m.focus.Active()
ctxKey := ircContextToKey(m.turnContext)
m.agent.InitContext(ctxKey)
m.agent.SetContext(ctxKey)
m.agent.SetScope(ircContextToMemoryScope(m.turnContext))
```

`Agent.SetScope` also calls:

```go
a.tools.SetScope(scope)
```

so the same scope becomes the default scope for `search_memory` and
`write_memory`.

## Storage Functions

### `Load`

```go
Load(workDir string) string
```

Reads:

```text
<WorkDir>/MEMORY.md
```

If `os.ReadFile` returns any error, including permission errors, it returns
the empty string. Errors are swallowed.

### `Save`

```go
Save(workDir string, content string) error
```

Writes `content` directly to:

```text
<WorkDir>/MEMORY.md
```

with mode `0644`.

It does not create `WorkDir`. It returns the raw `os.WriteFile` error.

No live UI or tool path calls `Save` for normal memory writes today.
`write_memory` uses append helpers instead.

### `HotPath`

```go
HotPath(sessionDir, workDir string, scope Scope) string
```

For root or zero scope:

```text
<WorkDir>/MEMORY.md
```

For non-root:

```text
<memoryBaseDir>/contexts/<relativePath>/HOT.md
```

### `LoadScoped`

```go
LoadScoped(sessionDir, workDir string, scope Scope) string
```

Reads `HotPath(sessionDir, workDir, scope)`. If any read error occurs, it
returns the empty string. Errors are swallowed.

For root scope, this is equivalent to loading `<WorkDir>/MEMORY.md`.

### `SaveScoped`

```go
SaveScoped(sessionDir, workDir string, scope Scope, content string) error
```

Creates the parent directory for `HotPath` with mode `0755`, then writes
`content` with mode `0644`.

Error strings:

```text
create scoped hot memory dir: <error>
```

The final write returns the raw `os.WriteFile` error.

It overwrites the file. It does not use `flock`.

No live UI or tool path calls `SaveScoped` for normal memory writes today.
Scoped writes from the LLM use `AppendHot` or `AppendDailyForScope`.

### `AppendHot`

```go
AppendHot(sessionDir, workDir string, scope Scope, when time.Time, title, content string) error
```

First:

```text
content = strings.TrimSpace(content)
```

If content is empty, it returns nil and writes nothing.

It creates the parent directory of `HotPath` with mode `0755`, opens the hot
file with:

```text
O_CREATE | O_APPEND | O_WRONLY, mode 0644
```

takes an exclusive `flock`, writes one markdown block, and unlocks on close.

Error strings:

```text
create hot memory dir: <error>
open hot memory file: <error>
flock hot memory: <error>
append hot memory: <error>
```

If `title` trims to empty, the heading is the timestamp:

```text
## <RFC3339 timestamp>

<trimmed content>

```

If `title` is present, the heading is:

```text
## <trimmed title> (<RFC3339 timestamp>)

<trimmed content>

```

The timestamp uses `when.Format(time.RFC3339)`.

### `DailyPath`

```go
DailyPath(sessionDir, workDir string, when time.Time) string
```

Equivalent to:

```go
DailyPathForScope(sessionDir, workDir, RootScope(), when)
```

Root daily path:

```text
<memoryBaseDir>/<YYYY-MM-DD>.md
```

### `DailyPathForScope`

```go
DailyPathForScope(sessionDir, workDir string, scope Scope, when time.Time) string
```

For root:

```text
<memoryBaseDir>/<YYYY-MM-DD>.md
```

For scoped memory:

```text
<memoryBaseDir>/contexts/<relativePath>/daily/<YYYY-MM-DD>.md
```

The date uses:

```text
when.Format("2006-01-02")
```

### `AppendDaily`

```go
AppendDaily(sessionDir, workDir string, when time.Time, content string) error
```

Equivalent to:

```go
AppendDailyForScope(sessionDir, workDir, RootScope(), when, content)
```

### `AppendDailyForScope`

```go
AppendDailyForScope(sessionDir, workDir string, scope Scope, when time.Time, content string) error
```

Trims content. If empty, returns nil and writes nothing.

Creates the parent daily directory with mode `0755`, opens the daily file with:

```text
O_CREATE | O_APPEND | O_WRONLY, mode 0644
```

takes an exclusive `flock`, writes one markdown block, and unlocks on close.

Error strings:

```text
create daily memory dir: <error>
open daily memory file: <error>
flock daily memory: <error>
append daily memory: <error>
```

Entry format:

```text
## <RFC3339 timestamp> pre-compaction flush

<trimmed content>

```

Despite the function name being generic, the heading text is always:

```text
pre-compaction flush
```

This is true for `write_memory` daily mode as well as compaction.

## Search Mechanics

### `Search`

```go
Search(sessionDir, workDir, query string, limit int) ([]SearchResult, error)
```

Equivalent to:

```go
SearchInScope(sessionDir, workDir, RootScope(), query, limit)
```

### `SearchInScope`

```go
SearchInScope(sessionDir, workDir string, scope Scope, query string, limit int) ([]SearchResult, error)
```

First:

```text
query = strings.TrimSpace(query)
```

If the query is empty, error:

```text
query is required
```

If `limit <= 0`, the limit becomes:

```text
5
```

The search terms are:

```text
strings.Fields(strings.ToLower(query))
```

If that somehow produces no fields, it uses the lowercased trimmed query as a
single term.

Candidate path order is:

```text
for each scope in current-to-root lineage:
    append scope HOT.md
    append every .md file in that scope's daily dir, newest filename first
```

Daily file listing skips directories and non-`.md` files. Daily files are
sorted reverse lexicographically, so normal `YYYY-MM-DD.md` files are searched
newest first.

If a daily directory does not exist, it is skipped. Any other `os.ReadDir`
error returns:

```text
read daily memory dir: <error>
```

For each candidate file:

1. Read the file.
2. If the file is missing, skip it.
3. If any other read error occurs, return:

```text
read memory file <path>: <error>
```

4. Lowercase the whole file.
5. Require every term to appear somewhere in the file.
6. Prefer an exact lowercased full-query match for the snippet anchor.
7. If no full-query match exists, use the first term that appears.
8. Return one `SearchResult` for the file, not one per match.
9. Stop when `len(results) >= limit`.

There is no ranking beyond path order. There is no stemming, fuzzy matching,
semantic retrieval, embedding index, or per-heading scoring.

Child scopes do not leak upward. A root search does not search channel/query
memory. A query search can see its parent channel and root if the query scope
has those parents.

### `SearchResult`

```go
type SearchResult struct {
    Source  string
    Heading string
    Snippet string
}
```

`Source` is:

```text
MEMORY.md
```

for root hot memory exactly at `<WorkDir>/MEMORY.md`.

Otherwise, it is relative to `filepath.Dir(SessionDir)` when possible. With
default paths, scoped sources look like:

```text
memory/<workspace-scope-name>/contexts/channels/dev/HOT.md
memory/<workspace-scope-name>/contexts/channels/dev/daily/2026-05-04.md
```

`Heading` is the nearest preceding markdown heading. A heading is any line
starting with:

```text
#
```

The heading text is:

```text
strings.TrimSpace(strings.TrimLeft(line, "#"))
```

So:

```text
### decision
```

becomes:

```text
decision
```

`Snippet` is centered around the matched byte index, converted through rune
counting, with a default max length of 260 runes. It is trimmed. If the snippet
starts after the beginning of the file, it is prefixed with:

```text
... 
```

If it ends before the end of the file, it is suffixed with:

```text
 ...
```

### `RenderSearchResults`

No hits:

```text
No memory matches found for query "<query>".
```

Hits:

```text
Memory matches for "<query>":

1. Source: <source>
Heading: <heading>
<snippet>

2. Source: <source>
Heading: <heading>
<snippet>
```

If `Heading` is empty, the `Heading:` line is omitted.

The renderer trims trailing right-side whitespace from the final output.

## Agent Wrappers

`internal/agent/context.go` re-exports the memory package for callers that
should not import `internal/memory` directly:

```go
LoadMemory(workDir)
SaveMemory(workDir, content)
LoadScopedMemory(sessionDir, workDir, scope)
SaveScopedMemory(sessionDir, workDir, scope, content)
ScopedHotMemoryPath(sessionDir, workDir, scope)
DailyMemoryPath(sessionDir, workDir, when)
ScopedDailyMemoryPath(sessionDir, workDir, scope, when)
AppendDailyMemory(sessionDir, workDir, when, content)
AppendScopedDailyMemory(sessionDir, workDir, scope, when, content)
SearchMemory(sessionDir, workDir, query, limit)
SearchScopedMemory(sessionDir, workDir, scope, query, limit)
RenderMemorySearchResults(query, results)
```

These are pass-through wrappers. They do not add validation or output changes.

## Startup Memory Injection

`agent.NewAgentWithStreamer` builds the system prompt, injects project context
files, then loads root memory:

```go
memory := LoadMemory(cfg.WorkDir)
if memory != "" {
    a.messages = append(a.messages,
        newUserMessage("Here is the session memory from previous work:\n\n"+memory),
        newAssistantMessage("Got it."),
    )
}
```

The exact user message sent to the LLM history is:

```text
Here is the session memory from previous work:

<contents of MEMORY.md>
```

The exact assistant acknowledgement added to history is:

```text
Got it.
```

This is bootstrap history. It is part of the LLM context, not a visible chat
message by itself.

`Agent.Reset()` reloads root `MEMORY.md` with the same exact two-message
bootstrap exchange. It also clears `injectedPaths`, so scoped HOT files can be
injected again after restart.

On splash, the UI separately checks root memory. If root memory exists, it
adds this visible system message:

```text
Loaded MEMORY.md from working directory
```

The splash does not mention scoped HOT memory.

## System Prompt Memory Instructions

The system prompt always contains this memory block:

```text
════════════════════════════
MEMORY WORKFLOW
════════════════════════════
- Before substantive work or when prior decision/history matters: call search_memory first; do not guess.
- After finishing meaningful work (a decision, a fix, a conclusion, a preference learned): call write_memory with a clear title and concise content. Skip trivia and small talk.
- Scope: omit (or 'current') for work tied to the active channel/query — usually correct. Use scope='root' only for global facts that apply everywhere. Use scope='channel'/'query' with name=… to write into a different context than the active one.
- daily=true for ephemeral session events worth archiving by date; default (hot file) for durable knowledge you'll want surfaced again.
- Consolidate: search before writing so you extend existing notes instead of duplicating them. Don't write what's already remembered.
```

The tool schemas are attached separately as provider tool definitions. The
model sees instructions to use `search_memory`/`write_memory`, but daily files
are not automatically injected. The model must call `search_memory` to retrieve
daily archive content unless daemon consolidation has folded it into HOT.

## Scoped HOT Injection

At turn start, `Agent.SetScope(scope)` does:

```go
a.scope = scope
a.tools.SetScope(scope)

hot := LoadScopedMemory(a.config.SessionDir, a.config.WorkDir, scope)
if hot == "" {
    return
}
path := ScopedHotMemoryPath(a.config.SessionDir, a.config.WorkDir, scope)
if a.injectedPaths[path] {
    return
}
a.injectedPaths[path] = true
a.messages = append(a.messages,
    newUserMessage("Context memory for "+scopeLabel(scope)+":\n\n"+hot),
    newAssistantMessage("Got it."),
)
```

`scopeLabel` returns:

```text
channel scope -> #<scope.Name>
query scope   -> <scope.Name>
root/default  -> root
```

The exact scoped user message sent into LLM history is:

```text
Context memory for <scopeLabel>:

<contents of HOT.md>
```

The exact assistant acknowledgement added to history is:

```text
Got it.
```

This injection happens once per hot file path per process. It is tracked in:

```go
Agent.injectedPaths map[string]bool
```

If HOT changes on disk later in the same process, `SetScope` will not inject
the new content again for the same path.

Root scope also has a hot path (`<WorkDir>/MEMORY.md`). Because `SetScope`
does not skip root, root `MEMORY.md` can be injected at bootstrap and then
again through `SetScope(root)` as:

```text
Context memory for root:

<contents of MEMORY.md>
```

This is a current duplication risk.

Important resume gap: `NewModel` calls `SetScope` before `ResumeSession`. If
that pre-resume scoped injection marks a path and `ResumeSession` then replaces
the active message slice, the injected memory message can be discarded while
the path remains marked. Later turns in that process will not re-inject that
same HOT file.

## `/memory` Command

Slash command:

```text
/memory
```

It never sends anything to the LLM. It only adds UI messages.

First it loads root memory from:

```text
<WorkDir>/MEMORY.md
```

If non-empty, it truncates to 1000 bytes:

```text
<first 1000 bytes>
... (truncated)
```

and displays one raw message with exact content:

```text
\033[1;36m--- MEMORY.md ---\033[0m
<memory>
```

Then it computes the current focus memory scope. If the scope is not root, it
loads scoped HOT memory. If scoped HOT is empty, it displays:

```text
No HOT.md for <activeLabel> yet.
```

If scoped HOT exists, it truncates to 1000 bytes using the same suffix and
displays one raw message:

```text
\033[1;36m--- HOT.md (<activeLabel>) ---\033[0m
<hot memory>
```

If root memory is empty and the active scope is root, output is:

```text
No MEMORY.md found in working directory.
```

If root memory is empty and the active scope is non-root, there is no root
missing message. The command only reports the scoped HOT status.

## Tool Registry Scope

`tools.Registry` stores:

```go
type Registry struct {
    WorkDir    string
    SessionDir string
    Scope      memory.Scope
    terminals  *terminalManager
}
```

`NewRegistry(workDir, sessionDir)` does not initialize `Scope`, so it starts as
zero-value root.

`SetScope(scope)` simply assigns:

```go
r.Scope = scope
```

The agent calls this through `Agent.SetScope` at turn start. Tool execution
then uses `r.Scope` as the default memory scope.

## `search_memory` Tool

Registry schema:

```json
{
  "name": "search_memory",
  "description": "Search the hot MEMORY.md file and durable daily markdown memory for past decisions, notes, and context relevant to the current worktree.",
  "parameters": {
    "type": "object",
    "properties": {
      "query": {
        "type": "string",
        "description": "Keywords or a short natural-language query describing what to recall"
      },
      "limit": {
        "type": "integer",
        "description": "Maximum number of memory matches to return (default: 5)"
      }
    },
    "required": ["query"]
  }
}
```

Executor:

```go
execSearchMemory(argsJSON string) (string, error)
```

Expected input JSON from the LLM/tool caller:

```json
{
  "query": "<query>",
  "limit": 5
}
```

`limit` may be omitted or zero.

Bad JSON returns:

```text
parse args: <error>
```

Empty query returns:

```text
query is required
```

Success returns `RenderSearchResults`.

No hits:

```text
No memory matches found for query "<query>".
```

Hits:

```text
Memory matches for "<query>":

1. Source: <source>
Heading: <heading>
<snippet>
```

The tool has no scope argument. It always searches `Registry.Scope` and that
scope's parents.

## `write_memory` Tool

Registry schema:

```json
{
  "name": "write_memory",
  "description": "Persist a memory entry (decision, preference, work-state note) into hot memory for the current scope, or override with scope='root' or a specific channel/query. Use 'daily' to append to the durable daily archive instead. Appended as a dated markdown section so search_memory can recall it later.",
  "parameters": {
    "type": "object",
    "properties": {
      "content": {
        "type": "string",
        "description": "Markdown content to remember. Plain prose or a bulleted list works."
      },
      "title": {
        "type": "string",
        "description": "Optional heading for the entry (e.g. 'decision: drop daemon')."
      },
      "scope": {
        "type": "string",
        "description": "Optional scope override: 'current' (default), 'root', 'channel', or 'query'."
      },
      "name": {
        "type": "string",
        "description": "Required when scope is 'channel' or 'query'. The channel (#name) or query (nick) to write to."
      },
      "daily": {
        "type": "boolean",
        "description": "If true, append to the durable daily archive instead of the hot file (default false)."
      }
    },
    "required": ["content"]
  }
}
```

Expected input JSON from the LLM/tool caller:

```json
{
  "content": "<markdown>",
  "title": "<optional title>",
  "scope": "current",
  "name": "<channel-or-query-name>",
  "daily": false
}
```

Bad JSON returns:

```text
parse args: <error>
```

Whitespace-only content returns:

```text
content is required
```

Scope resolution:

```text
scope omitted or "current" -> Registry.Scope
"root"                     -> RootScope()
"channel"                  -> ChannelScope(name, &RootScope())
"query"                    -> QueryScope(name, &RootScope())
```

For `scope="channel"` with empty `name`:

```text
name is required when scope='channel'
```

For `scope="query"` with empty `name`:

```text
name is required when scope='query'
```

Unknown scope:

```text
unknown scope "<scope>" (want 'current', 'root', 'channel', or 'query')
```

Hot mode (`daily` false or omitted) calls `AppendHot`.

Success:

```text
Wrote <len(content)> bytes to <HotPath>
```

The byte count is `len(args.Content)`, not the trimmed content length and not
the bytes written after title/timestamp formatting.

Daily mode (`daily=true`) first builds:

```text
body = content
if title trims non-empty:
    body = "### " + title + "\n\n" + content
```

Then it calls `AppendDailyForScope`.

Success:

```text
Appended <len(content)> bytes to daily memory (<DailyPathForScope>)
```

Again, the byte count is `len(args.Content)`, not the full body length.

## Typed LLM Wrappers

`internal/llm/typed_search_memory.go` and
`internal/llm/typed_write_memory.go` wrap the registry tools with
`fantasy.NewAgentTool`.

The registry stores OpenAI-compatible object schemas. The typed fantasy
wrappers expose fantasy `ToolInfo` differently: `Parameters` is only the
properties map, and `Required` is a separate slice. The fantasy schema does
not include nested top-level keys named:

```text
type
properties
required
```

at the parameter root. Tests enforce this because some providers reject the
extra nesting.

The typed wrappers are the active path for LLM-streamed tool calls:

```text
LLM tool call JSON -> typed struct -> json.Marshal(struct) -> Registry.Execute -> memory store
```

For `search_memory`, typed input is:

```go
type searchMemoryArgs struct {
    Query string `json:"query"`
    Limit int    `json:"limit,omitempty"`
}
```

For `write_memory`, typed input is:

```go
type writeMemoryArgs struct {
    Content string `json:"content"`
    Title   string `json:"title,omitempty"`
    Scope   string `json:"scope,omitempty"`
    Name    string `json:"name,omitempty"`
    Daily   bool   `json:"daily,omitempty"`
}
```

Both wrappers check cancellation before touching the memory store. A
pre-cancelled call returns a tool error response with content:

```text
Error: context canceled
```

Registry execution errors are also returned to the LLM as tool error text:

```text
Error: <registry error>
```

They are not returned as Go errors, so a bad memory tool call does not abort
the stream.

Successful tool output is returned to the LLM as plain text content. For
example:

```text
Memory matches for "linear sessions":

1. Source: MEMORY.md
Heading: decision: linear sessions (2026-05-04T10:11:12-04:00)
prefer flat history over forks
```

or:

```text
Wrote 31 bytes to /path/to/MEMORY.md
```

The agent also receives normal tool events for UI/tool-panel display through
the fantasy stream. Those event details are documented in
`docs/agent-loop.md`; memory does not add a separate event channel.

## Compaction To Daily Memory

`/compact` calls:

```go
m.agent.Compact(context.Background())
```

If a turn is streaming, visible output is:

```text
Can't compact while agent is working. Be patient.
```

On error:

```text
Compaction failed: <error>
```

On success:

```text
Compacted: ~<before> -> ~<after> tokens
```

`Agent.Compact(ctx)` does nothing if:

```text
len(a.messages) < 6
```

Otherwise it compacts:

```text
messages[1 : len(messages)-4]
```

Before summarizing, it sends this exact prompt to the LLM with no tools:

```text
Extract durable memory from this conversation slice before it is compacted.
Return concise markdown bullets covering only lasting facts: user preferences, decisions, completed work, relevant files, and open follow-ups.
Skip transient chatter and tool noise. If nothing deserves durable memory, reply with exactly NONE.

[<role>]: <message text truncated to 700 bytes>
[<role>]: <message text truncated to 700 bytes>
...
```

Only `text` stream events are accumulated. If the trimmed result is empty or
case-insensitive:

```text
none
```

then nothing is written.

Otherwise the result is written with:

```go
AppendScopedDailyMemory(a.config.SessionDir, a.config.WorkDir, a.scope, time.Now(), text)
```

So compaction memory goes to the active memory scope's daily archive, not to
HOT.

Then compaction sends the summary prompt to the LLM with no tools:

```text
Summarize the following conversation concisely, preserving all important technical details, decisions made, files modified, and current state:

[<role>]: <message text truncated to 500 bytes>
[<role>]: <message text truncated to 500 bytes>
...
```

The in-memory conversation is rebuilt as:

```text
system message
user message: [Previous conversation summary]:\n<summary>
assistant message: Got it, I have the context from the summary.
last 4 original messages
```

Compaction does not update hot memory. Compaction does not call
`write_memory`. Compaction does not adjust session save watermarks; that gap is
documented in `docs/sessions.md`.

## Daemon Memory Consolidation

The daemon job kind is:

```text
memory-consolidate
```

Args shape:

```json
{
  "session_dir": "<SessionDir>",
  "work_dir": "<WorkDir>",
  "scope_kind": "root",
  "scope_name": "<channel-or-query-name>",
  "since": "YYYY-MM-DD"
}
```

Envelope fallback:

```text
work_dir missing in args   -> job.WorkDir
scope_kind missing in args -> job.Scope.Kind
scope_name missing in args -> job.Scope.Name
```

`session_dir` must be in args. There is no envelope fallback for it.

Failures:

```text
memory-consolidate: parse args: <error>
memory-consolidate: work_dir is required
memory-consolidate: session_dir is required
memory-consolidate: scope_name is required for channel scope
memory-consolidate: scope_name is required for query scope
memory-consolidate: unknown scope_kind "<kind>"
memory-consolidate: since must be YYYY-MM-DD: <error>
memory-consolidate: list daily: <error>
memory-consolidate: scan hot: <error>
memory-consolidate: parse <dailyPath>: <error>
memory-consolidate: write hot: <error>
memory-consolidate: <context error>
```

Scope build:

```text
"" or "root" -> RootScope()
"channel"    -> ChannelScope(scope_name, nil)
"query"      -> QueryScope(scope_name, nil)
```

Daemon-built scopes are flat. They do not preserve a parent channel for query
scopes.

The job has a 60 second timeout and checks cancellation before major I/O and
between daily files.

The job resolves:

```text
dailyDir = dirname(DailyPathForScope(sessionDir, workDir, scope, time.Now()))
hotPath  = HotPath(sessionDir, workDir, scope)
```

`listDailyFiles` reads `dailyDir`, skips missing dirs, skips directories and
non-`.md` files, optionally filters by filename date >= `since`, and sorts
ascending by filename.

Malformed non-date filenames are skipped only when `since` is set. Without a
`since` cutoff, any `.md` file in the daily directory is included and then
parsed.

Daily parser input shape:

```text
## <timestamp> pre-compaction flush

<body>

## <timestamp> pre-compaction flush

<body>
```

`parseDailyEntries` splits on lines beginning with:

```text
## 
```

It takes the first whitespace-delimited field of the heading as the timestamp.
Malformed blocks with no timestamp are skipped silently. The body is preserved
trimmed, but heading text after the timestamp is ignored.

Dedupe marker:

```text
<!-- bitchtea-consolidated:<daily-filename>|<timestamp> -->
```

The job scans the existing HOT file for those markers. Missing HOT is treated
as empty. Truncated markers stop the marker scan to avoid an infinite loop.

For each not-yet-seen daily entry, it appends this block directly to HOT:

```text
## consolidated <timestamp> <!-- bitchtea-consolidated:<daily-filename>|<timestamp> -->

<trimmed body>

```

This direct append creates parent dirs with mode `0755`, opens HOT with:

```text
O_CREATE | O_APPEND | O_WRONLY, mode 0644
```

and writes the string. It does not use `flock`. The code comment says this is
acceptable because the daemon is single-instance, but it does not coordinate
with a simultaneous foreground `write_memory` process.

### Flock Asymmetry

The live foreground `write_memory` tool path (via `AppendHot` / `AppendDailyForScope`)
takes an exclusive `flock` on the hot and daily files before writing. The daemon
memory-consolidate job does **not** use `flock`. It opens the HOT file with
`O_CREATE | O_APPEND | O_WRONLY` and calls `f.WriteString(block)` directly
(`appendConsolidatedBlock` in `internal/daemon/jobs/memory_consolidate.go:279`).

The stated rationale is that the daemon is single-instance, so there is no
daemon-vs-daemon contention. However, foreground-vs-daemon contention is real:

```text
foreground write_memory       -> AppendHot   -> flock(HOT)
daemon memory-consolidate     -> appendConsolidatedBlock -> no flock
```

A live agent writing `write_memory` will block if another process holds the
file (but the daemon never takes the lock), so foreground writes will never be
blocked by the daemon. The reverse is not true: the daemon can write into HOT
while a foreground `flock` is held, producing interleaved or torn writes.

### Torn-Write Window

Because daemon consolidation appends a multi-line block (`## consolidated ...\n\n<body>\n\n`)
as a single `f.WriteString` call, each block is atomic at the `write(2)` level for
reasonably sized bodies (the block fits in a single kernel buffer). But there is
no coordination with foreground writers:

1. Foreground takes `flock`, starts appending a `## <title> ...` block.
2. Daemon writes `## consolidated ...` block to the same file descriptor (no lock).
3. Foreground finishes its block and releases `flock`.

The result is that a heading from one writer can appear in the middle of another
writer's body text, producing a semantically broken but structurally valid
markdown file. Search will still find headings, but snippet boundaries may cross
between unrelated entries.

This is currently accepted as an acceptable risk because:
- The daemon runs infrequently (minutes-scale heartbeat) while foreground writes
  are sub-second.
- The window for a foreground write to overlap with a daemon append is small.
- Search returns one result per file; torn entries at boundaries will still
  produce a result with the matched term.

### Consolidation Markers (Dedupe)

`handleMemoryConsolidate` uses HTML comments embedded in HOT heading lines to
prevent duplicate entries on re-run:

```text
<!-- bitchtea-consolidated:<daily-filename>|<entry-timestamp> -->
```

Before writing, `loadConsolidatedMarkers` scans the entire HOT file for existing
markers. For each daily entry not yet marked, `appendConsolidatedBlock` appends:

```text
## consolidated <ts> <!-- bitchtea-consolidated:<daily-basename>|<ts> -->

<trimmed body>

```

The marker includes both the source daily file basename and the entry's RFC3339
timestamp, so the same logical entry from the same daily file is never appended
twice. Rerunning consolidation against unchanged daily files produces
`entries_added: 0` and `entries_skipped: <previous-count>` -- the operation is
idempotent.

Marker scan truncation: if a `<!-- bitchtea-consolidated:` prefix is found but
no closing `-->` follows, the scan aborts to avoid an infinite loop. Truncated
markers from a mid-write crash will not prevent future dedup; the next run's
scan restarts from the beginning of the (now potentially fixed) file.

### No Live Notification

Consolidation writes new blocks into HOT, but the running bitchtea process does
not know that HOT changed. `Agent.SetScope` injects scoped HOT once per path per
process (`injectedPaths` dedup). If HOT was already injected, daemon-added
consolidated entries will not reach the LLM until:

- The process restarts and HOT is re-injected at bootstrap.
- The agent switches to a scope whose HOT has not been injected yet.
- A `search_memory` call finds the new entries in HOT during a live turn.

There is no inotify, signal, or channel to trigger a HOT reload in the running
process after daemon consolidation completes.

Successful result output JSON:

```json
{
  "hot_path": "<hot path>",
  "dailies_seen": <daily file count>,
  "entries_added": <new consolidated entries>,
  "entries_skipped": <entries already marked>
}
```

The Go field is named `EntriesSkip`, but the JSON key is:

```text
entries_skipped
```

## What Goes To The LLM

There are four memory paths into LLM context.

1. System prompt instructions:

```text
MEMORY WORKFLOW
...
```

These are always present.

2. Root memory bootstrap if `<WorkDir>/MEMORY.md` exists:

```text
user: Here is the session memory from previous work:

<MEMORY.md>

assistant: Got it.
```

3. Scoped HOT injection at turn start if the active scope has a non-empty HOT
file and has not been injected in the current process:

```text
user: Context memory for <scopeLabel>:

<HOT.md>

assistant: Got it.
```

4. Tool call/response traffic:

`search_memory` call input:

```json
{
  "query": "<query>",
  "limit": 5
}
```

`search_memory` output:

```text
Memory matches for "<query>":

1. Source: <source>
Heading: <heading>
<snippet>
```

or:

```text
No memory matches found for query "<query>".
```

`write_memory` call input:

```json
{
  "content": "<markdown>",
  "title": "<title>",
  "scope": "current",
  "name": "<name>",
  "daily": false
}
```

`write_memory` output:

```text
Wrote <N> bytes to <path>
```

or:

```text
Appended <N> bytes to daily memory (<path>)
```

or tool error text:

```text
Error: <error>
```

Daily archive content is not automatically sent to the LLM. It is visible to
the LLM only through `search_memory` results, daemon consolidation into HOT, or
manual inclusion elsewhere.

## Tests: What They Prove And What They Do Not

Strong tests:

- `TestScopedMemoryPaths` proves nested channel/query path layout and segment
  sanitization for representative names.
- `TestLoadSaveMemory` proves the agent wrapper can save and reload root
  `MEMORY.md`.
- `TestAppendDailyMemory` proves the root daily append heading includes the
  exact `pre-compaction flush` suffix and durable content.
- `TestSearchMemoryFindsHotAndDurableMarkdown` proves root search can find
  both `<WorkDir>/MEMORY.md` and a root daily archive file.
- `TestScopedMemorySearchInheritsParentsWithoutLeakingChildWrites` proves
  child scopes inherit parent/root reads and root does not search child writes.
- `TestRenderMemorySearchResults` proves the rendered hit shape includes query,
  source, heading, and snippet.
- `TestSearchMemoryTool` proves the registry-level tool can find root
  `MEMORY.md` and returns the `Memory matches for ...` banner.
- `TestWriteMemoryTool` proves registry-level root hot writes, channel override
  writes, daily writes, empty content failure, and missing channel name failure.
- Typed `search_memory` tests prove schema shape, successful hits, scope via
  `Registry.SetScope`, no-match text response, and cancellation error response.
- Typed `write_memory` tests prove schema shape, default-scope write, explicit
  root override, missing content tool error, and cancellation short-circuit.
- `TestCompactFlushesDailyMemoryBeforeSummaryRewrite` proves compaction writes
  durable extracted memory into the root daily file before rewriting the
  in-memory summary.
- The scoped compaction test proves a channel-scoped compact writes to the
  scoped daily path and not the root daily path.
- `TestPhase3CompactionFlushesFantasyMessagesToMemory` proves the same
  compaction path works when the agent history is fantasy-native.
- Daemon memory consolidation tests prove unique append, since cutoff,
  idempotency, cancellation, envelope scope fallback, and required-arg errors.

Shape-heavy or partial tests:

- There are no direct `internal/memory` package tests. Core store semantics are
  covered indirectly through agent, tools, and daemon tests.
- Typed wrapper tests explicitly exercise the wrapper seam, not the full memory
  store. Their comments claim underlying semantics are covered by
  `internal/memory`, but no such package tests currently exist.
- `TestWriteMemoryTool` checks that daily mode succeeds, but does not assert
  the exact daily file contents or heading shape.
- Search tests do not cover read errors from unreadable files, non-existent
  daily directories with permission failures, multi-file ordering across every
  lineage level, or snippet ellipsis boundaries.
- Render tests check substrings, not exact complete output.
- Compaction tests assert durable memory text appears on disk, but they do not
  pin the exact daily markdown block including heading/timestamp text.
- Daemon consolidation tests cover idempotency by marker, but not concurrent
  foreground writes to the same HOT file.
- `/memory` command behavior is not covered here by exact-output tests for all
  root/scoped combinations.

Known gaps that junior models should not paper over:

- Root `MEMORY.md` can be injected twice: once as startup memory and again as
  `Context memory for root`.
- Scoped HOT injection is once per path per process; edits to HOT are not
  re-injected during the same run.
- Pre-resume scoped injection can be discarded by `ResumeSession` while the
  path remains marked as injected.
- `Load`, `LoadScoped`, and UI loader paths swallow read errors and treat them
  as missing memory.
- `SaveScoped` overwrites without `flock`; daemon consolidation appends without
  `flock`.
- Search is lexical and path-ordered. It is not semantic retrieval.
- Search returns at most one hit per file, even if multiple headings match.
- `search_memory` has no scope override; only `write_memory` does.
- `write_memory` daily mode writes headings that still say
  `pre-compaction flush`, even when the write came directly from the tool.
- Daemon query-scope consolidation is flat and cannot express query-under-
  channel parentage.
- Existing stale docs saying `write_memory` is missing should be fixed or
  ignored in favor of this document and the current code.
