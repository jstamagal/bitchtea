# Phase 4 — Handoff Prompts

**Date:** 2026-05-10

---

## Handoff Prompt: D6 — `internal/toolerror` Package

### Target
New package `internal/toolerror/wrap.go` exporting `func Wrap(err error) string`.

### Single Entry Point
```go
package toolerror

const reflectionPrompt = "Reflect on the error above: (1) identify exactly what went wrong with the tool call, (2) explain why that mistake happened, (3) make the corrected tool call. Do NOT skip this reflection."

func Wrap(err error) string {
    if err == nil {
        return ""
    }
    return "<tool_call_error>\n<cause>" + err.Error() + "</cause>\n<reflection>" + reflectionPrompt + "</reflection>\n</tool_call_error>"
}
```

### Exact Call Sites to Rewrite

1. **`internal/tools/tools.go:31`** — delete the existing `wrapToolError` function and its `reflectionPrompt` constant. Add `import "github.com/jstamagal/bitchtea/internal/toolerror"` and replace every call site `wrapToolError(err)` with `toolerror.Wrap(err)`.

2. **`internal/mcp/client.go:308`** (`resultFromSDK` function) — when `err != nil`, replace `res.Text = err.Error()` with `res.Text = toolerror.Wrap(err)`. Add the same import.

### Flowchart Reference
`01-flowcharts/tool-registry.md` (error wrapping section)
`01-flowcharts/mcp.md` (call dispatch → `resultFromSDK`)

### Anti-Pattern Guards
- **Do NOT** make it a struct with methods — a package-level function is sufficient
- **Do NOT** add a registry or factory — there is only one error transformation
- **Do NOT** change the XML envelope format — preserve it exactly so existing LLM prompt engineering stays valid
- **Do NOT** export the `reflectionPrompt` constant — callers don't need it
- **Do NOT** wrap nil — return `""` for nil so callers can skip conditional checks

### Verification
Run `go build ./...` and `go test ./...` after changes. The MCP client has integration tests — verify they pass.

---

## Handoff Prompt: D8 — Shared `MutateConfig` Function

### Target
New function `MutateConfig(cfg *Config, key, value string) error` in `internal/config/mutate.go`.

### Single Entry Point
```go
// internal/config/mutate.go
package config

// MutateConfig applies a key=value setting to cfg.
// It handles all side effects: clearing Profile on provider/model/apikey/baseurl,
// setting Service='custom' on baseurl, etc.
// Returns an error for unrecognized keys.
func MutateConfig(cfg *Config, key, value string) error { ... }
```

### Exact Call Sites to Rewrite

1. **`internal/config/rc.go:193–305`** — the `applySetToConfig` switch statement (lines 196–305) is the target. Extract it into `MutateConfig` in a new file. Keep the rc-file line-by-line iteration in `ApplyRCSetCommands`, but replace the per-key block with `MutateConfig(cfg, key, value)`.

2. **`internal/ui/commands.go:129`** (`handleSetCommand`, key+value path, around line 200+ depending on build) — replace the inline field update logic with `config.MutateConfig(m.config, key, value)`. After the call, if the key was `profile`, additionally call `m.agent.SetProvider/Model/BaseURL/APIKey` from the loaded profile (this is the part `MutateConfig` cannot cover — agent re-wiring is commands-specific).

### Flowchart Reference
`01-flowcharts/config.md` (ApplyRCSetCommands flow)
`01-flowcharts/slash-commands.md` (/set handler)

### Anti-Pattern Guards
- **Do NOT** replace `ApplyProfile` — full profile loading is a different operation (replaces all fields vs. field-level update)
- **Do NOT** handle `/set bare-key` (display/picker) in `MutateConfig` — that's a read operation, not a mutation
- **Do NOT** add a feature flag — the function handles all existing keys explicitly
- **Do NOT** merge the rc loop iteration into `MutateConfig` — the loop belongs in rc.go, the per-key logic is what consolidates
- **Do NOT** change the side-effect rules (clearing Profile, setting Service='custom') — match the existing behavior exactly so rc files behave the same after the refactor
- **Verify**: After refactoring, run `bitchtea` with an rc file that sets `provider`, `model`, `baseurl`, and `profile` — verify each behaves identically to before

### Verification
- Run `go build ./...` and `go test ./...`
- Run `go test -race ./...`
- Manually test: write a `.bitchtearc` with `set model gpt-4o`, `set provider openai`, `set profile ollama` — verify each takes effect
- Compare session behavior with and without the refactor to confirm no behavioral changes

---

## Handoff Prompt: D2 — `internal/lockfile` Package (DEFERRED)

### Target
New package `internal/lockfile/lockfile.go` — deferred until a lock-related bug occurs or a new caller emerges.

### Current Call Sites (do not change yet)
- `internal/session/session.go:147` — `syscall.Flock(LOCK_EX)`
- `internal/memory/memory.go:103` — `syscall.Flock(LOCK_EX)` in `AppendHot`/`AppendDailyForScope`
- `internal/daemon/lock.go:31` — `Acquire()` pattern

### Flowchart Reference
`01-flowcharts/session.md` (APPEND path)
`01-flowcharts/memory.md` (AppendHot flow)
`01-flowcharts/daemon.md` (lock subgraph)

### Anti-Pattern Guards (for when this is implemented)
- **Do NOT** create a global singleton — lock files are per-resource
- **Do NOT** use a map as a cache — the OS flock already handles concurrent access correctly
- **Do NOT** add retry logic — flock is synchronous by design; retry belongs in callers
- Keep it minimal: `LockExclusive(path)` returning a `*Lock` with `Release()` method
