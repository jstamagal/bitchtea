# 🦍 THE BITCHTEA SCROLLS: STREAMING & LLM

Bitchtea talks to models through a unified streaming interface.

## RC Startup Flow (`~/.bitchtea/bitchtearc`)

Bitchtea reads startup commands from an RC file at `~/.bitchtea/bitchtearc` (resolved by `config.RCPath()`, which joins `~/.bitchtea/` with `bitchtearc`). This file is optional; if it does not exist, startup proceeds normally.

### Grammar

Each non-blank, non-comment line is treated as a command. Lines starting with `#` and blank lines are ignored. Commands are written without the leading `/` that the TUI uses:

```
# bitchtearc -- startup commands
set provider anthropic
set model claude-opus-4-6

# blank lines and comments are ignored
join #code
query buddy
```

Two kinds of lines are supported:

**`set` lines** -- modify configuration before the TUI boots. Recognized keys:

| Key          | Value                   | Effect                              |
|-------------|-------------------------|--------------------------------------|
| `provider`  | `openai` or `anthropic` | Set LLM provider                     |
| `model`     | model name              | Set model (e.g., `claude-opus-4-6`)  |
| `apikey`    | API key                 | Set API key                          |
| `baseurl`   | URL                     | Set API base URL                     |
| `nick`      | name                    | Set user nick                         |
| `profile`   | profile name            | Load a saved or built-in profile     |
| `sound`     | `on`/`off`              | Enable/disable notification sounds   |
| `auto-next` | `on`/`off`              | Enable/disable auto next-step prompts|
| `auto-idea` | `on`/`off`              | Enable/disable auto improvement ideas|

**Non-set lines** -- any other text is treated as a slash command without the leading `/`. These are executed by the TUI after startup, with message output suppressed (no visible startup chatter). Examples: `join #channel`, `query nick`, `set` (show settings).

### Startup Ordering

The RC file is processed in `main.go` via `applyStartupConfig()`:

1. `config.ParseRC()` reads the file and returns non-blank, non-comment lines.
2. `config.ApplyRCSetCommands(cfg, lines)` iterates the lines: `set` lines are applied to the config immediately; non-set lines are collected and returned separately.
3. If a `--profile` flag is given, the profile is loaded and applied on top of the RC-file config.
4. CLI flags (`--model`, `-m`, etc.) are then re-parsed on top, so explicit flags always win.
5. After session restore (if any), the remaining non-set RC lines are passed to `buildStartupModel()` → `m.ExecuteStartupCommand(line)` for each.

Within `ExecuteStartupCommand` (`internal/ui/model.go:327-341`), each command runs with `suppressMessages = true` so the startup commands do not produce visible chat messages. The model state (e.g., active context after `join #code`) is applied silently.

### Session Restore Interaction

Session restore happens before RC commands are executed. If `--resume` is provided, the session's entries are loaded into the model first, then RC commands run on top. This means RC commands like `join #code` will switch the active context after the session messages are loaded, leaving the session's message history visible but focusing a different context.

### Profile vs Manual Overrides

Loading a profile via `set profile <name>` applies all profile values (provider, model, base URL, API key). Any subsequent manual `set` command (e.g., `set model override-model`) clears the profile field (`cfg.Profile = ""`) so the profile is no longer considered active -- even though the remaining settings from the profile persist until explicitly changed. This prevents a stale profile label from masking manual overrides.

Invalid provider values are silently ignored (the config remains unchanged).

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

## Sound System

Bitchtea can play terminal bell sounds to signal the end of an agent turn.

### Toggle

Notifications are disabled by default. Enable or disable via `/set`:

```
/set sound on
/set sound off
```

### When Sounds Play

A sound plays once when an agent turn completes -- after all streaming text and tool calls finish and the final `done` event is received. This is the only trigger in the current UI (`internal/ui/model.go:651-652`).

### Sound Type

The sound type is configured through the `SoundType` config field (default: `"bell"`). It is not directly settable via `/set` -- only the notification on/off toggle is exposed at runtime. The sound type can be set through a profile or by modifying the config directly.

The `internal/sound` package defines these sound types:

| Sound    | Bell pattern | When used |
|----------|-------------|-----------|
| `bell`   | 1 BEL       | Default   |
| `done`   | 1 BEL       | Completion |
| `success`| 1 BEL       | Success   |
| `error`  | 3 BEL       | Error     |

All types produce one BEL character except `error`, which produces three.

### Implementation

Sound is implemented in `internal/sound/sound.go`. It writes the ASCII BEL character (`\a`, 0x07) to `os.Stdout` via the package-level `Output` writer (overridable in tests). There are no external audio dependencies -- the sound is purely a terminal bell character, and whether the user hears an audible beep, sees a visual flash, or nothing at all depends on the terminal emulator's bell configuration.

The package also exports `Beep()`, `Success()`, `Error()`, and `Done()` convenience functions, but only `Play()` is currently called from the UI layer.

## Headless Mode

Bitchtea can run a single prompt without the TUI via the `--headless` (or `-H`) flag. This is useful for scripting, pipes, and integration with other tools.

### Usage

```bash
# Prompt from a flag
bitchtea --headless --prompt "list all Go files in the project"

# Pipe a prompt from stdin
echo "check the build" | bitchtea --headless

# Combine flag and stdin (concatenated with a newline)
echo "then summarize" | bitchtea --headless --prompt "list all Go files"

# Resume a session in headless mode
bitchtea --headless --resume latest --prompt "continue where we left off"
```

When `--headless` is set without `--prompt` or piped stdin, bitchtea exits with an error (`main.go:240-242`).

### Output Streams

Headless mode splits output across two streams (`runHeadlessLoop`, `main.go:257-322`):

**stdout** -- model text output only. Each `text` event from the agent stream is written to stdout verbatim. This is the content the model generates.

**stderr** -- tool and status events in a structured machine-readable format:
- `[tool] <name> args=<truncated-json>` -- when a tool call starts
- `[tool] <name> result=<truncated-text>` -- when a tool succeeds
- `[tool] <name> error=<message> result=<truncated-text>` -- when a tool fails
- `[status] thinking` -- model is reasoning
- `[status] tool_call` -- model is issuing tool calls
- `[status] idle` -- no active state
- `[auto] <label>` -- when a follow-up prompt is auto-injected

Tool args and results are truncated at 200 characters (newlines replaced with `\n`) to keep stderr readable. Status lines use `[status]` prefix for easy filtering.

### Follow-Up Loop

After a turn completes, the agent may queue a follow-up prompt (`ag.MaybeQueueFollowUp()`). If one exists, headless mode automatically continues the conversation by sending the follow-up and streaming the next response. This repeats until no more follow-ups are queued. Each follow-up is announced on stderr with `[auto] <label>`.

A trailing newline is inserted between follow-up turns when the previous text output did not end with one, so each turn starts on a fresh line.

### Resume in Headless Mode

The `--resume` flag works with `--headless`. The session is loaded first, then the prompt is sent. This allows continuing a previous conversation non-interactively.

### Exit Codes

| Exit code | Meaning                                  |
|-----------|------------------------------------------|
| 0         | Success -- prompt processed completely   |
| 1         | Error -- bad args, missing API key, etc. |
