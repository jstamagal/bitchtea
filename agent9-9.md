# Agent 9 -- Extreme Minimalist and Digital Luddite

I look at this codebase and I see a 1,231-line `model.go`. I see a `sound/` package with five functions that all do the same thing. I see six ANSI art splash screens baked into the binary. I see a theme system with one theme and a struct definition repeated three times. I see a cost tracker that hardcodes prices from 2024 and will be wrong in six months. I see a tool panel sidebar that duplicates stats already shown in the status bar. This is not a terminal tool. This is a monument to feature creep dressed up in IRC nostalgia.

## Set 1 -- Five Instances of Needless Complexity or Bloated Dependencies (p < 0.5, the mean)

### 1. `sound/sound.go` -- An Entire Package for `print("\a")` -- [refactor]

The sound package contains five functions (`Play`, `Beep`, `Success`, `Error`, `Done`) that all ultimately call `print("\a")`. The difference between "success" and "error" is whether it prints one bell or three. This is 38 lines of code that could be replaced by a single inline `fmt.Print("\a")` wherever notification is needed. The package adds an import, a dependency edge, and an abstraction layer for something the terminal already does natively. Delete the package. If you want configurable bell behavior, a boolean flag in config is sufficient -- you do not need a sound abstraction layer.

### 2. `themes.go` -- A Theme System with One Theme and a Duplicated Struct -- [refactor]

The `Theme` var and the `themes` map both define the same anonymous struct with 12 fields. The `SetTheme` function copies it field-by-field via an identical struct literal. There is exactly one theme in the map ("bitchx") and four placeholder names in the README (nord, dracula, gruvbox, monokai) that do not exist. The `rebuildStyles` function recreates 15 lipgloss styles every time a theme changes, but the theme never changes because there is only one. Either commit to the theme system and actually add the themes, or strip it to global style variables initialized once in `init()`. The current state is the worst of both worlds: the complexity of a theming engine with the variety of a monochrome terminal.

### 3. `art.go` -- Six Splash Screens at 105 Lines -- [refactor]

Six hardcoded ANSI art strings taking up 105 lines in the binary. One of them is the word "bitchtea" in ASCII art. Two are decorative boxes. One is BitchX-style figlet. On a tool meant for rapid coding sessions, the user sees this exactly once and then scrolls past it forever. A single one-line string constant would convey the same identity. If the splash is truly the "soul of the application" (as the comment claims), the application has a very cheap soul. Pick one splash. Delete the other five. Free 80 lines.

### 4. `cost.go` -- Hardcoded Model Pricing That Decays -- [chore]

The pricing map hardcodes per-million-token costs for 12 models with a comment "as of 2024." Pricing changes. Models get renamed. New models launch. This file will be perpetually stale and any cost estimate it produces will be misleading. Either fetch pricing from an API or external config, or remove the cost tracking entirely and let users check their provider dashboard. Half-accurate cost estimates are worse than no cost estimates -- they breed false confidence in spending.

### 5. `model.go` handleCommand -- A 430-Line Switch Statement -- [refactor]

The `handleCommand` method is 430 lines long with 20+ cases, each doing inline string formatting, validation, and state mutation. This is not a function. This is a dispatch table pretending to be a function. The `/profile` subcommand alone is 70 lines of nested switch statements. Every new command makes this method longer. Commands should be registered as first-class objects with a `Name()`, `Help()`, and `Run()` method. The dispatch loop should be five lines. The current structure is unmaintainable at 1,231 lines total and will only get worse.

## Set 2 -- Five Radically Simplified Features (p < 0.2, the tail)

### 6. Stateless Zero-Config Mode -- [feat]

Strip profiles, `/apikey`, `/baseurl`, `/provider`, and `DetectProvider` entirely. Read one environment variable: `BITCHTEA_API_KEY`. If it starts with `sk-ant`, use Anthropic. Otherwise, use OpenAI. One env var. Zero config files. Zero profile persistence. The tool should start working the instant you install it, not after you configure three connection parameters. The current config flow has four separate commands for what should be one environment variable assignment in `.bashrc`.

