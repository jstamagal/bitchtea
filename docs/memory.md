# 🦍 MEMORY TIERS 🦍

This scroll documents how `bitchtea` remembers and forgets. 

## 1. IRC Tiers & Scope (`internal/memory/memory.go:22`)

Memory is partitioned into hierarchical scopes to prevent context pollution.

- **`ScopeRoot`**: The global truth. Maps to `MEMORY.md` at the project root.
- **`ScopeChannel`**: Context-specific memory. Maps to a `HOT.md` file for a specific IRC channel.
- **`ScopeQuery`**: Private DM memory.

### Inheritance
When searching memory (`SearchInScope`), the agent walks the **lineage** from the current scope up to the Root scope. This allows a channel to "inherit" global project knowledge (`memory.go:308`).

## 2. Memory Discovery

The agent automatically scans the canopy as it swings. 

### Discovery Walking (`internal/agent/context.go:14`)
`DiscoverContextFiles` walks UP from the working directory to the system root, looking for:
- `AGENTS.md`
- `CLAUDE.md`
- `.agents.md`
- `.claude.md`

These are concatenated and injected into the system prompt to provide project-specific rules.

### @File Expansion (`internal/agent/context.go:130`)
Inline file references (e.g., `fix @main.go`) are expanded into the prompt with their actual contents before being sent to the LLM.

## 3. Storage Paths

- **Hot Memory**: `~/.bitchtea/memory/<scope-hash>/contexts/<path>/HOT.md`
- **Daily Memory**: `~/.bitchtea/memory/<scope-hash>/daily/YYYY-MM-DD.md`

When a conversation is compacted, old entries are flushed to the daily memory files to keep the context window lean while preserving the "bones" for later recall (`memory.go:114`).

APE STRONK TOGETHER. 🦍💪🤝
