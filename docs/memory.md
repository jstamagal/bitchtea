# Memory, Scopes, Retrieval, and Storage

This document is the implementation truth for bitchtea memory. It covers the code paths that create, read, search, inject, compact, and consolidate memory. It is intentionally about the actual behavior in the repository, not the design that the IRC metaphor might imply.

Memory has two storage families:

- Root hot memory: `MEMORY.md` in the active worktree.
- Scoped memory: `HOT.md` and dated daily markdown files under the session data root, partitioned by worktree and IRC-style context.

The agent now has per-context LLM histories in `Agent.contextMsgs`. Memory scope is a separate but synchronized routing layer: at turn start the UI switches the active transcript context, derives the memory scope from that same IRC context, and points the tool registry at that scope. Session JSONL stores context labels, not memory paths; memory storage is re-derived from the active IRC context and the worktree/session directories.

## Source Map

The relevant code is split across these packages:

- `internal/memory/memory.go`: scope model, paths, file append helpers, lexical search, rendered search output.
- `internal/agent/context.go`: exported agent-facing wrappers around the memory package, plus context-file discovery.
- `internal/agent/agent.go`: boot-time root memory injection, scoped hot-memory injection, compaction-time daily memory extraction.
- `internal/tools/tools.go`: `search_memory` and `write_memory` tool definitions and execution.
- `internal/llm/typed_search_memory.go`: fantasy typed wrapper for `search_memory`.
- `internal/llm/typed_write_memory.go`: fantasy typed wrapper for `write_memory`.
- `internal/ui/model.go`: active IRC context to memory scope mapping.
- `internal/ui/commands.go`: `/memory` command output.
- `internal/daemon/jobs/memory_consolidate.go`: daemon job that folds daily archive entries into hot memory.

## Scope Model

`internal/memory/memory.go` defines:

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

The constructors are pure value builders:

- `RootScope()` returns `Scope{Kind: ScopeRoot}`.
- `ChannelScope(name, parent)` returns `Scope{Kind: ScopeChannel, Name: name, Parent: parent}`.
- `QueryScope(name, parent)` returns `Scope{Kind: ScopeQuery, Name: name, Parent: parent}`.

An empty `Scope{}` is treated as root by path and lineage helpers. This matters because `tools.NewRegistry` does not explicitly initialize `Registry.Scope`; the zero value still searches and writes root memory.

### UI Scope Mapping

`ircContextToMemoryScope` in `internal/ui/model.go` maps the active UI context into memory scope:

- Channel context becomes `ChannelMemoryScope(ctx.Channel, nil)`.
- Subchannel context becomes `ChannelMemoryScope(ctx.Sub, &parentChannel)`.
- Direct/query context becomes `QueryMemoryScope(ctx.Target, nil)`.
- Any other context becomes `RootMemoryScope()`.

The parent for a normal channel or direct query is usually nil. `Scope.lineage()` still appends root if no explicit root parent is present, so channel and query searches inherit root memory even when the UI-created scope has no parent pointer.

This mapping is related to, but distinct from, `ircContextToKey`, which maps the same IRC context to the per-context transcript key used by `Agent.contextMsgs` and session `Entry.Context`. The transcript key is a label such as `#main`, `#ops.build`, or `buddy`; the memory scope is a structured `memory.Scope` that later resolves to `MEMORY.md`, scoped `HOT.md`, and scoped daily archive paths.

### Scope Lineage

`Scope.lineage()` returns scopes in current-to-root order. For a nested query under a channel, the order is:

```text
query -> channel -> root
```

This order is search order. Child scopes do not leak upward. A root search only sees root memory; it does not enumerate every channel or query.

### Scoped Path Segments

`Scope.relativePath()` converts lineage into a filesystem path from root outward, skipping root:

- Channel segment: `channels/<sanitized-name>`
- Query segment: `queries/<sanitized-name>`

`sanitizeSegment` lowercases, trims, replaces one or more non-`[a-z0-9]` bytes with `-`, trims surrounding `-`, and returns `unnamed` if the result is empty.

Examples:

```text
ChannelScope("#Dev", nil)             -> channels/dev
QueryScope("Coding Buddy", nil)       -> queries/coding-buddy
QueryScope("Coding Buddy", &channel)  -> channels/dev/queries/coding-buddy
```

## Worktree Storage Root

Scoped memory is grouped by worktree. `memoryBaseDir(sessionDir, workDir)` returns:

```text
filepath.Join(filepath.Dir(sessionDir), "memory", scopeName(workDir))
```

With the default session directory `~/.bitchtea/sessions`, scoped memory lives under:

```text
~/.bitchtea/memory/<scopeName(workDir)>/
```

`scopeName(workDir)` does this:

1. `filepath.Clean(workDir)`.
2. Take the base directory name.
3. Lowercase it.
4. Replace non-alphanumeric runs with `-`.
5. Trim surrounding `-`.
6. Use `root` if the base name is empty or `.`.
7. Append an 8-hex-digit FNV-1a 32-bit hash of the cleaned full path.

The final format is:

```text
<sanitized-worktree-basename>-<8-hex-fnv32a>
```

The hash prevents two repos with the same basename from sharing scoped memory.

## Hot Memory Paths

`HotPath(sessionDir, workDir, scope)` returns the writable hot-memory path for a scope.

Root and empty scope keep the legacy repository-local path:

```text
<workDir>/MEMORY.md
```

Non-root scopes use:

```text
<dirname(sessionDir)>/memory/<scopeName(workDir)>/contexts/<scope.relativePath()>/HOT.md
```

Examples:

```text
root:
  /repo/MEMORY.md

channel "#dev":
  ~/.bitchtea/memory/repo-<hash>/contexts/channels/dev/HOT.md

query "alice":
  ~/.bitchtea/memory/repo-<hash>/contexts/queries/alice/HOT.md

query "alice" under channel "#dev":
  ~/.bitchtea/memory/repo-<hash>/contexts/channels/dev/queries/alice/HOT.md
```

## Daily Memory Paths

`DailyPath(sessionDir, workDir, when)` is root-only shorthand for `DailyPathForScope(..., RootScope(), when)`.

Root daily memory uses:

```text
<dirname(sessionDir)>/memory/<scopeName(workDir)>/<YYYY-MM-DD>.md
```

Scoped daily memory uses:

```text
<dirname(sessionDir)>/memory/<scopeName(workDir)>/contexts/<scope.relativePath()>/daily/<YYYY-MM-DD>.md
```

The date comes from `when.Format("2006-01-02")`. The timestamp written inside entries uses RFC3339.

## Raw File Operations

### `Load(workDir)`

Reads:

```text
<workDir>/MEMORY.md
```

Behavior:

- Returns the full file as a string if `os.ReadFile` succeeds.
- Returns `""` for every read error, not just missing-file errors.
- Does not create the file.
- Does not distinguish permission errors, malformed content, or absence.

### `Save(workDir, content)`

Writes:

```text
<workDir>/MEMORY.md
```

Behavior:

- Calls `os.WriteFile(path, []byte(content), 0644)`.
- Replaces the whole file.
- Does not create `workDir`.

### `LoadScoped(sessionDir, workDir, scope)`

Reads `HotPath(sessionDir, workDir, scope)`.

Behavior:

- Returns file contents on success.
- Returns `""` on any read error.
- Root scope reads `MEMORY.md`.
- Non-root scope reads scoped `HOT.md`.

### `SaveScoped(sessionDir, workDir, scope, content)`

Writes `HotPath(sessionDir, workDir, scope)`.

Behavior:

- Creates the parent directory with mode `0755`.
- Replaces the whole file with mode `0644`.
- Root scope writes `MEMORY.md`, after creating `workDir` if needed through `MkdirAll(filepath.Dir(path))`.

### `AppendHot(sessionDir, workDir, scope, when, title, content)`

Appends one markdown entry to `HotPath`.

Behavior:

- `strings.TrimSpace(content)` is applied first.
- Empty trimmed content is a no-op and returns nil.
- Parent directories are created with mode `0755`.
- File is opened with `os.O_CREATE|os.O_APPEND|os.O_WRONLY`, mode `0644`.
- A kernel `flock(LOCK_EX)` is held around the write.
- The lock is released with `flock(LOCK_UN)` in a deferred call.
- There is no `fsync`.

