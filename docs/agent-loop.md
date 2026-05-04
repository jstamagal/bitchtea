# Agent Loop, Event Messaging, and Turn Boundaries

This document is the source-of-truth map for the LLM agent flow in this
checkout. It is written from the current code, not from the older architecture
notes. Scope is intentionally narrow: the LLM agent loop, event messages,
tool dispatch, persistence at turn boundaries, cancellation, queueing, and the
commands/keys that affect those flows.

## Load-Bearing Package Boundary

The runtime path is:

```text
main.go / internal/ui
  -> internal/agent
    -> internal/llm
      -> charm.land/fantasy
        -> provider model stream
        -> fantasy tool dispatcher
          -> internal/llm tool wrapper
            -> internal/tools.Registry.Execute
```

The canonical in-memory transcript inside `Agent` is `[]fantasy.Message`.
`Client.StreamChat` still accepts `[]llm.Message`, so the agent converts at
the streamer boundary:

```text
agent.messages []fantasy.Message
  -> llm.FantasySliceToLLM
  -> Client.StreamChat([]llm.Message, Registry)
  -> fantasy.Agent.Stream
  -> llm.StreamEvent
  -> agent.Event
  -> UI/headless output
```

`internal/agent/event` owns the event struct to avoid a package cycle. The
`agent` package re-exports the types in `aliases.go` so older callers still
use `agent.Event` and `agent.State`.

## Event Types

`llm.StreamEvent` is the lower-level event emitted by `Client.StreamChat`.
`agent.Event` is the UI-facing event emitted by `Agent.SendMessage` and
`Agent.SendFollowUp`.

`llm.StreamEvent.Type` can be:

```text
text
thinking
tool_call
tool_result
usage
error
done
```

`agent.Event.Type` can be:

```text
text
thinking
tool_start
tool_result
state
error
done
```

State values are:

```text
StateIdle     = 0
StateThinking = 1
StateToolCall = 2
```

The agent maps lower-level events as follows:

```text
llm "text"        -> agent "text" unless a follow-up done token is being stripped
llm "thinking"    -> agent "thinking"
llm "usage"       -> cost tracker only, no UI event
llm "tool_call"   -> agent "state" StateToolCall, then agent "tool_start"
llm "tool_result" -> agent "tool_result"
llm "error"       -> agent "state" StateIdle, then "error", then "done", then return
llm "done"        -> splice rebuilt messages into agent.messages; final idle/done sent later
ctx.Done          -> agent "state" StateIdle, then "error", then "done", then return
```

Every public agent turn closes its `events` channel with `defer close(events)`.
A clean turn ends with these final events:

```text
state(StateIdle)
done
channel close
```

An errored or cancelled turn ends with:

```text
state(StateIdle)
error(<error>)
done
channel close
```

## What Starts A Turn

### TUI user input

In `internal/ui/model.go`, `Update(tea.KeyMsg{enter})` trims the textarea
value. Empty input does nothing. Slash commands go to `handleCommand`. Normal
chat input follows one of two paths:

If `m.streaming == false`, the UI appends a visible user message:

```text
 [HH:MM] <userNick> <input>
```

The exact rendered prefix is produced by `ChatMessage.Format`; styles may add
ANSI escape sequences, but the raw format string is:

```go
fmt.Sprintf(" %s %s %s", ts, nick, m.Content)
```

Then it calls `m.sendToAgent(input)`.

If `m.streaming == true`, the UI does not start a new agent turn. It appends to
`m.queued` and shows exactly:

```text
Queued message (agent is busy): <input>
```

Those queued messages are not sent to the lower-level `Agent.QueuePrompt`
machinery. They are only batched after the current turn reaches `agentDoneMsg`.

### Autonomous follow-up

The autonomous follow-up pipeline lets the agent continue working without
user input. It is controlled by two config flags, toggled at runtime:

- `/set auto-next on` — enables auto-next-steps re-prompting
- `/set auto-idea on` — enables auto-next-idea re-prompting
- `--auto-next-steps` / `--auto-next-idea` (headless CLI flags,
  `main.go:202-205`)

Both default to off.

#### State machine (`MaybeQueueFollowUp`, `internal/agent/agent.go:860`)

Failed or canceled turns never queue follow-up (`lastTurnState !=
turnStateCompleted`). When both config flags are off, the function always
returns nil.

The state machine cycles through follow-up kinds. The agent tracks
`lastCompletedFollowUp` — the kind of the last completed turn — to decide
what to queue next:

| Current `lastCompletedFollowUp` | Next action                                               |
|---------------------------------|-----------------------------------------------------------|
| None (initial user turn)        | Queue `auto-next-steps` if `AutoNextSteps` is on          |
|                                 | else queue `auto-next-idea` if `AutoNextIdea` is on       |
| `auto-next-steps`               | Check for `AUTONEXT_DONE` token. If found and             |
|                                 | `AutoNextIdea` is on → queue `auto-next-idea`.            |
|                                 | If not found → queue another `auto-next-steps`.           |
|                                 | If found and no `AutoNextIdea` → stop (nil).              |
| `auto-next-idea`                | Check for `AUTOIDEA_DONE` token. If found → stop (nil).   |
|                                 | If not found → back to `auto-next-steps`.                 |

The state machine is infinite: if the model never emits the done token, the
turn loops forever through auto-next-steps. There is no built-in max
iteration cap.

#### Magic tokens

Two sentinel strings signal the model considers a phase complete:

```go
autoNextDoneToken = "AUTONEXT_DONE"   // internal/agent/agent.go:94
autoIdeaDoneToken = "AUTOIDEA_DONE"   // internal/agent/agent.go:95
```

Detection (`assistantMarkedDone`, `internal/agent/agent.go:924`): trims the
assistant's raw output text, then checks
`strings.HasPrefix(strings.ToUpper(trimmed), token)`. This is
case-insensitive but the model is instructed to use the uppercase form.

#### The prompts

When auto-next-steps is queued, the agent sends (`AutoNextPrompt`,
`internal/agent/agent.go:844`):

```text
What are the next steps? If there is remaining work, do it now instead of
just describing it, including tests or verification when they matter. If
everything is done, start your response with AUTONEXT_DONE and briefly say
why.
```

When auto-next-idea is queued, the agent sends (`AutoIdeaPrompt`,
`internal/agent/agent.go:851`):

```text
Based on what you've done so far, pick the next highest-impact improvement
and implement it now. If there is nothing worthwhile left to improve, start
your response with AUTOIDEA_DONE and briefly say why.
```

#### Token stripping in streaming text

When a follow-up turn streams text chunks, the agent wraps the text handler
in a `followUpStreamSanitizer` (`internal/agent/agent.go:948`) that strips
the done token from the visible output.

The sanitizer has three states: `Undecided` (buffers until it sees whether
the chunk starts with a token), `Strip` (skips trailing whitespace/punctuation
after a token), and `Pass` (forwards chunks verbatim). It strips `AUTONEXT_DONE`
and `AUTOIDEA_DONE` when they appear as a leading token, along with any
trailing whitespace, `:`, or `-`.

After the stream ends, `sanitizeAssistantText` (`internal/agent/agent.go:932`)
runs a post-hoc cleanup on the final assistant content. This handles tokens
that arrived in a single chunk and were not split across streaming boundaries.
If stripping the token leaves an empty string, the content is replaced
with `"Done."`.

#### TUI path

After `agentDoneMsg`, the UI calls `m.agent.MaybeQueueFollowUp()` (called
inside `handleAgentDone` at `internal/ui/model.go:731`). If a request is
returned, the UI shows exactly:

```text
*** <label>: continuing...
```

The label is one of:

```text
auto-next-steps
auto-next-idea
```

Then it calls `m.sendFollowUpToAgent(req)` which starts a new sendMessage
goroutine with `sendMessage(ctx, req.Prompt, req.Kind, events)`.

#### Headless path

`runHeadlessLoop` (`main.go:257`) loops explicitly. After each turn it calls
`ag.MaybeQueueFollowUp()` (`main.go:303`). If the request is non-nil, it
writes to stderr:

```text
[auto] <label>
```

then calls `ag.SendFollowUp(ctx, followUp, events)` to start the next turn.
If stdout text did not end with `\n`, the headless loop prints a newline
before starting the follow-up turn (`main.go:307-312`). When all follow-ups
are done, the loop breaks and a final trailing newline is written if needed.

## `startAgentTurn`: The TUI Turn Boundary

All TUI agent turns pass through `Model.startAgentTurn`.

It first cancels any previous context if `m.cancel != nil`. Then it resets
queue/cancel ladder state:

```text
queueClearArmed = false
escStage = 0
escLast = zero time
ctrlCStage = 0
ctrlCLast = zero time
activeToolName = ""
activeToolCallID = ""
```

Then it freezes the routing context for persistence:

```go
m.turnContext = m.focus.Active()
ctxKey := ircContextToKey(m.turnContext)
```

It initializes and switches the agent history for that context:

```go
m.agent.InitContext(ctxKey)
m.agent.SetContext(ctxKey)
m.lastSavedMsgIdx = m.agent.SavedIdx(ctxKey)
m.agent.SetScope(ircContextToMemoryScope(m.turnContext))
```

Then it creates a fresh turn context and event channel:

```go
ctx, cancel := context.WithCancel(context.Background())
m.cancel = cancel
m.streaming = true
ch := make(chan agent.Event, 100)
m.eventCh = ch
```

