# Streaming & LLM

Bitchtea talks to models through the fantasy multi-provider engine. This
document covers the streaming loop, provider dispatch, error handling, cost
tracking, debug hooks, and prompt-cache integration.

## The Streamer Contract

Defined in `internal/llm/types.go:79`, the `ChatStreamer` interface is the
minimal surface consumed by the agent loop:

```go
type ChatStreamer interface {
    StreamChat(ctx context.Context, messages []Message, reg *tools.Registry, events chan<- StreamEvent)
}
```

The `reg` parameter is the live `tools.Registry` for this turn — passing the
full Registry (rather than just tool definitions) lets the implementation bind
each tool's `Run` callback to `Registry.Execute` without a separate handshake
step. It may be `nil` for tool-less turns (e.g. compaction summaries).

### StreamEvent Types

`StreamEvent` (`types.go:38`) is emitted on the channel for each phase of a
turn. Consumers switch on `Type`:

| Type | Fields | When |
| :--- | :--- | :--- |
| `text` | `Text` | Incremental text deltas from the model. |
| `thinking` | `Text` | Internal model reasoning (Anthropic, O1/O3). |
| `tool_call` | `ToolName`, `ToolArgs`, `ToolCallID` | The model has requested a tool execution. |
| `tool_result` | `ToolName`, `ToolCallID`, `Text` | The result of a tool execution. |
| `usage` | `Usage` (TokenUsage pointer) | Final token counts for one fantasy step. |
| `error` | `Error` | A non-retryable error ended the turn. |
| `done` | `Messages` | Normal turn completion, carries the full rebuilt transcript. |

The implementation **must** close the events channel after the terminal event
("done" or "error").

## Unified Loop Walkthrough

`Client.StreamChat` (`stream.go:32`) drives one full agent turn and owns
retry. Its structure:

```
StreamChat
  ├── for each retry attempt (0..len(retryBackoff)):
  │     ├── if attempt > 0: wait backoff, send "text" event to UI
  │     ├── call streamOnce(ctx, msgs, reg, events)
  │     ├── success? → return (done event already sent)
  │     ├── error is retryable? → continue loop
  │     └── non-retryable error? → send "error" event, return
  └── final attempt exhausted → send last error event
```

`streamOnce` (`stream.go:68`) performs a single attempt against fantasy:

1. **Lazy provider init** — calls `c.ensureModel(ctx)` which builds a
   `fantasy.Provider` + `fantasy.LanguageModel` from the current config on
   first call, then caches them.
2. **Message split** — `splitForFantasy` (`convert.go:16`) separates the
   tail user message (becomes the prompt), prior messages (becomes the
   `prior` slice), and system messages (concatenated into the system prompt).
3. **Tool assembly** — `translateTools(reg)` builds a
   `[]fantasy.AgentTool`, then `MCPTools` / `AssembleAgentTools` merges in
   MCP tools (see `docs/tools.md` "Tool Assembly Pipeline").
4. **Tool context** — a per-turn `ToolContextManager` wraps each tool's
   context with cancellation support.
5. **fantasy agent** — `fantasy.NewAgent(model, opts...)` is created with
   stop conditions, optional system prompt, and the assembled tool list.
6. **Stream** — `fa.Stream(ctx, AgentStreamCall{...})` runs the callbacks:
   - `PrepareStep`: sends a "thinking" event, drains mid-turn queued prompts
     (`promptDrain`), applies Anthropic cache markers.
   - `OnTextDelta`: emits `"text"` events.
   - `OnReasoningDelta`: emits `"thinking"` events.
   - `OnToolCall`: emits `"tool_call"` events.
   - `OnToolResult`: emits `"tool_result"` events (extracts text from
     `ToolResultOutputContent` via `toolResultText` at line 275).
   - `OnStreamFinish`: converts `fantasy.Usage` to `TokenUsage` and emits
     a `"usage"` event.
   - `OnError`: captures stream errors.
7. **Message accumulation** — after `Stream` returns, the result's steps are
   flattened back into `[]Message` via `fantasyToLLM` (`convert.go:84`).
8. **Done event** — a `"done"` event carrying the rebuilt messages is sent,
   and `streamOnce` returns nil (success).

The agent receives the "done" event and appends the rebuilt messages to its
own history. On error, no "done" event is sent — the agent handles "error"
by surfacing to the user.

### Message Conversion

`splitForFantasy` (`convert.go:16`) processes the `[]Message` transcript:

- The **tail user message** becomes `prompt`. If the transcript ends with
  an assistant or tool turn (e.g. restored session), `prompt` is empty and
  every message stays in `prior`.
- **System messages** are concatenated into `systemPrompt`, passed via
  `fantasy.WithSystemPrompt`.
- Each remaining message maps to a `fantasy.Message` with the appropriate
  role and parts (`TextPart`, `ToolCallPart`, `ToolResultPart`).

`fantasyToLLM` (`convert.go:84`) reverses the conversion for the rebuilt
transcript, mapping fantasy's multi-part messages back into bitchtea's
single-string + ToolCalls/ToolCallID shape. `LLMToFantasy` (`convert.go:181`)
is the inverse used during session restore.

### Tool Context Manager

`streamOnce` creates a `ToolContextManager` per turn (via
`NewToolContextManager(ctx)`) and stores it on the client. Each tool call gets
its own cancellable context derived from this manager. The agent exposes
`CancelTool` by reading `client.ToolContextManager()`, letting the UI cancel
mid-turn tool execution without aborting the entire fantasy step.

## Provider-Specific Quirks

`buildProvider` (`providers.go:30`) selects the fantasy provider package based
on `Client.Provider`:

### Anthropic (`providers.go:44`)

Uses `anthropic.New()` with optional `WithBaseURL` (after stripping `/v1`
suffix — the SDK appends it internally). No SSE wrapper — the Anthropic
provider handles `text/event-stream` natively.

### OpenAI (`providers.go:90`)

Uses `openai.New()` with `WithUseResponsesAPI()` (OpenAI Responses API, not
Chat Completions). Empty `baseURL` resolves to upstream OpenAI.

### OpenRouter / Vercel (`providers.go:106`, `providers.go:118`)

Recognized by host matching against `openrouter.DefaultURL` /
`vercel.DefaultURL`. Each uses its own fantasy provider package.

### OpenAI-Compatible (everything else)

Ollama, ZAI (openai-compatible variant), GitHub Copilot, AIHubMix, xAI,
custom local endpoints, etc. all fall through to `openaicompat.New()` with
the configured `baseURL`. The `openaicompat` provider sends a vanilla
OpenAI-chat-completions request; streaming is handled by the same
server-sent events protocol as upstream OpenAI.

### Service-Based Routing

When `Client.Service` is set (Phase 9), `routeByService` (`providers.go:58`)
dispatches by service identity rather than sniffing the host from `baseURL`.
Known identities: `openrouter`, `vercel`; everything else (including `ollama`,
`zai-openai`, `copilot`, `xai`, custom) uses `openaicompat`.

## Retry Logic

`StreamChat` implements exponential backoff for transient failures
(`stream.go:16-28`):

```go
var retryBackoff = []time.Duration{
    1 * time.Second, 2 * time.Second, 4 * time.Second,
    8 * time.Second, 16 * time.Second, 32 * time.Second, 64 * time.Second,
}
```

Seven attempts total (initial + 6 retries). Before each retry, a `"text"`
event is sent to the UI so the user sees what is happening. If all attempts
are exhausted, the last error is returned as an `"error"` event.

### isRetryable

`isRetryable` (`stream.go:200`) matches the error message against known
transient patterns:

- **Rate limiting**: "rate limit", "rate_limit", "too many requests", "429"
- **Server errors**: "503", "502", "504", "server overloaded", "server error",
  "internal server error", "service unavailable"
- **Network errors**: "connection refused", "connection reset", "broken pipe",
  "no such host", "dial tcp", "i/o timeout", "temporary failure"
- **TLS errors**: "tls handshake timeout"
- **Timeouts**: "deadline exceeded", "timed out", "timeout"
- **EOF**: "eof", "unexpected eof"

Everything else (auth failures, bad requests, model-not-found) is treated as
non-retryable and surfaced immediately to the user.

## ErrorHint Classification

`ErrorHint` (`errors.go:15`) maps common errors to short, user-friendly
one-liners for the UI status bar:

| Condition | Hint |
| :--- | :--- |
| `context.Canceled` | (empty — silent) |
| `fantasy.ProviderError` with `IsContextTooLarge` | "context too large — try /compact" |
| HTTP 401 | "auth failed — check /set apikey" |
| HTTP 403 | "access denied — your API key may lack permissions for this model" |
| HTTP 404 | "model not found — check the model name with your provider" |
| HTTP 408 / 429 / 5xx | provider-specific retry guidance |
| `net.OpError` with `Op == "dial"` | "cannot reach the server — is it running?" |
| "no such host" | "DNS lookup failed — check /set baseurl" |
| "connection refused" | "connection refused — is the local server running?" |
| x509 / TLS errors | "TLS error — check /set baseurl or your network proxy" |
| Deadline/timeout | "request timed out — model may be slow or network unstable" |