The exact entry format is:

```text
## <heading>

<trimmed content>

```

If `title` is blank after trimming:

```text
<heading> = <when.Format(time.RFC3339)>
```

If `title` is nonblank:

```text
<heading> = <trimmed title> (<when.Format(time.RFC3339)>)
```

### `AppendDailyForScope(sessionDir, workDir, scope, when, content)`

Appends one durable daily checkpoint to `DailyPathForScope`.

Behavior:

- `strings.TrimSpace(content)` is applied first.
- Empty trimmed content is a no-op and returns nil.
- Parent directories are created with mode `0755`.
- File is opened with `os.O_CREATE|os.O_APPEND|os.O_WRONLY`, mode `0644`.
- A kernel `flock(LOCK_EX)` is held around the write.
- There is no `fsync`.

The exact entry format is:

```text
## <when.Format(time.RFC3339)> pre-compaction flush

<trimmed content>

```

`AppendDaily(sessionDir, workDir, when, content)` calls this with root scope.

## Search

`Search(sessionDir, workDir, query, limit)` calls `SearchInScope` with root scope.

`SearchInScope(sessionDir, workDir, scope, query, limit)` is lexical markdown search. There is no embedding index, no semantic ranker, and no per-entry scoring.

### Query Validation

`query` is trimmed first.

If the trimmed query is empty, the function returns:

```text
query is required
```

If `limit <= 0`, limit becomes `5`.

`queryTerms(query)` lowercases the query and splits with `strings.Fields`. Since `SearchInScope` has already rejected an empty trimmed query, normal input returns one or more whitespace-separated terms.

### Candidate Path Collection

`candidatePaths(sessionDir, workDir, scope)` walks `scope.lineage()` in current-to-root order. For each ancestor, it appends:

1. That ancestor's `HotPath`.
2. Every `.md` file in that ancestor's daily directory, sorted by reverse path string.

For date-named daily files, reverse path string means newest `YYYY-MM-DD.md` first.

Missing daily directories are ignored. A non-missing directory read error aborts search with:

```text
read daily memory dir: <underlying error>
```

Missing candidate files are skipped. A non-missing file read error aborts search with:

```text
read memory file <path>: <underlying error>
```

### Matching

Each candidate file is read as a whole string. The whole file must contain every query term, case-insensitively. If one term is absent, the entire file is skipped.

If all terms are present, the match index is:

1. The first index of the full lowercased query string in the lowercased file, or
2. The first index of the first query term that appears.

Only one `SearchResult` is produced per candidate file. If a file has ten matching sections, the search still returns one hit for that file.

The result contains:

```go
type SearchResult struct {
    Source  string
    Heading string
    Snippet string
}
```

`Source` is produced by `formatSource`:

- Root `MEMORY.md` is rendered exactly as `MEMORY.md`.
- Any other path is rendered relative to `filepath.Dir(sessionDir)` when possible.
- If relative path calculation fails, the absolute path is used.

`Heading` is produced by `nearestMarkdownHeading(content, matchIdx)`:

- Splits content on `\n`.
- Tracks the last line before the match whose text starts with `#`.
- Removes leading `#` characters and trims whitespace.
- Returns `""` if no previous heading exists.

`Snippet` is produced by `extractSnippet(content, matchIdx, 260)`:

- Uses runes, not bytes, for snippet length.
- If content is at most 260 runes, returns the whole trimmed content.
- Otherwise returns a 260-rune window centered around the match where possible.
- Prefixes `... ` if the window did not start at the file beginning.
- Suffixes ` ...` if the window did not reach the file end.

Search stops when `len(results) >= limit`.

### Rendered Search Output

`RenderSearchResults(query, results)` is the exact string surface used by the `search_memory` tool.

No results:

```text
No memory matches found for query "<query>".
```

With results:

```text
Memory matches for "<query>":

1. Source: <source>
Heading: <heading>
<snippet>

2. Source: <source>
Heading: <heading>
<snippet>
```