Finally it starts `Agent.SendMessage` or `Agent.SendFollowUp` in a goroutine
and returns a Bubble Tea command that waits for the first event.

Strict boundary: the UI treats `m.eventCh` as the identity of the active turn.
Any `agentEventMsg` whose `ch != m.eventCh` is ignored. Any `agentDoneMsg`
whose non-nil `ch != m.eventCh` is ignored.

## `Agent.SendMessage` And `sendMessage`

`SendMessage(ctx, userMsg, events)` is a wrapper:

```go
a.sendMessage(ctx, userMsg, followUpKindNone, events)
```

`SendFollowUp(ctx, req, events)` closes the events channel immediately if
`req == nil`; otherwise it calls:

```go
a.sendMessage(ctx, req.Prompt, req.Kind, events)
```

`sendMessage` is the real turn implementation.

### Input expansion

The input first passes through `ExpandFileRefs(userMsg, a.config.WorkDir)`.
This splits on `strings.Fields`, so whitespace is normalized in the final
expanded prompt. Each word beginning with `@` is treated as a path.

If `@file` can be read, the replacement is exactly:

````text
@file (file contents below):
```
<file contents>
```
````

The file content is truncated at 30 KiB with:

```text
... (truncated at 30KB)
```

If the file cannot be read, the replacement is exactly:

```text
@file (file not found: <error>)
```

After expansion, `injectPerMessagePrefix` prepends `PerMessagePrefix` only if
that global string is non-empty. It is empty in this checkout, so user input is
unchanged after file expansion.

The agent appends one user message to `a.messages`:

```go
newUserMessage(expanded)
```

Then it increments:

```text
a.TurnCount++
```

### First event

Before the provider stream starts, the agent sends:

```go
Event{Type: "state", State: StateThinking}
```

The TUI displays this as a `MsgThink` placeholder whose content is exactly:

```text
thinking...
```

Visible raw format before styling is:

```text
 [HH:MM] 💭 thinking...
```

Important current behavior: `Client.streamOnce` sends a lower-level
`StreamEvent{Type:"thinking"}` with empty text in every `PrepareStep`. The UI
maps that to a `thinking` event and, if the current think placeholder still
says `thinking...`, replaces it with `ev.Text`. Because `ev.Text` is empty,
the first PrepareStep can erase the placeholder before any reasoning text
arrives. This is wired behavior, but it is not a polished visible state.

### Stream startup

The agent installs the mid-turn drain hook:

```go
a.client.SetPromptDrain(a.drainAndMirrorQueuedPrompts)
defer a.client.SetPromptDrain(nil)
```

Then it creates:

```go
streamEvents := make(chan llm.StreamEvent, 100)
```

and starts:

```go
go a.streamer.StreamChat(ctx, llm.FantasySliceToLLM(a.messages), a.tools, streamEvents)
```

The `for` loop does not range directly over `streamEvents`. It selects on both
`streamEvents` and `ctx.Done()` so cancellation can end the turn even if the
streamer goroutine is stuck and never closes the stream channel.

On `ctx.Done()`, `sendMessage` starts a background drainer:

```go
go func() { for range streamEvents {} }()
```

That prevents the streamer from blocking forever if it later tries to send or
close.

## `Client.StreamChat`

`Client.StreamChat(ctx, msgs, reg, events)` owns provider retries. It closes
`events` with `defer close(events)`.

It tries `streamOnce` once, then retries retryable errors after these delays:

```text
1s
2s
4s
8s
16s
32s
64s
```

Before each retry, it sends this as a normal text event:

```text


🦍 *retrying after server error… waiting <duration>* 🦍
```

The duration is Go's `time.Duration.String()` output, for example `1s`.

Retryable error detection is substring-based on the lowercased error text. It
matches phrases including `rate limit`, `429`, `503`, `server overloaded`,
`connection refused`, `deadline exceeded`, `timeout`, `no such host`,
`dial tcp`, and `temporary failure`.

If a retry delay is interrupted by context cancellation, `StreamChat` sends:

```go
StreamEvent{Type: "error", Error: ctx.Err()}
```

If an error is not retryable, it sends:

```go
StreamEvent{Type: "error", Error: err}
```

If all retries are exhausted, it sends the last error the same way.

## `streamOnce`: What Goes To Fantasy And The Provider

`streamOnce` first calls `ensureModel(ctx)`. That lazily builds and caches a
`fantasy.Provider` and `fantasy.LanguageModel` from:

```text
Client.Provider
Client.Service
Client.APIKey
Client.BaseURL
Client.Model
Client.DebugHook
```

The model reference returned by `ensureModel` is stable for the current turn,
even if slash commands later mutate client config and invalidate the cache for
future turns.

### Transcript split

The incoming `[]llm.Message` is split by `splitForFantasy` into:

```text
prompt       string
prior        []fantasy.Message
systemPrompt string
```

Rules:

1. The tail user message becomes `prompt`.
2. That tail user message is omitted from `prior`.
3. All system messages are removed from `prior` and concatenated into
   `systemPrompt` with `\n\n`.
4. User messages become `fantasy.MessageRoleUser` with one `TextPart`.
5. Assistant text becomes a `TextPart`; assistant tool calls become
   `ToolCallPart`s after the text.
6. Tool messages become one `ToolResultPart` with text output.
7. If the transcript does not end in a user message, `prompt` is empty and all
   non-system messages stay in `prior`.

### Agent options

`streamOnce` builds:

```go
opts := []fantasy.AgentOption{
    fantasy.WithStopConditions(fantasy.StepCountIs(64)),
}
```

If `systemPrompt != ""`, it adds:

```go
fantasy.WithSystemPrompt(systemPrompt)
```

If `reg != nil`, it assembles tools and adds:

```go
fantasy.WithTools(wrapToolsWithContext(assembled, toolCtxMgr)...)
```

Then it constructs:

```go
fa := fantasy.NewAgent(model, opts...)
```

### Local and MCP tools

`AssembleAgentTools(reg, mcpTools)` returns local tools first, then MCP tools.
Local names win if any duplicate appears.

Local tools are translated with `translateTools(reg)`. These names have typed
fantasy wrappers:

```text
read
write
edit
bash
search_memory
write_memory
```

These still use the generic `bitchteaTool` adapter:

```text
terminal_start
terminal_send
terminal_keys
terminal_snapshot
terminal_wait
terminal_resize
terminal_close
preview_image
```

The typed wrappers still re-marshal their typed input back to JSON and call
`Registry.Execute(ctx, name, argsJSON)`. The important difference is schema
generation and typed deserialization belong to `fantasy.NewAgentTool` instead
of the generic `bitchteaTool`.

Every local tool failure is converted into a fantasy text error response whose
content begins with:

```text
Error: <underlying error>
```

The wrapper returns `nil` as its Go error so fantasy keeps the agent loop alive.
The generic `bitchteaTool` follows the same rule.

MCP tools are named:

```text
mcp__<server>__<tool>
```

MCP tool failures are also returned as text error responses, not Go errors.

## Per-Tool Cancellation

Per-tool cancellation is the operational version of the phase-8 cancellation
state-machine design. The current implementation is centered on
`internal/llm/tool_context.go`:

- `NewToolContextManager(turnCtx)` creates a manager rooted at the turn
  context.
- `NewToolContext(toolCallID)` derives a child context for one tool call and
  stores its cancel function under the fantasy tool call ID.
- `CancelTool(toolCallID)` cancels one stored child context without cancelling
  the parent turn context.
- `CancelAll()` cancels every stored tool child context and clears the map.

### Manager lifetime in `streamOnce`

Every `Client.streamOnce` creates exactly one `ToolContextManager` for that
stream attempt:

```go
toolCtxMgr := NewToolContextManager(ctx)
c.toolCtx = toolCtxMgr
defer func() {
    toolCtxMgr.CancelAll()
    c.toolCtx = nil
}()
```

That deferred cleanup is the turn-end cleanup path. It runs on successful
completion, provider/tool errors that make `streamOnce` return, Esc turn
cancellation, Ctrl+C turn cancellation, signal cancellation, and any other
path that unwinds `streamOnce`. The UI does not call `CancelAll` directly; it
cancels the turn context, then `streamOnce` runs the defer and cancels any
tool contexts still present.

### Child contexts per tool call

When tools are assembled, `streamOnce` wraps them with:

```go
fantasy.WithTools(wrapToolsWithContext(assembled, toolCtxMgr)...)
```

`wrapToolsWithContext` returns `toolContextWrapper` instances. For each tool
invocation, `toolContextWrapper.Run` calls:

```go
toolCtx, cleanup := w.mgr.NewToolContext(call.ID)
defer cleanup()
return w.inner.Run(toolCtx, call)
```

The key is the fantasy `ToolCall.ID`, which also flows through UI events as
`ToolCallID`. `cleanup` cancels the child context and removes the map entry
when the tool returns, so completed tools are no longer cancellable.

If the parent turn context is already cancelled when `NewToolContext` runs, it
returns that already-done turn context and a no-op cleanup instead of storing
a child cancel function.

### Esc x1: cancel one active tool

The UI tracks the active tool from `tool_start` events:

```text
activeToolName = ev.ToolName
activeToolCallID = ev.ToolCallID
```

When the first meaningful Esc press arrives during streaming and
`activeToolName != ""`, `handleEscKey` calls:

```go
m.agent.CancelTool(m.activeToolCallID)
```

`Agent.CancelTool(toolCallID)` fetches the current manager with
`a.client.ToolContextManager()`. If there is no active stream manager, it
returns:

```text
no active turn
```

Otherwise it calls `ToolContextManager.CancelTool(toolCallID)`. If the tool
already finished and `cleanup` removed the entry, the exact error text is:

```text
no active tool with id <toolCallID>
```

The UI renders success and failure as system messages:

```text
Cancelled <activeToolName>.
Could not cancel <activeToolName>: <error>
```

On this path `handleEscKey` resets `escStage` to 0 and returns. It does not
call `cancelActiveTurn`, does not cancel `m.cancel`, and does not clear queued
messages. The parent turn context remains alive so fantasy can receive the
tool response and continue the model/tool loop.

### Tool result shape after a child-context cancel

Tool cancellation reaches the actual tool as `ctx.Done()` on the child context
passed to `Run`. The current tool wrappers keep cancellation model-visible by
returning fantasy tool responses rather than Go errors that abort the whole
stream:

- typed wrappers check `ctx.Err()` up front and return
  `fantasy.NewTextErrorResponse(fmt.Sprintf("Error: %v", err)), nil`.
- typed wrappers also wrap `Registry.Execute` errors with the same
  `"Error: %v"` text-error response shape.
- the generic `bitchteaTool` adapter wraps `Registry.Execute` errors with
  `fantasy.NewTextErrorResponse(fmt.Sprintf("Error: %v", err)), nil`.

This is different from the older phase-8 design note, which proposed a normal
text result of `"user cancelled this tool call"` for Esc x1. The current code
does not special-case that string; the invariant that matters operationally is
that the wrapper returns a `fantasy.ToolResponse` with `nil` Go error, so the
fantasy stream can stay alive.

`OnToolResult` then emits a lower-level `tool_result` event with the original
tool call ID. The agent maps that to `agent.Event{Type:"tool_result"}` and the
TUI clears `activeToolName` / `activeToolCallID` when the result matches the
active tool.

### Esc x2, Ctrl+C, and turn cancellation

Whole-turn cancellation is separate from per-tool cancellation:

- Esc x2 calls `cancelActiveTurnWithQueueArm("Interrupted by Esc.")`.
- Ctrl+C stage 1 while streaming calls `cancelActiveTurn(...)`.
- Ctrl+C stage 3 while streaming calls `cancelActiveTurn("Interrupted.", true)`.
- OS `SignalMsg` while streaming calls
  `cancelActiveTurn("Interrupted by signal.", true)`.
- `tea.QuitMsg` while streaming calls `m.cancel()` directly during shutdown.

`cancelActiveTurn` invokes the turn-level `m.cancel()` cancel function, marks
the UI idle, clears `activeToolName` and `activeToolCallID`, resets Esc state,
and optionally clears the steering queue. Because every per-tool child context
inherits from the turn context, cancelling the turn context also cancels all
active tool child contexts. When `streamOnce` unwinds, its deferred
`toolCtxMgr.CancelAll()` runs as a final cleanup and clears any still-registered
tool contexts.

Ctrl+C is intentionally blunter than Esc: it never calls `Agent.CancelTool` and
therefore never attempts tool-only cancellation.

### PrepareStep

Every fantasy step runs this `PrepareStep`:

1. Send `StreamEvent{Type:"thinking"}` to the agent.
2. Copy `stepOpts.Messages` into `msgs`.
3. If `c.promptDrain != nil`, drain queued prompts and append each drained
   string as `fantasy.NewUserMessage(text)` to `msgs`.
4. Apply Anthropic cache markers if enabled.
5. Return `fantasy.PrepareStepResult{Messages: msgs}`.

The cache marker only fires when:

```text
Client.Service == "anthropic"
```

It stamps `cache_control: {type:"ephemeral"}` on the last bootstrap message
inside the prepared message slice. The index is `BootstrapMsgCount - 1` as long
as `BootstrapMsgCount` is positive and not larger than the message slice.

Current gap: the agent-level prompt queue is not wired to TUI typing. `Agent`
has `QueuePrompt`, `ClearQueue`, and `drainAndMirrorQueuedPrompts`; the TUI has
its own `m.queued` path. Text typed while the agent works is therefore not
drained by `PrepareStep` today.

If some caller does use `Agent.QueuePrompt`, the model sees the raw text
drained in `PrepareStep`, but `a.messages` stores a different synthetic text:

```text
[queued prompt 1]: <text>
[queued prompt 2]: <text>
```

That means prepared provider context and persisted agent history do not match
exactly for this not-currently-wired path.

### Fantasy callbacks

The fantasy stream is called as:

```go
fa.Stream(ctx, fantasy.AgentStreamCall{
    Prompt: prompt,
    Messages: prior,
    PrepareStep: ...,
    OnTextDelta: ...,
    OnReasoningDelta: ...,
    OnToolCall: ...,
    OnToolResult: ...,
    OnStreamFinish: ...,
    OnError: ...,
})
```

Callback mapping:

```text
OnTextDelta(_, text)
  -> StreamEvent{Type:"text", Text:text}

OnReasoningDelta(_, text)
  -> StreamEvent{Type:"thinking", Text:text}

OnToolCall(call)
  -> StreamEvent{
       Type:"tool_call",
       ToolName:call.ToolName,
       ToolArgs:call.Input,
       ToolCallID:call.ToolCallID,
     }

OnToolResult(res)
  -> StreamEvent{
       Type:"tool_result",
       ToolCallID:res.ToolCallID,
       ToolName:res.ToolName,
       Text:toolResultText(res.Result),
     }

OnStreamFinish(usage, finishReason, providerMetadata)
  -> StreamEvent{Type:"usage", Usage:&TokenUsage{...}}

OnError(err)
  -> sets streamErr = err
```

`toolResultText` only extracts:

```text
fantasy.ToolResultOutputContentText
*fantasy.ToolResultOutputContentText
```

It returns `""` for media outputs, error outputs, typed nil text pointers, and
nil interfaces. This is a real limitation: a provider-visible tool error can
reach fantasy as an error response while the UI receives a `tool_result` event
with empty `Text` if fantasy reports the result as `ToolResultOutputContentError`
instead of text.

When fantasy returns successfully, `streamOnce` rebuilds a legacy transcript:

```go
for _, step := range result.Steps {
    for _, fm := range step.Messages {
        rebuilt = append(rebuilt, fantasyToLLM(fm))
    }
}
```

Then it sends:

```go
StreamEvent{Type:"done", Messages:rebuilt}
```

## Agent Handling Of Stream Events

### Text

For every lower-level `"text"` event, the agent appends to `textAccum`.

For normal user turns, it forwards the chunk exactly:

```go
Event{Type:"text", Text:ev.Text}
```

For autonomous follow-up turns, it runs `followUpStreamSanitizer` first. The
sanitizer hides leading done tokens:

```text
AUTONEXT_DONE
AUTOIDEA_DONE
```

If a token is stripped, leading spaces, tabs, CR/LF, colons, and hyphens after
the token are also stripped. Example streamed chunks:

```text
AUTONEXT_DONE
: all tests passed.
```

Visible text becomes exactly:

```text
all tests passed.
```

### Thinking

Lower-level `"thinking"` maps directly to:

```go
Event{Type:"thinking", Text:ev.Text}
```

In the TUI, if the latest message is `MsgThink` and its content is
`thinking...`, the content is replaced with `ev.Text`; otherwise `ev.Text` is
appended. If the latest message is not `MsgThink`, a new thinking message is
added with `Content: ev.Text`.

Headless mode ignores `thinking` events completely.

### Usage

If `ev.Usage != nil`, the agent calls:

```go
a.CostTracker.AddTokenUsage(*ev.Usage)
gotUsage = true
```

No visible event is emitted.

If no usage event arrived by the end of the turn, the agent estimates:

```text
input tokens  = a.EstimateTokens() before streaming
output tokens = len(textAccum.String()) / 4
```

### Tool call

For lower-level `"tool_call"`, the agent increments:

```go
a.ToolCalls[ev.ToolName]++
```

Then it emits:

```go
Event{Type:"state", State:StateToolCall}
Event{
    Type:"tool_start",
    ToolName:ev.ToolName,
    ToolCallID:ev.ToolCallID,
    ToolArgs:ev.ToolArgs,
}
```

The TUI sets:

```text
activeToolName = ev.ToolName
activeToolCallID = ev.ToolCallID
```

and shows exactly:

```text
calling <toolName>...
```

The visible raw format before styling is:

```text
 [HH:MM]   → <toolName>: calling <toolName>...
```

Headless mode writes to stderr:

```text
[tool] <toolName> args=<args>
```

Before headless logging, newlines in args are replaced with `\n`; if the
resulting string is longer than 200 bytes, it is truncated to the first 200
bytes plus:

```text
...
```

### Tool result

For lower-level `"tool_result"`, the agent emits:

```go
Event{
    Type:"tool_result",
    ToolName:ev.ToolName,
    ToolCallID:ev.ToolCallID,
    ToolResult:ev.Text,
}
```

The current agent mapping never sets `ToolError`, so the TUI normally treats
tool results as `MsgTool`, not `MsgError`.

If `activeToolName == ev.ToolName`, the TUI clears:

```text
activeToolName = ""
activeToolCallID = ""
```

Display truncation is line-based. If the result has more than 20 lines, the
visible content is:

```text
<first 20 lines>
... (<N> more lines)
```

where `<N>` is `len(lines)-20`.

Headless mode writes one of:

```text
[tool] <toolName> result=<result>
```

