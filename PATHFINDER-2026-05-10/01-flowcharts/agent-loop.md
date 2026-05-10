# Flowchart: Agent / LLM Loop

```mermaid
flowchart TD
    %% Entry points
    NewAgent([entry: NewAgent<br/>agent.go:129])
    SendMsg([entry: SendMessage<br/>agent.go:238])

    NewAgent --> NewAgentWithStreamer[["NewAgentWithStreamer<br/>agent.go:150"]]
    NewAgentWithStreamer --> BuildAgent[["build Agent struct<br/>agent.go:170"]]
    BuildAgent --> Bootstrap[["bootstrap messages<br/>system + context files + memory + persona<br/>agent.go:184-208"]]
    Bootstrap --> InitContext[["InitContext<br/>context_switch.go:30"]]
    InitContext --> ReturnAgent[["return *Agent"]]

    SendMsg --> sendMessage[["sendMessage<br/>agent.go:348"]]
    sendMessage --> TurnCheck[["turnActive.CompareAndSwap<br/>agent.go:352"]]
    TurnCheck -->|"reject concurrent"| RejectErr[["error event<br/>agent.go:353"]]
    TurnCheck -->|"ok"| AppendUser[["appendMessagesLocked<br/>newUserMessage<br/>agent.go:365"]]
    AppendUser --> StateThinking[["emit StateThinking event<br/>agent.go:373"]]

    StateThinking --> SetPromptDrain[["client.SetPromptDrain<br/>drainAndMirrorQueuedPrompts<br/>agent.go:379"]]
    SetPromptDrain --> Snapshot[["snapshotMessages<br/>agent.go:391"]]
    Snapshot --> StreamChat[["streamer.StreamChat<br/>stream.go:41"]]
    
    StreamChat --> FantasyGoroutine[["fantasy goroutine<br/>StreamChat runs async"]]

    StreamChat --> EventLoop[["event loop over streamEvents<br/>agent.go:401-515"]]
    EventLoop -->|receive StreamEvent| EvType{"ev.Type"}

    EvType -->|"text"| TextAccum[["textAccum.WriteString<br/>streamSanitizer.Consume<br/>emit text event<br/>agent.go:410-413"]]
    EvType -->|"thinking"| EmitThinking[["emit thinking event<br/>agent.go:416"]]
    EvType -->|"usage"| TrackUsage[["CostTracker.AddTokenUsage<br/>agent.go:419-422"]]
    EvType -->|"tool_call"| EmitToolStart[["emit tool_start event<br/>StateToolCall<br/>agent.go:426-432"]]
    
    EvType -->|"tool_result"| EmitToolResult[["emit tool_result event<br/>agent.go:435-440"]]
    
    EvType -->|"error"| HandleErr[["sanitizeOrphanedToolUses<br/>emit StateIdle + error + done<br/>agent.go:443-458"]]
    
    EvType -->|"done"| HandleDone[["HandleDone<br/>agent.go:460-495"]]
    HandleDone --> Flush[["streamSanitizer.Flush<br/>emit final text<br/>agent.go:461-463"]]
    Flush --> LiftMessages[["lift ev.Messages<br/>llm.Message → fantasy.Message<br/>agent.go:469-481"]]
    LiftMessages --> Sanitize[["sanitizeAssistantText<br/>agent.go:477"]]
    Sanitize --> AppendTurn[["appendMessagesLocked<br/>agent.go:488"]]
    AppendTurn --> CheckOrphans[["sanitizeOrphanedToolUses<br/>agent.go:494"]]
    CheckOrphans --> LoopEnd[["loop exits<br/>streamDone=true"]]

    LoopEnd --> Finalize[["emit StateIdle + done<br/>agent.go:522-525"]]

    EmitToolStart --> ToolDispatch["tool call dispatched<br/>by fantasy internally"]
    ToolDispatch --> RegistryExecute[["Registry.Execute<br/>internal/tools/*.go"]]
    RegistryExecute --> ToolResult["tool result returned to fantasy"]
    ToolResult --> EvType

    %% Context switching
    SetContext[["SetContext<br/>context_switch.go:13"]]
    SetContext --> SaveCur[["contextMsgs[currentContext] = messages<br/>context_switch.go:17"]]
    SaveCur --> LoadNext[["messages = contextMsgs[key]<br/>context_switch.go:19-20"]]
    LoadNext -->|key unknown| BootstrapNew[["bootstrap new slice<br/>context_switch.go:22-23"]]

    %% Compaction trigger
    CompactCmd[/"&compact command<br/>ui/commands.go:284"/]
    CompactCmd --> CompactCall[["agent.Compact<br/>agent.go:1025"]]
    CompactCall --> FlushMemory[["flushCompactedMessagesToDailyMemory<br/>agent.go:1042-1043"]]
    FlushMemory --> SummaryReq[["StreamChat summary request<br/>agent.go:1070"]]
    SummaryReq --> Rebuild[["rebuild: bootstrap + summary + keep<br/>agent.go:1089-1094"]]

    %% Side effects
    TextAccum -.-&gt;|"side effect"| CostEst[["CostTracker estimation<br/>agent.go:518-519"]]
    AppendTurn -.-&gt;|"side effect"| ContextSync[["contextMsgs[currentContext] updated<br/>agent.go:856-857"]]
    RegistryExecute -.-&gt;|"side effect"| FileIO["read/write/bash/exec<br/>tools package"]

    %% External dependencies
    subgraph External["External Dependencies"]
        Fantasy["© charm.land/fantasy<br/>(Agent + streaming)"]
        MCP["MCP Manager<br/>(optional)"]
        Tools["tools.Registry"]
        Cost["llm.CostTracker"]
        Memory["Memory (HOT.md / daily log)<br/>internal/memory"]
        LLMClient["llm.Client"]
    end

    NewAgentWithStreamer --> Fantasy
    StreamChat --> Fantasy
    RegistryExecute --> Tools
    EmitToolStart --> Fantasy
    ToolResult --> Fantasy
    TrackUsage --> Cost
    FlushMemory --> Memory
    NewAgentWithStreamer --> LLMClient

    style NewAgent fill:#bbf,stroke:#00f,stroke-width:2px
    style SendMsg fill:#bbf,stroke:#00f,stroke-width:2px
```

## Summary

**NewAgent** (agent.go:129): constructor flows through `NewAgentWithStreamer` → creates `llm.Client` + `tools.Registry` + bootstrap messages + per-context storage → returns `*Agent`.

**SendMessage** (agent.go:238) → `sendMessage` (agent.go:348):
1. Turn guard: `turnActive.CAS` rejects concurrent calls
2. Append user message under mutex
3. Emit `StateThinking`
4. Wire `SetPromptDrain` hook for mid-turn prompt draining
5. Snapshot messages → `streamer.StreamChat`
6. Event loop: text/ thinking/ usage/ tool_call/ tool_result/ done/ error
7. Tool calls dispatched via fantasy → `Registry.Execute` → results flow back
8. `HandleDone`: lift `ev.Messages` back to fantasy, append, sanitize orphans
9. Emit `StateIdle` + `done`

**Context switching** (`SetContext`): saves current slice to `contextMsgs`, loads target. New keys get bootstrap prefix.

**Compaction** (`/compact`): flush mid-section to daily memory → stream summary → rebuild as `[bootstrap] + [summary] + [last 4]`

**External dependencies:** fantasy (streaming + tool dispatch), tools.Registry, llm.CostTracker, internal/memory, llm.Client, optional MCP Manager
