# UI Components, Bubble Tea Models, and Views

This document is the source of truth for `internal/ui` in this checkout. It
covers the Charm stack usage, the Bubble Tea model, view composition, widget
state, slash commands, and the exact user-visible strings the UI emits.

The UI is not a tree of nested Bubble Tea models. It is one top-level model
with a few stateful helpers:

```text
Bubble Tea
  Model.Update / Model.View / Model.Init
    -> textarea.Model
    -> viewport.Model
    -> spinner.Model
    -> ToolPanel
    -> mp3Controller
    -> modelPicker
```

The UI also has two different histories:

- `messages []ChatMessage` is the viewport scrollback.
- `agent.messages []fantasy.Message` is the canonical agent transcript.

The scrollback is for display. The agent transcript is what gets persisted and
fed back into the LLM loop.

## Charm Stack Topology

### Bubble Tea

`internal/ui/model.go` implements `tea.Model`:

- `Init()` starts the terminal mode and schedules the first UI messages.
- `Update(tea.Msg)` handles all input, signals, agent events, and timer ticks.
- `View()` builds the frame that Bubble Tea renders to the terminal.

The model keeps `Update()` non-blocking. Anything long-running is pushed into a
`tea.Cmd` or a goroutine that sends a future message back to `Update()`.

### Bubbles

The UI uses three Bubbles components directly:

- `textarea.Model` for the input box.
- `viewport.Model` for scrollback.
- `spinner.Model` for the thinking / tool-call status indicator.

Mouse wheel support comes from Bubble Tea plus `viewport.MouseWheelEnabled`.
The textarea is configured with a fixed height and a hard character limit.

### Lip Gloss

Lip Gloss does all styling, borders, and horizontal joins:

- top and bottom bars
- timestamps and nick colors
- tool panel border and status colors
- picker overlays
- the MP3 panel

### Glamour and ANSI wrapping

Agent responses are rendered through Glamour when they look like markdown.
Everything else is wrapped with `ansi.Wordwrap`, which keeps ANSI escape codes
intact while still wrapping visible text.

## `internal/ui/model.go`

### Message wrappers and helpers

`agentEventMsg` carries a single `agent.Event` plus the channel it came from.
`agentDoneMsg` marks that the active event channel has closed. Both are used to
enforce strict turn boundaries.

`SignalMsg` wraps an OS signal so `main.go` can forward SIGINT / SIGTERM into
the Bubble Tea loop.

`queuedMsg` stores a typed message while the agent is busy. It carries:

- `text`
- `queuedAt`

`BackgroundActivity` stores notices from outside the active context:

- `Time`
- `Context`
- `Sender`
- `Summary`

The helper functions are:

- `normalizeContextLabel(label string)` trims the label and falls back to
  `"main"` when empty.
- `formatContextAddress(contextLabel, sender string)` formats either
  `"[context]"` or `"[context] <sender>"`.
- `(BackgroundActivity).displayLine()` returns the address plus the summary, or
  only the address if the summary is blank.

Exact output templates:

```text
[main]
[main] <nick>
[main] <nick> summary text
```

### Model fields

`Model` owns the entire UI runtime. The important fields are:

- `config` - resolved configuration and profile state.
- `agent` - the LLM / tool loop.
- `agentState` - idle, thinking, or tool-call state for the status bar.
- `cancel` and `eventCh` - the active turn boundary.
- `viewport`, `input`, `spinner` - the Bubbles widgets.
- `toolPanel`, `mp3` - side-panel helpers.
- `messages` - rendered scrollback.
- `viewContent` - cached viewport body text.
- `focus` - the active IRC-style context list and selection.
- `membership` - joined personas per channel.
- `backgroundActivity` / `backgroundUnread` - queued notices and unread count.
- `history` / `historyIdx` - local command / message history.
- `queued` / `queueClearArmed` - steering messages typed while busy.
- `escStage` / `ctrlCStage` and timestamps - graded cancel ladders.
- `session` - append-only session log.
- `lastSavedMsgIdx` / `contextSavedIdx` - session save watermarks.
- `turnContext` - the context that was active when the current turn began.
- `transcript` - the human-readable daily log writer.
- `debugMode` - toggles verbose API logging.
- `suppressMessages` - silences startup RC command output.
- `picker` / `pickerMsgIdx` / `pickerOnSelect` - the model picker overlay.

### `NewModel(cfg *config.Config) Model`

This is the model constructor. It wires together the whole UI and does the
initial I/O.

What it does:

- Builds a `textarea.Model` with:
  - placeholder `type something, coward...`
  - focus enabled
  - character limit `8192`
  - width `80`
  - height `3`
  - prompt `>> `
  - line numbers disabled
  - prompt / text styles from the global theme
- Builds a `spinner.Model` using the dot spinner and magenta styling.
- Builds the agent with `agent.NewAgent(cfg)`.
- Restores the focus list from the session directory.
- Restores membership state from the session directory.
- Tries to open the session log. Failure is non-fatal and prints to stderr:

```text
warning: session init failed: <error>
```

- Tries to open the transcript logger. Failure is non-fatal and prints to stderr:

```text
warning: transcript init failed: <error>
```

- Applies the restored focus to the agent memory scope before returning.

The constructor also creates:

- `toolPanel: NewToolPanel()`
- `mp3: newMP3Controller()`
- `streamBuffer: &strings.Builder{}`
- `contextSavedIdx` seeded for the active context

### `ResumeSession(sess *session.Session)`

This loads a prior session into both the agent and the scrollback.

Behavior:

- Replaces `m.session`.
- Groups session entries by `Entry.Context`, falling back to
  `agent.DefaultContextKey` when the stored context label is empty.
- Restores the default context with `RestoreMessages`.
- Restores all other contexts with `RestoreContextMessages`.
- Rebuilds `m.messages` from `session.DisplayEntries`.
- Reconstructs tool names from every tool call ID in the session entries.
- Truncates visible content to 500 bytes and appends:

```text
... (truncated from session)
```

Display mapping:

- user entries become `MsgUser` with `m.config.UserNick`
- assistant entries become `MsgAgent` with `m.config.AgentNick`
- tool entries become `MsgTool`
- system and unknown roles become `MsgSystem`

This method does not try to re-run the agent. It rehydrates the display and the
agent history only.

### `ExecuteStartupCommand(line string)`

This is the silent startup RC path.

Behavior:

- Trims whitespace.
- Ignores empty lines.
- Adds a leading slash if missing.
- Sets `suppressMessages = true`.
- Routes the command through `handleCommand`.
- Clears `suppressMessages` again before returning.

Important behavior:

- Any visible output generated while `suppressMessages` is set is dropped.
- That means startup RC commands mutate state but do not add visible chatter.

### `Init() tea.Cmd`

`Init()` returns a batch of startup commands:

- `textarea.Blink`
- spinner tick
- MP3 tick
- `tea.EnterAltScreen`
- `tea.EnableMouseCellMotion`
- `showSplash()`

This is the terminal bootstrap:

- enter the alt screen
- enable mouse motion
- start the cursor blink
- start spinner and MP3 timers
- inject the splash block into the viewport

### `showSplash()`

Returns a command that sends `splashMsg{}`. The actual splash content is added
inside `Update()`.

### `Update(msg tea.Msg)`

This is the core event loop. It is where all state mutation happens.

#### `tea.WindowSizeMsg`

On the first resize:

- stores `width` and `height`
- sets the textarea width to `Width - 4`
- forces the textarea height to `3`
- computes the viewport height as:

```text
height - 4 - inputHeight
```

- clamps viewport height to at least `1`
- creates the viewport on first resize
- enables mouse wheel scrolling
- sets mouse wheel delta to `3`
- marks the model ready

Viewport width reserves room for one side panel when possible:

- MP3 panel wins when visible and the terminal is wider than `90`
- otherwise the tool panel can reserve `ToolPanelWidth`
- if the window is too narrow, the panel is not reserved

After every resize, `refreshViewport()` is called, which re-renders the entire
scrollback and moves the viewport to the bottom.

#### `splashMsg`

The splash block adds raw, non-wrapped messages to the viewport:

- a random art block from `SplashArt()`
- `SplashTagline`
- `ConnectMsg`
- loaded context file count, if any
- `Loaded MEMORY.md from working directory`, if root memory exists
- `Session: <path>`, if a session is active
- `MOTD`
- the system prompt, if non-empty

Exact visible text templates:

```text
*** Connecting to <provider-or-profile>...
*** Using model: <model>
*** Working directory: <workdir>
```

```text
Type a message to start coding. Use /help for commands.
/model to switch models. /quit to exit. Don't be a wimp.
Use @filename to include file contents. /set auto-next on for autopilot.
```

The `/model` line in the splash and MOTD is stale. The root `/model` command
is intentionally not registered. The live path is `/set model`.

#### `tea.KeyMsg`

Key handling order matters:

1. If the model picker is open, it gets the key first and swallows it.
2. Up / Down can navigate queue or local history.
3. Ctrl+C uses the graded cancel ladder.
4. Esc uses the graded cancel ladder and also closes side panels first.
5. Enter sends a message, queues it, or runs a slash command.
6. Ctrl+P / Ctrl+N move history.
7. Page keys scroll the viewport.
8. Ctrl+T toggles the tool panel.
9. MP3 keys are handled next.
10. Anything left goes to the textarea update.

Important key behaviors:

- Up on an empty input with queued messages unqueues the most recent message
  back into the input and emits:

```text
Unqueued message: <text>
```

- Enter on empty trimmed input does nothing.
- Enter on slash input always runs the slash command, even if the agent is busy.
- Enter on normal input while streaming queues the message and emits:

```text
Queued message (agent is busy): <input>
```

- Enter on normal input while idle adds a user message and sends the turn to
  the agent immediately.

#### `SignalMsg`

If the agent is streaming and `cancel` is set, the model cancels the turn and
shows:

```text
Interrupted by signal.
```

Otherwise it quits.

#### `tea.SuspendMsg`

Returns `tea.Suspend`, which lets Bubble Tea restore the terminal around a
Ctrl+Z suspend.

#### `tea.QuitMsg`

Cleanup path before exit:

- cancels the active turn if streaming
- flushes the transcript stream
- clears queue state
- stops the MP3 controller
- closes the transcript logger
- returns `tea.Quit`

Note: this branch does not persist a new checkpoint. It is a cleanup path, not
a full turn boundary.

#### `agentEventMsg`

Only events from the current active `eventCh` are accepted.

That is the turn boundary guard. Any stale `agentEventMsg` from an old channel
is ignored.

After the event is processed, `Update()` chains another `waitForAgentEvent`
command so the event loop keeps draining the same channel.

#### `agentDoneMsg`

This is the real turn-finalization boundary.

What happens:

- flushes the transcript agent stream
- marks the model idle
- clears the active event channel and cancel function
- clears active tool tracking
- resets the cancel ladders
- syncs the last assistant display message from the agent
- plays the notification sound if enabled
- snapshots token and elapsed stats into the tool panel
- persists new agent messages to the session
- saves focus
- saves the checkpoint
- drains queued steering messages if any
- maybe triggers an automatic follow-up turn

Exact user-visible messages from this branch:

- `focus save failed: <error>`
- `checkpoint save failed: <error>`
- `Discarded <n> queued message(s) older than 2m0s — context changed. Re-send if still relevant.`
- `*** <label>: continuing...`

Queue drain behavior:

- queued messages older than `queueStaleThreshold` are discarded
- fresh queued messages are combined into one user turn:

```text
[queued msg 1]: first text
[queued msg 2]: second text
...
```

If there is no queue, the agent can schedule an autonomous follow-up through
`MaybeQueueFollowUp()`.

#### `mp3TickMsg`

Returns the next `mp3TickCmd()`. This keeps the MP3 status line moving once a
second.

#### `mp3DoneMsg`

Delegates to the MP3 controller and emits the returned status string if any.

#### `spinner.TickMsg`

Updates the spinner frame.

### `handleAgentEvent(ev agent.Event)`

This converts agent events into visible UI state.

Event mapping:

- `text` - append streamed text to the current agent message
- `thinking` - show or update the thinking placeholder
- `tool_start` - start a tool call block and the tool panel entry
- `tool_result` - add the tool result block, truncated for display
- `state` - update `agentState`
- `error` - render an error block, including `llm.ErrorHint` when available
- `done` - convert to `agentDoneMsg`

Details:

- The first `text` event replaces a `MsgThink` placeholder with a real
  `MsgAgent` entry before writing the first token.
- `streamBuffer` accumulates the live assistant response.
- The transcript logger receives agent chunks through `AppendAgentChunk()`.
- Tool output shown in the viewport is truncated to 20 lines.
- `toolPanel.FinishTool()` gets the same tool result string, but it truncates
  the stored result to 60 bytes.
- `StateThinking` inserts `thinking...` into the viewport.

Exact visible strings:

- tool start -> `calling <tool>...`
- tool result -> the tool result text, or an error-styled block if
  `ev.ToolError != nil`
- error -> `Error: <err>` plus, when available:

```text
  hint: <hint>
```

### `waitForAgentEvent(ch chan agent.Event)`

This is the command that blocks outside `Update()` and waits for the next event
from the active turn channel.

If the channel closes, it returns `agentDoneMsg{ch: ch}`.

### `ircContextToMemoryScope(ctx IRCContext)`

Maps the UI routing context into the agent memory scope:

- channel -> `agent.ChannelMemoryScope(ctx.Channel, nil)`
- subchannel -> channel parent plus a child channel scope
- direct -> query scope
- default -> root scope

### `sendToAgent` / `sendFollowUpToAgent`

Both are thin wrappers around `startAgentTurn()`:

- `sendToAgent` calls `agent.SendMessage`
- `sendFollowUpToAgent` calls `agent.SendFollowUp`

### `startAgentTurn(start func(context.Context, chan agent.Event))`

This establishes a strict new turn boundary.

Behavior:

- cancels any previous turn
- clears queue and cancel-ladder state
- freezes the active routing context into `turnContext`
- initializes and switches the agent context key
- records the agent save watermark for the context
- sets the memory scope from the current focus
- creates a fresh cancellable context
- creates a buffered event channel with capacity `100`
- stores the new `cancel` and `eventCh`
- starts the agent goroutine
- returns `waitForAgentEvent(ch)`

Important invariant:

- `m.eventCh` is the identity of the active turn.
- stale events from old channels are ignored.

### `cancelActiveTurn(message string, clearQueue bool)`

Cancels the active context, flushes the transcript stream, resets streaming
state, clears active tool tracking, and appends a system message.

If `clearQueue` is true, queued steering messages are dropped too.

### `handleCtrlCKey()`

This is a three-stage graded cancel ladder with a `3s` graduation window.

Stage 1:

- while streaming, cancels the active turn but leaves queued messages intact
- while idle, only warns about quitting

Stage 2:

- if queued messages exist, clears them
- otherwise warns again

Stage 3:

- quits

Exact messages:

```text
Interrupted. Press Ctrl+C again to clear queued messages; press it a third time to quit.
Interrupted. <n> queued message(s) remain. Press Ctrl+C again to clear them; press it a third time to quit.
Press Ctrl+C twice more to quit.
Cleared <n> queued message(s). Press Ctrl+C again to quit.
Press Ctrl+C again to quit.
```

### `handleEscKey()`

Esc has a different grading ladder and panel-closing priority.

Priority order:

1. Close the tool panel if it is open.
2. Close the MP3 panel if it is open.
3. Clear queued messages if queue clearing is armed.
4. If not streaming, reset the ladder and stop.
5. If an active tool is running, cancel that tool first.
6. Otherwise, the first Esc warns and the second Esc cancels the turn.

Exact messages:

```text
Tool panel closed.
MP3 panel closed.
Cleared <n> queued message(s).
Press Esc again to cancel the turn.
Cancelled <tool>.
Could not cancel <tool>: <error>
Interrupted by Esc.
```

The tool-only cancel path keeps the turn alive. Fantasy can continue after the
tool cancellation is acknowledged.

### `cancelActiveTurnWithQueueArm(message string)`

Calls `cancelActiveTurn(message, false)` and then arms queue clearing if the
queue is non-empty.

### `syncLastAssistantMessage()`