or, if `ToolError != nil`:

```text
[tool] <toolName> error=<error> result=<result>
```

The same newline escaping and 200-byte truncation used for args is applied to
headless result text.

### Error

For lower-level `"error"`:

```text
if errors.Is(ev.Error, context.Canceled):
    lastTurnState = turnStateCanceled
else:
    lastTurnState = turnStateErrored
```

Then the agent emits idle, error, and done, and returns immediately.

The TUI displays:

```text
Error: <error>
```

If `llm.ErrorHint(ev.Error)` returns a hint, it appends:

```text
  hint: <hint>
```

with a newline before the hint.

Headless mode returns the error to `main`, which prints to stderr:

```text
bitchtea: <error>
```

### Done

On lower-level `"done"`, the agent first flushes any buffered follow-up text
from the sanitizer.

Then it splices `ev.Messages` into `a.messages`. For each rebuilt legacy
message:

1. If `m.Role == "assistant"`, sanitize leading auto done tokens from
   `m.Content`.
2. Convert with `llm.LLMToFantasy(m)`.
3. Append to `a.messages`.

If no assistant message was included in `ev.Messages` but `textAccum` is
non-empty, the agent synthesizes and appends:

```go
newAssistantMessage(sanitizeAssistantText(textAccum.String()))
```

After the stream channel closes, the agent records:

```text
lastAssistantRaw = strings.TrimSpace(textAccum.String())
lastTurnState = turnStateCompleted
lastCompletedFollowUp = kind
```

Then it emits:

```go
Event{Type:"state", State:StateIdle}
Event{Type:"done"}
```

## TUI Handling Of Agent Events

The TUI waits for one event at a time with `waitForAgentEvent(ch)`. After
handling each `agentEventMsg`, it schedules another wait on the same channel.
If the channel closes, `waitForAgentEvent` returns `agentDoneMsg{ch: ch}`.

The bottom status bar reflects `m.agentState` on every render. The left side
content before styling is one of:

```text
 [<agentNick>] idle 
 [<agentNick>] <spinner> thinking... 
 [<agentNick>] <spinner> running tools... 
```

The spinner glyph is produced by Bubble Tea's dot spinner and changes over
time. The right side ends with:

```text
~<token estimate> tok | <elapsed>
```

If tool stats, MP3 status, or background activity are present, they are
prepended to the right side before the token/elapsed text and separated with
` | `.

### Text display

If the latest visible message is a thinking placeholder, the first text event
replaces that message with an empty `MsgAgent`:

```text
Type: MsgAgent
Nick: m.config.AgentNick
Content: ""
Context: m.focus.Active()
```

Then the text chunk is appended to `m.streamBuffer`, and the latest `MsgAgent`
content is replaced with `m.streamBuffer.String()`.

The transcript logger receives each text chunk through:

```go
m.transcript.AppendAgentChunk(last.Time, last.Nick, ev.Text)
```

### Done boundary work

`agent.Event{Type:"done"}` does not directly finalize the UI. It schedules
`agentDoneMsg` for the active channel.

`agentDoneMsg` does all end-of-turn cleanup:

```text
FinishAgentMessage()
streaming = false
agentState = StateIdle
eventCh = nil
cancel = nil
activeToolName = ""
activeToolCallID = ""
escStage = 0
escLast = zero time
ctrlCStage = 0
ctrlCLast = zero time
syncLastAssistantMessage()
```

If notification sound is enabled, it calls:

```go
sound.Play(m.config.SoundType)
```

The tool panel token and elapsed fields are refreshed from:

```go
m.agent.EstimateTokens()
m.agent.Elapsed()
```

Then session entries are appended for the frozen `m.turnContext`, not
necessarily the focus currently visible in the UI:

```go
ctxKey := ircContextToKey(m.turnContext)
msgs := m.agent.Messages()
savedIdx := m.agent.SavedIdx(ctxKey)
ctxLabel := m.turnContext.Label()
for i := savedIdx; i < len(msgs); i++ {
    e := session.EntryFromFantasyWithBootstrap(msgs[i], i < m.agent.BootstrapMessageCount())
    e.Context = ctxLabel
    m.session.Append(e)
}
m.agent.SetSavedIdx(ctxKey, len(msgs))
```

Focus is saved with:

```go
m.focus.Save(m.config.SessionDir)
```

The checkpoint file is written to:

```text
<SessionDir>/.bitchtea_checkpoint.json
```

with JSON fields:

```json
{
  "turn_count": <number>,
  "tool_calls": {"<toolName>": <count>},
  "model": "<model>",
  "timestamp": "<time>"
}
```

After persistence, queued user messages are processed, then autonomous
follow-up is considered.

## Queued TUI Messages

Typing Enter while `m.streaming == true` appends:

```go
queuedMsg{text: input, queuedAt: time.Now()}
```

and shows:

```text
Queued message (agent is busy): <input>
```

Pressing Up while the input is empty and queued messages exist pops the last
queued item into the textarea and shows:

```text
Unqueued message: <input>
```

At `agentDoneMsg`, if the oldest queued message is older than:

```text
2m0s
```

the UI clears the whole queue and shows exactly:

```text
Discarded <N> queued message(s) older than 2m0s — context changed. Re-send if still relevant.
```

If queued messages are fresh, they are batched into one new user turn. The
combined prompt is:

```text
[queued msg 1]: <first>
[queued msg 2]: <second>
```

with a single newline between entries. The UI displays that combined text as a
user message and immediately starts a new turn with it.

## Cancellation And Escape Ladders

### Ctrl+C in the TUI

The Ctrl+C ladder resets if more than 3 seconds pass between presses.

First Ctrl+C while streaming cancels the active turn. If no queued messages
exist, it shows:

```text
Interrupted. Press Ctrl+C again to clear queued messages; press it a third time to quit.
```

If queued messages exist, it shows:

```text
Interrupted. <N> queued message(s) remain. Press Ctrl+C again to clear them; press it a third time to quit.
```

First Ctrl+C while not streaming shows one of:

```text
<N> queued message(s) remain. Press Ctrl+C again to clear them; press it a third time to quit.
```

or:

```text
Press Ctrl+C twice more to quit.
```

Second Ctrl+C clears queued messages if any and shows:

```text
Cleared <N> queued message(s). Press Ctrl+C again to quit.
```

If there is no queue, it shows:

```text
Press Ctrl+C again to quit.
```

Third Ctrl+C quits. If streaming, it first calls:

```go
m.cancelActiveTurn("Interrupted.", true)
```

### Esc in the TUI

The Esc ladder resets if more than 1500 milliseconds pass between presses.

Esc first closes side panels before touching the turn:

```text
Tool panel closed.
MP3 panel closed.
```

If `queueClearArmed == true` and queued messages exist, Esc clears them and
shows:

```text
Cleared <N> queued message(s).
```

If not streaming, Esc resets ladder state and shows nothing.

If streaming and an active tool is known, the first meaningful Esc cancels that
tool only by calling:

```go
m.agent.CancelTool(m.activeToolCallID)
```

On success it shows:

```text
Cancelled <activeToolName>.
```

On failure it shows:

```text
Could not cancel <activeToolName>: <error>
```

The turn remains alive after a tool-only cancel.

If streaming and no active tool is known, first Esc shows:

```text
Press Esc again to cancel the turn.
```

Second Esc cancels the whole turn and shows:

```text
Interrupted by Esc.
```

If queued messages exist, this arms queue clearing for the next Esc.

### OS signals and quit

`SignalMsg` while streaming calls:

```go
m.cancelActiveTurn("Interrupted by signal.", true)
```

If not streaming, it quits.

`tea.QuitMsg` while streaming calls the turn cancel function, finishes the
transcript message, sets `streaming=false`, and clears queued messages. It then
closes MP3/transcript resources and quits.

## Slash Commands Affecting The Agent Loop

Only commands that alter the agent loop, turn state, transcript, or provider
boundary are listed here.

### `/restart`

If streaming, it calls `cancelActiveTurn("Restart", true)`, but then
immediately clears `m.messages`. The durable visible result of `/restart` is
therefore only:

```text
*** Conversation restarted. Fresh context.
```

It also calls `m.agent.Reset()`, resets `streamBuffer`, clears queued messages,
creates a new session file if possible, and resets save watermarks.

`Agent.Reset` rebuilds bootstrap messages from the current config, context
files, root memory, and persona anchor. It resets turn count, tool call stats,
cost tracker, start time, follow-up state, injected scoped-memory paths, and
per-context histories back to `#main`.

### `/compact`

If streaming, it shows:

```text
Can't compact while agent is working. Be patient.
```

Otherwise it calls `Agent.Compact(context.Background())`.

On error it shows:

```text
Compaction failed: <error>
```

On success it shows:

```text
Compacted: ~<before> -> ~<after> tokens
```

#### Internal flow (`Agent.Compact`, `internal/agent/agent.go:734`)

`Agent.Compact` is a tool-less LLM flow. It blocks inside the slash command
handler and reads lower-level stream events directly. The method does NOT emit
normal UI streaming events.

**Early-return thresholds.** If `len(a.messages) < 6`, compaction is a no-op —
there are too few messages to compact. If `a.bootstrapMsgCount >= end` (where
`end = len(a.messages) - 4`), compaction also returns nil — the compactable
window is empty.

**Compaction window.** The window to be compacted is the slice
`a.messages[bootstrapEnd:end]` where `end = len(a.messages) - 4` and
`bootstrapEnd = max(a.bootstrapMsgCount, 1)`. In practice this means:

