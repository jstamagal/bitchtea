# 🦍 STREAMING & LLM SHIM 🦍

This scroll documents the token pulse of `bitchtea`. 

## 1. The `StreamEvent` Contract

The `internal/llm` package (currently under reconstruction) defines the `StreamEvent` struct used to communicate between the provider transport and the agent.

### Event Types
- **`text`**: Standard assistant response chunk.
- **`thinking`**: Chain-of-thought tokens (e.g., Anthropic models).
- **`tool_call`**: Request for tool execution (contains `ID`, `Name`, and `Args`).
- **`usage`**: Final token usage and cost data.
- **`error`**: Transport or API failure.
- **`done`**: Signal for stream completion.

## 2. The Shim Strategy

`internal/llm` is transitioning to a thin shim over `charm.land/fantasy`. 

- **Interface**: `ChatStreamer` (`internal/agent/agent.go:119`) allows the agent to remain provider-agnostic.
- **Implementation**: The shim translates provider-specific events (OpenAI, Anthropic) into the unified `StreamEvent` format.
- **Flow**: `agent` calls `StreamChat` -> shim starts goroutine -> shim pipes events into channel -> `agent` consumes in a `for range` loop (`internal/agent/agent.go:222`).

## 3. Error Handling

- **Cancellation**: If the `context.Context` is canceled, the shim must terminate the underlying request and send an `error` event with `context.Canceled`.
- **Retries**: Transient API errors should be handled within the shim before surfacing an `error` event to the agent loop.

APE STRONK TOGETHER. 🦍💪🤝
