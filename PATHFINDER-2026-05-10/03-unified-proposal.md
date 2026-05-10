# Phase 3 — Unified Proposal

**Date:** 2026-05-10
**Orchestrator:** Pathfinder (self)

---

## Methodology

Every proposed unification must satisfy all three:
1. **Net reduction in code** — one path replaces ≥2, not an abstraction layer on top
2. **No loss of capability** — every caller of the old paths must be fully served by the new
3. **Anti-pattern rejection** — no registry/factory when a function call suffices; no feature flags

Deletions win. Simplicity wins.

---

## D6 → `internal/toolerror` Package

### Concern
`wrapToolError` (tools.go:31) produces an XML envelope `<tool_call_error><cause>...</cause><reflection>...</reflection></tool_call_error>` with a self-correction prompt. This pattern should also apply to:
- MCP tool call failures (`internal/mcp/client.go` result processing)
- Daemon job failures (optional, lower priority)

### Proposed Unified Design

**New package:** `internal/toolerror/`
**Single entry point:** `func Wrap(err error) string`

```go
// internal/toolerror/wrap.go

const reflectionPrompt = "Reflect on the error above: (1) identify exactly what went wrong with the tool call, (2) explain why that mistake happened, (3) make the corrected tool call. Do NOT skip this reflection."

func Wrap(err error) string {
    if err == nil {
        return ""
    }
    return "<tool_call_error>\n<cause>" + err.Error() + "</cause>\n<reflection>" + reflectionPrompt + "</reflection>\n</tool_call_error>"
}
```

### Call Site Rewrite

| Old Location | Change |
|---|---|
| `internal/tools/tools.go:31` | Replace inline `wrapToolError` with `toolerror.Wrap(err)` |
| `internal/mcp/client.go:308` (`resultFromSDK`) | Replace error result with `toolerror.Wrap(err)` |

### Loss of Capability?
No. MCP errors currently just return `err.Error()` string. The structured envelope with reflection prompt will improve model self-correction — strictly better.

### Anti-Pattern Rejection
- Not a registry/factory — single function
- Not a struct with state — stateless function
- No new abstraction layer — old code inlined it directly; new code makes it shareable

---

## D8 → Shared `MutateConfig` Function

### Concern
Config field mutation happens in three places with different side-effect profiles:
1. `applySetToConfig` (rc.go:196–305) — 110-line switch; setting provider/model/apikey/baseurl clears `Profile`; baseurl sets `Service='custom'`
2. `ApplyProfile` (config.go:480–500) — builds from `builtinProfiles` spec + env var filling
3. `/set key value` runtime dispatch (commands.go:129 + agent re-config after)

The rc and runtime paths are structurally similar but diverge on side effects.

### Proposed Unified Design

A single `MutateConfig(cfg *Config, key, value string) error` function in `internal/config/` that consolidates field-level validation and side-effect rules. Both rc.go and runtime `/set` call through it.

```go
// internal/config/mutate.go
func MutateConfig(cfg *Config, key, value string) error {
    // validate key
    // apply value to cfg
    // side effect: clear Profile fields where needed
    // side effect: set Service='custom' where needed
    return nil
}
```

### Call Site Rewrite

| Old Location | New |
|---|---|
| `internal/config/rc.go:196` (`applySetToConfig`) | `MutateConfig(cfg, key, value)` |
| `internal/ui/commands.go:129` (`handleSetCommand`, key+value path) | `config.MutateConfig(cfg, key, value)` |

**What stays different:** `/set bare-key` (show value/picker) and `/set` (list keys) are display concerns, not mutation — they stay in commands.go.

**What stays different:** `ApplyProfile` (profile loading) is a full-replace operation, not a field-level update — it stays separate.

### Loss of Capability?
No. The consolidated function handles the same keys with the same side effects.

### Anti-Pattern Rejection
- Not a registry/factory — switch statement with `goto case`-equivalent fallthrough is replaced by a lookup map
- No feature flag — all existing keys are explicitly handled
- Not adding a new abstraction layer — consolidating two existing code paths, not one

---

## D2 → `internal/lockfile` Package (Defer)

### Concern
`syscall.Flock` pattern appears in three locations:
- `session.go:147` — append lock
- `memory.go:103` — hot append lock
- `lock.go:31` — daemon singleton lock

### Proposed Unified Design

```go
// internal/lockfile/lockfile.go

// LockExclusive acquires a non-blocking exclusive flock on path.
// Caller must call Release().
func LockExclusive(path string) (*Lock, error)

// LockShared acquires a shared flock (for read-only concurrent access).
func LockShared(path string) (*Lock, error)

type Lock struct {
    f *os.File
    // ...
}

func (l *Lock) Release() error
```

### Why Defer
This consolidation saves ~30 lines across 3 call sites but introduces a new package. The current pattern is stable and well-understood. Unless a new call site emerges or the pattern needs to change (e.g., deadlock investigation), the cost of a new package outweighs the savings.

**Recommendation:** Flag this for the next time any lock-adjacent bug occurs, then fix it then.

---

## Combined System Diagram

Below is the proposed unified system incorporating D6 and D8. D2 is deferred.

```mermaid
flowchart TD
    subgraph "Config System (D8)"
        RC[rc.go: ParseRC + ApplyRCSetCommands] --> MC["MutateConfig(cfg, key, value)<br/>internal/config/mutate.go"]
        SET[commands.go: handleSetCommand] --> MC
        MC --> |key == provider/model/apikey/baseurl| CLEAR["cfg.Profile = ''<br/>Service = 'custom' if baseurl"]
        MC --> |key == profile| AP["ApplyProfile(cfg, p)<br/>config.go:480"]
        MC --> |other keys| DIRECT[direct field update]
        CLEAR --> CFG[cfg: Config]
        AP --> CFG
        DIRECT --> CFG
    end

    subgraph "Error Wrapping (D6)"
        TOOLS[tools.go: Execute] --> TW["toolerror.Wrap(err)<br/>internal/toolerror/wrap.go"]
        MCP[mcp/client.go: CallTool] --> TW
        TW --> XML["<tool_call_error><br/><cause>err</cause><br/><reflection>...</reflection><br/></tool_call_error>"]
    end

    subgraph "Tool Execution (existing)"
        EXEC[Execute(name, args)] --> DISPATCH[switch name dispatch]
        DISPATCH --> TW
    end

    subgraph "LLM Client (existing)"
        STREAM[StreamChat] --> FANTASY[fantasy.Stream]
        FANTASY --> TW
    end
```

---

## What Each Old Call Site Becomes

| ID | Old Call | New Call |
|---|---|---|
| D6 | `tools.go: wrapToolError(err)` | `toolerror.Wrap(err)` (1-line import change) |
| D6 | MCP `resultFromSDK` error branch | `toolerror.Wrap(err)` (1-line change in client.go) |
| D8 | `rc.go: applySetToConfig(cfg, k, v)` | `config.MutateConfig(cfg, k, v)` |
| D8 | `/set key value` in `handleSetCommand` | `config.MutateConfig(cfg, key, value)` |

---

## Loss Assessment

| Change | What's Lost | Acceptable? |
|---|---|---|
| D6 `toolerror.Wrap` | `wrapToolError` no longer tools-internal | YES — `toolerror` package is tools-adjacent, not external |
| D8 `MutateConfig` | separate rc-vs-runtime dispatch paths | YES — they did the same thing; consolidated is clearer |

**No capability lost in either change.**