Reads `agent.LastAssistantDisplayContent()` and replaces the most recent
`MsgAgent` scrollback entry with that content.

This is the final display correction after a turn ends.

### `addMessage(msg ChatMessage)`

This is the one place most visible messages enter the scrollback.

Behavior:

- no-ops when `suppressMessages` is set
- stamps `msg.Context` with the current active focus
- appends to `m.messages`
- writes the message to the transcript logger

Important detail:

- `addMessage()` is also used for raw splash art, picker blocks, and other
  non-chat content.
- The transcript logger strips ANSI codes, so raw scrollback content is stored
  as plain text in the daily log.

### `updateStreamingMessage()`

If the last scrollback message is an agent message, this rewrites its content
from `streamBuffer`.

This is the live typing effect.

### `refreshViewport()`

Rebuilds the viewport body from the whole `messages` slice.

Behavior:

- no-op until the model is ready
- computes wrap width as `viewport.Width - 2`, with a floor of `20`
- updates each message's width
- renders each `ChatMessage`
- wraps everything except raw ANSI art blocks
- joins the rendered messages with newlines
- sets the viewport content
- jumps the viewport to the bottom

Important consequence:

- Any refresh snaps the viewport to the newest content.
- Scrollback is live, not anchored.

### `View() string`

This assembles the final terminal frame.

When not ready, it returns exactly:

```text
initializing bitchtea...
```

The full frame is:

1. top bar
2. separator
3. viewport body, plus an optional side panel
4. separator
5. status bar
6. separator
7. input view

Top bar details:

- left side shows `bitchtea — <provider-or-profile>/<model> [<context>]`
- `[auto]` is added when auto-next steps are enabled
- `[idea]` is added when auto-next idea is enabled
- `[queued:N]` is added when there are queued messages
- right side shows the current time in `3:04pm` format

Status bar details:

- left side shows `[`agent nick`] idle`, or a spinner plus:
  - `thinking...`
  - `running tools...`
- right side shows, in order:
  - tool counts from `toolPanel.Stats`
  - MP3 status text if tracks exist
  - background activity summary if any
  - `~<tokens> tok | <elapsed>`

The live token / elapsed figures here come from the agent directly. The tool
panel gets its own snapshot at turn end.

Side panel precedence:

- MP3 panel wins when visible and the window is wide enough.
- otherwise the tool panel can appear while streaming.
- both panels are never shown at the same time.

### `SetActiveContext(label string)`

Compatibility wrapper that simply calls `focus.SetFocus(Channel(label))`.

It exists for older callers that only know a string label.

### `NotifyBackgroundActivity(activity BackgroundActivity)`

Adds a new background notice and increments the unread counter.

It also fills in defaults:

- blank context -> `main`
- zero time -> `time.Now()`
- blank summary -> `activity waiting`

### `backgroundStatus()`

Returns the compact status bar form, or an empty string when there is no
background activity.

Exact template:

```text
bg:<unread> /activity <latest-truncated-line>
```

The latest line is truncated to 22 runes.

### `backgroundActivityReport()`

Returns the `/activity` command output.

Exact templates:

```text
No background activity queued.
```

```text
Background activity:
  15:04 [context] <sender> summary text
```

### `handleCommand(input string)`

Slash command dispatcher.

Behavior:

- splits input with `strings.Fields`
- empty input returns immediately
- looks up the command name in the registry
- unknown command emits:

```text
Unknown command: /whatever. Try /help, genius.
```

Important detail:

- `helpCommandText` is a hard-coded string. It is not generated from the
  registry, so it can drift from the live command surface.

### `sysMsg(content string)` and `errMsg(content string)`

These are shorthand helpers that append a system or error message and refresh
the viewport.

### `handleMP3Key(msg tea.KeyMsg)`

Keyboard control for the MP3 panel.

It only activates when:

- the MP3 controller exists
- the MP3 panel is visible
- the input line is empty

Supported keys:

- space -> pause / resume
- left or `j` -> previous track
- right or `k` -> next track

Status routing:

- failures and `No MP3s...` messages become error messages
- everything else becomes a system message

### `formatTokens(n int)`

Formats token counts:

- `< 1000` -> decimal string
- `>= 1000` -> `%.1fk`

So `1000` becomes `1.0k`.

### `cloneToolStats(stats map[string]int)`

Returns a shallow copy of the tool count map.

This is used when saving checkpoints so the stored stats cannot be mutated by
later map writes.

## `internal/ui/message.go`

`MsgType` controls the view style:

- `MsgUser`
- `MsgAgent`
- `MsgSystem`
- `MsgError`
- `MsgTool`
- `MsgThink`
- `MsgRaw`

`ChatMessage` stores:

- `Time`
- `Type`
- `Nick`
- `Content`
- `Width`
- `Context`

### `ChatMessage.Format()`

This is the exact viewport rendering template.

```text
MsgUser   -> " [HH:MM] <nick> content"
MsgAgent  -> " [HH:MM] <nick> <markdown-rendered content>"
MsgSystem -> " [HH:MM] *** content"
MsgError  -> " [HH:MM] !!! content"
MsgTool   -> " [HH:MM]   → nick: content"
MsgThink  -> " [HH:MM] 💭 content"
MsgRaw    -> content verbatim
```

Notes:

- Agent content goes through `RenderMarkdown()`.
- If the message width is below `20`, the markdown renderer falls back to `100`
  so early startup output still wraps sensibly.
- `MsgRaw` bypasses styling and wrapping. It is used for splash art, memory
  dumps, picker dumps, and tree output.

## `internal/ui/render.go`

### `RenderMarkdown(content string, width int)`

Markdown rendering is heuristic, not parser-driven.

Behavior:

- empty input returns empty output
- content that does not look markdown-ish is returned as-is
- width `<= 0` falls back to `100`
- renderer creation failures return raw content
- render failures return raw content
- trailing whitespace and newlines from Glamour are trimmed

The markdown heuristic checks for:

- code fences
- bold markers
- headings
- list markers
- blockquotes
- numbered lists
- links
- images

### `markdownRendererForWidth(width int)`

This caches Glamour renderers by width.

Rules:

- cache size is capped at 8 widths
- widths are tracked with an LRU-style list
- renderer creation failure is cached as `nil` for that width

### `looksLikeMarkdown(s string)`

Cheap substring test only. It does not parse markdown structure.

### `WrapText(s string, width int)`

Wraps text with `ansi.Wordwrap`, preserving ANSI escape sequences.

Width `<= 0` returns the input unchanged.

## `internal/ui/styles.go` and `internal/ui/themes.go`

### Theme

The `Theme` package variable is a struct in `themes.go`:

| Field           | ANSI Color | Role                     |
|-----------------|------------|--------------------------|
| `Name`          | `"BitchX"` | Theme label (string, not a color) |
| `Cyan`          | `14`       | Primary accent, tool calls, input prompt |
| `Green`         | `10`       | User nicks, success       |
| `Magenta`       | `13`       | Agent nicks, thinking     |
| `Yellow`        | `11`       | System messages           |
| `Red`           | `9`        | Error messages            |
| `Blue`          | `12`       | Info, bar background      |
| `White`         | `15`       | Primary text, bar foreground |
| `Gray`          | `8`        | Timestamps, dim text, separators |
| `DarkBg`        | `0`        | Black background          |
| `BarBg`         | `4`        | Top/bottom bar background  |
| `ThinkingBarFg` | `15`       | Thinking bar foreground    |
| `ThinkingBarBg` | `4`        | Thinking bar background    |

The only built-in theme is **BitchX**. There is no mechanism for user-defined themes at runtime.

#### `/theme` slash command

Registered at `internal/ui/commands.go:45`. The handler (`handleThemeCommand`, line 404) is informational only — it reports the current (and only) theme name and does not accept arguments:

```text
Theme switching is disabled. Built-in theme: BitchX.
```

There is no runtime theme switch.

### Styles

The active style globals are rebuilt from the theme during package init:

- `TopBarStyle`
- `BottomBarStyle`
- `TimestampStyle`
- `UserNickStyle`
- `AgentNickStyle`
- `SystemMsgStyle`
- `ErrorMsgStyle`
- `ToolCallStyle`
- `ToolOutputStyle`
- `InputPromptStyle`
- `InputTextStyle`
- `SeparatorStyle`
- `ThinkingStyle`
- `StatsStyle`
- `DimStyle`
- `BoldWhite`
- `ThinkingBarStyle`

