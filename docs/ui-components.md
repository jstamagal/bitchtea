# UI Components

This document is the reference contract for bitchtea's Charm stack usage, Bubble Tea model, and rendered views. It describes the code that owns the screen, the exact messages it emits, and the current gaps in the wiring.

Primary files:

- `internal/ui/model.go`: top-level Bubble Tea model, event loop, turn routing, viewport refresh, splash, key handling.
- `internal/ui/message.go`: viewport message types and formatting.
- `internal/ui/render.go`: markdown rendering and ANSI-aware wrapping.
- `internal/ui/styles.go` / `internal/ui/themes.go`: Lipgloss styles and theme constants.
- `internal/ui/toolpanel.go`: collapsible tool sidebar.
- `internal/ui/mp3.go`: MP3 library/player panel and status bar data.
- `internal/ui/model_picker.go` / `internal/ui/model_picker_keys.go`: fuzzy picker overlay for `/models`.
- `internal/ui/transcript.go`: daily human-readable transcript log.
- `internal/ui/context.go` / `internal/ui/membership.go`: focus and membership state that the UI renders and persists.
- `internal/ui/commands.go`: slash-command handlers that mutate the screen.

## Stack Summary

The UI is a single Bubble Tea model with these live components:

- `textarea.Model` for input.
- `viewport.Model` for scrollback.
- `spinner.Model` for agent activity.
- `ToolPanel` for tool-call history.
- `mp3Controller` for the optional audio sidebar.
- `modelPicker` for `/models`.

Charm stack pieces are used directly:

- Bubble Tea drives `Update`, `View`, `Init`, and `tea.Cmd` scheduling.
- Bubbles provides `textarea`, `viewport`, and `spinner`.
- Lipgloss provides all bars, separators, colors, panels, and inline styling.
- Glamour renders agent markdown.
- `ansi.Wordwrap` keeps wrapped text ANSI-safe.

The UI is built around message passing, not blocking calls. The hot path is:

1. A key or agent event arrives as a `tea.Msg`.
2. `Update` mutates `Model`.
3. `Update` returns a `tea.Cmd` if the next step needs to wait for more agent events, mp3 completion, or a future tick.
4. `refreshViewport()` rebuilds the visible scrollback string and pushes it into `viewport.Model`.

`Update` is intended to stay non-blocking for the agent stream. Some slash-command handlers still do synchronous file IO directly in the handler path, so the guarantee is about the agent loop and event routing, not about every command being async.

## Top-Level Model

`Model` is the app state container. Its main fields fall into these buckets:

- Config: `config *config.Config`.
- Agent bridge: `agent`, `agentState`, `cancel`, `eventCh`.
- UI widgets: `viewport`, `input`, `spinner`, `toolPanel`, `mp3`.
- View state: `messages`, `viewContent`, `width`, `height`, `ready`, `streaming`, `streamBuffer`, `activeToolName`, `activeToolCallID`.
- Context state: `focus`, `membership`, `backgroundActivity`, `backgroundUnread`.
- Input history and queueing: `history`, `historyIdx`, `queued`, `queueClearArmed`, `escStage`, `escLast`, `ctrlCStage`, `ctrlCLast`.
- Session persistence: `session`, `lastSavedMsgIdx`, `contextSavedIdx`, `turnContext`, `transcript`.
- Debug / startup: `debugMode`, `suppressMessages`.
- Picker overlay: `picker`, `pickerMsgIdx`, `pickerOnSelect`.

Two details matter for turn boundaries:

- `turnContext` is frozen at the moment a user turn starts.
- `eventCh` is the active agent-event channel, and stale messages are ignored if their channel pointer does not match the current one.

## Initialization

`NewModel(cfg)` wires the widgets and runtime state:

- `textarea` is created with:
  - placeholder: `type something, coward...`
  - focus enabled
  - character limit `8192`
  - size `80x3`
  - prompt `>> `