The `Heading:` line is omitted for a result with an empty heading. The final returned string has trailing newlines trimmed.

## LLM Tool Surface

The memory tools are defined in `internal/tools/tools.go` and exposed through the provider tool schema built by `internal/llm`.

### `search_memory`

Tool schema:

```json
{
  "query": "string, required",
  "limit": "integer, optional"
}
```

Execution path:

```text
LLM tool call
  -> fantasy typed search_memory wrapper, when typed tools are used
  -> Registry.Execute(ctx, "search_memory", argsJSON)
  -> execSearchMemory(argsJSON)
  -> memory.SearchInScope(reg.SessionDir, reg.WorkDir, reg.Scope, args.Query, args.Limit)
  -> memory.RenderSearchResults(args.Query, results)
  -> tool result text back to the LLM
```

`execSearchMemory` unmarshals JSON into:

```go
struct {
    Query string `json:"query"`
    Limit int    `json:"limit"`
}
```

JSON parse errors return:

```text
parse args: <underlying error>
```

The tool does not accept `scope` or `name`. Scope comes from `Registry.Scope`, which is updated by `Agent.SetScope` before turns in the UI. In headless or direct registry use, the zero-value scope behaves as root.

The typed wrapper checks cancellation before dispatch. A cancelled context returns a fantasy text error response containing:

```text
Error: context canceled
```

The wrapper converts executor errors into fantasy tool error text and does not return them as Go errors to the caller.

### `write_memory`

Tool schema:

```json
{
  "content": "string, required",
  "title": "string, optional",
  "scope": "string, optional",
  "name": "string, optional",
  "daily": "boolean, optional"
}
```

Execution path:

```text
LLM tool call
  -> fantasy typed write_memory wrapper, when typed tools are used
  -> Registry.Execute(ctx, "write_memory", argsJSON)
  -> execWriteMemory(argsJSON)
  -> memory.AppendHot(...) or memory.AppendDailyForScope(...)
  -> tool result text back to the LLM
```

`execWriteMemory` unmarshals JSON into:

```go
struct {
    Content string `json:"content"`
    Title   string `json:"title"`
    Scope   string `json:"scope"`
    Name    string `json:"name"`
    Daily   bool   `json:"daily"`
}
```

JSON parse errors return:

```text
parse args: <underlying error>
```

If `strings.TrimSpace(content)` is empty, it returns:

```text
content is required
```

Scope resolution starts with `Registry.Scope`, then applies `args.Scope`:

- `""` or `"current"`: keep `Registry.Scope`.
- `"root"`: use `RootScope()`.
- `"channel"`: require `name`, then use `ChannelScope(args.Name, &root)`.
- `"query"`: require `name`, then use `QueryScope(args.Name, &root)`.

Missing channel/query name returns:

```text
name is required when scope='channel'
```

or:

```text
name is required when scope='query'
```

Unknown scope returns:

```text
unknown scope "<scope>" (want 'current', 'root', 'channel', or 'query')
```

For hot writes (`daily == false`), the tool calls `AppendHot` and returns:

```text
Wrote <len(args.Content)> bytes to <HotPath(sessionDir, workDir, scope)>
```

The byte count is the original `args.Content` string length, not the trimmed content length and not the number of bytes written including heading/newlines.

For daily writes (`daily == true`), the tool builds the daily body like this:

- If `title` is blank, body is `content`.
- If `title` is nonblank, body is:

```text
### <trimmed title>

<content>
```

Then it calls `AppendDailyForScope` and returns:

```text
Appended <len(args.Content)> bytes to daily memory (<DailyPathForScope(sessionDir, workDir, scope, now)>)
```

The typed wrapper checks cancellation before dispatch. A cancelled context returns a fantasy text error response containing:

```text
Error: context canceled
```

## Agent Boot and Turn Boundaries

### Root Memory at Agent Construction

`NewAgentWithStreamer` loads root memory with:

```go
memory := LoadMemory(cfg.WorkDir)
```

If it is nonempty, the agent appends two bootstrap messages to `Agent.messages`:

```text
user: Here is the session memory from previous work:

<MEMORY.md contents>
```

```text
assistant: Got it.
```

