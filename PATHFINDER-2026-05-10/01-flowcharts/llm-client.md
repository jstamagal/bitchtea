# Flowchart: LLM Client / Provider Abstraction

```mermaid
flowchart TD
    %% Entry point
    A["StreamChat(stream.go:41)<br/>ctx, []Message, *tools.Registry, chan<- StreamEvent"]

    %% Retry loop
    A --> B["retryBackoff<br/>stream.go:45-71<br/>1s → 2s → 4s → 8s → 16s → 32s → 64s"]
    B --> C{"attempt <=<br/>len(retryBackoff)"}
    C -->|"attempt > 0"| D["delay := retryBackoff[attempt-1]<br/>stream.go:47"]
    D --> E["safeSend: StreamEvent{Type:text<br/>'retrying after server error...'}<br/>stream.go:49-52"]
    E --> F{"ctx.Done()?"}
    F -->|"yes"| G["StreamEvent{Type:error}<br/>return<br/>stream.go:56-57"]
    F -->|"no"| H["time.After(delay)<br/>stream.go:54"]
    C -->|"attempt == 0"| I["streamOnce<br/>stream.go:61"]

    %% streamOnce
    I --> J["ensureModel<br/>client.go → builds/caches fantasy.Provider<br/>client.go:194"]
    J --> K{"Provider == 'anthropic'?"}
    K -->|"yes"| L["buildAnthropicProvider<br/>providers.go:44-54"]
    K -->|"no, openai/empty"| M{"Service != ''?"]
    M -->|"yes"| N["routeByService<br/>providers.go:58-86"]
    M -->|"no"| O["routeOpenAICompatible<br/>providers.go:90-143"]
    L --> P["anthropic.New(opts...)<br/>providers.go:46-52"]
    N --> Q{"Service == 'openrouter'?"]
    N -->|"yes"| R["openrouter.New<br/>providers.go:60-65"]
    Q -->|"vercel'"| S["vercel.New<br/>providers.go:67-72"]
    Q -->|"default"| T["openaicompat.New<br/>providers.go:77-84"]
    O --> U{"host(baseURL) matches?"}
    U -->|"openai default"| V["openai.New<br/>providers.go:107-116"]
    U -->|"openrouter default"| R
    U -->|"vercel default"| S
    U -->|"custom/other"| T

    J --> W["splitForFantasy(msgs)<br/>convert.go:16-80"]
    W --> X["prompt = TAIL user content<br/>prior = all non-TAIL messages<br/>systemPrompt = joined system messages"]
    W --> Y["fantasy.Message conversion<br/>user/assistant/tool/system → fantasy types"]

    %% Tool setup
    X --> Z{"reg != nil?"]
    Z -->|"yes"| AA["MCPTools(ctx, c.MCPManager())<br/>stream.go:271"]
    AA --> AB["AssembleAgentTools(reg, mcpTools)<br/>stream.go:272"]
    AB --> AC["wrapToolsWithContext(assembled, toolCtxMgr)<br/>stream.go:273"]
    Z -->|"no"| AD["opts: WithTools omitted"]
    AC --> AE[opts with fantasy.WithTools...]
    AD --> AE

    %% Sampling params
    X --> AF["applySamplingParams<br/>stream.go:95-124"]
    AF --> AG{"samplingParamsSupported(service)?"}
    AG -->|"yes"| AH["fantasy.WithTemperature, WithTopP, WithTopK<br/>stream.go:98-113"]
    AG -->|"no (anthropic/zai-anthropic)"| AI["log debug, skip params<br/>stream.go:101-115"]

    %% Anthropic effort
    AF --> AJ{"service == 'anthropic' &&<br/>effort != ''?"}
    AJ -->|"yes"| AK["anthropic.NewProviderOptions{Effort: ...}<br/>fantasy.WithProviderOptions<br/>stream.go:258-262"]
    AJ -->|"no"| AL[opts accumulate]

    AE --> AM["fantasy.NewAgent(model, opts...)<br/>stream.go:276"]

    %% Fantasy Stream call
    AM --> AN["fa.Stream(ctx, AgentStreamCall{...})<br/>stream.go:279-336"]

    AN --> AO["PrepareStep callback<br/>stream.go:283-300"]
    AO --> AP["safeSend: StreamEvent{Type:thinking}<br/>stream.go:284-286"]
    AP --> AQ["cachePromptDrain drain<br/>stream.go:290-296"]
    AQ --> AR["applyAnthropicCacheMarkers<br/>prepared, cacheService, cacheBoundaryIdx<br/>stream.go:298"]

    AN --> AS["OnTextDelta(text)<br/>stream.go:302-304"]
    AS --> AT["safeSend: StreamEvent{Type:text<br/>Text: text}"]

    AN --> AU["OnReasoningDelta(text)<br/>stream.go:306-308"]
    AU --> AV["safeSend: StreamEvent{Type:thinking<br/>Text: text}"]

    AN --> AW["OnToolCall(call)<br/>stream.go:310-317"]
    AW --> AX["safeSend: StreamEvent{Type:tool_call<br/>ToolName, ToolArgs, ToolCallID}"]

    AN --> AY["OnToolResult(res)<br/>stream.go:319-326"]
    AY --> AZ["safeSend: StreamEvent{Type:tool_result<br/>ToolCallID, ToolName, Text: toolResultText}"]

    AN --> BA["OnStreamFinish(usage, ...)<br/>stream.go:328-331"]
    BA --> BB["toLLMUsage(u)<br/>convert.go:150-157"]
    BB --> BC["safeSend: StreamEvent{Type:usage<br/>Usage: usage}"]

    AN --> BD["OnError(err)<br/>stream.go:333-335"]
    BD --> BE[streamErr = err]

    %% Post-stream processing
    AN --> BF{"streamErr != nil?"}
    BF -->|"yes"| BG["return streamErr<br/>stream.go:337-339"]
    BF -->|"no, err != nil"| BH["return err<br/>stream.go:340-342"]
    BF -->|"success"| BI["rebuilt := make([]Message, 0)<br/>stream.go:344"]
    BI --> BJ["for each step in result.Steps<br/>fantasyToLLM(fm) → Message<br/>stream.go:345-351"]
    BJ --> BK["safeSend: StreamEvent{Type:done<br/>Messages: rebuilt}<br/>stream.go:352"]
    BK --> BL["return nil<br/>stream.go:353"]

    %% Retry decision
    I --> BM{"streamOnce returns<br/>nil or error?"}
    BM -->|"nil"| BN["Exit StreamChat<br/>success"]
    BM -->|"error"| BO["isRetryable(err)<br/>stream.go:394-431"]
    BO -->|"retryable"| BP["lastErr = err<br/>continue retry loop<br/>stream.go:65-70"]
    BO -->|"not retryable"| BQ["safeSend: StreamEvent{Type:error}<br/>stream.go:68"]
    BQ --> BR["Exit StreamChat<br/>error"]
    BP --> B

    %% isRetryable detail
    BO -->|"context.Canceled"| BS["return false<br/>stream.go:398-400"]
    BO -->|"context.DeadlineExceeded<br/>io.EOF<br/>io.ErrUnexpectedEOF"| BT["return true<br/>stream.go:401-405"]
    BO -->|"*net.OpError<br/>*net.DNSError"| BU["return true<br/>stream.go:406-416"]
    BO -->|"HTTP 429/502/503/504"| BV["return true<br/>stream.go:419-421"]
    BO -->|"timeout/eof word boundary"| BW["return true<br/>stream.go:422-424"]
    BO -->|"rate limit phrase<br/>connection refused<br/>temporary failure"| BX["return true<br/>stream.go:425-429"]
```

## Summary

**StreamChat** (stream.go:41) is the single entry point. Wraps everything in an exponential backoff retry loop (1s→64s) for retryable errors (429, 5xx, network timeouts, "rate limit", "connection refused").

**streamOnce** (single attempt):
1. **ensureModel**: lazy-builds and caches fantasy.Provider via provider routing (Provider=="anthropic" → direct; Service != "" → service-based; host-based URL sniffing)
2. **splitForFantasy**: converts []Message (bitchtea JSONL) into fantasy's types; tail user → Prompt, prior → []fantasy.Message, system → concatenated
3. **Tool setup**: MCP tools merged, wrapped with per-call context, passed via `fantasy.WithTools`
4. **Sampling params**: forwarded only when service supports them (blocks anthropic/zai-anthropic)
5. **Anthropic effort**: attached when service="anthropic" and effort is set
6. **fantasy.NewAgent + fa.Stream**: issues the streaming call with callbacks for thinking/text/tool_calls/tool_results/usage/done

**Side effects:** per-turn ToolContextManager creation, PrepareStep prompt draining, Anthropic cache marker application

**External dependencies:** fantasy, fantasy/providers/*, catwalk, internal/tools, internal/mcp, internal/catalog
