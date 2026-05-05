> **Status:** SHIPPED

# Phase 8: Per-Tool Cancellation State Machine

Design for `bt-p8-state`. Sibling tasks `bt-p8-tool-context`, `bt-p8-ui-keys`, `bt-p8-continue`, and `bt-p8-race` implement the pieces described here. This doc is the contract those tasks must satisfy.

## Goals

1. Esc x1 cancels only the currently running tool. The agent loop keeps the turn alive and the model sees a synthetic "user cancelled" tool result so it can pivot.
2. Esc x2 cancels the whole turn (current behavior, preserved).
3. Esc x3 nukes the steering queue (current behavior, preserved).
4. Resolve `bt-s2z`: a tool interrupted by Ctrl+C must never leave the agent waiting for a `tool_result` that will never arrive.

Today's `internal/ui/model.go:954` admits the gap with: *"Tool-only cancel is not wired yet; cancelled the current turn while X was running."* That string goes away when this design lands.

## States

The state machine lives on `*Model`. Existing fields (`streaming`, `cancel`, `activeToolName`, `escStage`) cover most of it; the only new field is per-tool cancellation (see Ownership).

```
                ┌─────────┐
                │  idle   │ ◄────────────────┐
                └────┬────┘                  │
                     │ user submits          │ done / error
                     ▼                       │
              ┌──────────────┐               │
              │ running-turn │ ──────────────┤
              └──────┬───────┘               │
       tool_start    │                       │
                     ▼                       │
              ┌──────────────┐               │
        ┌──── │ running-tool │ ──────────────┤  tool_result (real)
        │     └──────┬───────┘               │
   Esc  │            │                       │
   x1   │            │ tool_result           │
        ▼            ▼ (real, OnToolResult)  │
 ┌────────────────┐  │                       │
 │ cancelling-    │  └───────────────────────┤
 │     tool       │                          │
 └───────┬────────┘                          │
         │ synthetic tool_result injected    │
         │ (continues turn)                  │
         └──────────► running-turn ──────────┘

  Esc x2 / Ctrl+C from any non-idle state ──► cancelling-turn ──► idle
  Esc x3 from cancelling-turn or idle      ──► clears queue, stays/returns idle
```

`cancelling-tool` and `cancelling-turn` are transient — they exist for the brief window between key press and the goroutine actually returning. The Bubble Tea `Update` must not block waiting for them; the next `agentEventMsg` (or absence of one) does the cleanup.

## Esc key ladder

The ladder graduates within `escGraduationWindow` (1.5s today). Stages reset outside the window.

| Press | State at press | Effect | Transcript line |
|-------|----------------|--------|-----------------|
| Esc x1 | `running-tool` | Cancel the *current tool only* via per-tool `CancelFunc`. Inject synthetic tool result (see contract). Turn keeps streaming. | `Cancelled tool: <name>. Model will continue.` |
| Esc x1 | `running-turn` (no active tool) | No tool to cut; arm stage 1 for x2. | `Press Esc again to cancel the turn.` |
| Esc x1 | panel open | Close the panel; do not advance stage. | (no line) |
| Esc x2 | `running-turn` or `running-tool` | `cancelActiveTurn`. Cancel the turn context. Sets `queueClearArmed`. | `Interrupted by Esc.` (+ queue hint if non-empty) |
| Esc x3 | `queueClearArmed` and queue non-empty | Clear the steering queue. | `Cleared queued messages.` |

Ctrl+C keeps its existing 3-press ladder (`handleCtrlCKey`): turn cancel, queue clear, quit. Ctrl+C is intentionally blunter than Esc — it never does tool-only cancel.

Panel open (mp3 / tool panel) takes priority over everything: Esc closes the panel without consuming a stage.

## Ownership of cancellation funcs

Two scopes, two cancellers, both owned by the **Model**:

| Scope | Field | Created in | Cancels |
|-------|-------|------------|---------|
| Turn | `m.cancel context.CancelFunc` | `startAgentTurn` (`model.go:828`) | The whole `agent.SendMessage` goroutine, all in-flight tool calls, and any subprocess via `exec.CommandContext`. |
| Tool | `m.cancelActiveTool context.CancelFunc` (new) | UI handler for `tool_start` event | The single in-flight tool call only. The turn ctx remains alive. |

Why the Model owns both: the Model is the single Bubble Tea actor that receives key events. Pushing the per-tool canceller into the Agent would require a key->Agent channel, which adds latency and a sync point we don't need.

How the per-tool ctx gets created (implemented by `bt-p8-tool-context`):

1. `llm.bitchteaTool.Run(ctx, call)` wraps the fantasy-supplied step ctx in `context.WithCancel`, stores the cancel fn somewhere the Model can reach (see below), then passes the derived ctx into `Registry.Execute`.
2. The Model receives the `tool_start` event and pulls the cancel fn for that ToolCallID. The simplest path: the agent emits the cancel fn alongside `tool_start` via a new channel/field on `Event`. Alternative: a registry indexed by ToolCallID held in `*Agent`, queried by the Model.
3. On `tool_result` (real), `OnToolResult` clears `m.cancelActiveTool` and `m.activeToolName`.
4. On Esc x1, the Model calls `m.cancelActiveTool()`, sets `m.activeToolName = ""`, and waits for the synthetic result to arrive.

`m.cancelActiveTool` MUST be cleared in three places: real `tool_result`, after Esc x1 fires, and inside `cancelActiveTurn` (alongside the existing `m.cancel = nil`).