- `spinner` is set to `spinner.Dot`.
- `agent.NewAgent(cfg)` constructs the agent-side state.
- `LoadFocusManager(cfg.SessionDir)` and `LoadMembershipManager(cfg.SessionDir)` restore UI-side sidecars.
- `session.New(cfg.SessionDir)` creates the current session log; failure is non-fatal and only prints a warning to stderr.
- `NewTranscriptLogger(cfg.LogDir)` creates the daily transcript logger; failure is also non-fatal and only prints a warning.
- The initial memory scope is derived from the restored active focus and pushed into the agent with `ag.SetScope(...)`.

If session or transcript initialization fails, the model still boots. Those subsystems are optional at runtime.

`Init()` returns a batch of startup commands:

- `textarea.Blink`
- `spinner.Tick`
- `mp3TickCmd()`
- `tea.EnterAltScreen`
- `tea.EnableMouseCellMotion`
- `m.showSplash()`

`showSplash()` returns a command that emits `splashMsg{}`.

## Startup Splash

When `splashMsg` is handled, the model appends these visible messages in order:

1. One random ANSI art block from `SplashArt()`.
2. `SplashTagline`.
3. `ConnectMsg`, formatted with provider/profile, model, and working directory.
4. If context files were discovered: `Loaded <N> context file(s) from project tree`.
5. If root memory exists: `Loaded MEMORY.md from working directory`.
6. If a session is attached: `Session: <session path>`.
7. `MOTD`.
8. The current system prompt, if non-empty.

The deterministic text templates are:

```text
\033[1;35m  « bitchtea » \033[0;37m— putting the BITCH back in your terminal since 2026\033[0m
\033[0;36m  an agentic coding harness for people who don't need hand-holding\033[0m

\033[1;33m  *** \033[1;37mConnecting to %s...\033[0m
\033[1;33m  *** \033[1;37mUsing model: \033[1;32m%s\033[0m
\033[1;33m  *** \033[1;37mWorking directory: \033[1;34m%s\033[0m

\033[0;90m  ─────────────────────────────────────────────────────────────\033[0m
\033[0;37m  Type a message to start coding. Use \033[1;33m/help\033[0;37m for commands.\033[0m
\033[0;37m  \033[1;33m/model\033[0;37m to switch models. \033[1;33m/quit\033[0;37m to exit. Don't be a wimp.\033[0m
\033[0;37m  Use \033[1;33m@filename\033[0;37m to include file contents. \033[1;33m/set auto-next on\033[0;37m for autopilot.\033[0m
\033[0;90m  ─────────────────────────────────────────────────────────────\033[0m
```

`SplashArt()` is intentionally random, so there is no single exact art block.

## Event Loop

`Update(msg)` is the UI control surface.

### Window resize

`tea.WindowSizeMsg` does this:

- Stores `width` and `height`.
- Resizes the textarea to `width - 4` by `3`.
- Computes viewport height as `height - 7`.
- Reserves side-panel width if the mp3 panel is visible or the tool panel is visible during streaming.
- Creates the viewport on the first size event.
- Enables mouse wheel scrolling with delta `3`.
- Calls `refreshViewport()`.

### Key handling

`tea.KeyMsg` is routed in this order:

1. If the model picker is open, it swallows the key.
2. `up`
   - If the textarea is empty and queued steering messages exist, the last queued message is pulled back into the input and a system message is added:

     ```text
     Unqueued message: <text>
     ```

   - Otherwise, if the cursor is at the top and history exists, history navigation moves upward.
3. `down`
   - If the cursor is at the bottom, history navigation moves downward.
4. `ctrl+c` runs the cancellation ladder.
5. `esc` runs the escape ladder.
6. `enter`
   - Trims the textarea.
   - Ignores empty input.
   - Clears the textarea.
   - Pushes the input into history.
   - Slash commands are dispatched immediately, regardless of focus or busy state.
   - If the agent is streaming, non-command input is queued and a system message is emitted:

     ```text
     Queued message (agent is busy): <text>
     ```

   - Otherwise a visible user message is appended and the text is sent to the agent.
