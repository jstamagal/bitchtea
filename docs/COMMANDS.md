# 🦍 BITCHTEA COMMAND CATALOG 🦍

Comprehensive list of all commands, directives, and flags for the `bitchtea` project.

## REPL Commands
Slash commands executed within the terminal UI.

### /activity
- **Syntax**: `/activity [clear]`
- **Source**: internal/ui/commands.go:39
- **Args**:
  - `clear` (string, optional): Clears the background activity queue.
- **Effect**: Displays current background task status or clears the log.
- **Notes**: Useful when tools are running in the background.

### /apikey
- **Syntax**: `/apikey <key>`
- **Source**: internal/ui/commands.go:47
- **Args**:
  - `key` (string, required): The API key for the current provider.
- **Effect**: Sets the API key in the current session context.
- **Notes**: Overrides environment variables; cleared if a new profile is loaded.

### /auto-idea
- **Syntax**: `/auto-idea`
- **Source**: internal/ui/commands.go:36
- **Args**: None
- **Effect**: Toggles the `AutoNextIdea` configuration setting.
- **Notes**: When on, the agent may automatically suggest new ideas/steps.

### /auto-next
- **Syntax**: `/auto-next`
- **Source**: internal/ui/commands.go:35
- **Args**: None
- **Effect**: Toggles the `AutoNextSteps` configuration setting.
- **Notes**: When on, the agent automatically executes sequential tool steps.

### /baseurl
- **Syntax**: `/baseurl <url>`
- **Source**: internal/ui/commands.go:46
- **Args**:
  - `url` (string, required): The base URL for the LLM API.
- **Effect**: Sets a custom API endpoint (e.g., for Ollama or LocalAI).
- **Notes**: bitchtea appends specific endpoint paths (`/chat/completions`) automatically.

### /channels (alias /ch)
- **Syntax**: `/channels`
- **Source**: internal/ui/commands.go:53
- **Args**: None
- **Effect**: Lists all open IRC-style conversation contexts.

### /clear
- **Syntax**: `/clear`
- **Source**: internal/ui/commands.go:31
- **Args**: None
- **Effect**: Wipes the current scrollback buffer in the UI.

### /compact
- **Syntax**: `/compact`
- **Source**: internal/ui/commands.go:32
- **Args**: None
- **Effect**: Triggers manual context compaction.
- **Notes**: Flushes old messages to daily memory to save tokens.

### /copy
- **Syntax**: `/copy [n]`
- **Source**: internal/ui/commands.go:33
- **Args**:
  - `n` (integer, optional): The index of the assistant message to copy.
- **Effect**: Copies the specified (or latest) assistant response to the clipboard.

### /debug
- **Syntax**: `/debug on|off`
- **Source**: internal/ui/commands.go:37
- **Args**:
  - `status` (on/off, required): Toggle state.
- **Effect**: Enables/disables verbose logging of API requests and tool events.

### /fork
- **Syntax**: `/fork`
- **Source**: internal/ui/commands.go:45
- **Args**: None
- **Effect**: Creates a new session branch from the current point.

### /help (alias /h)
- **Syntax**: `/help`
- **Source**: internal/ui/commands.go:28
- **Args**: None
- **Effect**: Displays the built-in command summary.

### /invite
- **Syntax**: `/invite <nick>`
- **Source**: internal/ui/commands.go:55
- **Args**:
  - `nick` (string, required): The persona/agent to invite.
- **Effect**: Adds a persona to the current channel context.

### /join
- **Syntax**: `/join <#channel>`
- **Source**: internal/ui/commands.go:50
- **Args**:
  - `channel` (string, required): The channel name to join.
- **Effect**: Switches focus to a new or existing channel context.

### /kick
- **Syntax**: `/kick <nick>`
- **Source**: internal/ui/commands.go:56
- **Args**:
  - `nick` (string, required): The persona to remove.
- **Effect**: Removes a persona from the current channel context.

### /memory
- **Syntax**: `/memory`
- **Source**: internal/ui/commands.go:42
- **Args**: None
- **Effect**: Displays the content of the project's `MEMORY.md`.

### /model
- **Syntax**: `/model <name>`
- **Source**: internal/ui/commands.go:30
- **Args**:
  - `name` (string, required): The LLM model ID (e.g., `gpt-4`).
- **Effect**: Switches the active model for the next turn.

### /mp3
- **Syntax**: `/mp3 [rescan|play|pause|next|prev]`
- **Source**: internal/ui/commands.go:40
- **Args**:
  - `cmd` (string, optional): Control command for the MP3 player.
- **Effect**: Toggles the MP3 panel or controls music playback.

### /msg
- **Syntax**: `/msg <nick> <text>`
- **Source**: internal/ui/commands.go:54
- **Args**:
  - `nick` (string, required): Recipient persona.
  - `text` (string, required): Message content.
- **Effect**: Sends a one-shot message to a persona without switching focus.

### /part
- **Syntax**: `/part [#channel]`
- **Source**: internal/ui/commands.go:51
- **Args**:
  - `channel` (string, optional): The channel to leave.
- **Effect**: Exits the specified or current context.

### /profile
- **Syntax**: `/profile save|load|delete <name>`
- **Source**: internal/ui/commands.go:49
- **Args**:
  - `action` (save/load/delete, required): Profile operation.
  - `name` (string, required): Profile identifier.