- The bootstrap prefix (system prompt, context files, persona anchor) is
  preserved untouched.
- The last 4 messages (typically the last two user/assistant exchanges) are
  preserved as the conversational "landing zone" after the summary.
- Everything between bootstrap and the last 4 is candidate for compaction.

**Step 1: memory extraction** (`flushCompactedMessagesToDailyMemory`,
`internal/agent/agent.go:800`). The compaction window is sent to the LLM
with this prompt verbatim:

```text
Extract durable memory from this conversation slice before it is compacted.
Return concise markdown bullets covering only lasting facts: user preferences, decisions, completed work, relevant files, and open follow-ups.
Skip transient chatter and tool noise. If nothing deserves durable memory, reply with exactly NONE.
```

If the LLM returns empty or exactly `NONE` (case-insensitive), no daily memory
is written. Otherwise the output is appended to the scoped daily memory file
via `AppendScopedDailyMemory`.

**Step 2: summary.** The compaction window is sent to the LLM again with this
prompt verbatim:

```text
Summarize the following conversation concisely, preserving all important technical details, decisions made, files modified, and current state:
```

Each message text is truncated to 500 characters in the prompt. The summary
stream runs without tools (`reg = nil`).

**Step 3: rebuilt transcript.** The compacted `a.messages` is assembled as:

```text
messages[:bootstrapEnd]          (bootstrap prefix preserved as-is)
user: [Previous conversation summary]:
<summary from step 2>
messages[end:]                   (last 4 original messages, the "landing zone")
```

There is no synthetic `"Got it"` assistant message inserted after the summary.
The last 4 original messages already include the existing assistant response
that followed the final compacted user message.

**bootstrapMsgCount is NOT reset** after compaction. The value remains what it
was before, which continues to point at the bootstrap prefix boundary of the
compacted array. Since the bootstrap prefix is copied verbatim into the rebuilt
slice, the old count is still correct.

**contextSavedIdx IS reset** to `len(a.messages)`, which means all messages in
the compacted array are marked as saved. This creates a session-save watermark
gap: the pre-compaction messages were already written to the session JSONL file,
but the compacted structure (summary message + last-4 re-insertion) is never
re-saved to the session log. New messages added after compaction will be
persisted normally from the new watermark.

### `/tokens`

Shows exactly:

```text
~<tokens> tokens | $<cost with 4 decimals> | <messages> messages | <turns> turns
```

Token estimate is `len(messageText(m)) / 4` summed across the active context
history. Cost comes from `CostTracker.EstimateCostFor(model, service)`.

### `/debug on|off`

`/debug on` installs a debug hook and shows:

```text
Debug mode: ON
```

For each upstream HTTP request, the hook adds a system message:

```text
[DEBUG] <METHOD> <URL>
Request Headers: <headers>
Request Body: <body>
Response Status: <statusCode>
```

`/debug off` clears the hook and shows:

```text
Debug mode: OFF
```

Invalid usage shows:

```text
Usage: /debug on|off
```

#### Hook installation path

`handleDebugCommand` (`internal/ui/commands.go:306`) calls `m.agent.SetDebugHook(...)` with a closure that formats a `DebugInfo` struct as a system message visible in the chat viewport.

`Agent.SetDebugHook` (`internal/agent/agent.go:696`) delegates to `Client.SetDebugHook(hook)` (`internal/llm/client.go:181`). That call installs the hook on the client struct AND invalidates the cached provider/model (`c.invalidateLocked()`), so the next agent turn rebuilds the HTTP transport with the debug wrapper. A `nil → nil` call is a no-op that preserves the cache.

When `ensureModel` rebuilds the provider on the next turn, `toProviderConfigLocked` (`internal/llm/client.go:225`) checks `c.DebugHook` and, if non-nil, constructs:

```go
cfg.http = &http.Client{Transport: newDebugTransport(http.DefaultTransport, c.DebugHook)}
```

`newDebugTransport` (`internal/llm/debug.go:28`) returns a `debugRoundTripper` that wraps `http.DefaultTransport` and calls the hook before (for errors) or after (for success) each `RoundTrip`.

#### `DebugInfo` shape

`DebugInfo` (`internal/llm/types.go:61`) captures:

```
Method         string
URL            string
RequestHeaders  map[string][]string
RequestBody    string
ResponseStatus int
ResponseHeaders map[string][]string
ResponseBody   string
```

#### Redaction

`debugRoundTripper.RoundTrip` (`internal/llm/debug.go:37`) clones request headers through `redactRequestHeader` (`internal/llm/debug.go:136`), which replaces values for these header keys with `[REDACTED]`:

- `Authorization` — scheme prefix is preserved when present (e.g., `Bearer [REDACTED]`)
- `Proxy-Authorization`
- `X-Api-Key`, `Api-Key`, `Openai-Api-Key`, `Anthropic-Api-Key`

Response headers are NOT redacted (no filter function is passed to `cloneHeader` for the response side).

#### SSE response bodies

`responseBodyKind` (`internal/llm/debug.go:102`) inspects `Content-Type` to decide how much of the response body to capture:

| `Content-Type`                | Behavior                                        |
|-------------------------------|--------------------------------------------------|
| `text/event-stream`           | Body reported as `"(stream)"` literal            |
| `application/json`            | Full body captured as string                     |
| `text/plain`                  | Full body captured as string                     |
| anything else                 | Body is skipped (empty string in `ResponseBody`) |

#### Provider cache invalidation

Because `SetDebugHook` always calls `c.invalidateLocked()` (for non-noop transitions), the cached `fantasy.Provider` and `fantasy.LanguageModel` are discarded. The next agent turn calls `ensureModel`, which rebuilds both from scratch. This ensures the HTTP client with the debug transport is always fresh, but also means the first turn after `/debug on` or `/debug off` pays a one-time provider initialization cost.

### `/set model <name>`

Calls `Agent.SetModel`, which updates `config.Model` and `Client.SetModel`.
The client invalidates its cached provider/model. Existing in-flight model
references are not mutated; future turns use the new model.

Visible output:

```text
*** Model switched to: <name>
```

### `/set provider <provider>`

Calls `Agent.SetProvider`, which updates `config.Provider` and invalidates the
client cache. Visible output begins:

```text
*** Provider set to: <provider>
  requests -> <transport endpoint preview>
```

An additional provider transport hint may be shown as a second system message.

### `/set baseurl <url>`

Calls `Agent.SetBaseURL`, which updates `config.BaseURL` and invalidates the
client cache. Visible output begins:

```text
*** Base URL set to: <url>
  requests -> <transport endpoint preview>
```

An additional provider transport hint may be shown as a second system message.

### `/set apikey <key>`

Calls `Agent.SetAPIKey`, which updates `config.APIKey` and invalidates the
client cache. Visible output:

```text
*** API key set: <masked key>
```

### `/set service <service>`

Updates service identity used by PrepareStep gates such as Anthropic cache
markers. It does not invalidate the cached provider because `Service` is not
part of provider wire-format construction.

### `/join`, `/query`, `/part`, `/channels`

These commands mutate `FocusManager`, not the agent directly. The agent context
is switched only when the next turn starts in `startAgentTurn`.

`/join <#channel>` shows:

```text
Joined #<channel>
```

`/query <persona>` shows:

```text
Query open: <persona>
```

`/part` or `/part <context>` shows one of:

```text
Parted <old> — now in <active>
Can't part the last context.
Not in context <target>.
```

`/channels` shows:

```text
Open contexts:
* #active
  #other
```

Member lists are appended as:

```text
 [member1, member2]
```

Current caveat: because slash commands still work while streaming, focus can
change while a turn is in progress. Session persistence uses frozen
`turnContext`, but visible streamed messages and transcript logging use
`m.focus.Active()` at the time each UI message is appended. That can label
visible output with a different context than the actual agent turn.

## Bootstrap And System Prompt Data

`NewAgentWithStreamer` constructs:

```text
Client
tools.Registry
system prompt
Agent.messages
context message maps
```

The first message is a system message. The first line format is exactly:

```text
System: <osPrettyName> (<GOARCH>) | Host: <hostname> | User: <USER> | Time: <YYYY-MM-DD HH:MM:SS MST> | CWD: <cfg.WorkDir>
```

Then the system prompt includes:

```text
TOOLS ARE LIVE FUNCTION CALLS
```

followed by fixed tool instructions and an "Available tools:" list. Each tool
line is built as:

```text
- <toolName>(<sorted arg summary>): <description>
```

Optional args have a `?` suffix in the summary. Example shape:

```text
- read(limit?:integer, offset?:integer, path:string): Read the contents of a file. For text files, returns content. Supports offset/limit for large files.
```

Then it includes:

```text
MEMORY WORKFLOW
```

with fixed instructions telling the model to use `search_memory` before
substantive work and `write_memory` after meaningful work.

The persona/style harness is appended from the `personaPrompt` string in
`internal/agent/agent.go`. That string is the exact source of truth. The same
string is also injected later as the final synthetic user message in the
persona anchor.

After the system message, `NewAgentWithStreamer` appends optional bootstrap
messages:

If context files are found by walking upward from `cfg.WorkDir`, one user
message is:

```text
Here are the project context files:

<DiscoverContextFiles output>
```

and the assistant reply is exactly:

```text
Got it. I've read the project context and will follow those conventions.
```

`DiscoverContextFiles` checks these filenames in every directory from workdir
up to filesystem root:

```text
AGENTS.md
CLAUDE.md
.agents.md
.claude.md
```

Each found file is formatted:

```text
# Context from <absolute path>

<file content>
```

Multiple files are joined with:

```text

---

```

If root `MEMORY.md` exists, one user message is:

```text
Here is the session memory from previous work:

<memory>
```

and the assistant reply is exactly:

```text
Got it.
```

Finally, the persona anchor appends:

```text
user: <personaPrompt>
assistant: <personaRehearsal>
```

`personaRehearsal` is exactly the string in `internal/agent/agent.go`.

#### Persona dual injection: system prompt + anchor pair

The persona prompt (`personaPrompt`, `internal/agent/agent.go:530`) is injected
in two places:

1. **System prompt** (`buildSystemPrompt`, `internal/agent/agent.go:430`): the
   full `personaPrompt` text is appended after a `🧠 PERSONA / STYLE` header
   and before the system prompt closing. This keeps the entire persona available
   as system-level guidance throughout the conversation.

2. **Bootstrap anchor** (`buildPersonaAnchor`, `internal/agent/agent.go:628`): a
   synthetic user/assistant exchange added right before `bootstrapMsgCount` is
   captured:
   ```go
   newUserMessage(personaPrompt),
   newAssistantMessage(personaRehearsal),
   ```
   where `personaRehearsal` (`internal/agent/agent.go:623`) is exactly:
   ```text
   🦍👑 ready. APES STRONG TOGETHER 🦍💪🤝
   ```

   This exchange is the last thing the model sees before the user's first real
   message, anchoring the voice/style as the model's most recent impression.

#### Compaction boundary

The persona anchor is part of the bootstrap prefix: `bootstrapMsgCount` is set
to `len(a.messages)` immediately after the anchor is appended
(`internal/agent/agent.go:181`). Compaction preserves
`messages[:bootstrapMsgCount]` untouched, so the persona anchor survives
compaction in every context.

The system prompt (which also carries `personaPrompt`) is not in the message
slice at all — fantasy passes it separately — so compaction does not affect it.

#### Disable path

There is no runtime config toggle. To disable the persona, edit the two
package-level variables in `internal/agent/agent.go`:

```go
var personaPrompt = ``       // line 530
var personaRehearsal = ``    // line 623
```

With both empty, `buildPersonaAnchor` returns two empty messages that still
take space in the transcript. For a complete removal, also edit `buildSystemPrompt`
to skip the persona section.

#### Per-message persona refresh

`PerMessagePrefix` (`internal/agent/agent.go:638`) is a separate mechanism. When
non-empty, each user message is prefixed with this string via
`injectPerMessagePrefix`. This is designed to keep the persona voice fresh in
long sessions. It is empty in the default checkout.

`bootstrapMsgCount` is set to `len(a.messages)` after this bootstrap. Fresh
agents also initialize:

```text
currentContext = "#main"
contextMsgs["#main"] = a.messages
contextSavedIdx["#main"] = 0
```

## Per-Context Histories

The current code does maintain separate message histories per IRC context:

```go
contextMsgs     map[ContextKey][]fantasy.Message
contextSavedIdx map[ContextKey]int
currentContext  ContextKey
```

This differs from older notes that describe per-context histories as not yet
implemented.

`SetContext(key)` saves the current `a.messages` into `contextMsgs` under the
old context, switches `currentContext`, and loads `contextMsgs[key]`. If the
target does not exist, it copies the bootstrap prefix:

```go
a.messages[:a.bootstrapMsgCount]
```

`InitContext(key)` creates that bootstrap-only context without switching.

`SetScope(scope)` updates both:

```go
a.scope = scope
a.tools.SetScope(scope)
```

If scoped hot memory exists and its path has not already been injected, it
appends:

```text
user: Context memory for <scopeLabel>:

<hot memory>
assistant: Got it.
```

The injection guard is global by path (`a.injectedPaths[path]`), not per
context key.

## Session Persistence

At the TUI done boundary, only messages after the per-context saved watermark
are appended. Each appended line is a `session.Entry` marshaled as JSON.

`EntryFromFantasyWithBootstrap` writes both:

1. `Msg`: canonical `fantasy.Message`
2. Legacy fields: `role`, `content`, `tool_calls`, `tool_call_id`

It sets:

```text
v = 1
bootstrap = true for i < BootstrapMessageCount()
legacy_lossy = true if the legacy projection cannot represent all fantasy parts
```

Lossy cases include:

```text
multiple text parts
reasoning parts
file parts
media tool-result output
error tool-result output
unknown fantasy part types
```

On restore, `FantasyFromEntries` uses `Msg` for v1 entries. For v0 entries it
synthesizes fantasy messages from the legacy fields. Tool entries without
`tool_call_id` are skipped because provider APIs reject them.

`DisplayEntries` hides entries with `Bootstrap == true` from replay display.

## Headless Output Contract

Headless mode sends assistant text chunks to stdout exactly as received after
agent-level sanitization. It writes status/tool/autonomous messages to stderr.

State events:

```text
[status] thinking
[status] tool_call
[status] idle
```

Tool start:

```text
[tool] <toolName> args=<truncated args>
```

Tool result:

```text
[tool] <toolName> result=<truncated result>
```

Tool error, if `ToolError` is populated:

```text
[tool] <toolName> error=<error> result=<truncated result>
```

Autonomous follow-up:

```text
[auto] <label>
```

If a follow-up is about to start and stdout text did not end with `\n`, the
headless loop prints a newline first. When all turns are done, it also ensures
stdout ends with a trailing newline.

## Tool Result Output Strings From Local Registry

These are the exact success/error strings returned by the core local tools
before fantasy wraps them.

`read` returns file content. With offset past EOF:

```text
read: offset <offset> is past end of file (file has <lineCount> lines)
```

Oversized read output is truncated at 50 KiB and suffixed:

```text
... (truncated)
```

`write` success:

```text
Wrote <byteCount> bytes to <absolute path>
```

`edit` success:

```text
Applied <N> edit(s) to <absolute path>
```

`edit` empty `oldText`:

```text
edit: oldText must not be empty (use the write tool to create a new file or replace its contents)
```

`edit` missing text:

```text
oldText not found in <path>: "<truncated oldText>"
```

`edit` non-unique text:

```text
oldText matches <count> times in <path> (must be unique): "<truncated oldText>"
```

`bash` runs:

```text
bash -c <command>
```

with `cmd.Dir = Registry.WorkDir`. stdout and stderr are combined. Non-zero
exit returns normal text, not a Go error, with:

```text

Exit code: <N>
```

Timeout error:

```text
command timed out after <seconds>s
```

Cancellation error:

```text
command cancelled
```

Start failure:

```text
failed to start command: <error>
```

Oversized bash output is truncated at 50 KiB on a UTF-8 rune boundary and
suffixed:

```text
... (truncated)
```

`write_memory` hot success:

```text
Wrote <byteCount> bytes to <hot memory path>
```

`write_memory` daily success:

```text
Appended <byteCount> bytes to daily memory (<daily memory path>)
```

`write_memory` empty content:

```text
content is required
```

`write_memory` missing scoped name:

```text
name is required when scope='channel'
name is required when scope='query'
```

`write_memory` bad scope:

```text
unknown scope "<scope>" (want 'current', 'root', 'channel', or 'query')
```

For typed wrappers and `bitchteaTool`, any Go error from `Registry.Execute` is
converted into a model-visible tool error response:

```text
Error: <the exact error above>
```

## Tests: What They Prove And What They Do Not

Strong tests:

- `TestSendMessageExitsOnContextCancel` proves the agent loop watches
  `ctx.Done()` and does not block forever waiting for a stuck streamer to close.
- `TestStreamChatSendsValidToolSchemaAndExecutesToolCall` proves a real
  `Client.StreamChat` call can advertise all registry tools, run fantasy
  around a real `read` tool, and rebuild a tool transcript.
- `TestPhase2TypedToolSmoke_ReadFileEndToEnd` proves the typed `read` wrapper
  executes against the real filesystem through fantasy and lands back in
  agent messages.
- `TestPhase3ToolTranscriptSurvivesTurn` proves assistant tool-call parts,
  tool-result parts, and final assistant text survive in `agent.Messages()`.
- `TestStaleAgentEventsAreIgnoredAfterChannelReplacement` and
  `TestStaleAgentDoneIsIgnoredAfterChannelReplacement` prove stale TUI events
  cannot finalize the wrong active channel.
- `TestToolContextManager_*` covers per-tool cancellation, turn cancellation
  propagation, cleanup, and concurrent access.

Shape-heavy or partial tests:

- `TestSendMessageExecutesToolCallWithoutNetwork` uses a fake streamer that
  pre-emits both `tool_call` and `tool_result`; it verifies event mapping but
  does not execute a real tool.
- `TestReplayToolLoop` uses replay fixtures and also pre-emits tool results;
  it is good for event replay shape, not real fantasy dispatch.
- `TestTranslateToolsRoutesPortedToolsThroughTypedWrappers` proves wrapper
  selection by type, not per-tool behavior.
- `TestTranslateToolsProducesValidSchemasForRealRegistryDefinitions` verifies
  schema shape safety, not provider acceptance.
- `TestToolResultText_UnknownErrorTypeReturnsEmpty` pins current empty output
  for fantasy error/media result types. That is documentation of a limitation,
  not proof that UI error display is correct.
- Queue tests cover the TUI post-turn queue (`m.queued`), not the lower-level
  `Agent.QueuePrompt`/`PrepareStep` mid-turn drain path.

