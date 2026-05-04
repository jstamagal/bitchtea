# LLM Agent Loop

This document is the foundational contract for bitchtea's LLM turn flow. It
focuses on the agent loop, event messaging, and strict turn boundaries. It is
written from the current implementation, not from the UI metaphor.

Primary files:

- `internal/agent/agent.go`: agent state, bootstrap, send loop, compaction,
  follow-ups, message history, memory scope.
- `internal/agent/context_switch.go`: per-context message histories.
- `internal/agent/event/event.go`: event and state values emitted by the agent.
- `internal/llm/stream.go`: fantasy streaming bridge, retries, tool callbacks,
  usage reporting, transcript rebuild.
- `internal/llm/client.go`: provider/model cache, prompt drain, MCP manager,
  per-turn tool context manager.
- `internal/llm/convert.go`: conversion between fantasy messages and the
  legacy `llm.Message` stream boundary.
- `internal/llm/tools.go`: local tool adapters and typed-wrapper selection.
- `internal/llm/tool_context.go`: per-tool cancellation.
- `internal/ui/model.go`: Bubble Tea event routing, visible output, queueing,
  cancellation, session save.
- `main.go`: headless event output.

## Ground Rules

The canonical in-memory agent transcript is `[]fantasy.Message` in
`Agent.messages`.

The streamer boundary is still legacy-shaped:

```go
StreamChat(ctx context.Context, messages []llm.Message, reg *tools.Registry, events chan<- llm.StreamEvent)
```

Every normal user turn appends exactly one user message before streaming:

```text
injectPerMessagePrefix(ExpandFileRefs(userInput, workDir))
```

If no `PerMessagePrefix` is configured, this is just the expanded user input.
`@file` expansion happens before the message enters history.

The UI has a separate visible chat log, `Model.messages []ChatMessage`. That
is not the LLM transcript. The UI log is what the user sees. The agent
transcript is what the LLM sees and what session persistence reads after a
turn closes.

The strict turn boundary is the `agent.Event{Type:"done"}` emitted by
`Agent.sendMessage`, followed by the agent event channel closing. In the TUI,
that `done` event schedules an `agentDoneMsg`, and `agentDoneMsg` performs the
post-turn cleanup, session append, queue drain, and optional autonomous
follow-up.

Do not confuse the two `done` layers:

- `llm.StreamEvent{Type:"done"}` is internal to the streamer boundary. It
  carries rebuilt `[]llm.Message` transcript data from fantasy back to the
  agent. The agent consumes it, splices those messages into `Agent.messages`,
  and keeps reading until the stream channel closes.
- `agent.Event{Type:"done"}` is emitted by `sendMessage` only after the stream
  channel has ended normally or after an error/cancellation path has already
  emitted idle and error state. This is the event the UI treats as the hard
  end of the turn.
- Channel close is a defensive second boundary. `waitForAgentEvent` also
  returns `agentDoneMsg` if it sees the channel close. If the UI already
  processed the explicit `done`, that later channel-close `agentDoneMsg` is
  stale and ignored because it carries the old channel pointer.

## Event Types

Agent events are defined in `internal/agent/event/event.go`.

States:

- `StateIdle`: numeric value `0`.
- `StateThinking`: numeric value `1`; means waiting for the LLM or the next
  fantasy step.
- `StateToolCall`: numeric value `2`; means a tool call was observed and the
  UI should show tool execution state.

Agent event type strings:

- `text`: streamed assistant text chunk. Uses `Event.Text`.
- `thinking`: streamed reasoning/thinking chunk. Uses `Event.Text`.
- `tool_start`: a tool call began. Uses `ToolName`, `ToolCallID`, `ToolArgs`.
- `tool_result`: a tool returned. Uses `ToolName`, `ToolCallID`, `ToolResult`.
- `state`: state transition. Uses `State`.
- `error`: terminal stream error or cancellation. Uses `Error`.
- `done`: logical end of this agent turn.

LLM stream event type strings:

- `text`: streamed assistant text.
- `thinking`: either a step-start marker with empty text or reasoning text.
- `tool_call`: fantasy observed a tool call.
- `tool_result`: fantasy received a tool result.
- `usage`: provider token usage.
- `error`: stream-level error.
- `done`: fantasy completed the full turn and carries rebuilt `[]llm.Message`.

`tool_result` currently has no populated `ToolError` at the agent event layer.
Local and MCP tool adapters intentionally convert tool failures into text error
responses, so the stream can continue. The UI therefore almost always sees
tool failures as `MsgTool`, not `MsgError`.

## Bootstrap

`NewAgentWithStreamer` builds the startup transcript in this order:

1. `client := llm.NewClient(cfg.APIKey, cfg.BaseURL, cfg.Model, cfg.Provider)`.
2. `client.SetService(cfg.Service)`.
3. If no test streamer was supplied, use the client itself as the streamer.
4. `toolRegistry := tools.NewRegistry(cfg.WorkDir, cfg.SessionDir)`.
5. `systemPrompt := buildSystemPrompt(cfg, toolRegistry.Definitions())`.
6. Initialize `Agent.messages` with one system `fantasy.Message`.
7. Discover project context files upward from `cfg.WorkDir`.
8. If any context files were found, append:

```text
user: Here are the project context files:

<concatenated context files>
assistant: Got it. I've read the project context and will follow those conventions.
```

9. Load root `MEMORY.md`.
10. If root memory exists, append:

```text
user: Here is the session memory from previous work:

<memory>
assistant: Got it.
```

11. Append the persona anchor:

```text
user: <personaPrompt>
assistant: <personaRehearsal>
```

12. Set `bootstrapMsgCount = len(messages)`.
13. Mirror that count into `Client.BootstrapMsgCount` with
    `pushBootstrapToClient`.
14. Initialize the default LLM context:

```go
currentContext = "#main"
contextMsgs["#main"] = messages
contextSavedIdx["#main"] = 0
```

The system prompt contains dynamic host fields. Its first line is exactly this
format:

```text
System: <osPrettyName> (<runtime.GOARCH>) | Host: <hostname> | User: <USER> | Time: <YYYY-MM-DD HH:MM:SS MST> | CWD: <cfg.WorkDir>
```

The rest of the system prompt is generated from the current tool definitions,
the memory workflow text, and the persona text.

## Contexts

The current implementation does have per-context LLM histories.

`DefaultContextKey` is `"#main"`.

`Agent.SetContext(key)` stores the current `a.messages` back into
`contextMsgs[a.currentContext]`, switches `currentContext`, then loads
`contextMsgs[key]`. If the target key does not exist, it clones the current
bootstrap prefix:

```go
a.messages[:a.bootstrapMsgCount]
```

`Agent.InitContext(key)` creates the context without switching to it.

`Model.startAgentTurn` captures the focused IRC context at submission time in
`m.turnContext`, initializes and switches the agent to that context, sets the
memory scope for that context, and starts a fresh `context.Context` for the
turn.

This matters for strict boundaries: changing focus with `/join` or `/query`
while no turn is active does not mutate the current in-flight prompt. When a
turn starts, the active focus is captured and later used for session stamping.

## Normal TUI Turn

A normal Enter key path in `Model.Update` does this:

1. Trim the textarea.
2. Ignore empty input.
3. Reset the input.
4. Append input to command history.
5. If the input starts with `/`, route to `handleCommand`.
6. If `m.streaming` is true, queue the input instead of sending it.
7. If idle, append a visible user message:

```text
[HH:MM] <userNick> <input>
```

8. Call `m.sendToAgent(input)`.

`sendToAgent` calls `startAgentTurn`. `startAgentTurn`:

1. Cancels any existing `m.cancel`.
2. Clears queue/cancel ladder flags.
3. Clears active tool fields.
4. Captures `m.turnContext = m.focus.Active()`.
5. Initializes and selects the corresponding agent context.
6. Sets the agent memory scope from the IRC context.
7. Creates `ctx, cancel := context.WithCancel(context.Background())`.
8. Sets `m.cancel`, `m.streaming = true`, and `m.eventCh`.
9. Starts `go m.agent.SendMessage(ctx, input, ch)`.
10. Returns `waitForAgentEvent(ch)`.

`waitForAgentEvent` reads exactly one event from the channel per Bubble Tea
command. If the channel is closed it returns `agentDoneMsg{ch: ch}`.

Stale events are ignored. `Model.Update(agentEventMsg)` checks:

```go
if msg.ch != m.eventCh { return m, nil }
```

`Model.Update(agentDoneMsg)` also ignores stale done messages when `msg.ch` is
non-nil and does not match the current channel.

## Agent Send Loop

`SendMessage` is a public wrapper:

```go
func (a *Agent) SendMessage(ctx context.Context, userMsg string, events chan<- Event) {
    a.sendMessage(ctx, userMsg, followUpKindNone, events)
}
```

`SendFollowUp` is the autonomous continuation wrapper. If the request is nil,
it closes the channel immediately. Otherwise it calls `sendMessage` with the
request prompt and follow-up kind.

`sendMessage` has a `defer close(events)`, so every exit path closes the agent
event channel.

Exact normal setup:

1. `a.activeFollowUpKind = kind`.
2. Expand `@file` refs.
3. Append the user message to `a.messages`.
4. Increment `a.TurnCount`.
5. Estimate input tokens from current history.
6. Create a follow-up stream sanitizer.
7. Emit:

```go
Event{Type: "state", State: StateThinking}
```

8. Install `a.client.SetPromptDrain(a.drainAndMirrorQueuedPrompts)`.
9. Defer clearing prompt drain with `SetPromptDrain(nil)`.
10. Create `streamEvents := make(chan llm.StreamEvent, 100)`.
11. Start:

```go
go a.streamer.StreamChat(ctx, llm.FantasySliceToLLM(a.messages), a.tools, streamEvents)
```

The agent loop then selects between `streamEvents` and `ctx.Done()`. It does
not range directly over `streamEvents`, because a cancelled turn must stop even
if the streamer or a tool is hanging.

### Agent Handling of LLM Events

`llm.StreamEvent{Type:"text"}`:

1. Append `ev.Text` to `textAccum`.
2. Feed the chunk through the follow-up sanitizer.
3. If sanitizer returns non-empty text, emit:

```go
Event{Type: "text", Text: safeText}
```

`llm.StreamEvent{Type:"thinking"}`:

```go
Event{Type: "thinking", Text: ev.Text}
```

`llm.StreamEvent{Type:"usage"}`:

If `ev.Usage != nil`, add provider usage to the cost tracker and set
`gotUsage = true`.

`llm.StreamEvent{Type:"tool_call"}`:

1. Increment `a.ToolCalls[ev.ToolName]`.
2. Emit:

```go
Event{Type: "state", State: StateToolCall}
```

3. Emit:

```go
Event{
    Type:       "tool_start",
    ToolName:   ev.ToolName,
    ToolCallID: ev.ToolCallID,
    ToolArgs:   ev.ToolArgs,
}
```

`llm.StreamEvent{Type:"tool_result"}`:

```go
Event{
    Type:       "tool_result",
    ToolName:   ev.ToolName,
    ToolCallID: ev.ToolCallID,
    ToolResult: ev.Text,
}
```

`llm.StreamEvent{Type:"error"}`:

1. Set `lastTurnState` to canceled if the error is `context.Canceled`, else
   errored.
2. Emit, in this exact order:

```go
Event{Type: "state", State: StateIdle}
Event{Type: "error", Error: ev.Error}
Event{Type: "done"}
```

3. Return, which closes the event channel.

`llm.StreamEvent{Type:"done"}`:

1. Flush any buffered follow-up sanitizer output.
2. For each `ev.Messages` item:
   - if role is `assistant`, sanitize autonomous done tokens from `Content`;
   - convert the legacy message back to fantasy with `llm.LLMToFantasy`;
   - append it to `a.messages`.
3. If no assistant message was present in `ev.Messages` but streamed text was
   accumulated, synthesize one assistant fantasy message from `textAccum`.

After the stream channel closes normally:

1. `lastAssistantRaw = strings.TrimSpace(textAccum.String())`.
2. If no provider usage was reported, add fallback usage:

```text
input = estimatedInputTokens
output = len(textAccum.String()) / 4
```

3. Set:

```go
lastTurnState = turnStateCompleted
lastCompletedFollowUp = kind
```

4. Emit:

```go
Event{Type: "state", State: StateIdle}
Event{Type: "done"}
```

5. Return and close the event channel.

### Cancellation From the Agent Select

If `ctx.Done()` fires while `sendMessage` is waiting:

1. Start a background goroutine to drain `streamEvents` so the streamer can
   eventually close without blocking.
2. Set `lastTurnState = turnStateCanceled`.
3. Emit, in this exact order:

```go
Event{Type: "state", State: StateIdle}
Event{Type: "error", Error: ctx.Err()}
Event{Type: "done"}
```

4. Return and close the event channel.

## LLM StreamChat

`Client.StreamChat` owns retry around `streamOnce`.

It always closes its `events` channel with `defer close(events)`.

Retryable errors are matched by substring against lower-cased error text. The
retry backoff sequence is exactly:

```text
1s, 2s, 4s, 8s, 16s, 32s, 64s
```

Before each retry attempt after the first, StreamChat emits this text chunk:

```text


🦍 *retrying after server error… waiting <delay>* 🦍
```

That string currently contains non-ASCII glyphs and an ellipsis. In the TUI it
is rendered as normal assistant text. In headless mode it is written to stdout.

If the retry sleep is cancelled, StreamChat emits:

```go
StreamEvent{Type: "error", Error: ctx.Err()}
```

If `streamOnce` returns a non-retryable error, StreamChat emits:

```go
StreamEvent{Type: "error", Error: err}
```

If all retries fail, it emits the last error the same way.

## streamOnce

`streamOnce` performs one fantasy agent call:

1. `ensureModel(ctx)` lazily builds or returns a cached fantasy language model.
2. `splitForFantasy(msgs)` returns:
   - `prompt`: tail user message content if the transcript ends in a user turn.
   - `prior`: every non-system message except the tail user prompt.
   - `systemPrompt`: all system message contents joined with blank lines.
3. Snapshot `Service` and `BootstrapMsgCount` under the client mutex.
4. Compute `cacheBoundaryIdx := bootstrapPreparedIndex(msgs, cacheBootstrap)`.
5. Create a per-turn `ToolContextManager`.
6. Store that manager on the client so the agent/UI can cancel one active tool.
7. Defer:
   - cancel all active tool contexts;
   - clear `c.toolCtx`.
8. Build fantasy options:
   - `fantasy.WithStopConditions(fantasy.StepCountIs(64))`;
   - `fantasy.WithSystemPrompt(systemPrompt)` when non-empty;
   - local and MCP tools when `reg != nil`.
9. Create `fantasy.NewAgent(model, opts...)`.
10. Call `fa.Stream` with callbacks.

The max step count is exactly `64`. This is a stop condition for one full
fantasy stream call, not a UI-level turn count.

### PrepareStep