- **Effect**: Manages named configurations for different providers/models.

### /provider
- **Syntax**: `/provider <name>`
- **Source**: internal/ui/commands.go:48
- **Args**:
  - `name` (string, required): openai or anthropic.
- **Effect**: Switches the API transport provider.

### /query
- **Syntax**: `/query <nick>`
- **Source**: internal/ui/commands.go:52
- **Args**:
  - `nick` (string, required): Persona nick.
- **Effect**: Opens a private direct message context with a persona.

### /quit (alias /q, /exit)
- **Syntax**: `/quit`
- **Source**: internal/ui/commands.go:27
- **Args**: None
- **Effect**: Exits the application.

### /sessions (alias /ls)
- **Syntax**: `/sessions`
- **Source**: internal/ui/commands.go:43
- **Args**: None
- **Effect**: Lists all saved session JSONL files.

### /set
- **Syntax**: `/set [key [value]]`
- **Source**: internal/ui/commands.go:29
- **Args**:
  - `key` (string, optional): Setting name.
  - `value` (string, optional): New value.
- **Effect**: Displays current settings or modifies a specific one.

### /sound
- **Syntax**: `/sound`
- **Source**: internal/ui/commands.go:38
- **Args**: None
- **Effect**: Toggles notification sounds (bing!).

### /theme
- **Syntax**: `/theme`
- **Source**: internal/ui/commands.go:41
- **Args**: None
- **Effect**: Rotates through available UI color themes.

### /tokens
- **Syntax**: `/tokens`
- **Source**: internal/ui/commands.go:34
- **Args**: None
- **Effect**: Shows an estimate of current context token usage.

### /tree
- **Syntax**: `/tree`
- **Source**: internal/ui/commands.go:44
- **Args**: None
- **Effect**: Displays a visual tree of the session history and branches.

## RC Directives
Configuration directives used in `.bitchtearc`. Use the syntax `set <key> <value>`.

### set apikey
- **Syntax**: `set apikey <value>`
- **Source**: internal/config/rc.go:138
- **Args**: API key string.
- **Effect**: Sets global API key on startup.

### set auto-idea
- **Syntax**: `set auto-idea on|off`
- **Source**: internal/config/rc.go:111
- **Args**: Boolean toggle.
- **Effect**: Enables/disables automatic idea suggestions on startup.

### set auto-next
- **Syntax**: `set auto-next on|off`
- **Source**: internal/config/rc.go:109
- **Args**: Boolean toggle.
- **Effect**: Enables/disables automatic tool chaining on startup.

### set baseurl
- **Syntax**: `set baseurl <value>`
- **Source**: internal/config/rc.go:145
- **Args**: URL string.
- **Effect**: Sets custom API base URL on startup.

### set model
- **Syntax**: `set model <value>`
- **Source**: internal/config/rc.go:132
- **Args**: Model ID string.
- **Effect**: Sets default model on startup.

### set nick
- **Syntax**: `set nick <value>`
- **Source**: internal/config/rc.go:151
- **Args**: Nick string.
- **Effect**: Sets user display name in contexts.

### set profile
- **Syntax**: `set profile <value>`
- **Source**: internal/config/rc.go:157
- **Args**: Profile name.
- **Effect**: Loads a saved profile on startup.

### set provider
- **Syntax**: `set provider openai|anthropic`
- **Source**: internal/config/rc.go:124
- **Args**: Provider name.
- **Effect**: Sets default API provider on startup.

### set sound
- **Syntax**: `set sound on|off`
- **Source**: internal/config/rc.go:107
- **Args**: Boolean toggle.
- **Effect**: Enables/disables notification sounds on startup.

## CLI Flags
Arguments passed to the `bitchtea` binary.

### --auto-next-idea
- **Syntax**: `bitchtea --auto-next-idea`
- **Source**: main.go:137
- **Args**: None.
- **Effect**: Enables automatic idea suggestions.

### --auto-next-steps
- **Syntax**: `bitchtea --auto-next-steps`
- **Source**: main.go:135
- **Args**: None.
- **Effect**: Enables automatic tool chaining.

### --headless (-H)
- **Syntax**: `bitchtea --headless`
- **Source**: main.go:139
- **Args**: None.
- **Effect**: Runs in non-interactive mode. Requires `--prompt` or piped stdin.

### --help (-h)
- **Syntax**: `bitchtea --help`
- **Source**: main.go:141
- **Args**: None.
- **Effect**: Prints usage information and exits.

### --model (-m)
- **Syntax**: `bitchtea --model <name>`
- **Source**: main.go:111
- **Args**:
  - `name` (string, required): Model ID.
- **Effect**: Overrides default model.

### --profile (-p)
- **Syntax**: `bitchtea --profile <name>`
- **Source**: main.go:127
- **Args**:
  - `name` (string, required): Profile name.
- **Effect**: Loads a specific configuration profile.

### --prompt
- **Syntax**: `bitchtea --prompt <text>`
- **Source**: main.go:131
- **Args**:
  - `text` (string, required): Initial prompt.
- **Effect**: Seeds the conversation with a message. Required for headless mode if no stdin.

### --resume (-r)
- **Syntax**: `bitchtea --resume [path]`
- **Source**: main.go:119
- **Args**:
  - `path` (string, optional): Path to session JSONL or "latest".
- **Effect**: Resumes a previous conversation.