7. `ctrl+p` / `ctrl+n` navigate history explicitly.
8. `pgup` / `pgdown` move the viewport.
9. `ctrl+t` toggles the tool-panel visibility flag.
10. If none of the above consumed the key, MP3 controls may consume it.

### Signal / lifecycle messages

- `SignalMsg` while streaming cancels the active turn with:

  ```text
  Interrupted by signal.
  ```

- `SignalMsg` while idle quits immediately.
- `tea.SuspendMsg` returns `tea.Suspend`.
- `tea.QuitMsg` cancels any active turn, finishes the transcript stream, stops MP3 playback, closes the transcript logger, and quits.

### Agent event messages

`agentEventMsg` and `agentDoneMsg` are wrapper messages that keep the UI on strict turn boundaries.

- `agentEventMsg{ch: ...}` is ignored if the channel does not match `m.eventCh`.
- `agentDoneMsg{ch: ...}` is ignored if the channel does not match `m.eventCh`.

This protects the current turn from late events emitted by a canceled or replaced agent stream.

`waitForAgentEvent(ch)` blocks inside a `tea.Cmd` until one of two things happens:

- The channel produces another `agent.Event`, which becomes an `agentEventMsg`.
- The channel closes, which becomes an `agentDoneMsg`.

## Agent Turn Flow

`sendToAgent` and `sendFollowUpToAgent` both call `startAgentTurn`.

`startAgentTurn` does the same setup for every turn:

1. Cancels any previous turn.
2. Clears queue-arming and escape ladders.
3. Clears active tool state.
4. Captures `m.turnContext = m.focus.Active()`.
5. Converts that context to the per-session transcript key.
6. Calls `m.agent.InitContext(ctxKey)`.
7. Calls `m.agent.SetContext(ctxKey)`.
8. Reads the saved message watermark for that context.
9. Converts the same IRC context into a memory scope.
10. Calls `m.agent.SetScope(...)`.
11. Creates a fresh `context.WithCancel`.
12. Marks the model streaming.
13. Creates a buffered event channel.
14. Starts the agent goroutine.
15. Returns `waitForAgentEvent(ch)`.

The important boundary is that `turnContext` is frozen at submission time. If the user switches focus after sending the message, that later focus does not re-label the turn that is already in flight.

### Agent event handling

`handleAgentEvent` maps agent events into visible UI state:

- `text`
  - If the last visible message is a thinking placeholder, it is replaced with a real agent message.
  - The chunk is appended to `streamBuffer`.
  - The last agent message is updated in place for live streaming.
  - The chunk is appended to the transcript logger.
- `thinking`
  - If a thinking placeholder already exists, its content is extended.
  - Otherwise a new `MsgThink` block with `thinking...` is appended.
- `tool_start`
  - Saves the active tool name and tool-call ID.
  - Adds a visible tool line:

    ```text
    calling <tool>...
    ```

  - Starts the tool panel entry.
- `tool_result`
  - Clears the active tool fields when the tool name matches.
  - Truncates displayed tool output to 20 lines.
  - Emits `MsgTool` or `MsgError` depending on whether the event carried a tool error.
  - Finishes the tool panel entry and stores the result text there truncated to 60 characters.
- `state`
  - Updates the agent state.
  - When the state becomes `StateThinking`, a `MsgThink` placeholder is added with `thinking...`.
- `error`
  - Emits an error message:

    ```text
    Error: <error>
      hint: <hint>
    ```

    The hint line is only present when `llm.ErrorHint(err)` returns one.
- `done`
  - Emits `agentDoneMsg{ch: current channel}`.

`agentDoneMsg` performs the post-turn cleanup:

- Finishes any buffered agent transcript line.
- Marks the model idle.
- Clears the event channel and cancel func.
- Resets active tool state and cancel ladders.
- Syncs the last assistant message from the agent's final display content.
- Plays the notification sound if enabled.
- Updates the tool-panel token/elapsed snapshot.
- Appends any unsaved agent messages into the session log.
- Saves focus and checkpoint sidecars.
- Drains queued steering messages or queues a follow-up turn.

If queued messages are older than `2m`, they are discarded with this exact warning:

```text
Discarded <N> queued message(s) older than 2m0s — context changed. Re-send if still relevant.
```

If fresh queued messages exist, they are combined into a single user message:

```text
[queued msg 1]: first
[queued msg 2]: second
```

If a follow-up is queued by the agent, the UI emits:

```text
*** <label>: continuing...
```

and immediately starts a follow-up turn.

## View Composition

`View()` returns the full screen. If the model is not ready yet, it returns:

```text
initializing bitchtea...
```

Once ready, the screen is composed in this order:

1. Top bar.
2. Separator.
3. Viewport, optionally joined horizontally with the MP3 panel or the tool panel.
4. Separator.
5. Status bar.
6. Separator.
7. Input textarea.

### Top bar

The top bar is built from:

- App name: `bitchtea`.
- Provider or profile name.
- Model name.
- Active context label.
- Flags:
  - `[auto]` when `AutoNextSteps` is enabled.
  - `[idea]` when `AutoNextIdea` is enabled.
- Queue count:
  - `[queued:N]` when messages are queued.
- Clock:
  - Right-aligned `time.Now().Format("3:04pm")`.

The top bar uses `TopBarStyle`.

### Viewport + side panel

The viewport is the scrollback buffer. `refreshViewport()` rebuilds the entire string every time content changes.

Rules:

- `wrapWidth = viewport.Width - 2`.
- Minimum wrap width is `20`.
- Every non-raw message is wrapped with `WrapText`.
- Raw messages are left alone.
- After rebuilding, the viewport is auto-scrolled to the bottom.

The optional side panel is chosen by priority:

1. MP3 panel if `mp3.visible` and width is greater than `90`.
2. Tool panel if the tool panel is visible, the agent is streaming, and width is greater than `80`.
3. No panel otherwise.

The tool panel is only rendered while streaming. Its `Visible` flag can be true while idle, but `View()` will still suppress it until the next active turn.

### Status bar

The left side is:

- `[<agent nick>] idle`
- `[<agent nick>] <spinner> thinking...`
- `[<agent nick>] <spinner> running tools...`

The right side is a summary of:

- Tool stats, in map iteration order.
- MP3 status if tracks exist.
- Background status if any background activity exists.
- Token estimate from the agent.
- Elapsed turn time.

When the agent is active, the status bar switches from `BottomBarStyle` to `ThinkingBarStyle`.

### Input area

The input area is the Bubbles textarea with the prompt `>> `. It is a 3-line field and is always the final band on screen.

## Message Formatting

`ChatMessage.Format()` defines the exact viewport line/block content for each message type.

The visible prefixes are:

```text
user:   [HH:MM] <nick> content
agent:  [HH:MM] <nick> <markdown-rendered content>
system: [HH:MM] *** content
error:  [HH:MM] !!! content
tool:   [HH:MM]  → nick: content
think:  [HH:MM] 💭 content
raw:    content unchanged
```

The important details:

- Agent messages are markdown-rendered with Glamour when they look like markdown.
- `Width < 20` falls back to a width of `100` for markdown rendering.
- Raw messages bypass wrapping and formatting.
- `ChatMessage.Context` is not shown in the viewport. It is persisted for session/session-adjacent routing, not for display.

### Markdown rendering

`RenderMarkdown(content, width)`:

- Returns the input unchanged if the string is empty.
- Returns the input unchanged if the string does not look like markdown.
- Uses Glamour with terminal auto-style and the requested wrap width.
- Caches renderers by width, keeping at most 8 widths alive.
- Falls back to raw text on renderer or render failure.