The `fantasy.ProviderError` type (`errors.As(err, &pe)`) wraps upstream HTTP
errors from all provider packages, giving a single code path for
status-code-based hints regardless of which provider is active.

## Channel Buffer and safeSend Backpressure

The events channel is created by the agent (not by `StreamChat`) with a buffer
of 100. All stream events are sent through `safeSend` (`stream.go:266`):

```go
func safeSend(ctx context.Context, ch chan<- StreamEvent, ev StreamEvent) error {
    select {
    case ch <- ev:
        return nil
    case <-ctx.Done():
        return ctx.Err()
    }
}
```

The 100-buffer depth absorbs bursts of `tool_call` + `tool_result` pairs
(multiple tools per step) and text deltas without blocking the fantasy
goroutine. When the buffer is full and the UI is slow to consume, `safeSend`
blocks on the channel send — the context cancellation path ensures the
producer never blocks forever.

If `ctx` is cancelled while blocked on a send, `safeSend` returns
`ctx.Err()`. The fantasy `On*` callbacks propagate this error up to
`streamOnce`, which aborts the current attempt and lets `StreamChat` either
retry or surface the cancellation.

## Done Event Contract

The `"done"` event is the normal terminal event. It carries `Messages []Message`
— the full rebuilt transcript from fantasy's response, including:
- Assistant messages with text content and/or `ToolCalls`.
- Tool result messages with `ToolCallID` and text content.
- Any follow-up assistant messages from multi-step tool loops.

The agent appends these messages to its own `messages` slice. "done" is always
the last event before the channel is closed. On error (retry-exhausted or
non-retryable), `"error"` is the terminal event and no `"done"` is sent.

## Anthropic Prompt Cache Markers

*(This section was drafted in the initial stub and is preserved verbatim — it
remains accurate.)*

`internal/llm/cache.go` places Anthropic prompt-cache metadata on the stable
bootstrap boundary during streaming preparation. `Client.streamOnce` snapshots
`Client.Service` and `Client.BootstrapMsgCount`, computes the bootstrap boundary
with `bootstrapPreparedIndex(msgs, BootstrapMsgCount)`, and runs
`applyAnthropicCacheMarkers` on the prepared fantasy messages before the
provider call.

The marker lands on the last bootstrap message: prepared-message index
`BootstrapMsgCount - 1`. That bootstrap prefix is the stable system-and-tools
setup for the turn, including injected workspace instructions and
persona/bootstrap messages, so marking the final bootstrap message lets
Anthropic reuse the expensive stable prefix on later requests. The provider
option is Anthropic `cache_control` with `type: "ephemeral"`. If there are no
bootstrap messages, or if the computed index is outside the prepared message
slice, no marker is written.

The gate is intentionally service-specific: markers are applied only when
`Client.Service == "anthropic"`. Other services get no marker, including
`openai` and the explicitly excluded `zai-anthropic` service. The
`zai-anthropic` exclusion remains until a captured-payload test proves that
upstream preserves Anthropic `cache_control` exactly.

`internal/llm/cache_test.go` pins those expectations:
- `TestApplyAnthropicCacheMarkers_AnthropicSetsBoundary` expects exactly one
  marker on message index `4` when the bootstrap count is `5`.
- `TestApplyAnthropicCacheMarkers_OpenAINoMarker` expects no marker for
  `openai`.
- `TestApplyAnthropicCacheMarkers_ZaiAnthropicExcluded` expects no marker for
  `zai-anthropic`.
- `TestApplyAnthropicCacheMarkers_NoBootstrapNoMarker` expects no marker when
  the bootstrap count is `0`.
- `TestBootstrapPreparedIndex` expects `BootstrapMsgCount - 1` for an
  in-range bootstrap boundary and `-1` when there is no valid boundary.

## Debug Hook Integration

When the user enables `/debug on`, `Client.SetDebugHook` (`client.go:181`)
installs a function that receives `DebugInfo` for each upstream HTTP request.
Setting the hook invalidates the cached provider because the HTTP client is
rebuilt with a `newDebugHTTPClient` transport wrapper.

