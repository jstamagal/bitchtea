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