The markdown heuristic is intentionally cheap and intentionally fuzzy. It treats any of these as a markdown hint:

- ````
- `**`
- `##`
- `- `
- `* `
- `> `
- `1. `
- `[`
- `![`

This means bracket-heavy plain text can be rendered as markdown even when it is not actually markdown. The tests currently verify the positive path and the wrapping behavior, not the false-positive boundary.

`WrapText(s, width)` is ANSI-aware and uses `ansi.Wordwrap`. It returns the original string when `width <= 0`.

## Theme and Styles

The built-in theme is named `BitchX`.

Theme colors:

- Cyan: `14`
- Green: `10`
- Magenta: `13`
- Yellow: `11`
- Red: `9`
- Blue: `12`
- White: `15`
- Gray: `8`
- Dark background: `0`
- Bar background: `4`

Style roles:

- `TopBarStyle` and `BottomBarStyle`: bold white on the bar background.
- `TimestampStyle`: gray.
- `UserNickStyle`: bold green.
- `AgentNickStyle`: bold magenta.
- `SystemMsgStyle`: bold yellow.
- `ErrorMsgStyle`: bold red.
- `ToolCallStyle`: cyan.
- `ToolOutputStyle`: gray in the current theme rebuild path.
- `InputPromptStyle`: bold cyan.
- `InputTextStyle`: white.
- `SeparatorStyle`: gray.
- `ThinkingStyle`: italic magenta.
- `StatsStyle`: gray.
- `DimStyle`: gray.
- `BoldWhite`: bold white.
- `ThinkingBarStyle`: bold white on the bar background.

`Separator(width)` returns a gray line of `─` repeated `width` times.

The tests only check that these styles render non-empty and that the active theme is `BitchX`. They do not snapshot the exact ANSI escape sequences.

## Tool Panel

`ToolPanel` is the collapsible tool sidebar.

Fixed width:

```text
28
```

Behavior:

- `StartTool(name)` appends a `running` status entry and increments the summary count for that tool.
- `FinishTool(name, result, isError)` walks backward to find the most recent running entry with the same name, stamps duration, status, and truncated result.
- `Clear()` resets the live tool list only. It is currently unused by the live model loop.
- `Render(height)` returns `""` if hidden or too short.

Rendered panel contents:

- Header: `Tools`
- Summary counts: `name(count)` in map iteration order.
- Token estimate:
  - `<1000` tokens: `Tokens: ~<n>`
  - `>=1000` tokens: `Tokens: ~<n.k>`
- Elapsed time: `Time: <duration truncated to second>`
- Recent tool calls:
  - `◉` for running
  - `✓` for done
  - `✗` for error

Current gap: the stats map is iterated in map order, so the summary ordering is not deterministic.

Current gap: the live model never calls `ToolPanel.Clear()`, so the sidebar accumulates tool history and counts across turns for the lifetime of the process.

## MP3 Panel

`mp3Controller` owns the optional MP3 sidebar and the bottom-bar playback status.

Constants:

- Panel width: `34`
- Status-bar width: `38`
- Visualizer bars: `8`
- Progress bar width: `10`
- Track-name truncation width: `24`

Library root:

- Default: `~/.bitchtea/mp3`
- Fallback when `UserHomeDir` fails: `.bitchtea/mp3`

Behavior:

- `scan()` reads the library directory.
  - Missing directory is not an error; it simply clears the track list.
  - Only `.mp3` files are included.
  - Duration decode errors are ignored and treated as zero duration.
  - Tracks are sorted case-insensitively by filename.
  - The current track is preserved by path when possible.
- `rescan()` returns:
  - `MP3 scan failed: <err>`
  - `No MP3s found in <dir>`
  - `Loaded <N> track(s) from <dir>`
- `toggle()`:
  - Hidden -> visible: rescans and starts playback if tracks exist.
  - Visible -> hidden: `MP3 panel hidden.`