Known gaps that junior models should not assume away:

- The lower-level mid-turn prompt drain exists but the TUI does not feed it.
- If the lower-level prompt drain is used, provider-prepared messages and
  persisted `a.messages` use different text for the same queued prompt.
- Empty `thinking` events from `PrepareStep` can erase the visible
  `thinking...` placeholder.
- `toolResultText` drops fantasy media and error tool-result outputs to empty
  strings for UI/headless events.
- `ToolError` is present on `agent.Event` but the agent mapping from
  `llm.StreamEvent` never sets it today.
- Slash commands can change focus mid-turn. Session persistence uses frozen
  `turnContext`, but visible streamed messages use current focus at event
  handling time.
- `/compact` blocks inside the command handler while it consumes stream events;
  unlike normal turns, it is not routed through the non-blocking Bubble Tea
  event pipeline.

## Per-Context Message Isolation

Per-context isolation lives in `internal/agent/context_switch.go` and the
`Agent` fields declared in `internal/agent/agent.go`. The canonical active
history is still:

```go
messages []fantasy.Message
```

The isolated histories are stored beside it:

```go
contextMsgs     map[ContextKey][]fantasy.Message
contextSavedIdx map[ContextKey]int
currentContext  ContextKey
```

`ContextKey` is a string alias. The default context is `DefaultContextKey`,
whose value is `"#main"`. UI routing converts IRC focus to keys with
`ircContextToKey`: channels become `#channel`, subchannels become
`#channel.sub`, direct queries become the target nick, and unknown focus falls
back to `agent.DefaultContextKey`.

### `contextMsgs` structure

`contextMsgs map[ContextKey][]fantasy.Message` owns one fantasy-native message
slice per chat context. The comments in `Agent` are load-bearing: each context
gets its own conversation slice, and the bootstrap prefix is duplicated into
each slice so `/join` and `/query` do not share ongoing conversation history.

At startup, `NewAgentWithStreamer` builds the normal bootstrap messages:
system prompt, discovered context files, root memory, and persona anchor. It
then records:

```go
a.bootstrapMsgCount = len(a.messages)
a.currentContext = DefaultContextKey
a.contextMsgs = map[ContextKey][]fantasy.Message{
    DefaultContextKey: a.messages,
}
a.contextSavedIdx = map[ContextKey]int{
    DefaultContextKey: 0,
}
```

### `SetContext` message slice swap

`SetContext(key ContextKey)` is the active-context swap. If `key` already
matches `a.currentContext`, it returns without changing anything. Otherwise it
first stores the currently active slice back into the map:

```go
a.contextMsgs[a.currentContext] = a.messages
```

Then it sets `a.currentContext = key`. If `contextMsgs[key]` exists, that slice
becomes `a.messages`. If it does not exist, `SetContext` clones only the
bootstrap prefix from the current active slice:

```go
a.messages = append([]fantasy.Message(nil), a.messages[:a.bootstrapMsgCount]...)
a.contextMsgs[key] = a.messages
```

That means a new context inherits startup/system grounding but not the prior
chat turns from the context the user switched away from.

### `InitContext` bootstrap cloning

`InitContext(key ContextKey)` prepares a context without switching to it. If
the key already exists, it is a no-op. Otherwise it clones:

```go
bootstrap := append([]fantasy.Message(nil), a.messages[:a.bootstrapMsgCount]...)
```

and stores:

```go
a.contextMsgs[key] = bootstrap
a.contextSavedIdx[key] = 0
```

The TUI uses this before every agent turn in `Model.startAgentTurn`:

```go
m.agent.InitContext(ctxKey)
m.agent.SetContext(ctxKey)
m.lastSavedMsgIdx = m.agent.SavedIdx(ctxKey)
m.agent.SetScope(ircContextToMemoryScope(m.turnContext))
```

`InitContext` is what lets the UI create a channel/query history before
`SetContext` makes that history active.

### `InjectNoteInContext` cross-context notes

`InjectNoteInContext(key ContextKey, note string)` appends a synthetic note to
a target context without switching to it. If `contextMsgs[key]` exists, it
appends to that stored slice:

```go
newUserMessage(note)
newAssistantMessage("Understood.")
```

If the target key does not exist, it appends the same synthetic exchange to
the current active `a.messages` slice instead. That fallback is deliberate in
the current code; callers that need strict off-context injection must ensure
the target has been initialized first.

### `RestoreContextMessages` on resume

`Model.ResumeSession` groups restored session entries by `Entry.Context`. Empty
context labels are treated as `agent.DefaultContextKey`.

For the default context, `ResumeSession` calls:

```go
m.agent.RestoreMessages(msgs)
m.agent.SetSavedIdx(defaultKey, len(msgs))
```

For non-default contexts, it calls:

```go
m.agent.RestoreContextMessages(key, msgs)
m.agent.SetSavedIdx(key, len(msgs))
```

`RestoreContextMessages(key ContextKey, messages []fantasy.Message)` restores
a specific context without switching to it. It copies the provided slice,
forces the first message to be the freshly built system prompt, and stores the
result in `a.contextMsgs[key]`. If the restored slice does not start with a
system message, it prepends one. Unlike `RestoreMessages`, it does not reset
turn counters, cost tracking, or follow-up state; it only restores that
context's stored transcript.

### Bootstrap duplication policy

Bootstrap messages are duplicated per context, not shared by reference as a
global transcript. A newly initialized or newly switched-to context receives
`a.messages[:a.bootstrapMsgCount]` copied into a new slice. Those bootstrap
messages include whatever startup built before `bootstrapMsgCount` was
captured: system prompt, discovered context files, root memory if present, and
the persona anchor.

`RestoreMessages` and `RestoreContextMessages` have a separate resume policy:
they refresh or prepend only the system prompt for restored history. On
`RestoreMessages`, `bootstrapMsgCount` is reset to 0 before the active context
map entry is synced. On `RestoreContextMessages`, the restored context is
stored with its refreshed system message, but `bootstrapMsgCount` is not
changed. Session persistence still uses `BootstrapMessageCount()` to decide
which leading messages are bootstrap when writing new entries.

### `contextSavedIdx` watermarks

`contextSavedIdx map[ContextKey]int` is the agent-side per-context session-save
watermark. The accessors are:

```go
SavedIdx(key ContextKey) int
SetSavedIdx(key ContextKey, idx int)
```

At `agentDoneMsg`, the UI freezes the context at turn start in
`m.turnContext`, converts it to `ctxKey`, and saves only new messages from that
context:

```go
msgs := m.agent.Messages()
savedIdx := m.agent.SavedIdx(ctxKey)
for i := savedIdx; i < len(msgs); i++ {
    e := session.EntryFromFantasyWithBootstrap(
        msgs[i],
        i < m.agent.BootstrapMessageCount(),
    )
    e.Context = ctxLabel
    m.session.Append(e)
}
m.agent.SetSavedIdx(ctxKey, len(msgs))
```

This prevents a turn in one channel/query from advancing the persistence
watermark for another channel/query. There is also a UI-local
`m.contextSavedIdx map[string]int` used by `saveCurrentContextMessages`, but
normal agent-turn persistence uses the agent's `SavedIdx`/`SetSavedIdx`
watermark.

### Compaction is per active context

`Agent.Compact(ctx)` operates only on the currently active `a.messages` slice.
It does not iterate over `contextMsgs` and does not compact inactive contexts.
After it rebuilds the active history as bootstrap prefix, summary message, and
last four messages, it writes the compacted result back to only the active map
entry:

```go
a.contextMsgs[a.currentContext] = a.messages
a.contextSavedIdx[a.currentContext] = len(a.messages)
```

The daily-memory flush inside compaction also uses the agent's current memory
scope, which the TUI sets at turn start with:

```go
m.agent.SetScope(ircContextToMemoryScope(m.turnContext))
```

So `/compact` summarizes the active context history and records its new save
watermark. Other contexts remain in `contextMsgs` exactly as they were until
the user switches to them and compacts those histories separately.

## `@file` Token Expansion Details

The `@file` expansion mechanism is implemented in `ExpandFileRefs`
(`internal/agent/context.go:130`). The Input expansion section above
(see "Input expansion") covers the basic flow; this section documents the
edge cases and design decisions.

### Multiple `@`-tokens in one prompt

Each whitespace-delimited word starting with `@` is independently resolved.
The function uses `strings.Fields` to split the input, processes each token
in order, and joins the results with spaces. There is no interaction between
tokens — `look at @main.go and @utils.go` becomes two separate file reads,
each expanded independently with its own success or error result. If one
succeeds and the other fails, both the inline content and the error message
appear in the final prompt.

### Path resolution

- If the `@`-prefixed path is absolute (checked via `filepath.IsAbs`),
  it is used as-is.
- If the path is relative, it is joined with `a.config.WorkDir` (the
  workspace root, set via `--work-dir` or inherited from the terminal's
  working directory).
- There is no search-path lookup, no `$PATH`-style resolution, and no
  upward directory walk (unlike `CLAUDE.md`/`AGENTS.md` discovery). The
  path resolves against exactly one directory.

### Whitespace normalization

Because the function splits on `strings.Fields` and re-joins with
`strings.Join(..., " ")`, all whitespace runs are collapsed to single
spaces. Leading and trailing whitespace is stripped. A message like
`"fix   @main.go\nplease"` becomes `"fix @main.go (file contents below):\n\n\`\`\`\n...\n\`\`\` please"`.
This is a property of `strings.Fields`, not specific to file expansion.