### `rebuildStyles()`

Re-creates every global style from the current `Theme` values.

### `CurrentThemeName()`

Returns `Theme.Name`.

### `Separator(width int)`

Returns a gray separator line made from repeated `─` characters.

### Theme wiring gap

`handleThemeCommand` says theme switching is disabled. The theme exists as a
single built-in palette and is not user-switchable at runtime.

## `internal/ui/toolpanel.go`

The tool panel is a stateful sidebar, not a Bubble Tea submodel.

### Types

- `ToolStatus`
  - `Name`
  - `Status`
  - `StartTime`
  - `Duration`
  - `Result`
- `ToolPanel`
  - `Visible`
  - `Tools`
  - `Stats`
  - `Tokens`
  - `Elapsed`

### `NewToolPanel()`

Creates a visible panel with an empty stats map.

### `StartTool(name string)`

Appends a running tool entry and increments the count for that tool name.

### `FinishTool(name, result string, isError bool)`

Finds the latest running tool with that name and marks it done or error.

Important details:

- duration is measured from `StartTime`
- result is truncated to 60 bytes, not 60 runes
- `Status` becomes `done` or `error`

### `Clear()`

Resets the recent tool list.

This helper exists, but the current UI path does not call it. Recent tool
history and stats therefore accumulate for the life of the process.

### `Render(height int)`

The panel returns an empty string when:

- it is hidden, or
- the available height is below 3

Visible output:

- rounded border
- `Tools` header
- `Recent` section when there are tool calls
- per-tool status icons:
  - running: `◉`
  - done: `✓`
  - error: `✗`
- tokens line when `Tokens > 0`
- time line when `Elapsed > 0`

Notes:

- stat lines are iterated from a map, so their order is not stable
- the render is height-limited and can slice off tail rows
- `Tokens` display uses a `k` suffix above 1000

## `internal/ui/model_picker.go` and `internal/ui/model_picker_keys.go`

The picker is a scrollback block rendered as a raw message. It is not a second
viewport or a modal window.

### `modelPicker`

Fields:

- `title`
- `models`
- `query`
- `cursor`
- `filtered`

### `newModelPicker(title, models)`

Copies the model list and immediately filters it.

Important detail:

- ordering is caller-controlled
- the constructor does not sort or dedupe

### `modelsForService(providers, service)`

Returns the model IDs for the provider whose ID matches `service`.

Behavior:

- service matching is case-insensitive
- blank service returns `nil`
- the provider's `DefaultLargeModelID` is floated to the front when present
- remaining models keep provider order
- empty model IDs are skipped

### `availableServices(providers)`

Returns sorted, lowercased provider IDs.

### `refilter()`

Substring filter, case-insensitive. Cursor is clamped to the new slice.

### `moveCursor(delta int)`

Moves the cursor with clamping only. There is no wraparound.

### `selected()`

Returns the highlighted model ID or `""` if the filter excludes everything.

### `appendQuery(s string)` and `backspace()`

Mutate the query string and refilter.

### `view(maxRows int)`

Render shape:

```text
<title>
filter> <query>
> selected model
  other model
  ...
  enter=pick  esc=cancel  type=filter
```

Behavior:

- `maxRows` floors at 5
- the list is windowed around the cursor
- `>` marks the selected row
- the selected row is yellow and bold
- empty results show:

```text
  (no matches — backspace to widen)
```

### Picker key routing

`openPicker(p, onSelect)`:

- stores the picker and callback
- appends the current picker view as a raw message
- records the picker message index
- refreshes the viewport

`closePicker()`:

- clears the picker state
- clears the callback
- resets the message index

`rerenderPicker()`:

- rewrites the stored raw message in place
- refreshes the viewport

`handlePickerKey(msg)`:

- Enter selects the current row
- Esc and Ctrl+C cancel
- Up and Down move by one
- Page keys move by `pickerVisibleRows`
- Backspace deletes one rune
- runes and space append to the filter
- unhandled keys are dropped silently so they do not leak into the textarea

Exact outputs:

```text
Picker cancelled.
Picker: no selection (filter excludes everything).
```

`pickerVisibleRows` is `12`.

## Model Picker Overlay

The `/models` command is the user-facing entry point for the model picker
overlay. The call chain is:

```text
/models
  -> handleModelsCommand
  -> loadModelCatalog()
  -> modelsForService(env.Providers, config.Service)
  -> newModelPicker(title, ids)
  -> openPicker(picker, applyModelSelection)
```

`handleModelsCommand` joins catalog data on `config.Service`, not on the
transport provider. It trims the active service, calls the package-level
`loadModelCatalog` seam, extracts model IDs with `modelsForService`, and opens
the picker with this title:

```text
models for <service> (<n> total) — type to filter
```

Failure stays in the command handler and never opens the overlay. Exact error
strings:

```text
models: no active service — set one with /set service <name> or load a profile (e.g. /profile openrouter).
models: catalog is empty — try BITCHTEA_CATWALK_AUTOUPDATE=true with BITCHTEA_CATWALK_URL set, or wait for the embedded snapshot.
models: no catalog data for service "<service>" — try BITCHTEA_CATWALK_AUTOUPDATE=true or check /profile.
```

When a service has no catalog match and provider IDs are available, the last
error gets a second line:

```text
  available services: <service>, <service>
```

`pickerOnSelect` is installed before the overlay is rendered. `openPicker`
stores both `m.picker` and `m.pickerOnSelect`, appends the initial
`modelPicker.view(pickerVisibleRows)` output as a `MsgRaw` scrollback message,
records `pickerMsgIdx`, and refreshes the viewport. The callback receives a
`*Model` because Bubble Tea passes future `Update` calls a fresh value copy;
the callback must mutate the live receiver from the keypress that selects the
model.

The `/models` callback is `applyModelSelection`. On Enter,
`handlePickerKey` reads `m.picker.selected()`, copies `m.pickerOnSelect`,
calls `closePicker`, and then invokes the callback when the selection is not
empty. `applyModelSelection` mirrors `/set model` behavior:

```go
m.agent.SetModel(choice)
clearLoadedProfile(m)
m.sysMsg(fmt.Sprintf("*** Model switched to: %s", choice))
```

That means selection updates `config.Model` through `Agent.SetModel`, invalidates
the client cache through the agent path, clears the loaded profile tag, and
emits:

```text
*** Model switched to: <choice>
```

While `m.picker != nil`, key handling short-circuits before normal input:

```text
Update(tea.KeyMsg)
  -> handlePickerKey
  -> return m, nil
```

The picker therefore overrides textarea editing, slash command submission,
history recall, the Esc ladder, the Ctrl+C ladder, and MP3 keys until it is
closed. Unhandled keys are dropped silently; they do not leak into the
textarea.

Picker keys:

| Key | Handler behavior |
| --- | --- |
| Enter | Selects the highlighted model. If the filter excludes everything, closes the picker and emits `Picker: no selection (filter excludes everything).` |
| Esc or Ctrl+C | Closes without selection and emits `Picker cancelled.` |
| Up / Down | Calls `moveCursor(-1)` or `moveCursor(1)` and rerenders the same raw message. |
| PgUp / PgDown | Moves by `pickerVisibleRows` rows and rerenders. |
| Backspace | Removes one rune from `query`, resets cursor to 0, refilters, and rerenders. |
| Printable runes and Space | Appends literal text to `query`, resets cursor to 0, refilters, and rerenders. |

Filtering is substring-based and case-insensitive in `refilter`; it trims the
query before matching. There is no fuzzy scorer, no sorting inside the picker,
and no wraparound cursor movement. Ordering is determined before construction:
`modelsForService` keeps the catalog provider order, floats
`DefaultLargeModelID` to the front when present, skips empty model IDs, and
dedupes IDs with a `seen` map.

Rendering is a windowed scrollback block, not an overlay pane. `view(maxRows)`
floors `maxRows` at 5, renders `filter> <query>`, centers the visible window
around the cursor when possible, marks the highlighted row with `> `, and adds
`...` when more rows exist below the visible window. The active UI uses
`pickerVisibleRows = 12`.

