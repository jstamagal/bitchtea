# bitchtea → fantasy + catwalk migration plan (v3)

Status: **DRAFT v3 — incorporates codex's 7 revisions to v2.** All fantasy type/field/callback names verified against `/home/admin/go/pkg/mod/charm.land/fantasy@v0.17.1/` source. Pending one final codex pass before implementation.

## Why v3 exists

v2 was accepted-with-revisions by codex. Architecture (fantasy owns the loop, option B) is correct. v3 fixes:

1. **Wrong fantasy v0.17.1 API names** in v2 (`AgentStreamCall.Prompt` is `string` not `[]Message`; messages live in `Messages`. Callbacks return `error`. `OnStreamFinish` takes 3 args. `WithMaxSteps` doesn't exist — use `WithStopConditions(StepCountIs(n))`. `PrepareStep` returns `(context.Context, PrepareStepResult, error)`.)
2. **Wrong tool wrapper types** in v2 (`ToolInfo.InputSchema` doesn't exist — it's `Parameters map[string]any` + `Required []string`. Interface returns `ToolResponse` not `ToolResult`. **Returning a Go `error` from `Run` aborts the stream** — use `fantasy.NewTextErrorResponse(content)` for normal tool failures.)
3. **Naive transcript construction** in v2 (single-assistant-message append couldn't represent a multi-step loop, plus mutating `a.messages` from inside `Run` raced fantasy's goroutine). Fix: walk `result.Steps[].Messages` after `Stream` returns.
4. **Event package split** needed type aliases (`type Event = event.Event`) for graceful caller migration.
5. **Tool event signaling** had two owners per event — now: `OnToolCall` fires `tool_start`, `OnToolResult` fires `tool_result`, `Run` only executes. Context-aware sends throughout.
6. **Provider routing** used substring matches — now uses `net/url` + provider package constants (`openai.DefaultURL` etc.). Adds `vercel.New`.
7. **Cache invalidation** needs a mutex for concurrent `StreamChat` + UI config commands.

Plus: stale Z.ai URLs in v2 (now correct against `internal/config/config.go`); old llm tests ported in parallel with implementation, not after.

## Goal

Build + tests green with bitchtea calling fantasy as its agent loop. The UI's `Event` stream stays exactly as it is. The session JSONL on disk stays exactly as it is. Everything between gets rewritten on top of fantasy.

This is **not** a one-night job; expect 2–3 sessions:
- Session 1 (this plan): types + provider wiring + tool wrappers + new agent loop. Build green, basic tests green.
- Session 2: race + vet clean, manual smoke across profiles, port the old llm tests fully.
- Session 3: codex code review, fix findings, cross-check docs, declare done.

## Out of scope (deferred, written down so we don't forget)

- Daemon rebuild (archived in `_attic/`). Verification gate explicitly skips `cmd/daemon`.
- Esc-graduation per `REBUILD_NOTES.md`.
- Per-tool `context.Context` for fast cancellation (related to Esc).
- MCP integration (modelcontextprotocol/go-sdk).
- Hooks (PreToolUse / PostToolUse).
- Provider OAuth (Hyper, Copilot).
- catwalk HTTP autoupdate / ETag caching — embedded snapshot only.
- Sub-agent / `agentic_fetch` pattern.
- Loop detection.
- Migrating `session.Entry` JSONL to fantasy-native message parts (we convert at boundary).

---

## Verified fantasy v0.17.1 API surface

These are the actual names/fields/signatures, read from the v0.17.1 source. No more "verify before code" hedges — these are correct now.

### Top-level

```go
fantasy.NewAgent(model fantasy.LanguageModel, opts ...fantasy.AgentOption) fantasy.Agent  // returns interface

type Agent interface {
    Generate(context.Context, AgentCall) (*AgentResult, error)
    Stream(context.Context, AgentStreamCall) (*AgentResult, error)
}
```

### `AgentStreamCall` (the fields we use)

```go
type AgentStreamCall struct {
    Prompt          string         // the *current* user prompt as a string
    Messages        []Message      // prior conversation history
    Headers         map[string]string
    ProviderOptions ProviderOptions

    StopWhen        []StopCondition
    PrepareStep     PrepareStepFunction

    // Stream callbacks (we use these)
    OnTextDelta     OnTextDeltaFunc        // func(id, text string) error
    OnReasoningDelta OnReasoningDeltaFunc  // func(id, text string) error
    OnToolCall      OnToolCallFunc         // func(toolCall ToolCallContent) error
    OnToolResult    OnToolResultFunc       // func(result ToolResultContent) error
    OnStreamFinish  OnStreamFinishFunc     // func(usage Usage, finishReason FinishReason, providerMetadata ProviderMetadata) error
    OnError         OnErrorFunc            // func(error)   <-- no return
    OnStepFinish    OnStepFinishFunc       // func(stepResult StepResult) error
}
```

### Stop conditions

```go
fantasy.StepCountIs(n int) StopCondition       // stop after N steps
fantasy.HasToolCall(toolName string) StopCondition  // takes a name
fantasy.HasContent(ContentType) StopCondition
fantasy.FinishReasonIs(FinishReason) StopCondition
fantasy.MaxTokensUsed(int64) StopCondition
```

### `PrepareStep`

```go
type PrepareStepFunction = func(
    ctx context.Context,
    options PrepareStepFunctionOptions,
) (context.Context, PrepareStepResult, error)

type PrepareStepFunctionOptions struct {
    Steps      []StepResult
    StepNumber int
    Model      LanguageModel
    Messages   []Message
}

type PrepareStepResult struct {
    Model           LanguageModel
    Messages        []Message
    System          *string
    ToolChoice      *ToolChoice
    ActiveTools     []string
    DisableAllTools bool
    Tools           []AgentTool
}
```

### Tool interface and types

```go
type AgentTool interface {
    Info() ToolInfo
    Run(ctx context.Context, params ToolCall) (ToolResponse, error)  // returning error = ABORT stream
    ProviderOptions() ProviderOptions
    SetProviderOptions(opts ProviderOptions)
}

type ToolInfo struct {
    Name        string
    Description string
    Parameters  map[string]any   // <-- this is the schema
    Required    []string
    Parallel    bool
}

type ToolCall struct { ID, Name, Input string }  // Input is the JSON args string

type ToolResponse struct {
    Type      string  // "text" | "image" | "media"
    Content   string
    Data      []byte
    MediaType string
    Metadata  string
    IsError   bool
}

// Helpers — use these, don't return Go errors for normal tool failures
func NewTextResponse(content string) ToolResponse
func NewTextErrorResponse(content string) ToolResponse  // <-- for normal tool failures
```

### Message + parts

```go
type MessageRole string
const (
    MessageRoleSystem    MessageRole = "system"
    MessageRoleUser      MessageRole = "user"
    MessageRoleAssistant MessageRole = "assistant"
    MessageRoleTool      MessageRole = "tool"
)

type Message struct {
    Role            MessageRole
    Content         []MessagePart
    ProviderOptions ProviderOptions
}

type MessagePart interface {
    GetType() ContentType
    Options() ProviderOptions
}

// Concrete parts we use:
type TextPart struct { Text string; ProviderOptions ProviderOptions }
type ReasoningPart struct { Text string; ProviderOptions ProviderOptions }
type ToolCallPart struct {
    ToolCallID, ToolName, Input string
    ProviderExecuted bool
    ProviderOptions  ProviderOptions
}
type ToolResultPart struct {
    ToolCallID       string
    Output           ToolResultOutputContent
    ProviderExecuted bool
    ProviderOptions  ProviderOptions
}

type ToolResultOutputContent interface { GetType() ToolResultContentType }
type ToolResultOutputContentText struct { Text string }  // implements ToolResultOutputContent
```

### Stream callback content types

```go
// fired by OnToolCall — distinct from ToolCallPart (which lives inside Message)
type ToolCallContent struct {
    ToolCallID, ToolName, Input string
    ProviderExecuted bool
    ProviderMetadata ProviderMetadata
    Invalid          bool
    ValidationError  error
}

// fired by OnToolResult
type ToolResultContent struct {
    ToolCallID, ToolName string
    Result               ToolResultOutputContent
    ClientMetadata       string
    ProviderExecuted     bool
    ProviderMetadata     ProviderMetadata
}
```

### Result + Usage

```go
type AgentResult struct {
    Steps      []StepResult
    Response   Response
    TotalUsage Usage
}

type StepResult struct {
    Response             // embedded — has FinishReason, Usage, Content, etc.
    Messages []Message   // the messages produced this step (this is what we walk for transcript)
}

type Usage struct {
    InputTokens, OutputTokens, TotalTokens int64
    ReasoningTokens                        int64
    CacheCreationTokens, CacheReadTokens   int64
}

type FinishReason string
const (
    FinishReasonStop          = "stop"
    FinishReasonLength        = "length"
    FinishReasonContentFilter = "content-filter"
    FinishReasonToolCalls     = "tool-calls"
    FinishReasonError         = "error"
    FinishReasonOther         = "other"
    FinishReasonUnknown       = "unknown"
)
```

### Provider package constants (verified)

| Package | Constant | Value |
|---|---|---|
| `providers/openai` | `openai.DefaultURL` | `https://api.openai.com/v1` |
| `providers/openrouter` | `openrouter.DefaultURL` | `https://openrouter.ai/api/v1` |
| `providers/vercel` | `vercel.DefaultURL` | `https://ai-gateway.vercel.sh/v1` |
| `providers/anthropic` | `anthropic.DefaultURL` | `https://api.anthropic.com` (no `/v1`!) |

Watch the Anthropic case: passing `https://api.anthropic.com/v1` (or any `/v1` suffix) through `anthropic.New(WithBaseURL(...))` may double-prefix depending on internal SDK behavior. Strip a trailing `/v1` from anthropic baseURLs before handing to `anthropic.New`.

### Provider package list (v0.17.1)

`anthropic`, `azure`, `bedrock`, `google`, `kronk`, `openai`, `openaicompat`, `openrouter`, `vercel`. No `zai-anthropic` package — zai-anthropic uses `anthropic.New(WithBaseURL("https://api.z.ai/api/anthropic"))`.

---

## Architectural decisions

### D1. fantasy owns the loop

`Agent.SendMessage`'s hand-rolled `for {}` loop dies. Replaced by:

```go
fa := fantasy.NewAgent(model,
    fantasy.WithSystemPrompt(sys),
    fantasy.WithTools(toolset...),                         // real, not passthrough
    fantasy.WithStopConditions(fantasy.StepCountIs(maxSteps)),
    fantasy.WithPrepareStep(prepStep),
)
result, err := fa.Stream(ctx, fantasy.AgentStreamCall{
    Prompt:   currentUserMessage,                          // <-- string, the new turn
    Messages: priorHistoryAsFantasyMessages,               // <-- []fantasy.Message, prior turns
    OnTextDelta: func(id, text string) error {
        return safeSend(ctx, events, agent.Event{Type: "text", Text: text})
    },
    OnReasoningDelta: func(id, text string) error {
        return safeSend(ctx, events, agent.Event{Type: "thinking", Text: text})
    },
    OnToolCall: func(call fantasy.ToolCallContent) error {
        // owner of: state, tool_start, ToolCalls counter
        a.ToolCalls[call.ToolName]++
        if err := safeSend(ctx, events, agent.Event{Type: "state", State: agent.StateToolCall}); err != nil { return err }
        return safeSend(ctx, events, agent.Event{Type: "tool_start", ToolName: call.ToolName, ToolArgs: call.Input})
    },
    OnToolResult: func(res fantasy.ToolResultContent) error {
        // owner of: tool_result
        text := ""
        if t, ok := res.Result.(fantasy.ToolResultOutputContentText); ok { text = t.Text }
        return safeSend(ctx, events, agent.Event{Type: "tool_result", ToolName: res.ToolName, ToolResult: text})
    },
    OnStreamFinish: func(u fantasy.Usage, _ fantasy.FinishReason, _ fantasy.ProviderMetadata) error {
        a.CostTracker.AddTokenUsage(toLLMUsage(u))
        return nil
    },
    OnError: func(err error) {
        _ = safeSend(ctx, events, agent.Event{Type: "error", Error: err})
    },
})
```

`safeSend` is context-aware:
```go
func safeSend(ctx context.Context, ch chan<- agent.Event, ev agent.Event) error {
    select {
    case ch <- ev:               return nil
    case <-ctx.Done():           return ctx.Err()
    }
}
```

This keeps a canceled UI from wedging fantasy's tool dispatcher goroutine.

### D2. Tools become real `fantasy.AgentTool`s

Each tool name in `internal/tools/tools.go` becomes a hand-rolled `fantasy.AgentTool` whose `Run` calls `Registry.Execute`. `internal/tools/tools.go` itself does **not** change.

```go
// internal/llm/tools.go (new)
type bitchteaTool struct {
    info fantasy.ToolInfo
    reg  *tools.Registry
}

func (t *bitchteaTool) Info() fantasy.ToolInfo { return t.info }

func (t *bitchteaTool) Run(ctx context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
    out, err := t.reg.Execute(ctx, call.Name, call.Input)
    if err != nil {
        // CRITICAL: return NewTextErrorResponse, NOT a Go error.
        // Returning a Go error aborts the entire stream (fantasy treats Run errors as critical).
        return fantasy.NewTextErrorResponse(fmt.Sprintf("Error: %v", err)), nil
    }
    return fantasy.NewTextResponse(out), nil
}

func (t *bitchteaTool) ProviderOptions() fantasy.ProviderOptions          { return nil }
func (t *bitchteaTool) SetProviderOptions(opts fantasy.ProviderOptions)   {}
```

`Run` does NOT touch `a.messages` or send on `events`. State + tool_start come from `OnToolCall`. tool_result comes from `OnToolResult`. The Run method's only job is to execute and return.

The toolset is built per-stream-call from `Registry.Definitions()`:

```go
func translateTools(reg *tools.Registry) []fantasy.AgentTool {
    defs := reg.Definitions()  // []llm.ToolDef — existing shape
    out := make([]fantasy.AgentTool, 0, len(defs))
    for _, d := range defs {
        // d.Function.Parameters is map[string]interface{} — split into Parameters + Required
        params, required := splitSchema(d.Function.Parameters)
        out = append(out, &bitchteaTool{
            info: fantasy.ToolInfo{
                Name:        d.Function.Name,
                Description: d.Function.Description,
                Parameters:  params,
                Required:    required,
                Parallel:    false,
            },
            reg: reg,
        })
    }
    return out
}
```

### D3. Transcript: walk `result.Steps[].Messages` after `Stream` returns

Old plan v2's "append one assistant message at the end" approach can't represent a multi-step loop. After `Stream` returns, fantasy's `result.Steps` contains every assistant message and tool result message in order. Walk it and convert back to `[]llm.Message`:

```go
// inside Agent.sendMessage, after fa.Stream returns:
for _, step := range result.Steps {
    for _, fm := range step.Messages {
        a.messages = append(a.messages, fantasyToLLM(fm))
    }
}
```

Where `fantasyToLLM` converts `fantasy.Message` (parts-based) back to `llm.Message{Role, Content, ToolCalls, ToolCallID}`:

- `Role: MessageRoleAssistant` + `Content: [TextPart{...}, ToolCallPart{...}, ...]` →
  `llm.Message{Role: "assistant", Content: textConcat, ToolCalls: [{ID, Type:"function", Function:{Name, Arguments: input}}]}`
- `Role: MessageRoleTool` + `Content: [ToolResultPart{ToolCallID, Output: ToolResultOutputContentText{Text}}]` →
  `llm.Message{Role: "tool", Content: text, ToolCallID: id}`
- `Role: MessageRoleUser` (rare in step output but possible) → keep as-is

Reverse direction (`llm.Message` → `fantasy.Message`) at the input boundary is in D4.

`a.messages` is mutated only on the agent's main goroutine, after `Stream` returns. No race with fantasy callbacks.

### D4. Input convert: `[]llm.Message` → `Prompt + []fantasy.Message`

Before calling `fa.Stream`, split `a.messages` into "prior history" (becomes `Messages`) and "current prompt" (becomes `Prompt string`).

```go
func splitForFantasy(msgs []llm.Message, currentUser string) (prompt string, prior []fantasy.Message) {
    prior = make([]fantasy.Message, 0, len(msgs))
    for _, m := range msgs {
        switch m.Role {
        case "system":
            // DROPPED — passed via fantasy.WithSystemPrompt instead. Avoids double-system.
            continue
        case "user":
            prior = append(prior, fantasy.Message{
                Role:    fantasy.MessageRoleUser,
                Content: []fantasy.MessagePart{fantasy.TextPart{Text: m.Content}},
            })
        case "assistant":
            parts := []fantasy.MessagePart{}
            if m.Content != "" {
                parts = append(parts, fantasy.TextPart{Text: m.Content})
            }
            for _, tc := range m.ToolCalls {
                parts = append(parts, fantasy.ToolCallPart{
                    ToolCallID: tc.ID,
                    ToolName:   tc.Function.Name,
                    Input:      tc.Function.Arguments,
                })
            }
            prior = append(prior, fantasy.Message{Role: fantasy.MessageRoleAssistant, Content: parts})
        case "tool":
            prior = append(prior, fantasy.Message{
                Role: fantasy.MessageRoleTool,
                Content: []fantasy.MessagePart{fantasy.ToolResultPart{
                    ToolCallID: m.ToolCallID,
                    Output:     fantasy.ToolResultOutputContentText{Text: m.Content},
                }},
            })
        }
    }
    return currentUser, prior
}
```

Note: `currentUser` is the *new* user message just appended. The convention is that `Prompt` carries the new turn; `Messages` carries everything before it. (Verify this matches fantasy's behavior in agent_test.go before final implementation — there's a chance fantasy expects the user prompt to also live in `Messages`.)

`session.Entry` JSONL stays byte-identical because we never write `[]fantasy.Message` to disk — only `[]llm.Message`.

### D5. Provider routing on (provider, parsed-baseURL) using fantasy constants

```go
import (
    "net/url"
    "strings"

    "charm.land/fantasy"
    "charm.land/fantasy/providers/anthropic"
    "charm.land/fantasy/providers/openai"
    "charm.land/fantasy/providers/openaicompat"
    "charm.land/fantasy/providers/openrouter"
    "charm.land/fantasy/providers/vercel"
)

func buildProvider(cfg ProviderConfig) (fantasy.Provider, error) {
    switch cfg.Provider {
    case "anthropic":
        baseURL := stripV1Suffix(cfg.BaseURL)  // anthropic.DefaultURL has no /v1
        return anthropic.New(
            anthropic.WithAPIKey(cfg.APIKey),
            anthropic.WithBaseURL(baseURL),
        )
    case "openai":
        return routeOpenAICompatible(cfg)
    }
    return nil, fmt.Errorf("unsupported provider: %q", cfg.Provider)
}

func routeOpenAICompatible(cfg ProviderConfig) (fantasy.Provider, error) {
    if cfg.BaseURL == "" {
        return openai.New(openai.WithAPIKey(cfg.APIKey), openai.WithUseResponsesAPI())
    }
    cfgHost, err := hostOf(cfg.BaseURL)
    if err != nil { return nil, err }

    switch {
    case cfgHost == hostOfMust(openai.DefaultURL):
        return openai.New(openai.WithAPIKey(cfg.APIKey), openai.WithUseResponsesAPI())
    case cfgHost == hostOfMust(openrouter.DefaultURL):
        return openrouter.New(openrouter.WithAPIKey(cfg.APIKey))
    case cfgHost == hostOfMust(vercel.DefaultURL):
        return vercel.New(vercel.WithAPIKey(cfg.APIKey))
    default:
        // ollama, zai-openai, copilot, aihubmix, avian, cortecs, huggingface, ionet, nebius, synthetic, venice, xai, custom local, etc.
        return openaicompat.New(
            openaicompat.WithAPIKey(cfg.APIKey),
            openaicompat.WithBaseURL(cfg.BaseURL),
        )
    }
}

func hostOf(rawURL string) (string, error) {
    u, err := url.Parse(rawURL)
    if err != nil { return "", err }
    return strings.ToLower(u.Host), nil
}

func hostOfMust(rawURL string) string {
    h, _ := hostOf(rawURL)
    return h
}

func stripV1Suffix(rawURL string) string {
    return strings.TrimSuffix(strings.TrimSuffix(rawURL, "/v1"), "/v1/")
}
```

Routing is host-based and uses `DefaultURL` constants from each provider package. If fantasy upstream changes a constant, this routing follows automatically.

Tests (mandatory in step 9 below) cover every built-in profile in `internal/config/config.go`:
- ollama → openaicompat (host `localhost:11434`)
- openrouter → openrouter (host `openrouter.ai`)
- aihubmix → openaicompat
- avian → openaicompat
- copilot → openaicompat (host `api.githubcopilot.com`)
- cortecs → openaicompat
- huggingface → openaicompat
- ionet → openaicompat
- nebius → openaicompat
- synthetic → openaicompat
- venice → openaicompat
- vercel → vercel (host `ai-gateway.vercel.sh`)
- xai → openaicompat
- zai-openai → openaicompat (host `api.z.ai`, baseURL `/api/coding/paas/v4`)
- zai-anthropic → anthropic (host `api.z.ai`, baseURL `/api/anthropic`)
- (no profile, OPENAI_API_KEY only) → openai

### D6. Cache invalidation + mutex

`Client` holds a cached `fantasy.Provider` + `fantasy.LanguageModel`, both rebuilt lazily. `Set*` methods invalidate. A `sync.Mutex` guards the cache because `StreamChat` (called from agent goroutine) and `Set*` (called from UI goroutine via slash commands) can race.

```go
type Client struct {
    mu sync.Mutex

    APIKey, BaseURL, Model, Provider string
    DebugHook func(DebugInfo)

    provider fantasy.Provider     // cached
    model    fantasy.LanguageModel // cached
}

func (c *Client) SetBaseURL(url string) { c.mu.Lock(); defer c.mu.Unlock(); c.BaseURL = url; c.invalidateLocked() }
func (c *Client) SetAPIKey(key string)  { c.mu.Lock(); defer c.mu.Unlock(); c.APIKey  = key; c.invalidateLocked() }
func (c *Client) SetModel(model string) { c.mu.Lock(); defer c.mu.Unlock(); c.Model   = model; c.invalidateLocked() }
func (c *Client) SetProvider(p string)  { c.mu.Lock(); defer c.mu.Unlock(); c.Provider= p;   c.invalidateLocked() }
func (c *Client) SetDebugHook(h func(DebugInfo)) {
    c.mu.Lock(); defer c.mu.Unlock()
    c.DebugHook = h
    c.invalidateLocked()  // HTTP client rebuilds — provider must rebuild too
}
func (c *Client) invalidateLocked() { c.provider = nil; c.model = nil }

func (c *Client) ensureModel(ctx context.Context) (fantasy.LanguageModel, error) {
    c.mu.Lock(); defer c.mu.Unlock()
    if c.model != nil { return c.model, nil }
    if c.provider == nil {
        p, err := buildProvider(c.toProviderConfig())
        if err != nil { return nil, err }
        c.provider = p
    }
    m, err := c.provider.LanguageModel(ctx, c.Model)
    if err != nil { return nil, err }
    c.model = m
    return m, nil
}
```

`StreamChat` calls `ensureModel` once at the top, then **releases** the mutex before invoking `fa.Stream` — we do not hold the mutex across the entire stream. If a `Set*` arrives mid-stream, it invalidates the cache for the *next* call but doesn't interrupt the in-flight one.

Test (step 9): `TestClientInvalidatesOnSet*` for each setter, `TestClientConcurrentSetAndStream` for the mid-stream case.

Conservative choice: rebuild both provider+model on every Set, even `SetModel` (which technically only needs to rebuild the model). Simpler reasoning, low cost.

### D7. PrepareStep: emit `state: StateThinking`

```go
prepStep := func(ctx context.Context, opts fantasy.PrepareStepFunctionOptions) (context.Context, fantasy.PrepareStepResult, error) {
    if err := safeSend(ctx, events, agent.Event{Type: "state", State: agent.StateThinking}); err != nil {
        return ctx, fantasy.PrepareStepResult{}, err
    }
    return ctx, fantasy.PrepareStepResult{}, nil  // empty result = use defaults
}
```

Real PrepareStep work (Anthropic prompt-cache markers, queued-prompt drain, MCP refresh) is a future phase.

### D8. CostTracker: catwalk pricing + fantasy.Usage

Public API stays exact: `NewCostTracker()`, `AddUsage(in, out int)`, `AddTokenUsage(usage llm.TokenUsage)`, `TotalTokens() int`, `EstimateCost(model string) float64`. `TokenUsage` exact field names: `InputTokens, OutputTokens, CacheCreationTokens, CacheReadTokens` (plus any others the existing tests reference — to be verified from git history when porting tests).

Internally:
- One-time load of catwalk catalog via `embedded.GetAll()`.
- `EstimateCost(model)` looks up by model ID, pulls `CostPer1MIn`, `CostPer1MOut`, `CostPer1MInCached`, `CostPer1MOutCached`.
- `AddTokenUsage` accumulates fantasy-derived `Usage` fields.
- Model not in catwalk → zero pricing, no error.

Conversion `fantasy.Usage → llm.TokenUsage` lives in `convert.go`:
```go
func toLLMUsage(u fantasy.Usage) llm.TokenUsage {
    return llm.TokenUsage{
        InputTokens:         int(u.InputTokens),
        OutputTokens:        int(u.OutputTokens),
        CacheCreationTokens: int(u.CacheCreationTokens),
        CacheReadTokens:     int(u.CacheReadTokens),
    }
}
```

### D9. DebugInfo: HTTP RoundTripper

`DebugInfo` is owned by `internal/llm/types.go`. `internal/llm/debug.go` only implements the RoundTripper, not the type.

`DebugInfo` fields: `Method, URL, RequestHeaders, ResponseHeaders map[string][]string; RequestBody, ResponseBody string; StatusCode int`.

The RoundTripper:
- Copies request body via `io.ReadAll` + `bytes.NewReader` rewind so the client still sees it.
- For responses: only captures body if `Content-Type` is `application/json` or `text/plain`. **Skips** `text/event-stream` (would consume the SSE body). Streaming responses: `ResponseBody = "(stream)"`.
- Wraps the provider's HTTP client via each provider's `WithHTTPClient` option.

`SetDebugHook` invalidates the cached provider/model (HTTP client rebuilds with provider). When `SetDebugHook(nil)`: same — provider rebuilds with default transport.

### D10. ErrorHint

```go
func ErrorHint(err error) string {
    if err == nil { return "" }
    var pe *fantasy.ProviderError
    if errors.As(err, &pe) {
        if pe.IsContextTooLarge() { return "Context too large. Try /compact." }
        switch pe.StatusCode {
        case 401: return "Auth failed. Check /apikey."
        case 429: return "Rate limited or out of quota."
        case 500, 502, 503: return "Provider error. Try again in a moment."
        }
    }
    msg := err.Error()
    switch {
    case errors.Is(err, context.Canceled):                   return ""
    case strings.Contains(msg, "no such host"):              return "DNS lookup failed."
    case strings.Contains(msg, "x509"):                      return "TLS error."
    case strings.Contains(msg, "connection refused"):        return "Connection refused — is the local server running?"
    }
    return ""
}
```

`fantasy.ProviderError` field/method names (`StatusCode`, `IsContextTooLarge`, `IsRetryable`, `Cause`) verified before final write — the writer reads `errors.go` in the fantasy package and corrects in this plan if drifted.

### D11. Compaction stays on a thin completion wrapper

`Agent.Compact` and `flushCompactedMessagesToDailyMemory` use a sibling helper:

```go
func (c *Client) CompleteText(ctx context.Context, msgs []llm.Message) (string, error) {
    model, err := c.ensureModel(ctx)
    if err != nil { return "", err }
    fa := fantasy.NewAgent(model)
    var out strings.Builder
    prompt, prior := splitForFantasy(msgs, "")
    _, err = fa.Stream(ctx, fantasy.AgentStreamCall{
        Prompt:   prompt,
        Messages: prior,
        OnTextDelta: func(_, t string) error { out.WriteString(t); return nil },
    })
    return out.String(), err
}
```

`ChatStreamer` interface stays. `*Client` still implements it. Test fakes don't break.

---

## Public surface — source-compatible (not byte-compatible)

```go
// types.go
type Message struct {
    Role       string `json:"role"`
    Content    string `json:"content"`
    ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
    ToolCallID string `json:"tool_call_id,omitempty"`
}
type StreamEvent struct {
    Type        string  // "text", "thinking", "tool_call", "usage", "error", "done"
    Text        string
    ToolName    string
    ToolArgs    string
    ToolCallID  string
    Usage       *TokenUsage
    Error       error
}
type ToolDef struct {
    Type     string `json:"type"`
    Function ToolFuncDef `json:"function"`
}
type ToolFuncDef struct {
    Name        string `json:"name"`
    Description string `json:"description"`
    Parameters  map[string]interface{} `json:"parameters"`
}
type ToolCall struct {
    ID       string `json:"id"`
    Type     string `json:"type"`
    Function FunctionCall `json:"function"`
}
type FunctionCall struct {
    Name      string `json:"name"`
    Arguments string `json:"arguments"`
}
type TokenUsage struct {
    InputTokens         int
    OutputTokens        int
    CacheCreationTokens int
    CacheReadTokens     int
}
type DebugInfo struct {
    Method, URL                     string
    RequestHeaders, ResponseHeaders map[string][]string
    RequestBody, ResponseBody       string
    StatusCode                      int
}
type ChatStreamer interface {
    StreamChat(ctx context.Context, msgs []Message, tools []ToolDef, events chan<- StreamEvent)
}

// client.go
type Client struct {
    APIKey, BaseURL, Model, Provider string  // public mutable fields
    DebugHook func(DebugInfo)                 // public assignable field
    // ... unexported fantasy state + mutex
}
func NewClient(apiKey, baseURL, model, provider string) *Client
func (c *Client) StreamChat(ctx, msgs, tools, events chan<- StreamEvent)  // ChatStreamer
func (c *Client) CompleteText(ctx, msgs) (string, error)
func (c *Client) SetDebugHook(hook func(DebugInfo))
func (c *Client) SetBaseURL(url string)
func (c *Client) SetAPIKey(key string)
func (c *Client) SetModel(model string)
func (c *Client) SetProvider(provider string)

// cost.go
type CostTracker struct{ ... }
func NewCostTracker() *CostTracker
func (c *CostTracker) AddUsage(in, out int)
func (c *CostTracker) AddTokenUsage(u TokenUsage)
func (c *CostTracker) TotalTokens() int
func (c *CostTracker) EstimateCost(model string) float64

// errors.go
func ErrorHint(err error) string
```

---

## Event package split

`agent.Event` and `agent.State` move to `internal/agent/event`. To preserve source compatibility for callers, **keep type aliases in `internal/agent`**:

```go
// internal/agent/aliases.go (new)
package agent

import "github.com/jstamagal/bitchtea/internal/agent/event"

type Event = event.Event
type State = event.State

const (
    StateIdle     = event.StateIdle
    StateThinking = event.StateThinking
    StateToolCall = event.StateToolCall
)
```

`internal/llm/tools.go` imports `internal/agent/event` directly (no cycle). Existing UI/tests keep using `agent.Event` and don't need to change.

---

## File layout for new `internal/llm/`

```
internal/llm/
├── types.go        — Message, StreamEvent, ToolCall, FunctionCall, ToolDef, ToolFuncDef, TokenUsage, DebugInfo, ChatStreamer
├── client.go       — Client struct + mutex, NewClient, ensureModel, invalidate, Set* methods, CompleteText, SetDebugHook
├── stream.go       — *Client.StreamChat: builds fantasy.Agent, runs Stream, walks result.Steps[].Messages
├── tools.go        — bitchteaTool wrapper, translateTools, splitSchema. Imports internal/agent/event.
├── convert.go      — splitForFantasy ([]Message → Prompt+[]fantasy.Message), fantasyToLLM (fantasy.Message → llm.Message), toLLMUsage
├── providers.go    — buildProvider, routeOpenAICompatible, hostOf, stripV1Suffix
├── cost.go         — CostTracker + catwalk embedded.GetAll() pricing
├── debug.go        — http.RoundTripper that captures DebugInfo (skips text/event-stream)
└── errors.go       — ErrorHint(error) → string with errors.As(*fantasy.ProviderError)
```

Approx 700–900 LoC total.

---

## Migration sequence

1. **Final plan review** — codex passes once more on this v3. Target: accept (no revisions).
2. **Carve `internal/agent/event` + add aliases.** Move `Event` and `State` types out of `agent.go`. Add `aliases.go`. `go build ./...` must stay green.
3. **Write `internal/llm/types.go`** — pure type re-decls.
4. **Window 1 fan-out** (parallel agents):
    - Claude: `providers.go` + `client.go` (riskiest — D5/D6).
    - codex (jstamagal): `debug.go` (RoundTripper).
    - codex (foreigner): `errors.go` (port + `errors.As`).
    - gemini-3.1-pro: `cost.go` (catwalk lookup).
5. **`go build ./...`** — fix drift between window-1 outputs.
6. **Port deleted llm tests in parallel with implementation.** Use `git log --oneline -- internal/llm/` + `git show <commit>:internal/llm/*_test.go` to recover. Streaming-parsing, retry/error-hint, cost coverage. Update for any signature drift. (gemini-3.1-flash via acpx.)
7. **Window 2 fan-out:**
    - codex (jstamagal): `convert.go` (mechanical part-translation).
    - codex (foreigner): `tools.go` (AgentTool wrapper + schema split).
    - Claude (after Window 2 lands): `stream.go` (the integrator — needs everything else).
8. **Rewrite `internal/agent/agent.go` `sendMessage`** — drop the old loop, call the new flow. Persona, `@file`, follow-up sanitizer, MaybeQueueFollowUp all stay.
9. **Mandatory tests:**
    - `TestBuildProvider_RoutesByHost` — every built-in profile in `internal/config/config.go` resolves to the expected provider package.
    - `TestClientInvalidatesOnSet*` — each setter rebuilds provider+model.
    - `TestClientConcurrentSetAndStream` — mid-stream Set* doesn't race.
    - `TestSplitForFantasy_DropsSystem` — system messages don't reach `Messages`.
    - `TestSplitForFantasy_RoundtripPreservesShape` — `splitForFantasy` + `fantasyToLLM` round-trips the contract.
    - `TestBitchteaToolReturnsErrorAsResponse` — `Run` never returns Go error for tool failures.
10. **`go test ./...`** must be green before race/vet.
11. **`go test -race ./...`** — fix any races. Likely candidates: `safeSend` correctness, `OnToolCall` and `Run` running concurrently, mutex coverage on cache.
12. **`go vet ./...`** — clean.
13. **Manual smoke across profiles** (codex required this — not just OPENAI_API_KEY):
    - `OPENAI_API_KEY=… ./bitchtea` — read README.md
    - `./bitchtea --profile openrouter` (if `OPENROUTER_API_KEY`) — same prompt
    - `./bitchtea --profile ollama` (if local ollama) — same prompt
    - `ANTHROPIC_API_KEY=… ./bitchtea --provider anthropic --model claude-sonnet-4-6` — same prompt
   For each: streaming text, ≥1 tool call (`read README.md`), `tool_result` displayed, `done` event, no panic, cost line non-empty.
14. **`/codex:review`** on the working tree. Triage findings.
15. **Cross-check `docs/streaming.md`, `docs/tools.md`, `docs/agent-loop.md`** against actual code. Drift = bug. Fix code (or doc, if code drifted earlier).

---

## Verification gates

```bash
go build -o bitchtea .
go build ./...
go test ./...
go test -race ./...
go vet ./...
```

`cmd/daemon` is **explicitly skipped** — daemon archived to `_attic/`.

Plus the four-profile manual smoke in step 13.

---

## Risks (codex's flags + ours, all addressed in plan)

- **`OnReasoningDelta` semantics** — fantasy's reasoning is Anthropic-extended-thinking. bitchtea's "thinking" event was OpenAI-style draft. Render as thinking either way; gate by provider if it looks weird.
- **Tool input streaming via `OnToolInputDelta`** — we ignore it. `OnToolCall` fires once with the complete input.
- **Stale provider/model on runtime config commands** — D6 mutex + cache invalidation. Tests in step 9 are mandatory.
- **OpenAI Responses API tool-call round-tripping** — fantasy's openai provider uses Responses API by default. Tool result messages may need different formatting than chat completions. Verify in step 13.
- **Anthropic baseURL `/v1` double-prefix** — `stripV1Suffix` in D5. Test it.
- **HTTP RoundTripper draining streams** — D9 explicitly skips `text/event-stream`. Test: hook captures static JSON correctly, stream still flows during a streaming call.
- **System prompt double-send** — D4 drops `Role:"system"` at the convert boundary. Test in step 9.
- **`Prompt` vs `Messages` semantics in `AgentStreamCall`** — currently the plan uses `Prompt` for the new turn, `Messages` for prior history. **Verify against fantasy `agent_test.go` and `agent_stream_test.go` in v0.17.1 before final implementation.** If fantasy expects the new user message to live inside `Messages` (with `Prompt` empty or duplicated), `splitForFantasy` updates accordingly. This is the single highest "did I read the API right" risk.
- **`Run` returning Go error aborts the stream** — D2 explicitly uses `NewTextErrorResponse` for normal failures. Test in step 9.
- **Concurrent `OnToolCall` + `Run` callbacks** — D1 uses context-aware sends and shared accumulator (`a.ToolCalls`) is only mutated from `OnToolCall` (single goroutine). No mutex needed there.
- **Z.ai URLs in plan must match config** — fixed: zai-openai is `https://api.z.ai/api/coding/paas/v4`, zai-anthropic is `https://api.z.ai/api/anthropic`.

---

## Future phases (for later sessions, not now)

- **Phase 2: typed tools.** Rewrite each tool in `internal/tools/` as `fantasy.NewAgentTool[ParamsStruct]`. Removes the `Execute(name, argsJSON)` switch.
- **Phase 3: fantasy-native messages end-to-end.** Drop `llm.Message`. `session.Entry` serializes `[]fantasy.MessagePart`. Requires JSONL migration.
- **Phase 4: real PrepareStep.** Anthropic prompt-cache markers, queued-prompt drain, MCP tool refresh.
- **Phase 5: catwalk HTTP autoupdate** + model picker.
- **Phase 6: MCP** via `modelcontextprotocol/go-sdk` + `WithProviderDefinedTools`.
- **Phase 7: daemon rebuild** — currently in `_attic/`.
- **Phase 8: per-tool Esc cancellation** per `REBUILD_NOTES.md`.
- **Phase 9: Service identity field** on `Profile`/`Config` so we don't route by URL host (the TODO(Phase6) marker in `internal/config/config.go`).
