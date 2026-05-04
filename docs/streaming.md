# 🦍 THE BITCHTEA SCROLLS: STREAMING & LLM

Bitchtea talks to models through a unified streaming interface.

## 📡 THE STREAMER CONTRACT

Defined in `internal/llm/types.go`, the `ChatStreamer` interface is the minimal surface for communication:

```go
type ChatStreamer interface {
    StreamChat(ctx context.Context, messages []Message, reg *tools.Registry, events chan<- StreamEvent)
}
```

### `StreamEvent` Types:
- **`text`**: Incremental tokens of the response.
- **`thinking`**: Internal model reasoning (if supported, e.g., O1/O3).
- **`tool_call`**: A request to run a tool.
- **`tool_result`**: The output of a tool execution.
- **`usage`**: Final token counts for cost estimation.
- **`done`**: Signal that the turn is finished, carries the final `Messages`.

## 🔌 THE FANTASY SHIM

Bitchtea uses `charm.land/fantasy` as its multi-provider engine. The `llm.Client` (in `internal/llm/client.go`) wraps this to provide:

1. **Lazy Initialization**: Providers and models are only built when needed.
2. **Provider Switching**: Seamless transition between OpenAI, Anthropic, and local Ollama.
3. **Debug Hooks**: Intercepts raw HTTP requests/responses for `/debug on` mode.

## 💰 COST TRACKING

The `CostTracker` (in `internal/llm/cost.go`) estimates USD spend in real-time based on input/output tokens and model-specific pricing tiers.

## Anthropic Prompt Cache Markers

`internal/llm/cache.go` places Anthropic prompt-cache metadata on the stable bootstrap boundary during streaming preparation. `Client.streamOnce` snapshots `Client.Service` and `Client.BootstrapMsgCount`, computes the bootstrap boundary with `bootstrapPreparedIndex(msgs, BootstrapMsgCount)`, and runs `applyAnthropicCacheMarkers` on the prepared Fantasy messages before the provider call.

The marker lands on the last bootstrap message: prepared-message index `BootstrapMsgCount - 1`. That bootstrap prefix is the stable system-and-tools setup for the turn, including injected workspace instructions and persona/bootstrap messages, so marking the final bootstrap message lets Anthropic reuse the expensive stable prefix on later requests. The provider option is Anthropic `cache_control` with `type: "ephemeral"`. If there are no bootstrap messages, or if the computed index is outside the prepared message slice, no marker is written.

The gate is intentionally service-specific: markers are applied only when `Client.Service == "anthropic"`. Other services get no marker, including `openai` and the explicitly excluded `zai-anthropic` service. The `zai-anthropic` exclusion remains until a captured-payload test proves that upstream preserves Anthropic `cache_control` exactly.

`internal/llm/cache_test.go` pins those expectations:

- `TestApplyAnthropicCacheMarkers_AnthropicSetsBoundary` expects exactly one marker on message index `4` when the bootstrap count is `5`.
- `TestApplyAnthropicCacheMarkers_OpenAINoMarker` expects no marker for `openai`.
- `TestApplyAnthropicCacheMarkers_ZaiAnthropicExcluded` expects no marker for `zai-anthropic`.
- `TestApplyAnthropicCacheMarkers_NoBootstrapNoMarker` expects no marker when the bootstrap count is `0`.
- `TestBootstrapPreparedIndex` expects `BootstrapMsgCount - 1` for an in-range bootstrap boundary and `-1` when there is no valid boundary.

🦍💪🤝 APES STRONK TOGETHER 🦍💪🤝
