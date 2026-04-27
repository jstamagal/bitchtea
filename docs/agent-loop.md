# 🦍 THE AGENT LOOP 🦍

This scroll describes the turn-taking cycle post-fantasy migration. **fantasy
now owns the inner LLM/tool loop** — the agent is a thin orchestration layer
that translates `llm.StreamEvent` into `agent.Event` for the UI.

## 1. Entry Points

- **`SendMessage`** (`internal/agent/agent.go`): Processes a raw user message.
- **`SendFollowUp`** (`internal/agent/agent.go`): Continues a chain after an
  auto-prompt (`auto-next-steps`, `auto-next-idea`).

Both converge on the private **`sendMessage`** function. There is **no `for {}`**
multi-step loop in the agent layer anymore — fantasy handles step iteration.

## 2. One Stream Per User Turn

`sendMessage` runs exactly **one** `streamer.StreamChat` call per user turn:

1. **Prep**: Expand `@file` refs, append the user message, increment
   `TurnCount`, set state to `StateThinking`.
2. **Stream**: Spawn the streamer goroutine. Read events on the channel:
   - `text` → run through `followUpStreamSanitizer` (strips
     `AUTONEXT_DONE` / `AUTOIDEA_DONE` control tokens for display) and
     forward as `agent.Event{Type:"text"}`.
   - `thinking` → forward as-is.
   - `usage` → accumulate into `CostTracker`.
   - `tool_call` → bump `ToolCalls[name]`, emit `tool_start` event.
     **Tool execution itself happens inside fantasy** via
     `bitchteaTool.Run`; the agent does not call `Registry.Execute`.
   - `tool_result` → emit `tool_result` event (UI feedback).
   - `error` → emit error + done, mark `lastTurnState`, return.
   - `done` → flush sanitizer, splice `ev.Messages` (the rebuilt
     transcript from `result.Steps[].Messages`) into `a.messages` after
     sanitizing assistant role content. Fallback: if `ev.Messages` is
     empty (test fakes), append a synthetic assistant message from the
     accumulated text deltas.
3. **Wrap-up**: Set `lastTurnState = turnStateCompleted`, emit
   `state=StateIdle` and `done`.

## 3. Tool Execution (inside fantasy)

When the model emits a tool call, fantasy invokes
`bitchteaTool.Run(ctx, ToolCall) ToolResponse`
(`internal/llm/tools.go`). That wrapper:

1. Calls `tools.Registry.Execute(ctx, name, args)`.
2. On success, returns `fantasy.NewTextResponse(output)`.
3. On error, returns `fantasy.NewTextErrorResponse(err.Error())` —
   never a Go error (returning a Go error aborts the entire stream).

Fantasy automatically threads the tool result back into the model and
streams the next step. The agent's `tool_result` event is for UI; the
canonical record lives in `result.Steps[].Messages`.

## 4. Cancellation

- Every tool execution uses a context derived from the turn context.
- `Esc` (Stage 1) cancels the active tool's context; the turn continues.
- `Ctrl+C` cancels the turn context — fantasy aborts the stream,
  `safeSend` returns `ctx.Err()` from callbacks, and `sendMessage`
  receives an `error` event with `context.Canceled`. State returns to
  `StateIdle`.

## 5. Pre-Loop Setup

Before the stream starts, the agent does:
- **`ExpandFileRefs`**: Converts `@path/to/file` into inline file content
  (`internal/agent/context.go`).
- **`injectPerMessagePrefix`**: Optional prefix prepended to every user
  message to keep persona fresh in long sessions.
- **Bootstrap injection** (in `NewAgentWithStreamer`): system prompt,
  AGENTS.md/CLAUDE.md context, `MEMORY.md`, persona anchor.

## 6. Follow-Up Continuations

After a successful turn, `MaybeQueueFollowUp` checks `cfg.AutoNextSteps`
and `cfg.AutoNextIdea` and may return a synthetic `FollowUpRequest`.
The UI submits it via `SendFollowUp`, which sets `activeFollowUpKind`
so the stream sanitizer knows to strip the corresponding done token
from the displayed text.

APE STRONK TOGETHER. 🦍💪🤝
