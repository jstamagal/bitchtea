# Signals and Keys

This document is generated from the TUI implementation, primarily
`internal/ui/model.go`, plus the picker, MP3, and tool-context handlers named
below. Do not treat key behavior as terminal folklore; the Bubble Tea update
loop is the source of truth.

Source files used for this pass: `internal/ui/model.go`,
`internal/ui/signal_test.go`, `internal/ui/model_picker_keys.go`,
`internal/ui/mp3.go`, `internal/llm/tool_context.go`,
`internal/llm/stream.go`, `internal/llm/tools.go`, and typed tool wrappers
under `internal/llm/typed_*.go`.

## 1. Signal Table

| Signal/message | Source | Behavior |
| --- | --- | --- |
| `SIGINT` | `main.go` registers it with `signal.Notify` and sends `ui.SignalMsg`. | If an agent turn is streaming and `m.cancel` is set, the UI calls `cancelActiveTurn("Interrupted by signal.", true)`, which cancels the turn and clears queued steering messages. Otherwise it returns `tea.Quit`. |
| `SIGTERM` | Same path as `SIGINT`: `signal.Notify` -> `ui.SignalMsg`. | Same behavior as `SIGINT`: active turns get `"Interrupted by signal."` and queue clearing; idle state quits. |
| `SIGWINCH` | Bubble Tea converts resize events into `tea.WindowSizeMsg`; it is not handled through `SignalMsg`. | `Update` stores width/height, resizes the textarea, recalculates viewport height, and narrows the viewport when the MP3 or tool side panel is visible and there is enough width. |
| `SIGTSTP` / Ctrl+Z | Bubble Tea emits `tea.SuspendMsg`. | `Update` returns `tea.Suspend`; `internal/ui/signal_test.go` asserts that running that command produces a `tea.SuspendMsg` and leaves the model non-streaming. |
| `tea.QuitMsg` | Bubble Tea quit lifecycle, including interrupt-driven quits. | If streaming, it calls `m.cancel()`, finishes the transcript agent message, marks streaming false, clears queued messages, stops MP3 playback, closes the transcript, and returns `tea.Quit`. |

## 2. Esc: Three-stage Ladder, 1.5s Window

The Esc ladder uses `escGraduationWindow = 1500 * time.Millisecond`. If more
than 1.5s passes after the previous Esc, `escStage` resets to 0 and
`queueClearArmed` resets to false.

Panel close happens before the ladder and does not count as a ladder stage:

- Visible tool panel: close it and emit `"Tool panel closed."`
- Visible MP3 panel: close it and emit `"MP3 panel closed."`

After panels are closed, Esc behavior is:

| Press | Condition | Behavior and exact message |
| --- | --- | --- |
| 1 | Streaming with an active tool name. | Calls `m.agent.CancelTool(m.activeToolCallID)`. On success, emits `"Cancelled %s."` with the tool name and resets `escStage` to 0. On failure, emits `"Could not cancel %s: %v"` and also resets `escStage` to 0. The turn stays alive. |
| 1 | Streaming, no active tool. | Leaves the turn running and emits `"Press Esc again to cancel the turn."` |
| 2 | Streaming. | Calls `cancelActiveTurnWithQueueArm("Interrupted by Esc.")`. The turn is cancelled, queued messages are preserved, and `queueClearArmed` is set only if queued messages exist. |
| 3 | Queue clear is armed and queued messages exist. | Clears the queue and emits `"Cleared %d queued message(s)."` |

If not streaming, Esc resets `escStage` and `queueClearArmed` and emits no
message.

## 3. Ctrl+C: Three-stage Ladder, 3s Window

The Ctrl+C ladder uses `ctrlCGraduationWindow = 3 * time.Second`. If more than
3s passes after the previous Ctrl+C, `ctrlCStage` resets to 0 before the new
press is counted.

Ctrl+C is separate from Esc: it does not cancel a single active tool and it
does not arm Esc-style queue clearing.

| Press | Condition | Behavior and exact message |
| --- | --- | --- |
| 1 | Streaming, no queued messages. | Cancels the turn, preserves the empty queue, and emits `"Interrupted. Press Ctrl+C again to clear queued messages; press it a third time to quit."` |
| 1 | Streaming with queued messages. | Cancels the turn, preserves queued messages, and emits `"Interrupted. %d queued message(s) remain. Press Ctrl+C again to clear them; press it a third time to quit."` |
| 1 | Idle with queued messages. | Leaves the app running and emits `"%d queued message(s) remain. Press Ctrl+C again to clear them; press it a third time to quit."` |
| 1 | Idle with no queue. | Leaves the app running and emits `"Press Ctrl+C twice more to quit."` |
| 2 | Queued messages exist. | Clears them and emits `"Cleared %d queued message(s). Press Ctrl+C again to quit."` |
| 2 | No queued messages. | Leaves the app running and emits `"Press Ctrl+C again to quit."` |
| 3 | Any state within the 3s window. | If streaming, cancels the turn with `"Interrupted."` and clears the queue. If idle with a queue, clears the queue. Then returns `tea.Quit`. |