## `internal/ui/clipboard.go`

The clipboard path is seam-driven so tests can replace OS-specific behavior.

Injected seams:

- `stdoutIsTerminal`
- `writeOSC52Clipboard`
- `lookPath`
- `runClipboardCommand`

### `parseCopyIndex(raw string)`

Parses a 1-based positive integer.

Exact error:

```text
Usage: /copy [n] where n is a positive assistant message number
```

### `assistantMessages(messages []ChatMessage)`

Returns non-empty assistant messages only.

### `copyAssistantMessage(selection int)`

Behavior:

- selection `0` means "last assistant response"
- there must be at least one non-empty assistant message
- selection greater than the available assistant message count is an error
- the message content is copied to the clipboard

Return values:

- target label: `last assistant response` or `assistant response N`
- method label: `OSC 52`, `pbcopy`, or `xclip`

Exact errors:

```text
No assistant responses available to copy.
Assistant message <n> does not exist. <available> available.
Clipboard copy failed. Need a terminal that accepts OSC 52 or a working pbcopy/xclip.
```

### `copyToClipboard(text string)`

Fallback order:

1. OSC 52 if stdout is a terminal
2. `pbcopy`
3. `xclip -selection clipboard`
4. fail

The first successful backend wins.

## `internal/ui/transcript.go`

The transcript logger writes a plain-text daily log and uses kernel file locks
to avoid interleaving writes from multiple processes.

### `TranscriptLogger`

Fields:

- `dir`
- `now`
- `mu`
- `currentDate`
- `file`
- `streamActive`
- `streamBuf`

### `NewTranscriptLogger(dir string)`

Returns `nil, nil` when `dir` is empty.

Otherwise it creates the directory and returns a logger or:

```text
create transcript dir: <error>
```

### `Close()`

Closes the current log file if one is open.

### `LogMessage(msg ChatMessage)`

Behavior:

- nil logger is a no-op
- `MsgThink` is skipped
- empty `MsgAgent` messages are skipped
- an active streamed agent message is flushed before any non-agent message
- everything else is formatted and written atomically

### `AppendAgentChunk(at, nick, chunk)`

This starts or continues a streamed agent log entry.

Behavior:

- sanitizes the chunk
- ignores empty chunks
- on the first chunk, writes the timestamp and nick prefix
- appends content without a newline

### `FinishAgentMessage()`

Flushes the current streamed agent message to disk.

### `writeAtomicLocked(s string)`

Writes under `syscall.Flock(LOCK_EX)` / `LOCK_UN`.

Important detail:

- if the write succeeds but unlock fails, the unlock error is returned
- file rotation is date-based

### `ensureFileLocked()`

Opens or rotates the daily file:

```text
YYYY-MM-DD.log
```

### `formatTranscriptMessage(msg ChatMessage)`

Exact log formats:

```text
[HH:MM] <nick> content
[HH:MM] <nick> multiline content
    continuation line

[HH:MM] *** content
[HH:MM] !!! content
[HH:MM] -> tool:
    indented tool output
raw text from MsgRaw
```

Notes:

- agent and raw content are sanitized before logging
- ANSI escape sequences are stripped
- CRLF / CR are normalized to LF
- trailing newlines are trimmed
- empty tool output still writes the `"[HH:MM] -> tool:"` prefix line
- `MsgRaw` is written without color codes, so splash art and picker views are
  stored as plain text in the transcript

### `transcriptLine`, `transcriptPrefix`, `indentTranscriptContent`,
`sanitizeTranscriptText`

These are formatting helpers:

- `transcriptLine` builds the timestamped one-line or multiline format
- `transcriptPrefix` yields `"[HH:MM] <nick> "`
- `indentTranscriptContent` indents every line with four spaces
- `sanitizeTranscriptText` strips ANSI and normalizes newlines

## MP3 Player

The MP3 controller (`internal/ui/mp3.go`) is a stateful widget that embeds a functional audio player directly into the TUI. It manages process execution, panel rendering, and track state.

### 1. Library Directory
The player scans the library directory, which defaults to `~/.bitchtea/mp3` (falling back to `.bitchtea/mp3` if the home directory cannot be determined). Users drop `.mp3` files here for the player to discover.

### 2. Player Detection Chain
To ensure cross-platform compatibility without heavy audio dependencies, the player delegates playback to an external shell process. It probes for installed players in the following order and uses the first one found:
1. `mpv` (`--no-video --really-quiet --no-terminal`)
2. `ffplay` (`-nodisp -autoexit -loglevel error`)
3. `mpg123` (`-q`)

Standard output and standard error from the subprocess are discarded.

### 3. Pause and Resume via OS Signals
Instead of relying on player-specific RPC protocols, the controller achieves pause and resume functionality by sending OS-level process control signals:
- **Pause**: Sends `SIGSTOP` (`syscall.SIGSTOP`) to freeze the player process.
- **Resume**: Sends `SIGCONT` (`syscall.SIGCONT`) to resume execution.
- **Stop**: Sends `SIGKILL` (`syscall.SIGKILL`) to terminate the process instantly.

### 4. Panel Rendering and Layout
When toggled visible, the MP3 panel renders as a sidebar with a rounded yellow border (`mp3PanelWidth = 34`). It only renders if there is enough vertical space (height >= 4). 
The panel includes:
- A header (`MP3 Player`) and library directory path.
- A controls cheat sheet.
- A `Now Playing` section showing the status text.
- A scrollable `Playlist` windowed around the current track, with the active track highlighted green and marked with `▶`.

### 5. Key Bindings
While the MP3 panel is visible and the input line is empty, the player accepts direct keyboard control:
- **`space`**: Toggles Pause/Resume.
- **`←` or `j`**: Skips to the previous track.
- **`→` or `k`**: Skips to the next track.

### 6. Track Scanning and Playlist Navigation
The `scan()` method reads the library directory, keeping only `.mp3` files. It computes track durations using `go-mp3` and sorts the playlist case-insensitively by name. Navigation methods (`next()` and `prev()`) support wraparound, moving from the last track to the first and vice versa.

### 7. Progress Bar and Visualizer Rendering
The player computes elapsed time based on process start time and any paused offset. The status line features two dynamic components:
- **Progress Bar**: Rendered as a 10-character wide bracketed bar using `█` for filled segments and `·` for remaining time.
- **Visualizer**: An 8-character pseudo-random audio visualizer block. It hashes the track's file path combined with the elapsed seconds to deterministically pick from 8 block-character height levels (`▁`, `▂`, `▃`, `▄`, `▅`, `▆`, `▇`, `█`), simulating a graphic equalizer.

### 8. Auto-Advance on Track Completion
A background goroutine waits on the player process to exit, sending an event to the `Done()` channel. To prevent race conditions with user interactions, a `generation` counter acts as an epoch guard. When an `mp3DoneMsg` arrives with the correct epoch, the controller automatically advances to the next track if the playlist contains more than one song.

## `internal/ui/context.go` and `internal/ui/context_helpers.go`

### `ContextKind`

- `KindChannel`
- `KindSubchannel`
- `KindDirect`

### `IRCContext`

Fields:

- `Kind`
- `Channel`
- `Sub`
- `Target`

### `Label()`

Canonical display labels:

- channel -> `#channel`
- subchannel -> `#channel.sub`
- direct -> `target`
- default -> `#main`

### `Channel(name string)`

Behavior:

- trims whitespace
- lowercases
- strips leading `#`
- empty names fall back to `main`

### `Subchannel(channel, sub string)`

Behavior:

- trims and lowercases both parts
- strips leading `#` from the channel
- empty channel falls back to `main`

### `Direct(target string)`

Trims whitespace only. Case is preserved because persona names are case
sensitive.

### `FocusManager`

This stores the ordered list of known contexts and the active index.

Methods:

- `NewFocusManager()` starts at `#main`
- `Active()` returns the current context
- `ActiveLabel()` returns the current label
- `SetFocus()` activates an existing context or appends a new one
- `Ensure()` adds a context without changing focus
- `Remove()` removes a context unless it is the last remaining one, then shifts
  focus left or wraps to the last remaining context