- `playIndex(idx)` starts the process via the first available player in this order:
  - `mpv`
  - `ffplay`
  - `mpg123`
- `togglePause()` toggles pause/resume when a process exists.
- `handleDone()` ignores stale generations, clears playback state, and auto-advances when there are multiple tracks.

The exact status text format is:

```text
♫ <track> <state-icon> [progress] <elapsed>/<total> <visualizer>
```

The state icon is:

- `▶` playing
- `▌▌` paused
- `■` loaded but not playing

The progress bar and visualizer are deterministic helpers, not audio analysis:

- `renderMP3ProgressBar` is based on elapsed/total ratio.
- `renderMP3Visualizer` is seeded from the track path and elapsed seconds; it is not sampling the audio waveform.

Exact panel text:

```text
MP3 Player
Dir: <library dir>
Controls: space pause, ←/j prev, →/k next

Drop .mp3 files into the library dir.
```

When tracks exist, the panel adds `Now Playing` and `Playlist` sections.

Current gap: tests use a fake process and stubbed duration reader. Real `mpv` / `ffplay` / `mpg123` integration is not covered.

## Model Picker Overlay

The model picker is a scrollback overlay, not a separate pane.

`/models` opens it when the active service matches a catalog provider.

Behavior:

- The catalog is loaded through a package-level seam.
- The service key comes from `cfg.Service`, not `cfg.Provider`.
- Provider IDs are matched case-insensitively.
- The default-large model is floated to the front when present.
- Substring filtering is case-insensitive.
- Cursor movement clamps; there is no wraparound.
- Enter selects the current item.
- Esc / Ctrl+C cancels.
- Printable runes append to the filter.
- Backspace removes one rune.
- PgUp / PgDown move by `pickerVisibleRows` (`12`).

The picker renders as a raw message block with these literal UI lines:

```text
<title>
filter> <query>
> selected item
  unselected item
  ...
  enter=pick  esc=cancel  type=filter
```

If there are no matches, it shows:

```text
  (no matches — backspace to widen)
```

Important wiring detail:

- `openPicker` appends the initial picker render as a `MsgRaw` message in scrollback.
- `rerenderPicker` mutates that message in place on every filter or cursor update.
- The picker swallows keys while open so the textarea never sees them.

Current gap: only the initial picker render is appended through `addMessage`; subsequent rerenders mutate the in-memory viewport message and do not create new transcript entries.

## Transcript Logger

The transcript logger is not the viewport and not the session JSONL. It is a daily human-readable log under `~/.bitchtea/logs/<date>.log`.

It uses kernel-level file locking so multiple bitchtea processes do not interleave writes mid-line.

Message rules:

- `MsgThink` is never logged.
- Empty `MsgAgent` messages are ignored.
- Streaming assistant chunks are buffered and flushed as one line.
- Non-agent messages force a flush before they are written.
- `MsgRaw` is sanitized and written as raw text.

Transcript line forms:

```text
[HH:MM] <nick> text
[HH:MM] *** text
[HH:MM] !!! text
[HH:MM] -> tool:
    indented result
```

Current gap: if a raw overlay such as the model picker is opened, the initial `MsgRaw` block is logged. That is intentional in the current code, but it means some UI-only overlays also enter the daily transcript.

## Background Activity

Background activity is runtime-only UI state. It is not session-persisted.

`NotifyBackgroundActivity` does this:

- Normalizes the context label.
- Stamps the current time if none is provided.
- Replaces an empty summary with `activity waiting`.
- Appends the activity to the in-memory queue.
- Increments `backgroundUnread`.

`backgroundStatus()` returns a compact bottom-bar summary:

```text
bg:<unread-count> /activity <latest activity line>
```

`backgroundActivityReport()` returns the full report shown by `/activity`:

```text
Background activity:
  09:01 [#ops] <deploy-bot> build failed
```

The `/activity` command either shows the report or clears the queue.

Exact outputs:

- With queued activity:

  ```text
  Background activity:
    [HH:MM] [#context] <sender> summary
  ```

- Clear:

  ```text
  Cleared <N> background activity notice(s).
  ```

Current gap: the queue is only cleared by `/activity clear`. Merely viewing it resets the unread count but does not remove the stored history.

## Context, Focus, and Membership

These are not visual widgets, but they drive the screen.

### FocusManager

`FocusManager` tracks the ordered open contexts and the active one.

It starts with:

```text
#main
```

It supports:

- `SetFocus`
- `Ensure`
- `Remove`
- `All`
- `ToState`
- `RestoreState`
- `Save`
- `LoadFocusManager`

Current gap: `RestoreState` clamps oversized active indexes but does not guard negative ones. The tests only cover the oversized case.

### MembershipManager

`MembershipManager` tracks persona membership per channel key.

It supports:

- `Invite`
- `Part`
- `Members`
- `IsJoined`
- `ToState`
- `RestoreState`
- `Save`
- `LoadMembershipManager`

`/channels` renders membership brackets by reading this manager.

Current gap: invalid membership sidecar files fall back silently to an empty manager.

## Slash Commands That Affect the Screen

`handleCommand` is the slash router. It uses `strings.Fields`, so it is not a shell parser and it does not support quoting. Unknown commands emit:

```text
Unknown command: /name. Try /help, genius.
```

The main UI-facing commands are:

- `/help`
  - Emits the exact `helpCommandText` block from `internal/ui/commands.go`.
- `/clear`
  - Clears visible chat history only.
- `/restart`
  - Resets the agent, clears visible chat, clears the queue, and starts a fresh session file.
  - Visible output:

    ```text
    *** Conversation restarted. Fresh context.
    ```

- `/compact`
  - Emits either:

    ```text
    Can't compact while agent is working. Be patient.
    ```

    or:

    ```text
    Compacted: ~<before> -> ~<after> tokens
    ```

- `/copy [n]`
  - Copies the last or nth assistant response.
  - Output:

    ```text
    Copied <target> via <method>.
    ```

- `/tokens`
  - Emits:

    ```text
    ~<tokens> tokens | $<cost> | <messages> messages | <turns> turns
    ```

- `/debug on|off`
  - Toggles verbose API logging.
  - Output:

    ```text
    Debug mode: ON
    Debug mode: OFF
    ```

- `/activity [clear]`
  - Shows or clears the background queue.
- `/mp3 [cmd]`
  - Toggles the MP3 panel or controls playback.
- `/theme`
  - Disabled in the current build:

    ```text
    Theme switching is disabled. Built-in theme: BitchX.
    ```

- `/memory`
  - Shows `MEMORY.md` and, when not in root context, the scoped `HOT.md`.
- `/sessions`
  - Lists saved sessions with pagination.
- `/resume <number>`
  - Loads a saved session and replays its visible history.
- `/tree`
  - Dumps the session tree as raw ANSI-cyan text.
- `/fork`
  - Forks the current session at the latest entry.
- `/set`
  - Shows or mutates config values. `service` is a UI-side verbatim field and is shown alongside the standard settings.
- `/profile`
  - Shows, loads, saves, or deletes profiles.
- `/models`
  - Opens the model picker overlay for the active service.
- `/join`, `/part`, `/query`, `/channels`, `/msg`, `/invite`, `/kick`
  - Manage IRC-style focus, membership, and context-specific catch-up.

### Settings and profiles

`/set` routes these keys specially:

- `provider`
- `model`
- `baseurl`
- `apikey`
- `service`

`service` is accepted verbatim and does not clear the loaded profile tag.

`handleBaseURLCommand` and `handleProviderCommand` both print the endpoint preview:

```text
requests -> <baseurl>/chat/completions
requests -> <baseurl>/messages
```

depending on provider.

`providerTransportHint` may add warnings when the base URL and provider look mismatched. Those warnings are visible system messages, not silent logs.