### 7. Single-File Session, Not a Tree -- [refactor]

The session system supports fork, tree visualization, parent IDs, and branch tags. The `/tree` command renders a linear list with box-drawing characters. The `/fork` command copies entries to a new file. This is branching complexity for a tool where 99% of users will have one continuous session. Flatten session persistence to a single append-only JSONL file. If someone wants to branch a conversation, they can copy the file with `cp`. The tree data structure adds 50 lines of session code and 80 lines of UI code for a feature used by approximately nobody.

### 8. Remove the Tool Panel Sidebar Entirely -- [refactor]

The tool panel (`toolpanel.go`, 156 lines) renders a collapsible sidebar showing tool call stats and recent tool activity. The status bar at the bottom already shows tool stats (`read(3) bash(2) | ~4k tokens | 12s`). The tool panel duplicates this information in a wider format that eats 28 columns of viewport width. It has its own rendering logic, its own data structures, and a toggle keybinding (`Ctrl+T`). The information it provides is already visible in the status bar. Delete the panel. The status bar is sufficient. The 28 columns of reclaimed horizontal space are more valuable than the duplicate information.

### 9. Kill `autoSaveMemory` -- [refactor]

Every three agent turns, `model.go` spawns a goroutine that writes a generated summary to `MEMORY.md`. The summary is constructed by iterating all messages, truncating each to 200 characters, and prepending a timestamp. This produces a file that is neither useful as context (it is a dump of truncated user inputs) nor useful as documentation (it has no structure beyond "User asked: ..."). The AGENTS.md explicitly says "Don't create MEMORY.md files." Yet the code creates one anyway, silently, every three turns. Delete `autoSaveMemory`. If the user wants memory persistence, they will ask for it. A tool should not silently write files to the working directory.

### 10. Replace Glamour with Plain Text -- [perf]

The markdown renderer (`render.go`) uses `glamour` with `WithAutoStyle()`, which pulls in `chroma` (syntax highlighting), `goldmark` (markdown parser), `goldmark-emoji`, `bluemonday` (HTML sanitizer), and `douceur` (CSS parser). That is five transitive dependencies for rendering markdown in a terminal that already has ANSI escape code support. The `looksLikeMarkdown` function already exists to short-circuit rendering for non-markdown content. Take the next step: for a coding assistant where the primary output is code blocks and tool results, render code blocks with a simple prefix and leave everything else as plain text. Strip `glamour` and its five transitive dependencies. The binary gets smaller. Startup gets faster. Rendering gets deterministic.

## Set 3 -- Five Ideas That Focus on Raw Data Intent, Ignoring UI Wrappers (p < 0.07, the tippy tip)

### 11. The Conversation Is Just Messages -- [refactor]

At its core, bitchtea shuttles messages between a human and an LLM. The `ChatMessage` type has 8 fields. The `llm.Message` type has 5 fields. The `session.Entry` type has 9 fields. Three message types for the same underlying concept: a role, content, and a timestamp. Unify them into one type. The UI formatting, session serialization, and API transport are all just projections of this single type. Three types means three conversion functions, three sets of field mappings, and three places where bugs can introduce data loss. One type means one source of truth.

### 12. stdin/stdout, Not a TUI -- [feat]

The most minimal interface for an LLM coding tool is: read a prompt from stdin, stream the response to stdout, write tool results inline. No TUI framework. No viewport. No spinner. No splash screen. No alt-screen buffer. The entire charm stack -- bubbletea, bubbles, glamour, lipgloss -- exists to render what is fundamentally a chat log in a terminal. A chat log is `fmt.Fprintf(os.Stderr, "[%s] %s: %s\n", timestamp, role, content)`. If the user wants a TUI, they can pipe it through `less` or `tmux`. The TUI is not the product. The agent loop is the product. The TUI is 60% of the codebase and adds zero agent capability.

### 13. Tool Results Are Just Strings -- [refactor]