- `All()` returns a copy of the context slice
- `ToState()` serializes to `session.FocusState`
- `RestoreState()` restores from a saved state
- `Save()` persists to the session directory
- `LoadFocusManager()` loads from disk or falls back to a fresh manager

Important gap:

- `RestoreState()` only clamps the active index on the high side.
- A malformed negative `ActiveIndex` is not explicitly clamped.

### `contextToRecord` and `recordToContext`

These map between UI contexts and session records.

Invalid saved records are dropped during restore.

### `ircContextToKey(ctx)`

This is the string key used for per-context agent message storage:

- channel -> `#channel`
- subchannel -> `#channel.sub`
- direct -> `target`
- default -> `agent.DefaultContextKey`

### `saveCurrentContextMessages()`

Persists unsaved messages for the current context.

Important gap:

- the helper exists, but the normal UI path does not call it.
- the actual turn-end save path uses the agent save watermark directly.

## Persona Model

Personas are named entities that can participate in channels or receive direct
messages. There are two layers:

### Agent persona

The agent has a built-in persona defined in `internal/agent/agent.go`:

- `personaPrompt` (line 530) — "I AM CODE APE. KING APE calls me SHE APE."
  A long-form persona harness written in cave-speak that establishes the agent's
  voice, values, and behavioral constraints.
- `personaRehearsal` (line 623) — `🦍👑 ready. APES STRONG TOGETHER 🦍💪🤝`.
- `buildPersonaAnchor()` (line 628) — constructs a synthetic user/assistant
  exchange that anchors the persona as the last bootstrap pair. This is injected
  after context files and MEMORY.md so the model's final impression before the
  user's first real message is the persona voice.

The persona anchor is rebuilt on every `Reset()` (line 1174) and on context
switch (`SetContext`, line 1108) so the model always sees it immediately before
user input.

### Invited personas (channel membership)

Personas tracked by `MembershipManager` are named strings — any string is a
valid persona name. They are:

- **Case-sensitive** (`internal/ui/context.go:66`: "persona names matter")
- Added to channels via `/invite`, removed via `/kick`
- Visible via the channel's member list
- DM-able via `/query <name>` or `/msg <name> <text>`, which creates a
  `KindDirect` context (`internal/ui/context.go:15`)

The session `Entry.Target` field (`internal/session/session.go:88`) records the
persona or nick for direct-message routing.

**Wiring gap**: Persona membership is metadata-only today. Inviting a persona
to a channel or DMing one does not inject that persona into the agent's system
prompt or tool routing. The agent only knows about its own built-in Code Ape
persona. Cross-reference `bt-wire.4` (tracked in beads) for the fix.

## `internal/ui/membership.go` and `internal/ui/invite.go`

### `MembershipManager`

Tracks persona membership per channel key.

Methods:

- `NewMembershipManager()` creates an empty manager
- `Invite()` adds a persona if both channel and persona are non-empty
- `Part()` removes a persona and drops empty channels
- `Members()` returns a sorted slice or `nil`
- `IsJoined()` checks membership
- `ToState()` serializes to `session.MembershipState`
- `RestoreState()` replaces the in-memory map from persisted state
- `Save()` persists to the session directory
- `LoadMembershipManager()` loads or falls back to empty

Important details:

- channel keys are normalized by stripping `#`, trimming whitespace, and
  lowercasing
- persona names are trimmed but not lowercased
- restore skips empty member lists

### `channelKeyFromCtx(ctx)`

Extracts the membership key for channel or subchannel contexts.

Direct contexts return `("", false)`.

### `normalizeMembershipKey(key)`

Strips a leading `#`, trims, and lowercases.

### `handleInviteCommand`

Usage:

```text
/invite <persona> [#channel]
```

Behavior:

- the explicit third argument only counts when it starts with `#`
- otherwise the current channel or subchannel is used
- direct contexts are rejected with:

```text
Cannot /invite in a DM context. Switch to a channel first.
```

- already joined personas emit:

```text
<persona> is already in #<channelKey>
```

- on success, membership is saved and the UI emits:

```text
*** <persona> joined #<channelKey>
```

Then it appends a catch-up block from session history, capped at 50 lines.

### `buildChannelCatchup(sess, channel, maxLines)`

This is UI-only text. It is not injected into the agent history.

Behavior:

- nil session -> `Catch-up: no session history available.`
- no matching history -> `Catch-up for <channel>: no prior conversation found.`
- otherwise it returns the last `maxLines` non-tool entries for that channel

Exact output shape:

```text
Catch-up for #channel (N messages):
  [user] content
  [assistant] content
```

It excludes:

- tool role entries
- entries with a non-empty `ToolCallID`

### `handleKickCommand`

Usage:

```text
/kick <persona>
```

Behavior:

- current channel or subchannel is used when possible
- direct contexts fall back to `main`
- missing membership emits:

```text
<persona> is not in #<channelKey>
```

- success emits:

```text
*** <persona> has been kicked from #<channelKey>
```

Important gap:

- membership save errors are ignored in both `/invite` and `/kick`.

## `internal/ui/commands.go`

### Registry helpers

- `loadModelCatalog` is a package-level seam. Production uses
  `catalog.Load(catalog.LoadOptions{})`; tests can replace it with a fixture.
- `registerSlashCommands(specs...)` expands alias groups into the lookup map.
- `lookupSlashCommand(name)` returns the registered handler and a boolean.

### Command registry

The live root slash commands are:

```text
/quit /q /exit
/help /h
/set
/clear
/restart
/compact
/copy
/tokens
/debug
/activity
/mp3
/theme
/memory
/sessions /ls
/resume
/tree
/fork
/profile
/models
/join
/part
/query
/channels /ch
/msg
/invite
/kick
```

Important mismatch:

- `/model` is mentioned in splash / MOTD copy, but it is not registered.
- the root `/provider`, `/baseurl`, `/apikey`, `/auto-next`, `/auto-idea`,
  and `/sound` commands are also intentionally absent.
- the supported path is `/set ...`.

### `helpCommandText`

The help command appends one system message with exactly this visible text:

```text
Commands:
  /join <#channel>    Switch focus to channel (creates if new)
  /part [#channel]    Leave context (default: current)
  /query <nick>       Route Enter persistently to nick
  /msg <nick> <text>  One-shot send to nick, no focus change
  /channels           List open contexts
  /set [key [value]]  Show or change a setting
                        keys: provider, model, baseurl, apikey, service,
                              nick, profile, sound, auto-next, auto-idea
                        e.g. /set apikey sk-..., /set provider anthropic
  /profile [cmd]      save/load/show/delete profiles (built-ins: ollama, openrouter, etc.)
                        bare /profile <name> loads the named profile
  /models             Open a fuzzy picker of models for the active service
                        (uses the catwalk catalog cache; offline-safe)
  /compact            Compact conversation context
  /clear              Clear chat display
  /restart            Reset agent and start a fresh conversation
  /copy [n]           Copy last or nth assistant response
  /tokens             Token usage estimate
  /memory             Show MEMORY.md contents
  /sessions           List saved sessions
  /resume <number>    Resume a session by number
  /tree               Show session tree
  /fork               Fork session
  /debug on|off       Toggle verbose API logging
  /activity [clear]   Show or clear queued background activity
  /mp3 [cmd]          Toggle MP3 panel and player
  /quit               Exit

  Use @filename to include file contents.
  Type while agent works to queue (steering).
  Ctrl+C to interrupt, again to quit.
```

### `handleQuitCommand`

Returns `tea.Quit`.

No extra output is emitted here. Cleanup happens in the `tea.QuitMsg` branch of
`Update()`.

### `handleHelpCommand`

Appends the help block above as a system message and refreshes the viewport.

### `handleSetCommand`

This command does both "show current value" and "set value".

Behavior:

- `/set` with no extra args lists every known setting
- `/set <key>` prints the current value of that key
- `/set service` is special and prints the service identity
- `/set <key> <value>` sets the key

Exact error:

```text
Unknown setting "<key>". Valid keys: ...
```

Exact generic outputs:

```text
Settings:
  <key> = <value>
  service = <unset-or-value>
```

```text
<key> = <value>
```

The special handlers are:

- `provider` -> `handleProviderCommand`
- `model` -> `handleModelCommand`
- `baseurl` -> `handleBaseURLCommand`
- `apikey` -> `handleAPIKeyCommand`
- `service` -> `handleServiceSet`

All other keys go through `config.ApplySet()`.

`profile` is also accepted through `config.ApplySet()`. After the config is
updated, the UI syncs the agent transport fields from the profile or emits a
profile lookup error if the profile tag is not present.

Config-only keys:

- `nick`
- `sound`
- `auto-next`
- `auto-idea`

These do not touch the agent transport state.

Important note:

- `strings.TrimPrefix(input, parts[0]+" "+parts[1])` is used to recover the
  remainder of the line, so values can contain spaces.

### `settingLabel(key string)`

Capitalizes dash-separated words:

- `auto-next` -> `Auto Next`
- `auto-idea` -> `Auto Idea`

### `handleModelCommand`

This is the manual model setter used by `/set model`.

Outputs:

- no arg:

```text
Current model: <model>. Usage: /set model <name>
```

- with arg:

```text
*** Model switched to: <newModel>
```

It also clears the loaded profile tag so the top bar reflects the manual
override.

### `handleClearCommand`

Clears the visible scrollback only.

Important gap:

- it does not reset the agent transcript, session log, or focus state.

### `handleRestartCommand`

Resets the conversation.

Behavior:

- if streaming, cancels the active turn first
- resets the agent
- clears visible messages and queued steering messages
- resets the stream buffer
- tries to create a fresh session file
- resets the save watermarks
- emits:

```text
*** Conversation restarted. Fresh context.
```

Important gap:

- focus and membership are preserved.
- session creation failure is ignored.

### `handleCompactCommand`

Compacts agent context.

Outputs:

```text
Can't compact while agent is working. Be patient.
Compaction failed: <error>
Compacted: ~<before> -> ~<after> tokens
```

The compaction call is not cancelable from the UI, because it uses
`context.Background()`.

### `handleCopyCommand`

Copies the last or nth assistant response.

Outputs:

```text
Copied last assistant response via OSC 52.
Copied assistant response 2 via pbcopy.
```

Possible errors come from `parseCopyIndex()` and `copyAssistantMessage()`.

### `handleTokensCommand`

Outputs:

```text
~<tokens> tokens | $<cost> | <message count> messages | <turn count> turns
```

This is a snapshot of the agent's current counters.

### `handleDebugCommand`

Outputs:

```text
Debug mode: OFF. Usage: /debug on|off
Debug mode: ON
Debug mode: OFF
Usage: /debug on|off
```

When debug mode is on, every intercepted request writes a system message in
this exact shape:

```text
[DEBUG] <method> <url>
Request Headers: <map>
Request Body: <body>
Response Status: <status>
```

Important gap:

- this logs raw request headers and body into the chat scrollback and transcript.

### `handleActivityCommand`

Outputs:

- no args -> the full report from `backgroundActivityReport()`
- `clear` -> `Cleared <n> background activity notice(s).`
- anything else -> `Usage: /activity [clear]`

Listing background activity also resets `backgroundUnread` to zero.

### `handleMP3Command`

Outputs:

```text
MP3 controller unavailable.
MP3 panel hidden.
No MP3s found in <dir>
Loaded <n> track(s) from <dir>
MP3 panel ready. <track>
Now playing: <track>
Paused: <track>
Resumed: <track>
Finished: <track>
MP3 playback ended: <err>
MP3 playback failed: <error>
Resume failed: <error>
Pause failed: <error>
Usage: /mp3 [rescan|play|pause|next|prev]
```

Behavior:

- bare `/mp3` toggles the panel and may start playback
- `rescan`, `play`, `pause` / `toggle`, `next`, and `prev` are explicit
  subcommands
- statuses containing `failed`, or starting with `No MP3s` or `Usage:`, are
  shown as errors

### `handleThemeCommand`

Outputs:

```text
Theme switching is disabled. Built-in theme: BitchX.
```

### `handleMemoryCommand`

Root memory:

- shows `MEMORY.md` from the working directory
- truncates to 1000 bytes and appends `... (truncated)` if needed
- raw output begins with:

```text
\033[1;36m--- MEMORY.md ---\033[0m
```

Scoped memory:

- when the active scope is not root, it shows the scoped `HOT.md`
- blank scoped memory emits:

```text
No HOT.md for <label> yet.
```

Root-missing case:

```text
No MEMORY.md found in working directory.
```

### `handleSessionsCommand`

Outputs:

```text
No saved sessions.
```

or:

```text
Sessions:
  1. <session.Info>
  2. <session.Info>
  Resume: /resume <number>
```

When there is more than one page:

```text
Sessions (page X/Y):
  ...
  ... use /sessions <nextPage> for next page
  Resume: /resume <number>
```

Page size is 20.

### `handleResumeCommand`

Outputs:

```text
Usage: /resume <number>  (use /sessions to list)
Invalid session number: <arg>
No saved sessions.
Session <n> not found. <available> sessions available.
Error loading session: <error>
Resumed session <n>: <basename>
```

If a turn is streaming when resume is requested, the current turn is cancelled
first with:

```text
Session resume
```

### `handleTreeCommand`

Outputs:

- no active session -> `No active session.`
- otherwise the session tree, wrapped in cyan ANSI escape codes

Raw shape:

```text
\033[1;36m<session.Tree()>\033[0m
```

### `handleForkCommand`

Outputs:

```text
No session to fork from.
Fork failed: <error>
Forked to new session: <new path>
```

### `handleBaseURLCommand`

Outputs:

```text
Base URL: <base>
Usage: /set baseurl <url>
*** Base URL set to: <url>
  requests -> <preview>
```

It may also emit one or more advisory transport warnings from
`providerTransportHint()`.

### `handleAPIKeyCommand`

Outputs:

```text
API Key: <masked>
Usage: /set apikey <key>
*** API key set: <masked>
```

### `handleProviderCommand`

Outputs:

```text
Provider: <provider>
Usage: /set provider <openai|anthropic>
*** Provider set to: <prov>
  requests -> <preview>
```

It may also emit transport warnings.

### `handleProfileCommand`

Supported actions:

- `show`
- `save`
- `load`
- `delete`
- bare profile name, which acts like `load`

Outputs:

```text
Profiles: <comma-separated list>
Usage: /profile save <name> | /profile load <name> | /profile show <name> | /profile delete <name>
```

```text
Profile: <name>
  provider=<...> service=<...> model=<...>
  baseurl=<...>
  endpoint=<...>
  apikey=<masked>
```

```text
*** Profile saved: <name> (provider=<...> service=<...> model=<...>)
*** Profile loaded: <name>
  provider=<...> service=<...> model=<...>
  baseurl=<...>
  endpoint=<...>
  apikey=<masked>
*** Profile deleted: <name>
```

Additional behavior:

- a loaded profile with no API key emits:

```text
This profile did not provide an API key. Set one with /set apikey <key> or the matching env var before connecting.
```

### `applyProfileToModel(m, name, p, verbose)`

Applies the profile to both config and agent state.

It also clears the loaded profile label only when the caller does so later.

If `verbose` is true, the full profile details are shown. Otherwise only the
short loaded banner is shown.

### `clearLoadedProfile(m)`

Sets `cfg.Profile` to the empty string.

### `handleServiceSet`

Outputs:

```text
Usage: /set service <value>
Service set to: <value>
```

Important detail:

- the value is accepted verbatim
- there is no validation
- setting the service does not clear the loaded profile tag

### `setKeysWithService()`

Returns the normal `config.SetKeys()` list plus `service`.

### `serviceDisplay(value string)`

Returns:

- `<unset>` for an empty value
- the raw value otherwise

### `maskSecret(value string)`

Returns:

- `<unset>` when blank
- the raw string when length is 8 or less
- otherwise the first 4 and last 4 characters with `...` in between

### `profileLookupMessage(name string)`

Distinguishes providers from profiles.

Outputs:

```text
openai is a provider, not a profile. Use /set provider openai or /profile to list profiles.
anthropic is a provider, not a profile. Use /set provider anthropic or /profile to list profiles.
Unknown profile "<name>". Use /profile load <name> or /profile to list profiles.
```

### `transportEndpointPreview(provider, baseURL string)`

