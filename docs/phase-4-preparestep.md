# Phase 4: PrepareStep Responsibilities and Ordering

Status: design only. Implementation tracked under epic `bt-p4`. Sibling tasks
`bt-p4-cache`, `bt-p4-queue`, `bt-p4-tool-refresh`, and `bt-p4-verify`
implement and test the pieces described here. This doc is the contract those
tasks must satisfy.

## What is PrepareStep

`PrepareStep` is the per-step hook fantasy gives us between turns of an
in-flight `Stream` call (`/home/admin/go/pkg/mod/charm.land/fantasy@v0.17.1/agent.go:96-112`).
Fantasy invokes it **once per model call** inside the agent loop, after the
prior step's tool results have been folded into `stepInputMessages` and
*before* `stepModel.Generate` runs (`agent.go:380-462`).

Today, `internal/llm/stream.go:93` installs a near-trivial PrepareStep that
only emits a `thinking` event. Phase 4 grows it into the single chokepoint
that owns per-step request mutation. Concerns currently scattered across
`internal/llm`, `internal/agent`, and `internal/ui` move under this one hook.

Signature reminder:

```
func(stepCtx context.Context, opts fantasy.PrepareStepFunctionOptions)
    (context.Context, fantasy.PrepareStepResult, error)
```

`PrepareStepFunctionOptions` carries `Steps`, `StepNumber`, `Model`, and the
already-assembled `Messages` for this step. `PrepareStepResult` lets us
substitute `Messages`, `Model`, `System`, `ToolChoice`, `ActiveTools`,
`Tools`, or `DisableAllTools` for this step only.

## Concerns owned by PrepareStep

PrepareStep owns five concerns. They run in a fixed order on every step.

### 1. Cancellation pre-check (Phase 8 interaction)

Cheapest, no allocations, no I/O. Returning `ctx.Err()` from PrepareStep
aborts the fantasy loop cleanly via the existing `OnError` path
(`stream.go:131`) and our existing `errors.Is(ev.Error, context.Canceled)`
branch in `agent.go:231` records `lastTurnState = turnStateCanceled`.

This must run before queue drain or tool refresh so that an Esc x2 / Ctrl+C
arriving between steps does not silently consume a queued prompt or trigger
a tool-list rebuild for a turn that is already dying. See
`docs/phase-8-cancellation-state.md` for the per-tool cancellation path; this
hook covers the *between-steps* gap that Phase 8 does not.

### 2. Queued-prompt drain (`bt-p4-queue`)

Today the UI batches queued user messages into one combined string at
turn-boundary (commit `48f23c0`, `internal/ui/model.go:604`). That fixed the
"each queued msg is a stale orphan turn" bug but it only fires *between*
agent turns — anything typed mid-turn still waits.

PrepareStep is the right place to drain mid-turn queue: between steps the
agent has just executed a tool, the model is about to be called again, and
adding `[queued msg N]: ...` lines as a synthetic user message is safe
because the assistant's next reply hasn't started yet.

The drain operates on a queue handed to PrepareStep at construction (a
`<-chan string` or a `func() []string` getter exposed by the agent) — not on
`Model.queued` directly. Drained strings get folded into a new
`fantasy.Message` of role `user` and appended to `prepared.Messages`. See
"Write scopes" for the `a.messages` mirroring rule.

### 3. Tool refresh (`bt-p4-tool-refresh`)

For future MCP tools: the registry can change between steps. PrepareStep
calls a refresh hook (e.g. `a.tools.Snapshot()`), retranslates via
`translateTools`, and writes the slice into `prepared.Tools`. No-op when the
registry's revision counter is unchanged from the previous step.

Refresh runs after queue drain because a queued message may itself trigger a
tool reload (e.g. user pastes `/mcp connect …` then a real prompt — current
behavior is queue-clear-on-slash, but the ordering matters once that
restriction relaxes).

### 4. Cache markers (`bt-p4-cache`)

