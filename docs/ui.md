# UI Architecture 🦍

The `bitchtea` user interface is built using the **Bubble Tea** (The Elm Architecture) framework.

## Components

### The Model (`internal/ui/model.go`)
The central state machine. It manages:
- **Viewport**: The scrollable transcript area.
- **Textarea**: The multi-line input bar.
- **Streaming State**: Tracks whether the agent is currently "thinking".
- **Tool Panel**: A side panel for monitoring background activity.
- **MP3 Player**: An optional panel for music playback.

### Viewport Routing
When the agent emits an `Event` (text chunk, tool call, etc.), it is wrapped in an `agentEventMsg` and sent to the UI. The UI's `Update` method processes these:
- **`text`**: Appended to the current streaming message.
- **`thinking`**: Rendered in a distinct "thought" style.
- **`tool_call`**: Triggers the visibility of the tool panel.

### Themes (`internal/ui/themes.go`)
BitchX-inspired themes using `lipgloss`. You can rotate themes with the `/theme` command. Themes define colors for the status bar, system messages, and user/assistant roles.

### Transcripts
The `Transcript` type handles the rendering of chat messages into a string that fits the viewport width. It uses `glamour` for Markdown rendering.

## Commands Dispatch
Input starting with `/` is routed through `internal/ui/commands.go`. The `handleCommand` method in `model.go` acts as the dispatcher, looking up registered handlers.

---
APE STRONK TOGETHER. 🦍💪🤝