Returns the request path preview:

- anthropic -> `<baseURL>/messages`
- everything else -> `<baseURL>/chat/completions`

### `providerTransportHint(provider, baseURL string)`

Returns advisory warnings only. It does not block anything.

Possible warning shapes:

- base URL already includes `/chat/completions`
- base URL already includes `/messages`
- Anthropic transport with an OpenAI-style base URL
- OpenAI transport with an Anthropic-style base URL

### `handleModelsCommand`

This command opens the model picker for the active service.

Behavior:

- empty service -> error
- empty catalog -> error
- no provider for the service -> error with the available services list
- otherwise opens the picker titled `models for <service> (<n> total) — type to filter`

Exact errors:

```text
models: no active service - set one with /set service <name> or load a profile (e.g. /profile openrouter).
models: catalog is empty - try BITCHTEA_CATWALK_AUTOUPDATE=true with BITCHTEA_CATWALK_URL set, or wait for the embedded snapshot.
models: no catalog data for service "<service>" - try BITCHTEA_CATWALK_AUTOUPDATE=true or check /profile.
```

Important detail:

- the catalog loader is a package-level seam (`loadModelCatalog`) so tests can
  inject fixture data.

### `applyModelSelection(m *Model, choice string)`

Mirror of `/set model` for the picker callback.

Outputs:

```text
*** Model switched to: <choice>
```

### `handleJoinCommand`

Outputs:

```text
Usage: /join <#channel>
Joined #<channel>
```

Behavior:

- focus is changed immediately
- focus is saved to disk
- on save failure, the UI emits:

```text
focus save: <error>
```

### `handlePartCommand`

Outputs:

```text
Can't part the last context.
Not in context <label>.
Parted <oldLabel> — now in <activeLabel>
```

Behavior:

- if the argument starts with `#`, it removes a channel context
- otherwise it tries a direct context
- if there is no argument, it removes the current focus
- the help text only advertises `[#channel]`, but the direct persona form is
  wired and live
- on save failure, the UI emits:

```text
focus save: <error>
```

### `handleQueryCommand`

Outputs:

```text
Usage: /query <persona>
Query open: <persona>
```

This opens a direct-message context and persists it.

On save failure, the UI emits:

```text
focus save: <error>
```

### `handleChannelsCommand`

Outputs:

```text
Open contexts:
* #main [alice, bob]
  #dev
```

Behavior:

- contexts are listed in focus order
- the active context is marked with `* `
- channel membership is shown in square brackets when present
- member lists are sorted alphabetically

### `handleMsgCommand`

Usage:

```text
/msg <nick> <text>
```

Behavior:

- if streaming, the message is queued as steering text and the UI emits:

```text
Queued /msg to <nick> (agent busy).
```

- otherwise a user message is appended with visible content:

```text
→<nick>: <text>
```

and the agent receives:

```text
[to:<nick>] <text>
```

### `handleInviteCommand`

Covered in `membership.go` and `invite.go`.

### `handleKickCommand`

Covered in `membership.go` and `invite.go`.

## `internal/ui/art.go`

This file holds the startup splash art and the fixed welcome text.

### `splashArts` (6 variants)

Six hard-coded ANSI art blocks stored as string constants:

| Variable      | ANSI Color |
|---------------|------------|
| `splashArt1` | Cyan (`\033[1;36m`) — bitchtea ASCII logo |
| `splashArt2` | Red (`\033[1;31m`) — BITCHTEA ASCII logo |
| `splashArt3` | Magenta (`\033[1;35m`) — "bitchtea" ASCII + subtitle |
| `splashArt4` | Yellow (`\033[1;33m`) — framed box with v0.1.0 |
| `splashArt5` | Green (`\033[1;32m`) — framed box with tagline |
| `splashArt6` | Red (`\033[1;31m`) — ASCII art blocks |

### `SplashArt()`

Returns one of the six art blocks chosen at random (`rand.Intn(6)`).

Important detail:

- the art selection is not deterministic
- the raw art is stored as `MsgRaw`
- the transcript logger strips ANSI codes, so the log gets plain text only

### Splash wire-up (`model.go`)

The splash is not rendered at startup in `Init()`. Instead, `Init()` returns a
`splashMsg` command via `showSplash()` (line 354):

```go
func (m Model) showSplash() tea.Cmd {
    return func() tea.Msg { return splashMsg{} }
}
```

When `Update()` receives `splashMsg` (line 407), it emits the following in
order:

1. `SplashArt()` — random ANSI art block (`MsgRaw`)
2. `SplashTagline` — the `« bitchtea » — putting the BITCH back...` tagline
   (`MsgRaw`)
3. `ConnectMsg` — provider/profile, model, and working directory (`MsgRaw`)
4. Context file count (`MsgSystem`)
5. Memory status or absence (`MsgSystem`)
6. Session path (`MsgSystem`)
7. `MOTD` — the help banner (`MsgRaw`)
8. System prompt, if non-empty (`MsgSystem`)

Then `refreshViewport()` scrolls to the bottom.

Important details:

- the splash is **TUI-only** -- `runHeadless()` (`main.go:245`) starts the agent
  loop directly without ever touching the Bubble Tea model, so no splash is
  emitted in headless mode
- the ANSI art blocks are fixed-width and do **not** adapt to terminal width
- the transcript logger strips ANSI codes from `MsgRaw` messages, so the session
  log gets plain text only

### `SplashTagline`

Visible text:

```text
« bitchtea » — putting the BITCH back in your terminal since 2026
an agentic coding harness for people who don't need hand-holding
```

### `ConnectMsg`

Visible text template:

```text
*** Connecting to %s...
*** Using model: %s
*** Working directory: %s
```

### `MOTD`

Visible text:

```text
─────────────────────────────────────────────────────────────
Type a message to start coding. Use /help for commands.
/model to switch models. /quit to exit. Don't be a wimp.
Use @filename to include file contents. /set auto-next on for autopilot.
─────────────────────────────────────────────────────────────
```

Important mismatch:

- the `/model` mention is stale, because `/model` is not a registered command.

## Wiring Gaps and Test Coverage Notes

This is where the current UI is not fully wired or where tests only prove the
shape of the behavior.

### Stale or incomplete wiring

- `/model` is still mentioned in splash and MOTD, but the root command is
  not registered.
- `helpCommandText` is hard-coded instead of being generated from the command
  registry, so it can drift.
- `saveCurrentContextMessages()` exists but is not called by the normal save
  path.
- `ToolPanel.Clear()` exists but is not used in the live flow.
- `FocusManager.RestoreState()` does not explicitly clamp negative active
  indexes.
- `/invite` and `/kick` ignore membership save errors.
- Persona membership is metadata-only; invited personas are not injected into
  the agent's system prompt or tool routing (tracked as bt-wire.4).
- session append errors at turn end are ignored.
- `handleRestartCommand()` ignores `session.New()` failure.
- `/theme` is informational only; there is no runtime theme switch.

### Test-shape-only coverage

- `render_test.go` checks markdown and wrapping behavior, but not exact Glamour
  output bytes for every case.
- `toolpanel_test.go` checks state and presence of headers, not exact panel
  geometry.
- `transcript_test.go` checks log shape and key substrings, not cross-process
  contention failure modes.
- `clipboard_test.go` uses seams for OSC 52 / pbcopy / xclip, not real terminal
  integration.
- `mp3_test.go` uses fake duration readers and fake processes, not real player
  binaries.
- `model_picker` tests check filtering and selection behavior, not terminal
  interaction under all resize paths.
- `themes_test.go` checks that styles exist, not the rendered visual palette.
- `message_test.go` checks formatting shape, not terminal color bytes.
- `startup_rc_test.go` checks that startup commands are silent, not every RC
  command path.
- `model_turn_test.go` exercises queueing and cancel ladders, but it still
  relies on the agent event seam rather than an end-to-end provider turn.

### Behavior worth remembering

- `refreshViewport()` always jumps to the bottom.
- MP3 and tool side panels are mutually exclusive in the main frame.
- `MsgRaw` content is logged without ANSI colors.
- `toolPanel.Stats` order is map-order dependent.
- queue draining happens only after `agentDoneMsg`.
- stale queued messages are discarded only when the current turn ends.