This injection is root-only. Scoped `HOT.md` is not loaded at construction.

### System Prompt Memory Instructions

The system prompt includes the memory workflow text from `writeMemoryPrompt`. The operational requirements are:

- The agent should call `search_memory` before substantive work when prior decisions or history matter.
- The agent should call `write_memory` after meaningful work.
- Omitted or `current` scope is intended to mean active channel/query.
- `root` is for global facts.
- `channel` and `query` plus `name` can write to a different context.
- `daily=true` is for dated archive entries.
- The agent should search before writing to avoid duplicates.

This is prompt policy. Enforcement is partial: the tools enforce argument validity and scope routing, but nothing forces the model to search or write at the right moments.

### Scoped Hot Memory Injection

`Agent.SetScope(scope)` does three things:

1. Stores `a.scope = scope`.
2. Calls `a.tools.SetScope(scope)` so memory tools use the same scope.
3. Loads scoped hot memory from `LoadScopedMemory(a.cfg.SessionDir, a.cfg.WorkDir, scope)`.

If scoped hot memory is empty, no messages are injected.

If scoped hot memory is nonempty and its path has not already been injected in this agent lifecycle, two messages are appended:

```text
user: Context memory for <scopeLabel(scope)>:

<HOT.md contents>
```

```text
assistant: Got it.
```

`scopeLabel` renders:

- Channel: `#` followed by `scope.Name`.
- Query: `scope.Name`.
- Root/default: `root`.

The injected-path guard is path-based. Re-entering the same scoped path does not inject it again. `Agent.Reset()` clears `injectedPaths`, so scoped hot memory can be injected again after reset. Memory written after injection is not automatically re-injected into the transcript, although `search_memory` can still retrieve it from disk.

### Turn Start

`Model.startAgentTurn` calls:

```go
m.agent.InitContext(ctxKey)
m.agent.SetContext(ctxKey)
m.agent.SetScope(ircContextToMemoryScope(m.turnContext))
```

before starting the LLM turn. That is the strict turn boundary where the active UI context is copied into both the agent transcript context and the tool registry memory scope. Tool calls during that turn use the scope set there unless `write_memory` explicitly overrides it.

The transcript switch and memory-scope switch are separate calls. `SetContext` controls which `[]fantasy.Message` slice the LLM sees. `SetScope` controls which hot/daily memory files `search_memory`, `write_memory`, and scoped HOT injection use. If one is changed without the other in future code, context history and memory routing can diverge.

## Compaction and Daily Memory

`Agent.Compact(ctx)` both flushes durable memory and rewrites old transcript history.

If `len(a.messages) < 6`, compaction is a no-op. Exactly six messages do compact.

Before any LLM call, it checks `ctx.Err()`. If the context is already cancelled, it returns that error and does not start streaming.

For compacting histories, it computes:

```go
end := len(a.messages) - 4
```

It flushes:

```go
a.messages[1:end]
```

to daily memory extraction before replacing the transcript summary. Message `0` is the system prompt and is excluded. The last four messages are retained verbatim and are excluded from the flush slice.

### Durable Memory Extraction Prompt

`flushCompactedMessagesToDailyMemory` calls the streamer with one user message and no tool registry. The exact prompt shape is:

```text
Extract durable memory from this conversation slice before it is compacted.
Return concise markdown bullets covering only lasting facts: user preferences, decisions, completed work, relevant files, and open follow-ups.
Skip transient chatter and tool noise. If nothing deserves durable memory, reply with exactly NONE.

[<role>]: <truncateStr(messageText, 700)>
[<role>]: <truncateStr(messageText, 700)>
```

Only stream events with `Type == "text"` are accumulated. `done` ends naturally because the event channel closes. Error events are ignored here.

The accumulated text is trimmed. If it is empty or case-insensitively equal to `NONE`, nothing is appended.

Otherwise the text is appended with:

```go
AppendScopedDailyMemory(a.cfg.SessionDir, a.cfg.WorkDir, a.scope, time.Now(), text)
```

The daily flush is therefore scoped to the agent's active scope at compaction time.

### Transcript Summary Prompt