Anthropic prompt caching uses `cache_control: {type: "ephemeral"}` blocks
attached to specific message parts via `ProviderOptions`
(`charm.land/fantasy@v0.17.1/providers/anthropic/provider_options.go:125-199`).
PrepareStep walks `prepared.Messages` and stamps cache markers on the
last stable boundary — typically the system prompt and the most recent
tool-result chain — so subsequent steps in the same turn reuse the prefix.

Cache markers run *after* queue drain because adding a user message changes
where the "stable prefix" ends. Stamping first and then appending invalidates
the marker we just placed.

Provider-gated: see "Per-provider divergence" below.

### 5. Provider gates

Some options are only legal for some providers. Today the only one in flight
is the Anthropic cache marker (gate above), but this slot is also where the
following land as they ship:

- OpenRouter `reasoning` parameter forwarding.
- Ollama `stream_options.include_usage` suppression.
- Future provider-specific `ProviderOptions` on `prepared`.

These are last because they read the *final* shape of `prepared.Messages`
and `prepared.Tools` after the earlier concerns mutated them.

## Ordering

On every step, PrepareStep runs the five concerns in this order:

1. **Cancellation check** — `if err := stepCtx.Err(); err != nil { return … err }`. Cheap, must precede everything else.
2. **Queued-prompt drain** — pull pending user strings, append to `prepared.Messages` as one synthetic user `fantasy.Message`. Mirror into `a.messages` (see write scopes).
3. **Tool refresh** — if the registry revision changed, rebuild `prepared.Tools`. Skipped when nothing changed.
4. **Cache markers** — walk `prepared.Messages` once, attach `cache_control: ephemeral` `ProviderOptions` to the last cacheable boundary. Provider-gated.
5. **Provider gates** — apply remaining per-provider option fixups.

Justification for the ordering:

- Cancellation first: every later step is wasted work if the turn is dying.
- Drain before tool refresh: a drained message may legitimately request a tool reconfiguration.
- Drain before cache markers: appending a message moves the cacheable prefix boundary.
- Cache markers before provider gates: gates may need to inspect or adjust the cache marker shape (e.g. strip it for a non-Anthropic upstream that crept in via `Service: "custom"`).
- Provider gates last: they want the final `prepared` shape.

A second cancellation check at the end (after gates, before returning) is acceptable but not required — the next step's check will catch it within microseconds.

## Write scopes

PrepareStep mutates exactly two things:

| Target | Mutates? | Notes |
|--------|----------|-------|
| `prepared.Messages` | **Yes** | Queue drain appends; cache markers attach `ProviderOptions` to existing parts. |
| `prepared.Tools` | **Yes** | Tool refresh replaces the slice when the registry revision changed. |
| `prepared.System` | **No (Phase 4)** | System prompt is fixed at `Stream` setup time. Re-injecting per-step would invalidate Anthropic cache. Reserved for a later phase. |
| `prepared.Model` / `ToolChoice` / `ActiveTools` / `DisableAllTools` | **No (Phase 4)** | Out of scope. |
| `stepCtx` (returned ctx) | **No** | Returned unchanged. Per-tool cancellation lives one layer deeper (Phase 8, `bitchteaTool.Run`). |
| `a.messages` (agent's persistent slice) | **Yes, but only for queue drain** | Drained messages must be mirrored into `a.messages` as `Role: "user"` so session save and compaction see them. The mirror happens *inside* the PrepareStep closure, which closes over `*Agent`. Cache markers and tool refresh do **not** touch `a.messages` — they are per-step-only. |
| `a.scope`, `a.bootstrapMsgCount`, `a.TurnCount`, `a.ToolCalls` | **No** | Owned by `sendMessage`. `TurnCount` already incremented before `Stream` was called; per-step counters do not exist. |
| `CostTracker`, registry contents | **No** | Read-only from PrepareStep. |
| UI state (`Model.queued`, viewport) | **No directly** | The drain pulls *via* a queue accessor that the UI populated; PrepareStep never imports `internal/ui`. |

The `a.messages` mirror rule for the queue drain matters: if the model
crashes between this step and the next, the next session-resume must see the
drained messages in history. Otherwise the user's typed-and-queued work
disappears.

## Failure handling

PrepareStep can return `(stepCtx, PrepareStepResult{}, err)`. Fantasy
treats a non-nil error as fatal and unwinds the `Stream` call (`agent.go:395`).

Rules:

- **Context errors** (`context.Canceled`, `context.DeadlineExceeded`): return as-is. The existing `errors.Is(ev.Error, context.Canceled)` branch in `agent.go:231` already maps these to `turnStateCanceled`.
- **Drain errors**: there should not be any — pulling from a buffered channel is infallible. If a future drain implementation can fail (e.g. reading queued prompts from disk), wrap the error with `fmt.Errorf("queue drain: %w", err)` and return; the user sees a normal error event.
- **Tool refresh errors**: log and continue with the *previous* `prepared.Tools` slice. A registry reload failure must not kill an in-progress turn — the model can keep working with the tools it already has.
- **Cache marker errors**: must not happen (pure-CPU walk over `Messages`). Defensive: if a marker placement fails for a structural reason, drop the cache marker for this step and continue. Logging the warning once per session is enough.
- **Provider gate errors**: same as cache markers — degrade by skipping the gate, not by killing the turn.

The general principle: only cancellation-class errors propagate up. Everything else is best-effort and falls back to a slightly less-optimized step.

## Per-provider divergence

Today, gating is by `cfg.Provider` (`"openai"` or `"anthropic"`). Phase 9
(`docs/phase-9-service-identity.md`) adds `cfg.Service` as the proper gating
field; PrepareStep code should read whichever exists when it lands and
prefer `Service` once available.

| Concern | Anthropic | OpenAI-compatible (incl. zai-anthropic over OpenAI wire) | Ollama | OpenRouter |
|---------|-----------|----------------------------------------------------------|--------|------------|
| Cancellation check | always | always | always | always |
| Queue drain | always | always | always | always |
| Tool refresh | always | always | always | always |
| Cache markers | **on** (native + `service: anthropic`) | off (off for `zai-anthropic` until verified upstream) | off | off |
| `include_usage` suppression | n/a | depends on `Service` | **on** (suppress) | off |
| `reasoning` forwarding | via `ProviderOptions` already | off | off | **on** when model supports it |

The cancellation check, queue drain, and tool refresh have **no per-provider
divergence** — they operate on bitchtea-internal state (ctx, queue, registry)
that is provider-agnostic. Only concerns 4 and 5 branch on provider/service.

## Testing surface (`bt-p4-verify`)

The verification ticket owns the actual test cases; this section names the
shape they have to fit:

- A fake `fantasy.LanguageModel` that drives a multi-step turn so PrepareStep is invoked more than once.
- Captured `prepared.Messages` snapshots per step to assert (a) queue drain ordering is FIFO, (b) cache markers land on the expected boundary, (c) tool refresh swaps the slice exactly when the registry revision bumped.
- A `context.Cancel` race test that cancels between two model calls and asserts the loop exits via the cancellation branch with no further `Generate` invocations.
- `go test -race ./...` clean.

## Open questions

- **Queue accessor shape**: pass a `<-chan string` (PrepareStep drains until empty non-blocking) or a `func() []string` getter the agent exposes? Channel is simpler but couples lifetimes; getter is more testable. Leaning getter.
- **Mid-turn queue drain semantics**: does a mid-turn drained prompt count as a new "TurnCount"? Proposal: no — it is part of the same logical turn that the user steered. The status bar already shows it via the queue indicator.
- **Cache marker scope for `zai-anthropic`**: `service: zai-anthropic` speaks Anthropic wire format but is an upstream proxy. Until a captured-payload test confirms cache_control round-trips correctly, treat as **off**. Re-enable when verified.
- **Tool refresh frequency**: every step is safe but possibly wasteful. A revision counter on `*tools.Registry` is cheap; adding it is in scope for `bt-p4-tool-refresh`.
- **PrepareStep concurrency**: fantasy invokes the hook synchronously, but it closes over `*Agent`. If a future change ever runs steps concurrently, the `a.messages` mirror needs a mutex. Not an issue today.
