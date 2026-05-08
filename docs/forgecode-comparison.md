# Bitchtea ↔ Forgecode comparison

A factual comparison of bitchtea's tool surface and architectural patterns vs. **Forgecode** ([router-for-me/CLIProxyAPI maintainers' agent harness](https://github.com/router-for-me/CLIProxyAPI), Rust workspace at `/home/admin/forgecode/` and `~/forgecode/`). Compiled from inventories produced during the 2026-05-08 audit pass; see PLAN.md section 2.1 for context.

This doc is for figuring out **what to steal from forgecode next**. It is NOT a quality judgment — both projects optimize for different scopes. Forgecode is the #1 harness on TerminalBench Hard; bitchtea optimizes for IRC-style multi-context chat with persistent memory + a full PTY suite.

---

## Tool surface — counts and lists

| Project | Tool count | Tools |
|---|---|---|
| **Forgecode** | 16 | `read`, `write`, `fs_search`, `sem_search`, `remove`, `patch`, `multi_patch`, `undo`, `shell`, `fetch`, `followup`, `plan`, `skill`, `todo_write`, `todo_read`, `task` |
| **Bitchtea** | 14 | `read`, `write`, `edit`, `bash`, `terminal_start`, `terminal_send`, `terminal_keys`, `terminal_snapshot`, `terminal_wait`, `terminal_resize`, `terminal_close`, `search_memory`, `write_memory`, `preview_image` |

---

## What forgecode has that bitchtea doesn't

### `sem_search` — vector/semantic codebase search
Forgecode ships a vector-backed semantic search over the workspace. Bitchtea relies on `bash` calling `rg`/`grep` for text search — no semantic layer. This is genuinely useful for "find the thing that does X" queries where the model doesn't know the exact name.

**Steal-cost:** high. Requires an embedding model, an index, and a vector store. Probably not worth it solo; if added, gate behind a config flag.

### `multi_patch` — batched string-replace edits in one call
A single tool call applies N independent edits to one file. Bitchtea has `edit` but each edit is its own tool call. Reduces round-trips on large refactors.

**Steal-cost:** low. One new tool that loops over an `edits[]` array internally. Worth adding as bead.

### `undo` — undo last file operation
Tracked sequence of file ops, with reversal. Bitchtea has nothing equivalent — you'd manually `git checkout` to recover.

**Steal-cost:** medium. Needs an in-process op log + reverse semantics for write/edit/remove.

### `fetch` — HTTP GET
Pulls a URL and returns the body. Bitchtea has nothing — model would have to use `bash curl`, which is fine but less integrated (no auto-truncation, no MIME handling).

**Steal-cost:** low. One new tool wrapping `net/http`.

### `followup` — interactive question-with-options to the user
Mid-turn, the model can ask the user a multiple-choice question and pause for the answer before continuing. Bitchtea's TUI doesn't have this hook.

**Steal-cost:** medium. Needs a UI prompt component + an event type the agent waits on.

### `plan` — create/update plan files
Workflow scaffolding — model maintains a `plan.md` for multi-step tasks. Bitchtea's memory + `MEMORY.md` partially overlaps but isn't the same shape.

**Steal-cost:** low. Could be implemented as a thin wrapper around `write` with a fixed naming convention.

### `skill` — fetch named skill definitions
Skills live as named documents the model can pull on demand (separate from system-prompt context). Useful for keeping the system prompt small but giving the model access to detailed playbooks when needed.

**Steal-cost:** medium. Needs a skills directory + a lookup tool. Conceptually similar to `search_memory` but read-only and named instead of searched.

### `todo_write` / `todo_read` — task tracking
In-memory todo list scoped to the conversation. Bitchtea has TaskCreate/TaskList/TaskUpdate via the harness, but those aren't surfaced as model tools — only as out-of-band Claude Code task tools. Forgecode's are agent-callable.

**Steal-cost:** low. Two thin tools backed by an in-memory map per session.

### `task` — agent delegation
The model can spawn a sub-agent with a focused task. Returns the sub-agent's final output. Bitchtea has nothing equivalent — you'd have to drop to the harness layer to spawn another agent.

**Steal-cost:** high. Needs the full sub-agent runtime. Probably out of scope unless bitchtea grows into a multi-agent orchestrator.

---

## What bitchtea has that forgecode doesn't

### Full PTY suite (7 tools vs. forgecode's 1 `shell`)
`terminal_start` / `terminal_send` / `terminal_keys` / `terminal_snapshot` / `terminal_wait` / `terminal_resize` / `terminal_close`. Forgecode's `shell` is one-shot — run command, get output, done. Bitchtea can drive interactive REPLs, vim, ncurses TUIs, anything that needs a real PTY. **This is bitchtea's standout differentiator.**

### `search_memory` / `write_memory` with scoped memory
Two-tier memory (hot MEMORY.md + daily archive) with scope (root / channel / query). Forgecode has nothing equivalent — its `plan` and skill mechanisms are different shapes.

### `preview_image`
ANSI block-art image rendering in terminal. Niche but novel.

### Per-context message histories
`internal/agent/context_switch.go` — `/join #channel`, `/query nick` swap the agent's active message slice. Forgecode is single-conversation.

### Daemon-backed checkpoint + memory consolidation
`internal/daemon/` runs `session-checkpoint` and `memory-consolidate` jobs out of process. Forgecode is in-process only.

### MCP integration
`internal/mcp/` lets bitchtea expose itself as MCP server AND consume MCP tools from external servers. Forgecode doesn't speak MCP.

---

## Architectural patterns where forgecode is genuinely better

### 1. Tool descriptions as standalone `.md` files with template interpolation
Forgecode keeps each tool's description in its own markdown file under `crates/forge_domain/src/tools/descriptions/`, baked at compile time via a `#[derive(ToolDescription)]` proc macro. Templates use `{{tool_names.shell}}` etc. so cross-references stay correct as tool names evolve.

**Bitchtea today:** descriptions live inline in `Definitions()` in `internal/tools/tools.go`. Easy to drift, no template support.

**Steal:** use `//go:embed` + `text/template` for tool descriptions in Go. Bead `bt-zer` (deferred refactor) loosely covers this as part of typed-input-structs work.

### 2. Structured tool-error result with inline reflection prompt ✓ ADOPTED
Forgecode wraps every tool error in `<tool_call_error><cause>...</cause><reflection>...</reflection></tool_call_error>` where the reflection text is a static markdown nudge telling the model what to try next. **Agent E shipped this in commit `61c00a3` as Pattern 1.**

### 3. Read-before-edit guard via `files_accessed` set ✓ ADOPTED
Tracks files read in the current turn; rejects edit/write(overwrite=true) calls on files not yet read. **Agent E shipped this in commit `61c00a3` as Pattern 2.**

### 4. Head+tail truncation with overflow temp file pointer ✓ ADOPTED
Preserves head + separator + tail; writes full output to a temp file; embeds the temp path in the result so the model can `read` the rest. **Agent E shipped this in commit `61c00a3` as Pattern 3.**

### 5. Per-tool timeout with explicit bypass for delegating tools ✓ ADOPTED
Wraps every tool call in `context.WithTimeout`, with bypass for the agent-delegation `task` tool. **Agent E shipped this in commit `61c00a3` as Pattern 4. SET key `tool_timeout` exposed.**

### 6. Typed input structs with auto-generated JSON Schema [bead bt-zer, deferred]
Forgecode defines each tool's parameters as a named Rust struct with `#[derive(JsonSchema)]`, so the JSON Schema sent to the model is derived directly from the same types used for deserialization — they cannot diverge.

**Bitchtea today:** hand-written `map[string]interface{}` schemas in `Definitions()`, separately re-parsed with anonymous structs in each `exec*` function. Dual maintenance.

**Steal-cost:** large refactor. Bead `bt-zer`. Defer until current changes settle.

### 7. Loop detection via consecutive-call counter
Forgecode tracks `consecutive_calls` and fires an intervention prompt when the model is stuck in a loop. Bitchtea has nothing equivalent — agents can spin forever (well, until the 64-step `maxAgentSteps` cap).

**Steal-cost:** low. Track per-tool consecutive-call count; inject a reflection nudge after N (configurable, e.g., 5) identical-arg calls.

### 8. Schema aliases for parameter names
Forgecode adds `#[serde(alias = "path")]` on `file_path` etc. — catches the model's natural intuition about field names without relying on exact training-data recall.

**Steal-cost:** low (and high-value). When typed input structs land (bt-zer), include common aliases on each field: `path`/`file_path`, `text`/`content`, `query`/`pattern`/`search`, etc.

### 9. Compile-time description baking via proc-macro
Descriptions live outside Rust code (easier to edit) but get embedded into the binary at build time. No file I/O at runtime.

**Steal:** Go's `//go:embed` does this trivially. Bundle in with the tool-descriptions-as-files refactor (#1 above + bt-zer).

### 10. Runtime template variables in descriptions
`{{config.stdoutMaxPrefixLength}}` etc. — descriptions can reference live config values, so the model sees the actual current limits.

**Steal-cost:** low. Once descriptions are templated (Go's `text/template`), pass the live config in as the template's `.Config` field.

---

## Architectural patterns where bitchtea is genuinely better

### 1. PTY suite is way more capable than forgecode's `shell`
Seven coordinated tools for driving interactive terminal apps (REPLs, editors, TUIs). Forgecode's `shell` is one-shot — can't drive vim, can't paste into a REPL, can't wait for a prompt to appear before sending input. For terminal-heavy workflows (which TerminalBench Hard is named for, ironically) bitchtea has the deeper toolkit.

### 2. Per-context message histories
IRC-style `/join`/`/query` separation lets one bitchtea session juggle multiple conversations cleanly. Forgecode is one-conversation-at-a-time.

### 3. Daemon-backed checkpoint + memory consolidation
Out-of-process jobs handle expensive work (session checkpointing, memory consolidation) without blocking the TUI loop. Forgecode does this in-process.

### 4. MCP both ways (server + client)
Bitchtea can expose itself as an MCP server AND consume tools from external MCP servers. Forgecode doesn't speak MCP at all.

### 5. Two-tier scoped memory model
Hot `MEMORY.md` per scope + daily durable archive; per-channel/per-query/per-root scoping. Forgecode's skill/plan files are simpler but less scope-aware.

### 6. Persona-as-runtime-loadable file (post-2026-05-08)
`set persona_file` lets the persona content live outside the repo, swappable at runtime. Useful for keeping spicy / private personas off public github while still shipping a clean default. Forgecode's persona is hardcoded in templates.

---

## Top 5 patterns for bitchtea to adopt next (ranked by impact-per-effort)

1. **Schema aliases on parameter names** — paired with bt-zer typed-input-structs; lets the model name fields naturally without rote recall. Tiny code change, real quality-of-life win.
2. **Loop detection via consecutive-call counter** — catches stuck-in-a-rut agents before they burn through the 64-step cap. Low complexity.
3. **Tool descriptions as embedded .md files with templating** — paired with bt-zer; makes descriptions editable without recompile and keeps live config values surfaced to the model.
4. **`multi_patch` tool** — reduces round-trips on multi-edit refactors. Low complexity.
5. **`fetch` tool** — clean HTTP GET integrated with truncation/MIME handling. Low complexity.

---

## What this comparison is NOT

- Not a comprehensive Forgecode feature list (e.g., its OAuth fan-out, multi-provider routing, beta header injection — those live in CLIProxyAPI, not the agent harness layer)
- Not a quality judgment on either project; both target different scopes
- Not a roadmap commitment — beads are the source of truth for what's actually in flight

End of comparison.