## 4. Tool Cancellation Chain

The Esc tool-only path is wired through these layers:

1. `handleEscKey` checks `m.activeToolName != ""` and calls
   `m.agent.CancelTool(m.activeToolCallID)`.
2. `Agent.CancelTool` fetches the current `ToolContextManager` from the LLM
   client. If none exists, it returns `"no active turn"`.
3. `Client.streamOnce` creates `NewToolContextManager(ctx)` for the turn,
   stores it on the client, and wraps all assembled tools with
   `wrapToolsWithContext`.
4. `toolContextWrapper.Run` calls `NewToolContext(call.ID)` and passes that
   child context into the real tool `Run`.
5. `ToolContextManager.CancelTool` looks up the tool call ID and invokes that
   context's cancel function. If the ID is missing, it returns
   `"no active tool with id %s"`.
6. Tool wrappers surface cancellation as model-visible tool responses, not Go
   errors that abort the fantasy stream. The generic adapter returns
   `fantasy.NewTextErrorResponse(fmt.Sprintf("Error: %v", err))`; typed tools
   use the same `"Error: %v"` shape for cancelled contexts.
7. Fantasy emits the resulting tool result through `OnToolResult`, which the UI
   receives as a `tool_result` event while the overall turn can continue.

Turn cancellation is different: `cancelActiveTurn` invokes the turn-level
`m.cancel()`, finishes the transcript agent message, marks the agent idle,
clears active tool IDs, resets Esc state, optionally clears queued messages,
and emits the supplied system message. Since every tool context is a child of
the turn context, Esc x2 and streaming Ctrl+C cancel active tools through the
parent context rather than through `Agent.CancelTool`. When `streamOnce`
unwinds, its deferred `toolCtxMgr.CancelAll()` cancels and clears any remaining
tool child contexts. See `docs/agent-loop.md` section `Per-Tool Cancellation`
for the full manager lifecycle.

## 5. Picker-mode Key Overrides

When `m.picker != nil`, `Update` routes every key to `handlePickerKey` before
the textarea, history, Esc ladder, Ctrl+C ladder, MP3 controls, or slash router.
Unhandled picker keys are dropped silently.

Model picker keys:

| Key | Behavior |
| --- | --- |
| Enter | Selects `m.picker.selected()`, closes the picker, and calls the selection callback. If no choice exists, emits `"Picker: no selection (filter excludes everything)."` |
| Esc or Ctrl+C | Closes the picker and emits `"Picker cancelled."` |
| Up / Down | Moves the picker cursor by one row and rerenders the picker message. |
| PgUp / PgDown | Moves by `pickerVisibleRows` rows and rerenders. |
| Backspace | Removes one rune from the filter and rerenders. |
| Printable runes, including Space | Appends to the filter and rerenders. |

MP3 keys are handled only when the MP3 panel is visible and the textarea is
empty. They run after the main key switch, so picker mode and the Esc/Ctrl+C
ladders win first.

| Key | Behavior and exact messages from MP3 control paths |
| --- | --- |
| Space | Toggles pause/resume. Possible fixed messages include `"No MP3 track is playing."`; dynamic success/failure messages are `"Paused: %s"`, `"Resumed: %s"`, `"Pause failed: %v"`, and `"Resume failed: %v"`. |
| Left or `j` | Previous track via `m.mp3.prev()`. |
| Right or `k` | Next track via `m.mp3.next()`. |

MP3 status strings that contain `"failed"` or start with `"No MP3s"` are
rendered as error messages; other MP3 statuses are rendered as system messages.

### Model picker overlay precedence

The `/models` picker is the highest-priority key consumer while open. The
`Update(tea.KeyMsg)` path checks `m.picker != nil` before history, textarea,
Esc, Ctrl+C, slash command, and MP3 handling, then routes the key to
`handlePickerKey` and returns immediately.

Practical consequences:

- Esc and Ctrl+C close the picker with `"Picker cancelled."`; they do not reach
  the Esc or Ctrl+C cancellation ladders while the picker is open.
- Up, Down, PgUp, and PgDown move the picker cursor or page window; they do
  not recall input history or scroll the chat viewport.
- Backspace, printable runes, and Space edit the picker filter; they do not
  edit the textarea.