After durable memory extraction, `Compact` sends another no-tool streamer call with one user message shaped as:

```text
Summarize the following conversation concisely, preserving all important technical details, decisions made, files modified, and current state:

[<role>]: <truncateStr(messageText, 500)>
[<role>]: <truncateStr(messageText, 500)>
```

Only `text` stream events are accumulated. Error events are ignored here too.

The agent rewrites history to:

```text
system prompt
user: [Previous conversation summary]:

<summary text>
assistant: Got it.
<the previous last four messages, preserving tool metadata>
```

Tests verify that tool call metadata and tool result metadata survive when they are in the retained last four messages.

## `/memory` UI Command

`handleMemoryCommand` in `internal/ui/commands.go` is display-only. It does not search daily archives and does not call the LLM.

Behavior:

1. Load root `MEMORY.md` with `agent.LoadMemory(workDir)`.
2. If nonempty, truncate to 1000 bytes and append:

```text
\033[1;36m--- MEMORY.md ---\033[0m
<memory contents>
```

If truncation happens, this suffix is appended:

```text

... (truncated)
```

3. Convert the active UI context to memory scope.
4. If the scope is non-root, load scoped `HOT.md`.
5. If scoped hot memory is empty, show the system message:

```text
No HOT.md for <active.Label()> yet.
```

6. If scoped hot memory is nonempty, truncate to 1000 bytes and append:

```text
\033[1;36m--- HOT.md (<active.Label()>) ---\033[0m
<hot contents>
```

7. If root memory is empty and active scope is root, show:

```text
No MEMORY.md found in working directory.
```

The help table line is:

```text
/memory             Show MEMORY.md contents
```

That help text is incomplete for non-root contexts because the command can also display scoped `HOT.md`.

## Daemon Daily Consolidation

The daemon job kind is:

```text
memory-consolidate
```

It is implemented in `internal/daemon/jobs/memory_consolidate.go`.

### Job Args

The JSON args shape is:

```json
{
  "session_dir": "string",
  "work_dir": "string",
  "scope_kind": "root|channel|query",
  "scope_name": "string",
  "since": "YYYY-MM-DD"
}
```

`work_dir` can fall back to `job.WorkDir`. Scope kind/name can fall back to `job.Scope`. `session_dir` must be in args.

Missing work dir returns:

```text
memory-consolidate: work_dir is required
```

Missing session dir returns:

```text
memory-consolidate: session_dir is required
```

For channel/query scope without name:

```text
scope_name is required for channel/query scope
```

For unknown scope kind:

```text
unknown scope_kind "<kind>"
```

For invalid `since`:

```text
since must be YYYY-MM-DD: <underlying parse error>
```

### Scope Construction

`buildScope` supports root, channel, and query. Channel/query scopes created by the daemon have no nested parent. That means daemon consolidation can target a flat channel or query scope, but it cannot reconstruct a UI subchannel or query-under-channel lineage from `scope_kind` and `scope_name` alone.

### Daily File Selection

The job computes the daily directory from:

```go
filepath.Dir(memory.DailyPathForScope(sessionDir, workDir, scope, time.Now()))
```

`listDailyFiles`:

- Returns no files if the daily directory does not exist.
- Includes only non-directory `.md` files.
- If `since` is set, parses the filename stem as `YYYY-MM-DD` and skips files before the cutoff.
- If `since` is set and a filename stem is not a date, skips it.
- Sorts selected files ascending by path.

### Daily Entry Parser

`parseDailyEntries` reads a daily markdown file and splits entries on lines beginning with:

```text
## 
```

For each heading, it uses the first whitespace-delimited field after `## ` as the entry timestamp. Body lines until the next heading are trimmed and stored. Entries without a timestamp are skipped.

It is designed to read entries emitted by `AppendDailyForScope`, whose heading starts with an RFC3339 timestamp.

### Consolidated Hot Entry Format

The daemon appends daily entries into `HOT.md` with markers. The marker prefix is:

```text
<!-- bitchtea-consolidated:
```

The marker format is:

```text
<!-- bitchtea-consolidated:<daily filename>|<entry timestamp> -->
```

The exact appended block is:

```text
## consolidated <entry timestamp> <marker>

<trimmed entry body>

```

The daemon writes directly with `os.OpenFile(... O_CREATE|O_APPEND|O_WRONLY, 0644)` and creates parents with `0755`. It does not use `memory.AppendHot` and does not take a file lock; the implementation relies on the daemon's single-process lock model.

### Idempotency

Before appending, `loadConsolidatedMarkers` scans existing `HOT.md` for markers. If a marker is already present, the entry is skipped.

This makes repeated consolidation idempotent for entries with unique `(daily filename, timestamp)` pairs. It also means two different entries in the same daily file with the same timestamp collide and the later one is skipped. The test suite currently treats that collision as duplicate suppression.

### Job Output

Successful output JSON has this shape:

```json
{
  "hot_path": "<path>",
  "dailies_seen": <number>,
  "entries_added": <number>,
  "entries_skipped": <number>
}
```

Cancellation is checked before processing files and inside the file loop. A pre-cancelled context fails and does not create `HOT.md`.

## Test Coverage and Gaps

Existing tests cover the important happy paths and some boundaries:

- `internal/agent/context_test.go` covers root load/save, daily append heading shape, hot and daily search, scoped path layout, parent inheritance, child non-leakage, and rendered search output shape.
- `internal/tools/tools_test.go` covers `search_memory` through the registry, `write_memory` root hot writes, search-after-write, channel override writes, daily writes, missing content, and missing channel name.
- `internal/llm/typed_search_memory_test.go` covers typed wrapper schema, successful text response, registry scope routing, empty result as normal text, and cancellation as a tool error response.
- `internal/llm/typed_write_memory_test.go` covers typed wrapper schema, default-scope writes, root override over registry scope, missing content as tool error, and cancellation short-circuiting before filesystem writes.
- `internal/agent/compact_test.go` covers compaction boundary behavior, durable daily flush before summary rewrite, scoped daily flush path, retention of system prompt and last four messages, tool metadata preservation, scoped `HOT.md` injection once, and cancelled compaction before stream start.
- `internal/daemon/jobs/memory_consolidate_test.go` covers unique append, since cutoff, idempotency, cancellation, envelope-scope fallback, and required `work_dir`/`session_dir`.

Important gaps and sharp edges:

- There is no dedicated `internal/memory` package test file; most core behavior is tested through agent wrappers or tools.
- Per-context transcript switching and memory scope switching are separate operations that happen together in `Model.startAgentTurn`. Tests should cover them together; otherwise a future change could preserve isolated histories while routing memory to the wrong scope, or vice versa.
- Search tests mostly assert substrings and result shape. They do not exhaustively prove ranking, all-term matching, snippet centering, heading selection, read-error handling, or limit cutoff.
- Typed tool tests explicitly test the fantasy wrapper seam, not deep storage semantics.
- `search_memory` has no scope override argument. It only searches the registry's current scope.
- `write_memory` can override to a flat channel/query scope, but the override API cannot express nested channel/query parentage.
- Root hot memory lives in the repository as `MEMORY.md`; scoped hot memory lives outside the repo under the bitchtea data directory. That split is intentional compatibility behavior but easy to misread.
- `Load` and `LoadScoped` return empty string on any read failure, so permission errors and missing files look identical to callers.
- Search is lexical, case-insensitive, and file-level. It is not semantic retrieval and does not return multiple matches from one file.
- Compaction memory extraction depends on the LLM returning durable bullets. There is no verifier that the extracted facts are true, complete, or non-duplicative.
- Compaction ignores streamer error events during both memory extraction and summary generation.
- The prompt tells the model to search and write memory, but this is not enforced beyond normal tool availability.
- The `/memory` command does not show daily archives.
- The `/memory` help line says only `MEMORY.md` even though scoped contexts can display `HOT.md`.
- Daemon consolidation cannot reconstruct nested scope parents from its current `scope_kind`/`scope_name` args.
- Daemon marker idempotency keys on daily filename plus timestamp, so same-timestamp distinct entries collide.
- Append helpers use `flock` but no `fsync`; daemon consolidation appends without `flock`.
