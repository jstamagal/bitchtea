# 🦍 STREAMING & LLM SHIM 🦍

This scroll documents the token pulse of `bitchtea` post-fantasy migration.

## 1. The `StreamEvent` Contract

`internal/llm/types.go` defines the `StreamEvent` struct used to communicate
between the provider transport and the agent.

### Event Types
- **`text`**: Standard assistant response chunk.
- **`thinking`**: Reasoning tokens (Anthropic and reasoning models).
- **`tool_call`**: Request for tool execution (`ToolCallID`, `ToolName`, `ToolArgs`).
- **`tool_result`**: Tool finished — `ToolCallID`, `ToolName`, `Text` payload.
- **`usage`**: Per-stream token usage report (one per step in fantasy).
- **`error`**: Transport, provider, or context-cancellation failure.
- **`done`**: Terminal event. Carries `Messages []Message` — the rebuilt
  transcript from `result.Steps[].Messages`. The agent layer appends these
  to its own `a.messages` log on receipt.

## 2. The Fantasy Shim (`internal/llm`)

`internal/llm` is a thin shim over `charm.land/fantasy v0.17.1`.

- **Interface**: `ChatStreamer` (`internal/llm/types.go`). Signature:
  `StreamChat(ctx, messages, *tools.Registry, events chan<- StreamEvent)`.
  The Registry parameter (not `[]ToolDef`) lets the shim bind tool `Run`
  callbacks directly to `Registry.Execute`.
- **Implementation**: `Client.StreamChat` (`internal/llm/stream.go`)
  builds a `fantasy.Agent`, wires every callback (`OnTextDelta`,
  `OnReasoningDelta`, `OnToolCall`, `OnToolResult`, `OnStreamFinish`,
  `OnError`, `PrepareStep`), and runs `fa.Stream`. Fantasy owns the
  inner agent loop — it dispatches tool calls into `bitchteaTool.Run`
  (`internal/llm/tools.go`) and feeds results back to the model.
- **Transcript**: After `Stream` returns, the shim walks
  `result.Steps[].Messages`, converts each via `fantasyToLLM`
  (`internal/llm/convert.go`), and ships the slice on the `done` event.
- **Stop condition**: `fantasy.WithStopConditions(fantasy.StepCountIs(64))`.

## 3. Cancellation & Errors

- **Cancellation**: All channel sends from inside fantasy callbacks go
  through `safeSend`, which selects on `ctx.Done()`. A canceled context
  bubbles `ctx.Err()` back into fantasy and aborts the stream cleanly.
- **Provider errors**: Surface as `*fantasy.ProviderError` on the `error`
  event. The UI dispatches `ErrorHint` (`internal/llm/errors.go`) on this
  type to render a one-line status hint.
- **Retries**: Fantasy handles retryable HTTP statuses (408/429/5xx)
  internally via `ProviderError.IsRetryable()`. The shim does not retry.

APE STRONK TOGETHER. 🦍💪🤝