### Truncation

Files larger than 30 KB are truncated with the suffix
`\n... (truncated at 30KB)`. The constant is hardcoded at
`context.go:145` (`maxSize = 30 * 1024`). There is no configuration knob
for this limit.

### Error handling

When the file cannot be read (permission denied, not found, is a directory,
etc.), the token is replaced with:

```text
@file (file not found: <os error>)
```

The error message includes the raw `os.ReadFile` error string. The
expansion does **not** halt on error — other `@`-tokens in the same input
are still processed, and the non-`@` words are preserved. This means a
typo like `fix @non-existent.go and @main.go` still loads `main.go` and
reports the error for `non-existent.go`.

There is no retry, no fuzzy path resolution, and no file-watch fallback.
The error is a one-shot diagnostic embedded inline in the prompt.

### Source reference

Implementation: `internal/agent/context.go:128-159`
Tests: `internal/agent/context_test.go:220-250`

## REPL Turn Data-Flow

The full round-trip from keystroke to response update flows through 9 hops:

```text
internal/ui/model.go:Update
  │  tea.KeyMsg{enter} → trim input → "/" → handleCommand
  │                                     else → sendToAgent(input)
  ▼
internal/ui/model.go:sendToAgent
  │  ① "Enter" captured, routes to startAgentTurn
  ▼
internal/ui/model.go:startAgentTurn
  │  ② cancel previous turn, create fresh ctx+cancel
  │     ch := make(chan agent.Event, 100), m.eventCh = ch
  │     m.agent.InitContext(ctxKey)
  │     m.agent.SetContext(ctxKey)
  │     m.agent.SetScope(memoryScope)
  │     streaming = true
  │     start(ctx, ch)  ← fires the goroutine below
  ▼
go m.agent.SendMessage(ctx, input, ch)       ③ goroutine
  │  ExpandFileRefs → append to a.messages
  │  StreamChat goroutine starts
  ▼
go a.streamer.StreamChat(ctx, llm.FantasySliceToLLM( ④ goroutine
  │     a.messages), a.tools, streamEvents)
  │  Retry loop (1s/2s/4s/8s/16s/32s/64s)
  ▼
llm/stream.go:streamOnce
  │  ⑤ fantasy.Agent.Stream(ctx, call{Prompt, Prior, PrepareStep,
  │     OnTextDelta, OnReasoningDelta, OnToolCall, OnToolResult,
  │     OnStreamFinish, OnError})
  │     → llm.StreamEvent{text|thinking|tool_call|tool_result|usage|error|done}
  ▼
agent/agent.go:sendMessage select loop
  │  ⑥ select on streamEvents / ctx.Done()
  │     text        → streamSanitizer.Consume → Event{text}
  │     thinking    → Event{thinking}
  │     tool_call   → a.ToolCalls[t]++; Event{state, tool_start}
  │     tool_result → Event{tool_result}
  │     error       → Event{state, error, done}; return
  │     done        → splice ev.Messages into a.messages
  │                   Event{state:StateIdle, done}
  ▼
internal/ui/model.go:handleAgentEvent(agentEventMsg)
  │  ⑦ per-event viewport update:
  │     text        → append chunk to streamBuffer
  │     tool_start  → show "calling <toolName>..."
  │     tool_result → show truncated result
  │     state       → update status bar (thinking/running tools)
  ├─ chain: waitForAgentEvent(m.eventCh)  ← next event
  ▼
internal/ui/model.go:agentDoneMsg
  │  ⑧ streaming=false, agentState=StateIdle
  │     session persist (per-context watermark)
  │     focus.Save(), SaveCheckpoint()
  │     queued message drain → next turn or idle
  ▼
internal/ui/model.go:Update
  │  ⑨ idle — wait for next tea.KeyMsg
```

The channel identity guard is load-bearing: `agentEventMsg` and `agentDoneMsg`
both check `msg.ch != nil && msg.ch != m.eventCh` and silently drop stale events
from previous turns that arrive after a new turn has started. This prevents a
slow goroutine from a cancelled turn from corrupting the active turn's state.

### Turn lifecycle timing

```text
Update(enter) ──→ startAgentTurn ──→ go SendMessage ──→ StreamChat ──→ ...
     │  (synchronous)     (synchronous)     (goroutine)     (goroutine)
     ▼
waitForAgentEvent ──→ handleAgentEvent ──→ waitForAgentEvent ──→ ... ──→ agentDoneMsg
     (tea.Cmd)           (Update passes)      (next tea.Cmd)               (final)
```

All actual LLM I/O and tool execution runs in goroutines. The Bubble Tea event
loop never blocks — it schedules `waitForAgentEvent` as a `tea.Cmd`, processes
the returned `agentEventMsg` in `Update`, and immediately schedules the next
`waitForAgentEvent` for the same channel. Only the `agentDoneMsg` handler
terminates the chain.

## Focus / Context Switch Flow

When the user switches IRC contexts (`/join`, `/query`, `/part`) or the session
resumes, the focus manager and agent coordinate to swap the active message
history and memory scope.

### Slash-command path (interactive)

```text
User types /join #channel  (or /query nick, /part)
  │
  ▼
commands.go:handleJoinCommand
  │  ① m.focus.SetFocus(Channel("#channel"))
  │     → updates FocusManager.active index
  │     → saves to .bitchtea_focus.json
  ▼
model.go:syncAgentContextIfIdle(ctx)
  │  ② (no-op if streaming — context swap deferred to next turn)
  │
  │  If idle:
  │    agent.InitContext(ctxKey)
  │      → if ctxKey not in contextMsgs, clone bootstrap prefix into new slice
  │    agent.SetContext(ctxKey)
  │      → save a.messages → contextMsgs[oldKey]
  │      → load contextMsgs[ctxKey] → a.messages
  │      → set currentContext = ctxKey
  │    agent.SetScope(memoryScope)
  │      → update agent.scope + tools.SetScope
  │      → inject HOT.md if not previously injected
  │    m.lastSavedIdx = agent.SavedIdx(ctxKey)
  ▼
Next agent turn: model.go:startAgentTurn
  │  ③ Called on next user message (or auto follow-up).
  │     Re-runs InitContext, SetContext, SetScope to refresh even
  │     if syncAgentContextIfIdle already ran.
  │     This is where per-context history diverges:
  │     each context gets its own []fantasy.Message slice.
  ▼
agent.sendMessage appends to active a.messages
  │  The appended messages belong to the focused context.
  │  Other contexts' histories are untouched in contextMsgs.
```

### Deferred switch during streaming

When a context switch command runs while the agent is streaming (m.streaming ==
true), `syncAgentContextIfIdle` returns without touching the agent:

```text
/join #help  (while streaming)
  │
  ▼
focus.SetFocus("#help")                   ① focus updates immediately
  ▼
syncAgentContextIfIdle → returns early     ② streaming guard
  ▼
(agent still streaming against old context)
  ▼
Turn ends → agentDoneMsg                   ③ session persist uses frozen m.turnContext
  ▼
Next user message → startAgentTurn         ④ picks up #help via active focus:
  InitContext("#help")
  SetContext("#help")
  SetScope("#help memory scope")
```

The session persistence at `agentDoneMsg` uses `m.turnContext`, which was frozen
at turn start. So a streaming turn that started in `#general` persists its
messages under `#general` even if the user switches focus to `#help` mid-turn.

### Restore path (session resume)

```text
main.go:buildStartupModel
  │
  ▼
ui.NewModel(cfg)
  │  agent.NewAgent(cfg)                   ① fresh agent, empty contextMsgs
  │  LoadFocusManager(cfg.SessionDir)       ② restore FocusManager from JSON
  │  session.New(cfg.SessionDir)
  ▼
m.ResumeSession(sess)
  │  ③ session.FantasyFromEntries per group
  │     group by Entry.Context ("#main", "#channel", "nick", ...)
  │
  ├─ RestoreMessages(defaultGroup)
  │     → force system prompt, reset counters
  │     → a.messages = restored slice
  │
  ├─ RestoreContextMessages("#channel", group)
  │     → agent.contextMsgs["#channel"] = restored slice
  │     → force system prompt at index 0
  │
  ├─ RestoreContextMessages("nick", group)
  │     → agent.contextMsgs["nick"] = restored slice
  │
  └─ SetSavedIdx per group                 ④ per-context session watermarks
```

`RestoreContextMessages` never switches the active context — it only populates
`contextMsgs` so lazy `SetContext` on the first turn will find preloaded
history. The active context after resume is whatever `FocusManager.Active()`
returns from the restored `.bitchtea_focus.json`.

### File references

| Step | File | Function |
|------|------|----------|
| ① focus | `internal/ui/context.go:100` | `FocusManager.SetFocus` |
| ② sync | `internal/ui/model.go:912` | `syncAgentContextIfIdle` |
| ② init | `internal/agent/context_switch.go:34` | `Agent.InitContext` |
| ② swap | `internal/agent/context_switch.go:47` | `Agent.SetContext` |
| ② scope | `internal/agent/agent.go:688` | `Agent.SetScope` |
| ③ turn | `internal/ui/model.go:941` | `startAgentTurn` |
| resume-① | `internal/ui/model.go:234` | `ResumeSession` |
| resume-② | `internal/agent/context_switch.go:75` | `RestoreContextMessages` |
| resume-③ | `internal/session/session.go:396` | `FantasyFromEntries` |