- Enter selects the highlighted model through the picker callback; it does not
  submit the textarea or run a slash command.
- Unhandled keys are dropped silently until the picker closes.

## 6. Suspend/resume Lifecycle

Ctrl+Z reaches the model as `tea.SuspendMsg`. The handler is deliberately
small:

```go
case tea.SuspendMsg:
    return m, tea.Suspend
```

Bubble Tea owns terminal restoration and resume. The regression test
`TestSuspendMsgHandling` verifies that the command returned by the handler is
non-nil, that executing it returns a `tea.SuspendMsg`, and that the model is
not placed into streaming state by suspend handling.

## 7. Textarea Line-edit Keys

After the app-level key handlers run, all unhandled messages go through
`m.input.Update(msg)`, so Bubble Tea's textarea owns normal line editing,
cursor movement, deletion, and newline insertion.

App-level overrides before textarea editing:

- Enter submits trimmed input. Empty input is ignored. If the agent is busy,
  the text is queued and the UI emits `"Queued message (agent is busy): %s"`.
  Otherwise the message is sent to the agent. The code comment is explicit:
  `Enter sends; Shift+Enter / Alt+Enter adds newline`.
- Up with an empty textarea and queued messages pops the newest queued message
  into the input and emits `"Unqueued message: %s"`.
- Up on the first textarea line navigates backward through input history.
- Down on the last textarea line navigates forward through input history or
  clears the input after the newest history item.
- Ctrl+P and Ctrl+N always navigate history.
- PgUp and PgDown scroll the chat viewport.
- Ctrl+T toggles the tool panel.
- Esc and Ctrl+C are intercepted by their ladders and do not reach the
  textarea.

The textarea is initialized with placeholder `"type something, coward..."`, a
character limit of 8192, width 80, height 3, prompt `">> "`, focused state, and
line numbers disabled.

## 8. Design rationale

Originally documented in `archive/phase-8-cancellation-state.md` (archived).

**The Model owns both cancellers (turn and per-tool).** The Model is the
single Bubble Tea actor that receives key events. Pushing the per-tool
canceller into the Agent would require a key->Agent channel, which adds
latency and a sync point we do not need. Two scopes, two cancellers, both
on `*Model`: `m.cancel` (turn) and the lookup that backs
`m.agent.CancelTool(m.activeToolCallID)` (single in-flight tool). The
per-tool path lives on the LLM client's `ToolContextManager` precisely so
the Model can reach it through `Agent.CancelTool` without owning the map
itself.

**Ctrl+C is intentionally blunter than Esc.** Ctrl+C never does tool-only
cancel — it only goes to turn-cancel, queue-clear, quit. Two reasons. (1)
Ctrl+C is the universal "kill what is happening" key; users hitting it
during a runaway tool expect the *turn* to die, not the tool to be silently
swapped for a synthetic result and the model to keep going. (2) Ctrl+C
already serves the third escalation step (quit), so giving it a separate
tool-only stage would either dilute that meaning or require a four-press
ladder.

**Panel close has priority over both ladders.** Esc with the tool panel or
MP3 panel open closes the panel without consuming a stage. The user
intuition for Esc-with-overlay is "dismiss the overlay"; consuming a ladder
stage would make the next deliberate Esc behave one stage further along
than the user expects.

**Esc x3 / Ctrl+C-third-press graduate within a window.** The ladders
reset (1.5s for Esc, 3s for Ctrl+C) so a user who bumps the key once
during a long stream is not one keypress away from clearing their queue
or quitting on their next intentional press. Different windows because
Ctrl+C carries a quit risk that Esc does not — a slower graduation gives
more chance to abort.

**Cancellation propagation goes through context, not custom signaling.**
Every tool context is a child of the turn context, so Esc x2 / streaming
Ctrl+C cancel active tools through the parent context rather than
through `Agent.CancelTool`. A separate code path for the
"turn-cancel-also-cancels-tools" case would duplicate the wrap-and-track
logic that already exists; deriving from the parent ctx gives it for free.

**The synthetic tool result must be a `fantasy.ToolResponse`, not a Go
error.** This is the root cause of `bt-s2z`. Returning an error from
`bitchteaTool.Run` aborts the whole fantasy stream — fantasy treats it as
"the model machinery is broken, give up". Returning a normal text response
("user cancelled this tool call") feeds the model a result it can pivot
on, leaving the turn alive. The discriminator: tool ctx cancelled while
turn ctx is alive means user-cancelled-tool (synthetic result), turn ctx
cancelled means real cancellation (let the stream die through fantasy's
normal abort path), tool returned its own error means tool-error (existing
`NewTextErrorResponse` path).
