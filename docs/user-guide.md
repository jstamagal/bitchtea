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

🦍💪🤝 APES STRONK TOGETHER 🦍💪🤝