Before every fantasy step, `PrepareStep`:

1. Emits:

```go
StreamEvent{Type: "thinking"}
```

This event has empty `Text`. The TUI treats it as a thinking event and can
replace the placeholder content with an empty string; later reasoning chunks
append to that same thinking message.

2. Copies `stepOpts.Messages` into local `msgs`.
3. If `c.promptDrain` is non-nil, call it.
4. For every drained text, append:

```go
fantasy.NewUserMessage(text)
```

to the prepared messages.

5. Apply Anthropic prompt-cache markers when the service and bootstrap boundary
   qualify.
6. Return `fantasy.PrepareStepResult{Messages: msgs}`.

`promptDrain` is installed by the agent for the duration of a turn. It calls
`Agent.drainAndMirrorQueuedPrompts`, which appends queued prompts to the
canonical transcript as:

```text
[queued prompt 1]: <text>
[queued prompt 2]: <text>
```

and returns the raw queued text values to `PrepareStep`.

Important wiring note: the TUI's `Model.queued` path does not call
`Agent.QueuePrompt`; it batches queued messages after `agentDoneMsg`. The
mid-turn prompt drain exists at the agent/client layer, but no TUI key path is
currently wired to feed it. Tests mostly exercise the post-turn UI queue, not
the client `PrepareStep` drain end to end.

### Fantasy Callbacks

`OnTextDelta` emits:

```go
StreamEvent{Type: "text", Text: text}
```

`OnReasoningDelta` emits:

```go
StreamEvent{Type: "thinking", Text: text}
```

`OnToolCall` emits:

```go
StreamEvent{
    Type:       "tool_call",
    ToolName:   call.ToolName,
    ToolArgs:   call.Input,
    ToolCallID: call.ToolCallID,
}
```

`OnToolResult` emits:

```go
StreamEvent{
    Type:       "tool_result",
    ToolCallID: res.ToolCallID,
    ToolName:   res.ToolName,
    Text:       toolResultText(res.Result),
}
```

`toolResultText` only returns text for `fantasy.ToolResultOutputContentText`
or its pointer form. Non-text tool result content becomes an empty string at
this event layer.

`OnStreamFinish` converts fantasy usage and emits:

```go
StreamEvent{Type: "usage", Usage: &usage}
```

`OnError` stores the error in `streamErr`. It does not emit by itself.

After `fa.Stream` returns:

1. If `streamErr != nil`, return it to the retry layer.
2. If `err != nil`, return it to the retry layer.
3. Rebuild `[]llm.Message` from every message in every result step.
4. Emit:

```go
StreamEvent{Type: "done", Messages: rebuilt}
```

## Message Conversion

`splitForFantasy`:

- Extracts the tail user message into `prompt` only when the last recognized
  role in the transcript is `user`.
- Drops system messages from `prior` and concatenates their content into the
  system prompt.
- Converts user messages to fantasy user text messages.
- Converts assistant `Content` to a text part and every legacy `ToolCall` to a
  `ToolCallPart`.
- Converts tool messages to `ToolResultPart` with text output.

If the transcript ends with assistant or tool, no prompt is extracted and all
messages stay in prior.

`fantasyToLLM`:

- Concatenates text parts into `Message.Content`.
- Converts assistant tool call parts into legacy `ToolCalls`.
- Converts tool result parts into `ToolCallID` plus text content.
- Drops unsupported fantasy parts from the legacy projection.

`LLMToFantasy`:

- `tool` becomes one `ToolResultPart`.
- `assistant` becomes optional text part plus one `ToolCallPart` per legacy
  tool call.
- all other roles become one text part if content is non-empty.

Known lossy fields:

- Reasoning parts have no legacy representation.
- File/media/source parts have no legacy representation.
- Provider options are not preserved.
- Tool result output is restored only as text.

## Tool Surface

`streamOnce` assembles tools on every turn when `reg != nil`.

Local tools are built first. MCP tools are appended after local tools. Local
names win if anything collides.

Typed local wrappers currently exist for:

- `read`
- `write`
- `edit`
- `bash`
- `search_memory`
- `write_memory`

These wrappers own schema, JSON decoding, and dispatch, but they still bottom
out in registry behavior for the actual local operation.

Generic compatibility adapter still handles:

- `terminal_start`
- `terminal_send`
- `terminal_keys`
- `terminal_snapshot`
- `terminal_wait`
- `terminal_resize`
- `terminal_close`
- `preview_image`

Do not add new tools through the generic adapter. New local tools should get a
typed wrapper and be wired into `typedToolFor`.

Tool adapter failure contract:

- Generic local adapter calls `Registry.Execute(ctx, call.Name, call.Input)`.
- If registry returns an error, the adapter returns
  `fantasy.NewTextErrorResponse("Error: <err>"), nil`.
- MCP adapter does the same for manager errors and MCP error results.
- Returning a Go error from `Run` aborts the whole fantasy stream, so local and
  MCP adapters avoid that for normal tool failures.

Per-tool cancellation:

- `streamOnce` creates one `ToolContextManager` per turn.
- `wrapToolsWithContext` wraps every tool.
- Each tool call gets a child context keyed by fantasy's tool call ID.
- `Agent.CancelTool(id)` calls through to `Client.ToolContextManager`.
- Cancelling a tool cancels only that child context, not the turn context.
- At turn end, all remaining tool contexts are cancelled and the manager is
  cleared.

`Registry.Execute` checks `ctx.Err()` before dispatch so even tools that do
not poll context internally can be stopped before starting. Long-running tools
must still use the context internally to stop promptly once already running.

## Visible TUI Output

Visible chat messages are formatted by `ChatMessage.Format`.

Timestamps are dynamic and always use 24-hour `HH:MM`.

Visible user message format:

```text
 [HH:MM] <userNick> <content>
```

Visible assistant message format:

```text
 [HH:MM] <agentNick> <markdown-rendered content>
```

Visible system message format:

```text
 [HH:MM] *** <content>
```

Visible error message format:

```text
 [HH:MM] !!! <content>
```

Visible tool message format:

```text
 [HH:MM]   -> <toolName>: <content>
```

The actual arrow is the Unicode right arrow used in the source string:
`"  → %s:"`.

Visible thinking message format:

```text
 [HH:MM] <thinking-icon> <content>
```

The thinking icon is the Unicode thought balloon used in the source string.

The exact terminal bytes include Lipgloss styling escape sequences depending
on theme and terminal capabilities. The structural text above is the stable
content contract.

### TUI Event Rendering

Agent `state` with `StateThinking`:

- resets the stream buffer;
- appends visible thinking placeholder:

```text
thinking...
```

Agent `thinking`:

- if the last visible message is `MsgThink` and its content is exactly
  `thinking...`, replace that content with `ev.Text`;
- otherwise append `ev.Text` to the current thinking message;
- if there is no current thinking message, append a new one.

Because `PrepareStep` emits a `thinking` event with empty text, the placeholder
can become an empty thinking message before real reasoning arrives.

Agent `text`:

- if the last visible message is `MsgThink`, replace it with an empty
  `MsgAgent` preserving the timestamp;
- append the text chunk to `streamBuffer`;
- update the last agent message in place;
- append the chunk to the transcript logger.

Agent `tool_start`:

- set `activeToolName` and `activeToolCallID`;
- append:

```text
calling <toolName>...
```

as a `MsgTool` whose nick is the tool name;
- call `toolPanel.StartTool(toolName)`.

Agent `tool_result`:

- clear active tool fields if the result tool name matches;
- truncate visible result to the first 20 lines, adding:

```text
... (<N> more lines)
```

when truncated;
- append a `MsgTool` unless `ToolError` is non-nil;
- call `toolPanel.FinishTool`.

Agent `error`:

- clear active tool fields;
- append:

```text
Error: <ev.Error>
```

- if `llm.ErrorHint(ev.Error)` returns text, append:

```text
  hint: <hint>
```

Agent `done`:

- return a Bubble Tea command that produces `agentDoneMsg` for the same channel.

## agentDoneMsg

`agentDoneMsg` is the TUI's hard post-turn boundary.

If stale, it is ignored.

If current, it:

1. Finishes any transcript streaming message.
2. Sets:

```go
streaming = false
agentState = StateIdle
eventCh = nil
cancel = nil
activeToolName = ""
activeToolCallID = ""
escStage = 0
ctrlCStage = 0
```

3. Syncs the last visible assistant message from
   `agent.LastAssistantDisplayContent()`.
4. Plays a notification sound if enabled.
5. Updates tool panel token and elapsed counters.
6. Appends new agent transcript messages to the JSONL session, stamping the
   context captured at turn start.
7. Saves focus state.
8. Saves a checkpoint containing turn count, tool call stats, and model.
9. Processes queued user messages.
10. Starts autonomous follow-up if configured.

Session append uses per-context saved watermarks:

```go
savedIdx := m.agent.SavedIdx(ctxKey)
for i := savedIdx; i < len(msgs); i++ { append entry }
m.agent.SetSavedIdx(ctxKey, len(msgs))
```

Bootstrap messages are marked using:

```go
i < m.agent.BootstrapMessageCount()
```

## Queued Input

There are two queue systems.

### UI Queue

When the user presses Enter while `m.streaming` is true:

1. The input is appended to `m.queued` with `queuedAt = time.Now()`.
2. Visible system output is exactly:

```text
Queued message (agent is busy): <input>
```

3. Nothing is sent to the LLM yet.

After `agentDoneMsg`, if the oldest queued message is older than
`2 * time.Minute`, all queued messages are discarded. Visible output:

```text
Discarded <N> queued message(s) older than 2m0s — context changed. Re-send if still relevant.
```

That string contains a Unicode em dash in the source.

If the queue is fresh, all messages are batched into one new user turn:

```text
[queued msg 1]: <text>
[queued msg 2]: <text>
```

The TUI appends that combined text as a visible `MsgUser`, then starts a new
agent turn with exactly that combined string.

Pressing Up with an empty input and queued messages removes the last queued
message, puts it back in the textarea, and shows:

```text
Unqueued message: <input>
```

`/msg <nick> <text>` while streaming appends this queued text:

```text
[to:<nick>] <text>
```

and shows:

```text
Queued /msg to <nick> (agent busy).
```

### Agent Prompt Drain Queue

`Agent.QueuePrompt(text)` appends to `Agent.promptQueue`. During fantasy
`PrepareStep`, the client prompt drain can pull those prompts mid-turn.

When drained, the agent transcript receives:

```text
[queued prompt 1]: <text>
```

but fantasy receives raw `text` as `fantasy.NewUserMessage(text)`.

Current caveat: the normal TUI queue does not call `Agent.QueuePrompt`, so this
mid-turn drain path is not wired to user keystrokes. It is infrastructure for a
different steering path.

## Cancellation

### Esc

Esc behavior is stateful and time-windowed. The window is exactly `1500ms`.

Esc first closes open panels without counting toward cancellation:

- If tool panel visible: show `Tool panel closed.`
- Else if MP3 panel visible: show `MP3 panel closed.`

If `queueClearArmed` is true and queued messages exist, Esc clears them and
shows:

```text
Cleared <N> queued message(s).
```

If not streaming, Esc resets the Esc ladder and does nothing visible.

During a turn:

- First meaningful Esc with an active tool calls
  `agent.CancelTool(activeToolCallID)`.
- On success it shows:

```text
Cancelled <activeToolName>.
```

- On failure it shows:

```text
Could not cancel <activeToolName>: <err>
```

and the turn remains alive either way.

First meaningful Esc without an active tool shows:

```text
Press Esc again to cancel the turn.
```

Second meaningful Esc cancels the turn and shows:

```text
Interrupted by Esc.
```

Queued messages are preserved; if any exist, `queueClearArmed` is set so a
subsequent Esc clears them.

### Ctrl+C

Ctrl+C behavior uses a `3s` graduation window.

First Ctrl+C while streaming cancels the active turn. If no queued messages
exist, visible output is exactly:

```text
Interrupted. Press Ctrl+C again to clear queued messages; press it a third time to quit.
```

If queued messages exist:

```text
Interrupted. <N> queued message(s) remain. Press Ctrl+C again to clear them; press it a third time to quit.
```

First Ctrl+C while idle with no queue:

```text
Press Ctrl+C twice more to quit.
```

First Ctrl+C while idle with queued messages:

```text
<N> queued message(s) remain. Press Ctrl+C again to clear them; press it a third time to quit.
```

Second Ctrl+C with queued messages:

```text
Cleared <N> queued message(s). Press Ctrl+C again to quit.
```

Second Ctrl+C without queued messages:

```text
Press Ctrl+C again to quit.
```

Third Ctrl+C cancels any active turn, clears queued messages, and returns
`tea.Quit`.

### OS Signal

`SignalMsg` while streaming calls:

```go
cancelActiveTurn("Interrupted by signal.", true)
```

This cancels the turn and clears queued messages.

`SignalMsg` while idle quits.

`tea.QuitMsg` cancels any active stream, finishes transcript logging, clears
queued messages, stops MP3, closes transcript, and quits.

## Headless Output

Headless mode is implemented in `runHeadlessLoop`.

For each turn it creates an agent event channel and starts either
`SendMessage` or `SendFollowUp`.

On autonomous follow-up, before starting the follow-up stream, stderr receives:

```text
[auto] <followUp.Label>
```

Agent `text` writes exactly `ev.Text` to stdout with no prefix.

Agent `tool_start` writes to stderr:

```text
[tool] <ToolName> args=<ToolArgs truncated to 200 bytes with newlines escaped>
```

Agent `tool_result` with nil `ToolError` writes to stderr:

```text
[tool] <ToolName> result=<ToolResult truncated to 200 bytes with newlines escaped>
```

Agent `tool_result` with non-nil `ToolError` writes to stderr:

```text
[tool] <ToolName> error=<ToolError> result=<ToolResult truncated to 200 bytes with newlines escaped>
```

Agent `state` writes to stderr:

```text
[status] thinking
[status] tool_call
[status] idle
```

The label is selected by `headlessStateLabel`. Unknown states render as
`idle`.

Agent `error` returns the error from `runHeadlessLoop`.

Agent `done` has no direct output.

After each turn, headless checks `MaybeQueueFollowUp`. If a follow-up will run
and stdout did not end in a newline, it writes one newline first.

When all turns are done, if stdout still did not end in a newline, it writes a
trailing newline.

## Autonomous Follow-Ups

`MaybeQueueFollowUp` only returns a request when:

- the previous turn state is `turnStateCompleted`;
- at least one of `AutoNextSteps` or `AutoNextIdea` is enabled.

Canceled and errored turns never auto-continue.

`AutoNextPrompt()` is exactly:

```text
What are the next steps? If there is remaining work, do it now instead of just describing it, including tests or verification when they matter. If everything is done, start your response with AUTONEXT_DONE and briefly say why.
```

`AutoIdeaPrompt()` is exactly:

```text
Based on what you've done so far, pick the next highest-impact improvement and implement it now. If there is nothing worthwhile left to improve, start your response with AUTOIDEA_DONE and briefly say why.
```

Follow-up routing:

- After a normal user turn, prefer auto-next-steps if enabled.
- If steps were running and the last raw assistant text starts with
  `AUTONEXT_DONE`, switch to auto-next-idea if enabled.
- If idea was running and the last raw assistant text starts with
  `AUTOIDEA_DONE`, stop.
- If idea was running but did not mark done, return to auto-next-steps.

Control-token detection uppercases the trimmed assistant text and requires the
token at the start.

Display sanitization strips leading `AUTONEXT_DONE` or `AUTOIDEA_DONE` from
follow-up streamed text and from assistant messages spliced from
`ev.Messages`. If the token is the whole content, the stored display text
becomes:

```text
Done.
```

## Compaction

`Agent.Compact(ctx)` is not a normal user turn, but it uses the same streamer
interface.

If `len(a.messages) < 6`, it returns nil and does nothing.

Otherwise:

1. Check context cancellation.
2. Set `end := len(a.messages) - 4`.
3. Flush messages `a.messages[1:end]` to scoped daily memory by asking the
   streamer to extract durable memory.
4. Ask the streamer for a compact summary of those same middle messages.
5. Replace history with:

```text
system prompt
user: [Previous conversation summary]:
<summary>
assistant: Got it, I have the context from the summary.
<last 4 original messages>
```

The durable-memory extraction prompt asks the model to reply exactly `NONE` if
nothing should be saved. Empty output and case-insensitive `none` skip memory
append.

The TUI `/compact` command refuses while streaming and shows:

```text
Can't compact while agent is working. Be patient.
```

On success it shows:

```text
Compacted: ~<before> -> ~<after> tokens
```

On failure it shows:

```text
Compaction failed: <err>
```

## Commands That Affect Turn Boundaries

`/set model <name>`:

- Calls `agent.SetModel`.
- Invalidates the cached provider/model through the client.
- Visible output:

```text
*** Model switched to: <name>
```

`/set provider <provider>`:

- Calls `agent.SetProvider`.
- Invalidates the cached provider/model.
- Visible output:

```text
*** Provider set to: <provider>
  requests -> <endpoint preview>
```

May also output a transport warning.

`/set baseurl <url>`:

- Calls `agent.SetBaseURL`.
- Invalidates the cached provider/model.
- Visible output:

```text
*** Base URL set to: <url>
  requests -> <endpoint preview>
```

May also output a transport warning.

`/set apikey <key>`:

- Calls `agent.SetAPIKey`.
- Invalidates the cached provider/model.
- Visible output:

```text
*** API key set: <masked key>
```

`/set service <value>`:

- Updates `m.config.Service`.
- Current caveat: it does not call `agent.SetService`, so the underlying
  client service identity is not updated for subsequent turns unless some
  other path rebuilds or sets it. This means service-gated behavior such as
  Anthropic cache marker placement can be stale after `/set service`.
- Visible output:

```text
Service set to: <value>
```

`/profile load <name>` and bare `/profile <name>`:

- Apply profile fields to config.
- Call agent setters for model, base URL, API key, and provider.
- Current caveat: profile application also does not call `agent.SetService`,
  even though profile output displays service. The agent client can continue
  using its previous service identity.

`/join <#channel>`:

- Changes UI focus and saves focus.
- Does not immediately switch the agent transcript; the switch happens at the
  next `startAgentTurn`.
- Visible output:

```text
Joined <ctx.Label()>
```

`/query <persona>`:

- Changes UI focus to a direct context and saves focus.
- Visible output:

```text
Query open: <ctx.Label()>
```

`/msg <nick> <text>`:

- If idle, sends a normal turn with agent input:

```text
[to:<nick>] <text>
```

- Visible user message:

```text
→<nick>: <text>
```

- If streaming, queues `[to:<nick>] <text>` as described above.

`/restart`:

- If streaming, cancels the active turn with visible output `Restart`.
- Calls `agent.Reset`.
- Clears visible messages, queue, stream buffer.
- Opens a fresh session if possible.
- Visible output:

```text
*** Conversation restarted. Fresh context.
```

`/resume <number>`:

- If streaming, cancels active turn with visible output `Session resume`.
- Loads session entries and calls `ResumeSession`.
- Visible output:

```text
Resumed session <number>: <basename>
```

`/debug on`:

- Installs a debug hook and invalidates the provider cache.
- Visible output:

```text
Debug mode: ON
```

Each captured request adds a visible system message:

```text
[DEBUG] <METHOD> <URL>
Request Headers: <headers>
Request Body: <body>
Response Status: <status>
```

Because this hook captures `m` by value inside `handleDebugCommand`, UI updates
from async HTTP debug callbacks are suspect: they mutate the captured model
copy, not necessarily the live Bubble Tea model. Treat `/debug on` visible
streaming as not fully wired until proven with an integration test.

`/debug off`:

- Clears the debug hook.
- Visible output:

```text
Debug mode: OFF
```

## Things Not Fully Wired Or Risky