The tool system in `tools.go` (312 lines) maintains a registry of four tools (read, write, edit, bash), each with a JSON schema definition, argument parsing, and result formatting. Every tool call goes through JSON marshaling and unmarshaling even though all four tools run locally and return strings. The JSON layer exists because the OpenAI/Anthropic tool calling protocol uses JSON. But the internal execution path does not need JSON. Define tools as `func(ctx context.Context, args string) (string, error)`. Serialize to JSON only at the API boundary. The internal tool execution is a map of four functions. That is 20 lines, not 312.

### 14. Streaming Is Just io.Copy -- [refactor]

The streaming clients in `llm/client.go` and `llm/anthropic.go` (289 + 357 = 646 lines) implement SSE parsing, line buffering, event dispatching, and token extraction for two providers. The core logic is: open an HTTP connection, read lines, parse JSON, extract text, send it down a channel. Both providers converge on the same `Event` type. The 646 lines exist because each provider's SSE format is slightly different. Abstract the difference behind a single interface that takes an HTTP response and returns a channel of strings. The provider-specific JSON parsing is 30 lines each. The rest is boilerplate that should not exist.

### 15. The Config Does Not Need a Struct -- [refactor]

`config.go` defines a `Config` struct with 12 fields, a `DefaultConfig()` function, a `DetectProvider()` function with its own priority logic, a `Profile` struct, and four functions for profile CRUD. The config is read once at startup and mutated by slash commands. This is a map of string keys to string values. Use `os.Getenv` directly at the call sites that need each value. Store overrides in a `map[string]string`. The struct forces every config access to go through a pointer to a shared object, which is why `model.go` passes `m.config` around and why profile loading has to call four separate setter methods on the agent. A map does not need setters. A map does not need `ApplyProfile`.

## Set 4 -- Five Suggestions Requiring Physical Removal (p < 0.07, beyond the tip)

### 16. Remove the `/theme` Command -- [refactor]

There is one theme. The command to switch themes exists, but switching does nothing meaningful because there is nothing to switch to. The `/theme` command is a promise that was never fulfilled. Remove the command. Remove the `themes` map. Remove `SetTheme`, `ListThemes`, `CurrentThemeName`. Keep the style variables as globals initialized once. The user cannot miss what they never had.

### 17. Remove the Notification Sound Toggle -- [refactor]

`/sound` toggles `config.NotificationSound`, which controls whether `\a` is printed when the agent finishes. A terminal bell is the most minimal notification possible. Making it toggleable adds a config field, a slash command handler, a conditional in the event loop, and a help text line. If someone does not want terminal bells, they configure their terminal emulator. The tool should not manage terminal emulator settings. Remove the toggle. Always bell. If the user's terminal is configured to suppress bells, it will suppress bells. That is the terminal's job.

### 18. Remove the `/memory` Command -- [refactor]

`/memory` loads `MEMORY.md` from the working directory and displays it, truncated to 1000 characters. The AGENTS.md says not to use MEMORY.md. The context discovery system already loads AGENTS.md and CLAUDE.md. Memory is a file the tool should not know about. Remove the command. Remove `agent.LoadMemory` and `agent.SaveMemory`. If the user wants to reference a file, they use `@MEMORY.md`. That mechanism already exists. `/memory` is a special case of `@file` reference that adds unnecessary coupling between the tool and a specific filename convention.

### 19. Remove `/undo` -- [refactor]

`/undo` runs `git checkout -- .`, which reverts all unstaged changes. This is `git checkout -- .`. A user of a coding assistant knows how to run `git checkout -- .`. Embedding this as a slash command means the tool has git opinions beyond its scope. Remove it. The tool's git integration should be limited to `/diff`, `/status`, and `/commit` -- commands that provide information, not commands that mutate the working tree on the user's behalf without confirmation.

### 20. Remove `autoNextIdea` -- [refactor]

`autoNextIdea` fires after `autoNextSteps` completes, sending a brainstorming prompt to the agent. This is an agent asking itself for ideas after it finishes working. The ideas go into the conversation history, consume tokens, and are never acted upon because the auto-next loop does not execute tool calls from brainstorming results. It is a token-burning feedback loop that produces decorative output. Remove the flag, the prompt, the toggle command, and the conditional in the done handler. `autoNextSteps` is already borderline; `autoNextIdea` is past the border.