## Active tool ID tracking

The UI already tracks `m.activeToolName` from `tool_start` / `tool_result`. Add `m.activeToolCallID string` so we can match against the *correct* concurrent call if/when fantasy ever runs tools in parallel (today `Parallel: false`, but the contract should not assume that forever).

Cleared on: real `tool_result` matching the ID, after Esc x1 cancellation, in `cancelActiveTurn`, and on `done`/`error` events.

## User-cancelled tool result contract

When Esc x1 fires, `bitchteaTool.Run` (in `internal/llm/tools.go`) sees its derived ctx cancelled. It MUST return a *normal* `fantasy.ToolResponse` rather than a Go error. Returning an error aborts the whole fantasy stream — that is what `bt-s2z` is reporting.

Exact return shape (replaces today's `fantasy.NewTextErrorResponse(fmt.Sprintf("Error: %v", err))` for the cancellation case):

```go
if errors.Is(ctx.Err(), context.Canceled) && turnCtx.Err() == nil {
    // tool ctx cancelled but turn is alive -> user pressed Esc x1
    return fantasy.NewTextResponse("user cancelled this tool call"), nil
}
```

Discriminate carefully:
- Tool ctx cancelled, turn ctx alive → user cancelled tool. Return synthetic result, keep going.
- Turn ctx cancelled → real cancellation. Return whatever fantasy expects when the whole stream is dying; the agent will route the resulting `error` event through its existing `errors.Is(ev.Error, context.Canceled)` branch.
- Tool returned its own error → unchanged: `NewTextErrorResponse`.

The synthetic result is injected back into the message stream as a normal `tool` role message with the original `ToolCallID`. The model sees it as if the tool had completed with that text; no special handling required on the agent side.

## Transcript effects

| Event | Viewport line |
|-------|---------------|
| Esc x1 cancels a tool | `Cancelled tool: <name>. Model will continue.` (system msg) — tool panel entry for that call shows status `cancelled` |
| Esc x1 with no active tool | `Press Esc again to cancel the turn.` (system msg, no other state change) |
| Esc x2 (turn cancel) | `Interrupted by Esc.` (existing) — assistant message in flight is finalized in place |
| Esc x3 (queue wipe) | `Cleared queued messages.` (existing) |
| Ctrl+C in stream | unchanged from today |

The cancelled tool result text the *model* sees and the system line the *user* sees are deliberately different. The model gets a terse machine-friendly string; the user gets the readable status line.

## Interaction with bt-s2z

`bt-s2z` reproduces by Ctrl+C-ing during a `bash` tool that's blocked on interactive auth. Today the bash subprocess dies (SIGKILL via `exec.CommandContext`), but `bitchteaTool.Run` then returns `fantasy.NewTextErrorResponse("Error: <ctx canceled>")` *and* the turn ctx is also cancelled, which races with fantasy's own stream-abort path. The agent records `lastTurnState = turnStateCanceled` but on resume the next turn finds itself replaying a message history where the tool call has no result.

This design fixes it two ways:

1. **Tool-scoped cancellation path**: the synthetic `"user cancelled this tool call"` result is appended to `a.messages` exactly like a real tool result, leaving the history well-formed regardless of what the user does next.
2. **Turn-scoped cancellation path** (Esc x2 / Ctrl+C): on resume, if the last entry in `a.messages` is an assistant message containing a tool call with no matching `tool` reply, the agent must inject `"user cancelled this tool call"` for that ID before sending the next user turn. This is the `bt-p8-continue` task. The state machine here just guarantees the gap is detectable: a turn ends in `turnStateCanceled` AND the message tail has an unanswered tool_call → inject synthetic results, then proceed.

## Open questions

- Where does the per-tool `CancelFunc` actually live so the Model can grab it? Three options: (a) emitted on `Event{Type: "tool_start", Cancel: fn}`, (b) registry on `*Agent` keyed by `ToolCallID`, (c) `*tools.Registry` tracks live calls. Leaning (a) for simplicity; (b) if we ever support parallel tools and need lookups outside event flow.
- Does Esc x1 during a `terminal_*` PTY tool just cancel the wait/snapshot, or does it kill the PTY? Proposed: cancel only the in-flight call (e.g. `terminal_wait`), leave the PTY alive — `terminal_close` is a separate, explicit tool.
- Should the synthetic result text be configurable per-tool? Probably no for v1; a single constant is easier to test and to reason about.
- Cost-tracking: a cancelled tool call still consumed input tokens. Count it as usual; nothing to design here.

## Status

All work shipped. Per-tool cancellation, Esc/Ctrl+C ladder semantics,
ToolContextManager, and the synthetic tool-result contract are live (see
`internal/llm/tool_context.go` and `internal/ui/model.go`). One operational
deviation from this design: the wrappers now return
`fantasy.NewTextErrorResponse(fmt.Sprintf("Error: %v", err))` rather than a
literal `"user cancelled this tool call"` string for Esc x1; what matters
operationally is that they return a `fantasy.ToolResponse` with `nil` Go
error so the fantasy stream stays alive. Design rationale (Model-owns-both,
Ctrl+C-blunter-than-Esc, panel-priority, synthetic-result-must-be-
ToolResponse-not-error) was ported into `docs/signals-and-keys.md` under the
Design rationale section, with complementary detail already present in
`docs/agent-loop.md`'s Per-Tool Cancellation section. This document is
retained for historical context.