### Routing commands

- `/join #channel`
  - Switches focus to the channel and persists focus sidecar.
  - Visible output:

    ```text
    Joined #channel
    ```

- `/part`
  - Leaves the current context or a named context.
  - Visible output:

    ```text
    Parted #old — now in #new
    ```

- `/query nick`
  - Opens a direct context and persists focus sidecar.
  - Visible output:

    ```text
    Query open: nick
    ```

- `/channels`
  - Lists all open contexts, marks the active one with `* `, and shows membership brackets when present.
- `/msg nick text`
  - In idle mode, emits a visible user message:

    ```text
    →nick: text
    ```

    and sends the agent `[to:nick] text`.
  - In streaming mode, queues `[to:nick] text` and prints:

    ```text
    Queued /msg to nick (agent busy).
    ```

- `/invite persona [#channel]`
  - Adds membership, saves membership sidecar, prints a join notice, and appends a catch-up block.
  - Current gap: the catch-up is visible only; it is not injected into the agent transcript.
- `/kick persona`
  - Removes membership, saves membership sidecar, and prints a kick notice.

### Model picker outputs

When the active service is unset or the catalog is unavailable, `/models` emits error text rather than opening the picker.

Exact error forms:

```text
models: no active service — set one with /set service <name> or load a profile (e.g. /profile openrouter).
models: catalog is empty — try BITCHTEA_CATWALK_AUTOUPDATE=true with BITCHTEA_CATWALK_URL set, or wait for the embedded snapshot.
models: no catalog data for service "<service>" — try BITCHTEA_CATWALK_AUTOUPDATE=true or check /profile.<hint>
```

When the picker opens, the title is:

```text
models for <service> (<N> total) — type to filter
```

Picker completion messages:

- Enter with selection: the picker closes and `applyModelSelection` emits:

  ```text
  *** Model switched to: <model>
  ```

- Enter with no selection:

  ```text
  Picker: no selection (filter excludes everything).
  ```

- Escape / Ctrl+C:

  ```text
  Picker cancelled.
  ```

## Tests and Coverage Gaps

The package has good behavioral tests in some places and shape-only tests in others.

Better failure-mode coverage:

- `model_turn_test.go` exercises the cancel ladders, stale channel guards, queue draining, follow-up turns, and context-sensitive view output.
- `resume_v1_test.go` exercises the actual resume path across v0/v1 session formats.
- `models_command_test.go` drives the picker through `Update` and checks selection behavior.

Shape-heavy coverage:

- `message_test.go` checks substring presence and panic safety, not exact ANSI escape output.
- `render_test.go` checks heuristics and width behavior, not exact Glamour snapshots.
- `toolpanel_test.go` checks that the panel renders, not the exact border or map-order summary.
- `themes_test.go` checks that styles are non-empty and the theme name is `BitchX`, not exact palette rendering.
- `mp3_test.go` checks status-string shape, not exact player integration or live audio output.
- `clipboard_test.go` stubs OS clipboard paths and validates the fallback order, but it does not exercise a real terminal clipboard or real `pbcopy` / `xclip` installation.

Current wiring gaps and stale helpers:

- `saveCurrentContextMessages()` in `internal/ui/context_helpers.go` is currently unused. The live session append path is the `agentDoneMsg` handler in `model.go`.
- `ToolPanel.Clear()` exists but is not called by the live turn loop, so tool history accumulates across turns.
- `FocusManager.RestoreState()` does not clamp negative active indexes.
- `/invite` catch-up is not injected back into the agent context.
- `ResumeSession()` appends replayed visible history to the existing chat slice; it does not clear prior viewport content first.
- The tool summary order in the status bar and tool panel is nondeterministic because it iterates Go maps.
- The MP3 panel uses fake visualizers and player fallbacks; it is not doing real waveform analysis.

That is the current UI contract. If a change alters a string, a panel width, or a state transition here, update this doc and the tests together.
