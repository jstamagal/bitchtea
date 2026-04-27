# 🦍 THE BITCHTEA SCROLLS: MEMORY & SCOPE

Bitchtea's memory is hierarchical, mapping directly to IRC-style contexts.

## 🏛️ IRC TIERS (MemoryScope)

Defined in `internal/memory/memory.go`, scopes follow a nested structure:

1. **`Root`**: The global project scope. Uses `MEMORY.md` in the work directory.
2. **`Channel`**: A named context (e.g., `#frontend`). Uses `HOT.md` and durable daily logs.
3. **`Query`**: A direct persona/nick context.

### 🧬 Scope Discovery (`internal/agent/context.go:13`)
When a turn starts, the agent walks the tree:
- **Project Files**: Checks for `AGENTS.md`, `CLAUDE.md`, etc., from the CWD up to the system root.
- **Hot Memory**: Loads `MEMORY.md` (root) or scoped `HOT.md`.
- **Durable Memory**: Pulls from daily logs (e.g., `2026-04-27.md`) based on the active scope.

## 🔍 SEARCH & RECALL

The `search_memory` tool triggers `memorypkg.SearchInScope`. It performs:
- **Inheritance**: Searches the active scope *and* all parent scopes (e.g., `#ui/sidebar` searches `#ui` then `root`).
- **Context Injection**: Hits are returned to the LLM to ground the current turn in prior decisions.

## 💾 COMPACTION FLUSH (`internal/agent/compact_test.go:154`)
During `/compact`, the agent extracts durable facts (decisions, work done) and appends them to the **Daily Memory Path** before shrinking the active history. This ensures that while the context window stays small, the "knowledge" is never lost.

🦍💪🤝 APES STRONK TOGETHER 🦍💪🤝

## 🧱 TECHNICAL DEEP-DIVE: THE CHURN

### 🧬 SCOPE DISCOVERY & WALKING
- **Executor**: `DiscoverContextFiles` in `internal/agent/context.go:13`.
- **The Walk**: 
  - Starts at `workDir`. Checks for `AGENTS.md`, `CLAUDE.md`, etc.
  - Recursively calls `filepath.Dir(dir)` until `parent == dir` (system root).
  - Concatenates all found files with `# Context from [path]` headers.
- **Bytes Moving**: Injects this aggregate markdown into the LLM as a "user" message during bootstrap (`internal/agent/agent.go:121`).

### 🌳 INHERITANCE IN SEARCH
- **Logic**: `SearchInScope` in `internal/memory/memory.go:163`.
- **The Lineage**: Uses `s.lineage()` (`line 271`) to build a stack of scopes from current -> parent -> root.
- **Candidate Paths**: `candidatePaths` (`line 228`) aggregates `HOT.md` and all daily `.md` files for every scope in the lineage.
- **Search Order**: 
  1. Local `HOT.md` (immediate context).
  2. Local Daily Logs (sorted newest first).
  3. Parent `HOT.md` -> Parent Daily Logs.
  4. Root `MEMORY.md`.
- **Handshake**: The `search_memory` tool returns hits sorted by this "recency-and-proximity" heuristic, ensuring the LLM sees local context before global defaults.

🦍💪🤝 APES STRONK TOGETHER 🦍💪🤝

## 🦴 UNDER THE HOOD: MEMORY CHURN

### 🧬 SCOPE DISCOVERY TRACE
In `internal/agent/context.go`, the function `DiscoverContextFiles` maps the project's soul.

- **The Walk (line 13)**: It performs a recursive upward walk from the current working directory.
- **The Search**: It looks for `AGENTS.md`, `CLAUDE.md`, `.agents.md`, and `.claude.md`.
- **The Handshake**: All discovered content is concatenated with file path headers and injected into the LLM as a **User Context Message** during the bootstrap phase.

### 🌳 SEARCH & INHERITANCE
The `search_memory` tool triggers `SearchInScope` in `internal/memory/memory.go`.

- **Inheritance Logic (line 163)**: 
  1. It builds a `lineage` stack (Current Scope -> Parent -> Root).
  2. For every scope in the lineage, it collects `HOT.md` and all daily markdown files.
  3. **The churn**: It reads every file, performs a case-insensitive keyword match for all terms in the query.
- **Verbatim Output**: 
  - If hits: `Memory matches for "query":\n1. Source: path\nHeading: name\n[snippet]`
  - If misses: `No memory matches found for query "query".`