`/set service` and profile service propagation are incomplete. `Agent.SetService`
exists and forwards to `Client.SetService`, but UI service changes do not call
it. Anything gated by `Client.Service` can use stale identity after a service
switch.

The mid-turn `Agent.QueuePrompt` / `Client.SetPromptDrain` path exists, but the
normal TUI queue does not feed it. User typing while the agent works is drained
only after the current turn's `agentDoneMsg`, not during `PrepareStep`.

`PrepareStep` emits a `thinking` event with empty text. The TUI can replace
`thinking...` with an empty thinking message. This is harmless visually but
means reasoning display tests should cover the empty first marker.

Tool errors are represented as successful fantasy tool responses containing
error text. This preserves the LLM loop, but the UI generally cannot style
tool failures as `MsgError` because `Event.ToolError` is not set.

`tool_result` display matches tools by name, not call ID, in the tool panel.
`FinishTool` marks the most recent running tool with that name. That is OK for
serial tools and mostly OK for repeated tool names, but it is not a strict
tool-call-ID accounting model.

`ToolContextManager.NewToolContext` stores cancel funcs by tool call ID. If two
simultaneous calls reused an ID, the later one would overwrite the earlier
cancel func. Fantasy should provide unique IDs; the manager assumes that.

`Compact` reads all stream events but only pays attention to `text`; it ignores
stream `error` events. If a streamer reports an error event and closes without
returning a Go error, compaction may proceed with an empty or partial summary.

`Client.StreamChat` retry text is emitted as assistant `text`. That means retry
notices become part of visible assistant output and `textAccum`, and can be
stored as assistant text if the eventual turn succeeds without rebuilt
assistant messages excluding it.

`LastAssistantDisplayContent` reads from stored messages, not the stream buffer.
On `agentDoneMsg`, it can replace the last visible assistant message with the
sanitized stored assistant content. This is intended for follow-up tokens but
can surprise tests that assert the exact accumulated stream text when
`ev.Messages` differs from streamed chunks.

The TUI debug hook captures and mutates a model copy. It is not a reliable
Bubble Tea message path.

`llm.StreamEvent{Type:"done"}` does not mean the TUI turn is finished. Tests
that stop as soon as they see the LLM `done` event are testing the stream
shape, not the UI/session boundary. The real boundary is the later
`agent.Event{Type:"done"}` plus `agentDoneMsg` cleanup.

## Test Coverage Reality

Strong tests:

- `TestSendMessageExitsOnContextCancel` verifies the agent does not hang on a
  cancelled context when the streamer never closes.
- `TestSendMessageEmitsDoneAfterStreamError` verifies error turns emit idle,
  error, and done.
- `TestPhase2TypedToolSmoke_ReadFileEndToEnd` uses a real `llm.Client`, fake
  fantasy language model, real workspace file, typed read wrapper, registry
  execution, tool result, and final assistant text. This is an actual
  end-to-end failure-mode smoke, not just shape testing.
- `TestPhase3ToolTranscriptSurvivesTurn` and
  `TestPhase3FollowUpSanitizationStripsDoneTokenInFantasyParts` pin transcript
  splicing and sanitization.
- UI stale channel tests verify old events and old done messages cannot close
  or drain the new active turn.
- Esc/Ctrl+C tests cover the cancel ladders, stale done after cancel, queue
  preservation, and rapid/interleaved keypress no-panic cases.

Shape-heavy or weaker tests:

- `TestSendMessageExecutesToolCallWithoutNetwork` does not execute a real tool;
  the fake streamer emits both `tool_call` and `tool_result`. It verifies agent
  event forwarding, not tool execution.
- `TestSendMessageWithInjectedStreamer` verifies streaming text and fallback
  token accounting with a fake event source. It is useful but does not cover
  provider, fantasy, or tools.
- `TestSendMessageForwardsThinkingEvents` verifies forwarding/concatenation,
  not provider reasoning semantics.
- `TestSystemPromptIncludesLiveToolDefinitions` checks prompt text contains
  tool names and guidance. It does not prove schemas sent to providers are
  accepted.
- `TestAgentDoneWritesCheckpointInsteadOfMemoryFile` verifies checkpoint write
  and absence of root `MEMORY.md` writes, but not session append correctness.
- Rapid Esc/Ctrl+C interleave tests are no-panic tests; they intentionally do
  not assert a final semantic state.
- Many command tests assert visible strings and config mutation. They do not
  necessarily prove the underlying client cache/service state is updated.

Missing high-value tests:

- `/set service` and profile load should assert `Client.Service` changes before
  the next turn.
- Mid-turn `Agent.QueuePrompt` should be exercised through a real
  `PrepareStep` with a fake fantasy model that takes multiple steps.
- Tool cancellation should be covered with a real long-running registry tool
  path and a real `CancelTool` call by tool call ID.
- UI tool result error styling should be tested after deciding whether
  `ToolError` should ever be populated.
- Compaction should fail loudly when the streamer emits an `error` event.
- Debug hook UI should be routed through Bubble Tea messages or documented as
  best-effort logging only, then tested accordingly.