The debug transport (`debug.go:37`) intercepts every `RoundTrip`:

1. Reads the full request body into `DebugInfo.RequestBody` and replaces it
   with a reusable `NopCloser` so the provider sees the original bytes.
2. On the response path, classifies the content type:
   - `text/event-stream` → body set to `"(stream)"` (never buffered).
   - `application/json` or `text/plain` → body is read and captured.
   - Everything else → body is skipped.
3. **Header redaction**: Authorization, Proxy-Authorization, X-Api-Key,
   Api-Key, and provider-specific key headers are redacted to `[REDACTED]`
   before being passed to the hook.
4. Calls `hook(info)` synchronously.

After the hook returns, the original response is returned unmodified.

## Usage Event Reporting and CostTracker

Each fantasy step's `OnStreamFinish` callback emits a `"usage"` event
containing a `TokenUsage` struct with input tokens, output tokens, cache
creation tokens, and cache read tokens (as reported by the provider; cache
fields are zero for providers that don't report them).

### CostTracker

`CostTracker` (`cost.go:20`) accumulates usage across a session and converts
to dollar estimates:

- **AddUsage(input, output)**: legacy method for raw token deltas.
- **AddTokenUsage(u TokenUsage)**: preferred — also accumulates cache tokens.
- **EstimateCost(model)**: returns USD cost for accumulated tokens.
- **EstimateCostFor(model, service)**: service-aware pricing lookup.

### Price Sources

Pricing comes from one of two sources, with a fallback chain:

1. **Per-tracker override** — `CostTracker.SetPriceSource(src)`. Used by
   `main.go` to wire in a `CatalogPriceSource`.
2. **Package default** — `SetDefaultPriceSource(src)` sets the default for
   all future trackers. Defaults to `EmbeddedPriceSource()`.
3. **Embedded floor** — the `catwalk` package's bundled pricing snapshot.
   Always available, even offline.

`CatalogPriceSource` (`cost.go:167`) is backed by the autoupdate catalog
(`internal/catalog`). It joins on `Service ↔ InferenceProvider` (the Phase 5
audit contract): when `service` is set, only that provider's models are
searched. On any miss, it falls through to the embedded source.

`FormatCost` (`cost.go:117`) formats the float for display:
- `< $0.001` → `"<$0.001"`
- `< $0.01` → four decimal places
- `< $1.00` → three decimal places
- Otherwise → two decimal places

### Pricing Model

The estimated cost accounts for three tiers per model:

```
cost = (regularInput / 1M) * costPer1MIn
     + (output      / 1M) * costPer1MOut
     + (cacheCreate  / 1M) * costPer1MInCached
     + (cacheRead    / 1M) * costPer1MInCached
```

Where `regularInput = input - cacheCreate - cacheRead`. If the cache counts
exceed total input (impossible in practice but defended), `regularInput`
falls back to total input. If `costPer1MInCached` is zero (not reported by
the provider), it falls back to `costPer1MIn`.

### Usage Event Flow

During a multi-step tool loop:

1. Each fantasy step emits its own `"usage"` event with per-step token
   counts (tested in `TestStreamChat_usageAccumulation`,
   `stream_test.go:442`).
2. The agent wires `CostTracker.AddTokenUsage` into its event handler, so
   every `"usage"` event increments the session counters.
3. After the turn, the cost can be queried via `EstimateCostFor(model,
   service)`.

### Complete Streaming Test Coverage

Stream behavior is tested in `internal/llm/stream_test.go` and
`internal/llm/tools_test.go`:

- `TestSafeSend_*` (4 tests, `stream_test.go:28-140`): backpressure,
  buffered vs unbuffered, cancellation-while-blocked.
- `TestToolResultText_*` (7 tests, `stream_test.go:150-214`): text
  extraction from fantasy output shapes.
- `TestStreamChat_midStreamError` (stream_test.go:343): tool executes but
  then the model fails.
- `TestStreamChat_contextCancelledDuringTool` (stream_test.go:376):
  cancellation propagation through the tool loop.
- `TestStreamChat_usageAccumulation` (stream_test.go:442): per-step usage
  events accumulate correctly.
- `TestStreamChat_emptyToolResult` (stream_test.go:475): tool returning
  empty content.
- `TestStreamChat_malformedEvent` (stream_test.go:494): unknown stream part
  types are skipped.
- `TestStreamChatSendsValidToolSchemaAndExecutesToolCall`
  (tools_test.go:586): end-to-end tool call with schema validation.
